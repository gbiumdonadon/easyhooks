package streams

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testStream = "events:in"
	testGroup  = "webhook-workers"
)

func newRedisForTest(t *testing.T) *goredis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
}

func TestQueueDepthMonitor_Hysteresis(t *testing.T) {
	rdb := newRedisForTest(t)
	defer rdb.Close()

	// highWater=1000, lowWaterPct=80 → lowWater=800
	mon := NewQueueDepthMonitor(rdb, testStream, testGroup, 1000, 80)

	// Below high → not shedding
	mon.Update(500)
	assert.False(t, mon.ShouldShed(), "below high should not shed")
	assert.Equal(t, int64(500), mon.Backlog())

	// Cross high → shed
	mon.Update(1000)
	assert.True(t, mon.ShouldShed(), "at high should shed")

	// Between low and high → still shed (hysteresis)
	mon.Update(900)
	assert.True(t, mon.ShouldShed(), "between low and high after engaging should still shed")

	// At low → release
	mon.Update(800)
	assert.False(t, mon.ShouldShed(), "at low should release")

	// Below low → still released
	mon.Update(100)
	assert.False(t, mon.ShouldShed(), "below low should remain released")
}

func TestQueueDepthMonitor_NoFlappingBetweenWatermarks(t *testing.T) {
	rdb := newRedisForTest(t)
	defer rdb.Close()
	mon := NewQueueDepthMonitor(rdb, testStream, testGroup, 1000, 80)

	// Oscillating between 850 and 950 (both inside the band) without ever
	// hitting high or low — state must not change.
	mon.Update(950) // not shedding initially, 950 < 1000 → still not shedding
	assert.False(t, mon.ShouldShed())
	mon.Update(850)
	assert.False(t, mon.ShouldShed())

	// Now engage by crossing high...
	mon.Update(1100)
	assert.True(t, mon.ShouldShed())

	// ...and oscillate inside the band again — state must stay engaged.
	mon.Update(950)
	assert.True(t, mon.ShouldShed())
	mon.Update(850)
	assert.True(t, mon.ShouldShed())
	mon.Update(950)
	assert.True(t, mon.ShouldShed())
}

func TestQueueDepthMonitor_DisabledWhenHighWaterZero(t *testing.T) {
	rdb := newRedisForTest(t)
	defer rdb.Close()

	mon := NewQueueDepthMonitor(rdb, testStream, testGroup, 0, 80)
	mon.Update(1_000_000)
	assert.False(t, mon.ShouldShed(), "high_water<=0 should disable shedding")
}

func TestQueueDepthMonitor_RunPollsBacklog(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	ctx := context.Background()

	// Seed the consumer group on an empty stream so XINFO GROUPS has
	// something to report. MKSTREAM creates events:in lazily.
	require.NoError(t, rdb.XGroupCreateMkStream(ctx, testStream, testGroup, "$").Err())

	// Publish entries that the group has not consumed yet — these should
	// surface as `lag` in XINFO GROUPS.
	//
	// NOTE: miniredis 2.37.0's XINFO GROUPS implementation reports
	// `lag = len(stream.entries)` and `pending = 0` regardless of XACK state
	// (see cmd_stream.go:571–576). That is enough to exercise the polling
	// pipeline end-to-end here; the precise lag/pending split against XACK is
	// only fully accurate against real Redis 7+, which is verified
	// out-of-band via the smoke load test.
	const n = 7
	for i := 0; i < n; i++ {
		_, err := rdb.XAdd(ctx, &goredis.XAddArgs{
			Stream: testStream,
			Values: map[string]interface{}{"k": "v"},
		}).Result()
		require.NoError(t, err)
	}

	mon := NewQueueDepthMonitor(rdb, testStream, testGroup, 100, 80)

	runCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	go mon.Run(runCtx, 20*time.Millisecond)

	// Wait until at least one poll completes.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if mon.Backlog() == n {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Equal(t, int64(n), mon.Backlog(),
		"monitor should reflect lag+pending of the consumer group")
}

func TestQueueDepthMonitor_RunIgnoresMissingGroup(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	ctx := context.Background()

	// Stream exists but the group does NOT — simulates a misconfigured
	// deployment or a poll that races EnsureGroup. The monitor should keep
	// the previous backlog (zero) and never panic.
	_, err := rdb.XAdd(ctx, &goredis.XAddArgs{
		Stream: testStream,
		Values: map[string]interface{}{"k": "v"},
	}).Result()
	require.NoError(t, err)

	mon := NewQueueDepthMonitor(rdb, testStream, "nonexistent-group", 100, 80)

	runCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	go mon.Run(runCtx, 10*time.Millisecond)

	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, int64(0), mon.Backlog(),
		"missing group should leave backlog at the initial zero")
	assert.False(t, mon.ShouldShed(),
		"missing group should not engage the shedder")
}

func TestQueueDepthMonitor_ClampsLowWaterPct(t *testing.T) {
	rdb := newRedisForTest(t)
	defer rdb.Close()

	// lowWaterPct above 100 → clamped to 100 → low == high.
	mon := NewQueueDepthMonitor(rdb, testStream, testGroup, 1000, 200)
	assert.Equal(t, int64(1000), mon.lowWater)

	// lowWaterPct < 0 → clamped to 0 → low = 0 (release as soon as backlog
	// dips to 0, effectively disabling hysteresis on the release side).
	mon = NewQueueDepthMonitor(rdb, testStream, testGroup, 1000, -10)
	assert.Equal(t, int64(0), mon.lowWater)
}
