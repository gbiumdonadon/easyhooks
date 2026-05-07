package streams

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/easyhooks/easyhooks/internal/observability"
)

// QueueDepthMonitor periodically samples the consumer-group backlog of the
// ingestion stream so that the hot path (IngestWebhook) can decide whether to
// shed load by reading a pair of atomics — without issuing a Redis round-trip
// per request.
//
// The signal is the real backlog as seen by the worker, not the physical
// stream length:
//
//	backlog = lag + pending
//
//   - lag     — entries on the stream not yet delivered by XREADGROUP > to
//     this group (Redis 7+ exposes it directly via XINFO GROUPS).
//   - pending — entries already delivered but not yet XACKed (still in PEL).
//
// XLEN is intentionally NOT used: XACK does not decrement XLEN, so once the
// stream has accumulated history above the high-water mark XLEN stays there
// forever and the shedder would lock on permanently — even with the worker
// fully caught up. lag+pending goes to zero whenever the worker is in sync,
// regardless of how many entries ever passed through the stream.
//
// Hysteresis prevents flapping: once the backlog crosses highWater the monitor
// flips into "shedding" mode and only releases when the backlog drops back to
// lowWater (typically 80% of high). Outside those crossings the state is
// preserved between polls.
type QueueDepthMonitor struct {
	rdb       *goredis.Client
	stream    string
	group     string
	highWater int64
	lowWater  int64

	backlog  atomic.Int64
	shedding atomic.Bool
}

// NewQueueDepthMonitor builds a monitor for the given stream/consumer-group.
// The lowWaterPct is clamped to [0, 100]; values <= 0 disable hysteresis
// (release immediately when backlog dips below high). highWater <= 0 disables
// load shedding entirely.
func NewQueueDepthMonitor(rdb *goredis.Client, stream, group string, highWater int64, lowWaterPct int) *QueueDepthMonitor {
	if lowWaterPct < 0 {
		lowWaterPct = 0
	}
	if lowWaterPct > 100 {
		lowWaterPct = 100
	}
	low := highWater * int64(lowWaterPct) / 100
	if low > highWater {
		low = highWater
	}
	return &QueueDepthMonitor{
		rdb:       rdb,
		stream:    stream,
		group:     group,
		highWater: highWater,
		lowWater:  low,
	}
}

// Backlog returns the last observed lag+pending value (0 before the first
// successful poll, which means ShouldShed is also false at startup).
func (m *QueueDepthMonitor) Backlog() int64 {
	return m.backlog.Load()
}

// ShouldShed reports whether new ingestion requests should be rejected with a
// 429. The flag flips on at >= highWater and off at <= lowWater.
func (m *QueueDepthMonitor) ShouldShed() bool {
	if m.highWater <= 0 {
		return false
	}
	return m.shedding.Load()
}

// Update applies a freshly sampled backlog and refreshes the shedding state.
// It is exported for tests but the production path always goes through Run.
func (m *QueueDepthMonitor) Update(backlog int64) {
	m.backlog.Store(backlog)
	observability.IngestQueueDepth.WithLabelValues(m.stream).Set(float64(backlog))

	if m.highWater <= 0 {
		return
	}

	if !m.shedding.Load() && backlog >= m.highWater {
		m.shedding.Store(true)
		observability.IngestLoadSheddingActive.Set(1)
		slog.Warn("Load shedding engaged",
			"stream", m.stream, "group", m.group, "backlog", backlog, "high_water", m.highWater,
		)
		return
	}
	if m.shedding.Load() && backlog <= m.lowWater {
		m.shedding.Store(false)
		observability.IngestLoadSheddingActive.Set(0)
		slog.Info("Load shedding released",
			"stream", m.stream, "group", m.group, "backlog", backlog, "low_water", m.lowWater,
		)
	}
}

// Run starts the polling loop and blocks until ctx is cancelled. A time.Ticker
// (not time.Sleep) keeps the cadence stable even when XINFO takes longer than
// expected on a saturated Redis.
func (m *QueueDepthMonitor) Run(ctx context.Context, pollInterval time.Duration) {
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	slog.Info("Queue depth monitor started",
		"stream", m.stream,
		"group", m.group,
		"poll_interval", pollInterval,
		"high_water", m.highWater,
		"low_water", m.lowWater,
	)

	for {
		select {
		case <-ctx.Done():
			slog.Info("Queue depth monitor stopped", "stream", m.stream, "group", m.group)
			return
		case <-ticker.C:
			m.poll(ctx)
		}
	}
}

// poll runs one sample of XINFO GROUPS and feeds the result to Update. Errors
// (Redis unreachable, group missing, lag unknown) are logged at WARN and skip
// the Update call so the previous shedding state is preserved — flipping the
// shedder on/off based on a transient infrastructure blip would be worse than
// staying with stale data for one tick.
func (m *QueueDepthMonitor) poll(ctx context.Context) {
	groups, err := m.rdb.XInfoGroups(ctx, m.stream).Result()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		slog.Warn("XINFO GROUPS failed in queue depth monitor",
			"stream", m.stream, "group", m.group, "error", err,
		)
		return
	}

	for _, g := range groups {
		if g.Name != m.group {
			continue
		}
		// Redis returns Lag = -1 when it cannot be computed (e.g. group
		// recreated with an explicit start id rather than $). EnsureGroup
		// always uses $, so this should not happen in practice — but if it
		// does, refuse to update so we don't accidentally engage the shedder
		// on a bogus zero or release it on a bogus huge value.
		if g.Lag < 0 {
			slog.Warn("Consumer group lag is undefined; skipping update",
				"stream", m.stream, "group", m.group, "pending", g.Pending,
			)
			return
		}
		m.Update(g.Lag + g.Pending)
		return
	}

	slog.Warn("Consumer group not found in XINFO GROUPS; skipping update",
		"stream", m.stream, "group", m.group,
	)
}
