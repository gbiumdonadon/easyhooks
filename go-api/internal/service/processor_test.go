package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/service"
	"github.com/easyhooks/easyhooks/internal/streams"
)

func newTestConfig() *config.Config {
	return &config.Config{
		AdminSeedToken:           "test-admin",
		AppSecretKey:             "test-key",
		EventStreamKey:           "events:in",
		DLQStreamKey:             "events:failed",
		ConsumerGroup:            "test-workers",
		StreamBlockMs:            100,
		StreamCount:              10,
		WorkerMaxRetries:         3,
		WorkerBackoffBaseMs:      10, // fast for tests
		IdempotencyTTL:           86400,
		TenantEventsStreamPrefix: "stream:tenant:",
		StreamMaxLen:             1000,
		StreamHistoryCount:       50,
	}
}

func newMiniredisClient(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return mr, client
}

func makeMessage(tenantID, eventID string, payload []byte) redis.XMessage {
	return redis.XMessage{
		ID: "0-0",
		Values: map[string]interface{}{
			streams.FieldTenantID: tenantID,
			streams.FieldEventID:  eventID,
			streams.FieldPayload:  string(payload),
		},
	}
}

func TestExtractEnvelope(t *testing.T) {
	tenantID := uuid.New().String()
	eventID := "evt-123"
	payload := []byte(`{"test":true}`)

	env, err := service.ExtractEnvelope(makeMessage(tenantID, eventID, payload))
	require.NoError(t, err)
	assert.Equal(t, tenantID, env.TenantID)
	assert.Equal(t, eventID, env.EventID)
	assert.Equal(t, payload, env.Payload)
}

func TestExtractEnvelope_MissingFields(t *testing.T) {
	_, err := service.ExtractEnvelope(redis.XMessage{Values: map[string]interface{}{}})
	assert.Error(t, err)
}

func TestIdempotency_DuplicateDropped(t *testing.T) {
	cfg := newTestConfig()
	_, rdb := newMiniredisClient(t)
	ctx := context.Background()

	tenantID := uuid.New().String()
	eventID := "evt-unique-1"
	callCount := 0

	handler := func(ctx context.Context, env service.WebhookEnvelope) error {
		callCount++
		return nil
	}

	msg := makeMessage(tenantID, eventID, []byte(`{}`))

	require.NoError(t, service.ProcessMessage(ctx, msg, rdb, cfg, handler))
	assert.Equal(t, 1, callCount)

	require.NoError(t, service.ProcessMessage(ctx, msg, rdb, cfg, handler))
	assert.Equal(t, 1, callCount, "handler should not be called again for duplicate")
}

func TestRetry_SucceedsOnThirdAttempt(t *testing.T) {
	cfg := newTestConfig()
	_, rdb := newMiniredisClient(t)
	ctx := context.Background()

	callCount := 0
	handler := func(ctx context.Context, env service.WebhookEnvelope) error {
		callCount++
		if callCount < 3 {
			return assert.AnError
		}
		return nil
	}

	msg := makeMessage(uuid.New().String(), "evt-retry-1", []byte(`{}`))
	require.NoError(t, service.ProcessMessage(ctx, msg, rdb, cfg, handler))
	assert.Equal(t, 3, callCount)
}

func TestRetry_ExhaustedSendsToDLQStream(t *testing.T) {
	cfg := newTestConfig()
	cfg.WorkerMaxRetries = 2
	_, rdb := newMiniredisClient(t)
	ctx := context.Background()

	tenantID := uuid.New().String()
	handler := func(ctx context.Context, env service.WebhookEnvelope) error {
		return assert.AnError // always fail
	}

	msg := makeMessage(tenantID, "evt-dlq-1", []byte(`{"failed":true}`))
	require.NoError(t, service.ProcessMessage(ctx, msg, rdb, cfg, handler))

	entries, err := rdb.XRange(ctx, cfg.DLQStreamKey, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, entries, 1, "DLQ stream must contain the failed event")

	v := entries[0].Values
	assert.Equal(t, tenantID, v[streams.FieldTenantID])
	assert.Equal(t, "evt-dlq-1", v[streams.FieldEventID])
	assert.Equal(t, `{"failed":true}`, v[streams.FieldPayload])
	assert.NotEmpty(t, v[streams.FieldOriginalError])
}

func TestBackoffDuration(t *testing.T) {
	cfg := newTestConfig()
	cfg.WorkerBackoffBaseMs = 100

	start := time.Now()
	callCount := 0
	handler := func(ctx context.Context, env service.WebhookEnvelope) error {
		callCount++
		if callCount < 3 {
			return assert.AnError
		}
		return nil
	}
	_, rdb := newMiniredisClient(t)
	msg := makeMessage(uuid.New().String(), "evt-backoff-1", []byte(`{}`))
	_ = service.ProcessMessage(context.Background(), msg, rdb, cfg, handler)
	elapsed := time.Since(start)
	// Total backoff should be at least 100ms + 200ms = 300ms.
	assert.Greater(t, elapsed.Milliseconds(), int64(250))
}
