package streams_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/easyhooks/easyhooks/internal/streams"
)

const (
	testStream = "events:in"
	testDLQ    = "events:failed"
	testGroup  = "webhook-workers"
)

func newRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestEnsureGroup_CreatesAndIsIdempotent(t *testing.T) {
	rdb := newRedis(t)
	ctx := context.Background()

	require.NoError(t, streams.EnsureGroup(ctx, rdb, testStream, testGroup))
	// Second call should not error (BUSYGROUP swallowed).
	require.NoError(t, streams.EnsureGroup(ctx, rdb, testStream, testGroup))
}

func TestPublishReadAck_RoundTrip(t *testing.T) {
	rdb := newRedis(t)
	ctx := context.Background()

	require.NoError(t, streams.EnsureGroup(ctx, rdb, testStream, testGroup))

	id, err := streams.Publish(ctx, rdb, testStream, "tenant-1", "evt-1", []byte(`{"x":1}`), 0)
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	r := streams.NewReader(rdb, testStream, testGroup, "consumer-A", 100, 10)
	msgs, err := r.Read(ctx)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	got := msgs[0]
	assert.Equal(t, id, got.ID)
	assert.Equal(t, "tenant-1", got.Values[streams.FieldTenantID])
	assert.Equal(t, "evt-1", got.Values[streams.FieldEventID])
	assert.Equal(t, `{"x":1}`, got.Values[streams.FieldPayload])

	require.NoError(t, r.Ack(ctx, got.ID))

	// After ack, the second Read should observe nothing new.
	msgs, err = r.Read(ctx)
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestPublish_CapsStreamWithMaxLenApprox(t *testing.T) {
	rdb := newRedis(t)
	ctx := context.Background()

	const cap int64 = 20
	const total = 100

	for i := 0; i < total; i++ {
		_, err := streams.Publish(ctx, rdb, testStream, "tenant-cap",
			fmt.Sprintf("evt-%d", i), []byte(`{}`), cap)
		require.NoError(t, err)
	}

	// Approx (~) lets Redis trim whole macro-nodes at once, so XLEN may sit a
	// bit above the requested cap. Use a generous slack to keep the assertion
	// stable across miniredis versions while still proving the trim happened.
	xlen, err := rdb.XLen(ctx, testStream).Result()
	require.NoError(t, err)
	assert.LessOrEqual(t, xlen, cap+50, "stream length should be capped near MAXLEN ~ %d", cap)
	assert.Less(t, xlen, int64(total), "without trim XLEN would equal total publishes (%d)", total)

	// The most recent entry must still be readable — trim only removes the
	// oldest entries, never the just-appended one.
	entries, err := rdb.XRevRange(ctx, testStream, "+", "-").Result()
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	assert.Equal(t, fmt.Sprintf("evt-%d", total-1), entries[0].Values[streams.FieldEventID])
}

func TestPublish_NoCapWhenMaxLenApproxIsZero(t *testing.T) {
	rdb := newRedis(t)
	ctx := context.Background()

	const total = 50
	for i := 0; i < total; i++ {
		_, err := streams.Publish(ctx, rdb, testStream, "tenant-uncap",
			fmt.Sprintf("evt-%d", i), []byte(`{}`), 0)
		require.NoError(t, err)
	}
	xlen, err := rdb.XLen(ctx, testStream).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(total), xlen, "passing 0 must keep legacy untrimmed behavior")
}

func TestPublishDLQ_StoresOriginalError(t *testing.T) {
	rdb := newRedis(t)
	ctx := context.Background()

	id, err := streams.PublishDLQ(ctx, rdb, testDLQ, "tenant-2", "evt-2", []byte("payload"), errors.New("boom"), 0)
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	entries, err := rdb.XRange(ctx, testDLQ, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, entries, 1)

	v := entries[0].Values
	assert.Equal(t, "tenant-2", v[streams.FieldTenantID])
	assert.Equal(t, "evt-2", v[streams.FieldEventID])
	assert.Equal(t, "payload", v[streams.FieldPayload])
	assert.Equal(t, "boom", v[streams.FieldOriginalError])
}

func TestPublishDLQ_CapsStreamWithMaxLenApprox(t *testing.T) {
	rdb := newRedis(t)
	ctx := context.Background()

	const cap int64 = 15
	const total = 80
	for i := 0; i < total; i++ {
		_, err := streams.PublishDLQ(ctx, rdb, testDLQ, "tenant-cap",
			fmt.Sprintf("evt-%d", i), []byte("payload"), errors.New("boom"), cap)
		require.NoError(t, err)
	}
	xlen, err := rdb.XLen(ctx, testDLQ).Result()
	require.NoError(t, err)
	assert.LessOrEqual(t, xlen, cap+50, "DLQ length should be capped near MAXLEN ~ %d", cap)
	assert.Less(t, xlen, int64(total), "without trim XLEN would equal total publishes (%d)", total)
}

func TestTrimMaxLenApprox_RemovesExcessEntries(t *testing.T) {
	rdb := newRedis(t)
	ctx := context.Background()

	// Seed without trimming so we have known excess to trim afterwards.
	const seeded = 50
	for i := 0; i < seeded; i++ {
		_, err := streams.Publish(ctx, rdb, testStream, "tenant-trim",
			fmt.Sprintf("evt-%d", i), []byte(`{}`), 0)
		require.NoError(t, err)
	}

	const cap int64 = 10
	trimmed, err := streams.TrimMaxLenApprox(ctx, rdb, testStream, cap)
	require.NoError(t, err)
	assert.Greater(t, trimmed, int64(0), "should report at least one trimmed entry")

	xlen, err := rdb.XLen(ctx, testStream).Result()
	require.NoError(t, err)
	assert.LessOrEqual(t, xlen, cap+50, "post-trim XLEN must be near the cap")
	assert.Less(t, xlen, int64(seeded), "post-trim XLEN must be lower than the seeded count")
}

func TestTrimMaxLenApprox_NoOpWhenMaxLenZero(t *testing.T) {
	rdb := newRedis(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := streams.Publish(ctx, rdb, testStream, "tenant-noop",
			fmt.Sprintf("evt-%d", i), []byte(`{}`), 0)
		require.NoError(t, err)
	}
	trimmed, err := streams.TrimMaxLenApprox(ctx, rdb, testStream, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), trimmed)

	xlen, err := rdb.XLen(ctx, testStream).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(5), xlen, "no-op trim must leave the stream untouched")
}

func TestReader_BlockTimeoutReturnsEmpty(t *testing.T) {
	rdb := newRedis(t)
	ctx := context.Background()
	require.NoError(t, streams.EnsureGroup(ctx, rdb, testStream, testGroup))

	r := streams.NewReader(rdb, testStream, testGroup, "consumer-B", 50, 10)
	msgs, err := r.Read(ctx)
	require.NoError(t, err)
	assert.Empty(t, msgs)
}
