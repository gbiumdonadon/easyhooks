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

func newRedisForTest(t *testing.T) *goredis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
}

func TestQueueDepthMonitor_Hysteresis(t *testing.T) {
	rdb := newRedisForTest(t)
	defer rdb.Close()

	// highWater=1000, lowWaterPct=80 → lowWater=800
	mon := NewQueueDepthMonitor(rdb, "events:in", 1000, 80)

	// Below high → not shedding
	mon.Update(500)
	assert.False(t, mon.ShouldShed(), "below high should not shed")
	assert.Equal(t, int64(500), mon.Depth())

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
	mon := NewQueueDepthMonitor(rdb, "events:in", 1000, 80)

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

	mon := NewQueueDepthMonitor(rdb, "events:in", 0, 80)
	mon.Update(1_000_000)
	assert.False(t, mon.ShouldShed(), "high_water<=0 should disable shedding")
}

func TestQueueDepthMonitor_RunPollsXLen(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	for i := 0; i < 7; i++ {
		_, err := rdb.XAdd(ctx, &goredis.XAddArgs{
			Stream: "events:in",
			Values: map[string]interface{}{"k": "v"},
		}).Result()
		require.NoError(t, err)
	}

	mon := NewQueueDepthMonitor(rdb, "events:in", 100, 80)

	runCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	go mon.Run(runCtx, 20*time.Millisecond)

	// Wait until at least one poll completes.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if mon.Depth() == 7 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Equal(t, int64(7), mon.Depth(), "monitor should reflect XLEN of the stream")
}

func TestQueueDepthMonitor_ClampsLowWaterPct(t *testing.T) {
	rdb := newRedisForTest(t)
	defer rdb.Close()

	// lowWaterPct above 100 → clamped to 100 → low == high.
	mon := NewQueueDepthMonitor(rdb, "events:in", 1000, 200)
	assert.Equal(t, int64(1000), mon.lowWater)

	// lowWaterPct < 0 → clamped to 0 → low = 0 (release as soon as depth dips
	// to 0, effectively disabling hysteresis on the release side).
	mon = NewQueueDepthMonitor(rdb, "events:in", 1000, -10)
	assert.Equal(t, int64(0), mon.lowWater)
}
