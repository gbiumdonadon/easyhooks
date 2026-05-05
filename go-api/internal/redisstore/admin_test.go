package redisstore_test

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/easyhooks/easyhooks/internal/redisstore"
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

func TestSeedSuperAdmin_FirstCallStoresHash(t *testing.T) {
	rdb := newRedis(t)
	ctx := context.Background()

	require.NoError(t, redisstore.SeedSuperAdmin(ctx, rdb, "bootstrap-token"))

	hash, err := rdb.Get(ctx, redisstore.AdminTokenHashKey).Result()
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.NotEqual(t, "bootstrap-token", hash, "stored value must be the bcrypt hash, not the raw token")
}

func TestSeedSuperAdmin_SecondCallIsNoOp(t *testing.T) {
	rdb := newRedis(t)
	ctx := context.Background()

	require.NoError(t, redisstore.SeedSuperAdmin(ctx, rdb, "first-token"))
	first, err := rdb.Get(ctx, redisstore.AdminTokenHashKey).Result()
	require.NoError(t, err)

	require.NoError(t, redisstore.SeedSuperAdmin(ctx, rdb, "second-token"))
	second, err := rdb.Get(ctx, redisstore.AdminTokenHashKey).Result()
	require.NoError(t, err)

	assert.Equal(t, first, second, "seed must not overwrite an existing hash")
}

func TestVerifyAdmin(t *testing.T) {
	rdb := newRedis(t)
	ctx := context.Background()

	ok, err := redisstore.VerifyAdmin(ctx, rdb, "anything")
	assert.ErrorIs(t, err, redisstore.ErrAdminNotSeeded)
	assert.False(t, ok)

	require.NoError(t, redisstore.SeedSuperAdmin(ctx, rdb, "real-token"))

	ok, err = redisstore.VerifyAdmin(ctx, rdb, "real-token")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = redisstore.VerifyAdmin(ctx, rdb, "wrong-token")
	require.NoError(t, err)
	assert.False(t, ok)
}
