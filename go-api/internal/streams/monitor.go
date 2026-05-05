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

// QueueDepthMonitor periodically samples XLEN on the ingestion stream so that
// the hot path (IngestWebhook) can decide whether to shed load by reading a
// pair of atomics — without issuing a Redis round-trip per request.
//
// Hysteresis prevents flapping: once the depth crosses highWater the monitor
// flips into "shedding" mode and only releases when the depth drops back to
// lowWater (typically 80% of high). Outside those crossings the state is
// preserved between polls.
type QueueDepthMonitor struct {
	rdb       *goredis.Client
	stream    string
	highWater int64
	lowWater  int64

	depth    atomic.Int64
	shedding atomic.Bool
}

// NewQueueDepthMonitor builds a monitor for the given stream. The lowWaterPct
// is clamped to [0, 100]; values <= 0 disable hysteresis (release immediately
// when depth dips below high). highWater <= 0 disables load shedding entirely.
func NewQueueDepthMonitor(rdb *goredis.Client, stream string, highWater int64, lowWaterPct int) *QueueDepthMonitor {
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
		highWater: highWater,
		lowWater:  low,
	}
}

// Depth returns the last observed XLEN value (0 before the first successful
// poll, which means ShouldShed is also false at startup).
func (m *QueueDepthMonitor) Depth() int64 {
	return m.depth.Load()
}

// ShouldShed reports whether new ingestion requests should be rejected with a
// 429. The flag flips on at >= highWater and off at <= lowWater.
func (m *QueueDepthMonitor) ShouldShed() bool {
	if m.highWater <= 0 {
		return false
	}
	return m.shedding.Load()
}

// Update applies a freshly sampled depth and refreshes the shedding state.
// It is exported for tests but the production path always goes through Run.
func (m *QueueDepthMonitor) Update(depth int64) {
	m.depth.Store(depth)
	observability.IngestQueueDepth.WithLabelValues(m.stream).Set(float64(depth))

	if m.highWater <= 0 {
		return
	}

	if !m.shedding.Load() && depth >= m.highWater {
		m.shedding.Store(true)
		observability.IngestLoadSheddingActive.Set(1)
		slog.Warn("Load shedding engaged",
			"stream", m.stream, "depth", depth, "high_water", m.highWater,
		)
		return
	}
	if m.shedding.Load() && depth <= m.lowWater {
		m.shedding.Store(false)
		observability.IngestLoadSheddingActive.Set(0)
		slog.Info("Load shedding released",
			"stream", m.stream, "depth", depth, "low_water", m.lowWater,
		)
	}
}

// Run starts the polling loop and blocks until ctx is cancelled. A time.Ticker
// (not time.Sleep) keeps the cadence stable even when XLEN takes longer than
// expected on a saturated Redis.
func (m *QueueDepthMonitor) Run(ctx context.Context, pollInterval time.Duration) {
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	slog.Info("Queue depth monitor started",
		"stream", m.stream,
		"poll_interval", pollInterval,
		"high_water", m.highWater,
		"low_water", m.lowWater,
	)

	for {
		select {
		case <-ctx.Done():
			slog.Info("Queue depth monitor stopped", "stream", m.stream)
			return
		case <-ticker.C:
			n, err := m.rdb.XLen(ctx, m.stream).Result()
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				slog.Warn("XLEN failed in queue depth monitor", "stream", m.stream, "error", err)
				continue
			}
			m.Update(n)
		}
	}
}
