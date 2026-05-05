package streams_test

import (
	"context"
	"errors"
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

	id, err := streams.Publish(ctx, rdb, testStream, "tenant-1", "evt-1", []byte(`{"x":1}`))
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

func TestPublishDLQ_StoresOriginalError(t *testing.T) {
	rdb := newRedis(t)
	ctx := context.Background()

	id, err := streams.PublishDLQ(ctx, rdb, testDLQ, "tenant-2", "evt-2", []byte("payload"), errors.New("boom"))
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

func TestReader_BlockTimeoutReturnsEmpty(t *testing.T) {
	rdb := newRedis(t)
	ctx := context.Background()
	require.NoError(t, streams.EnsureGroup(ctx, rdb, testStream, testGroup))

	r := streams.NewReader(rdb, testStream, testGroup, "consumer-B", 50, 10)
	msgs, err := r.Read(ctx)
	require.NoError(t, err)
	assert.Empty(t, msgs)
}
