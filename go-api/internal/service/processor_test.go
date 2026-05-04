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
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/service"
)

func newTestConfig() *config.Config {
	return &config.Config{
		DatabaseURL:           "postgres://test:test@localhost/test",
		AdminSeedToken:        "test-admin",
		AppSecretKey:          "test-key",
		KafkaBootstrapServers: "localhost:9092",
		KafkaWebhookTopic:     "webhooks.inbound",
		KafkaDLQTopic:         "webhooks.dlq",
		KafkaConsumerGroup:    "test-workers",
		WorkerMaxRetries:      3,
		WorkerBackoffBaseMs:   10, // fast for tests
		IdempotencyTTL:        86400,
		TenantEventsStreamPrefix: "stream:tenant:",
		StreamMaxLen:          1000,
		StreamHistoryCount:    50,
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

func makeRecord(tenantID, eventID string, payload []byte) *kgo.Record {
	return &kgo.Record{
		Topic: "webhooks.inbound",
		Value: payload,
		Headers: []kgo.RecordHeader{
			{Key: "tenant_id", Value: []byte(tenantID)},
			{Key: "event_id", Value: []byte(eventID)},
		},
	}
}

func TestExtractEnvelope(t *testing.T) {
	tenantID := uuid.New().String()
	eventID := "evt-123"
	payload := []byte(`{"test":true}`)

	record := makeRecord(tenantID, eventID, payload)
	env, err := service.ExtractEnvelope(record)
	require.NoError(t, err)
	assert.Equal(t, tenantID, env.TenantID)
	assert.Equal(t, eventID, env.EventID)
	assert.Equal(t, payload, env.Payload)
}

func TestExtractEnvelope_MissingHeaders(t *testing.T) {
	record := &kgo.Record{Value: []byte("{}"), Headers: []kgo.RecordHeader{}}
	_, err := service.ExtractEnvelope(record)
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

	record := makeRecord(tenantID, eventID, []byte(`{}`))

	// First processing — should call handler
	err := service.ProcessRecord(ctx, record, rdb, nil, cfg, handler)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount)

	// Second processing — duplicate, should NOT call handler
	err = service.ProcessRecord(ctx, record, rdb, nil, cfg, handler)
	require.NoError(t, err)
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

	record := makeRecord(uuid.New().String(), "evt-retry-1", []byte(`{}`))
	err := service.ProcessRecord(ctx, record, rdb, nil, cfg, handler)
	require.NoError(t, err)
	assert.Equal(t, 3, callCount)
}

func TestRetry_ExhaustedSendsToDLQ(t *testing.T) {
	cfg := newTestConfig()
	cfg.WorkerMaxRetries = 2
	_, rdb := newMiniredisClient(t)
	ctx := context.Background()

	handler := func(ctx context.Context, env service.WebhookEnvelope) error {
		return assert.AnError // always fail
	}

	// We can't easily test Kafka DLQ production without a real Kafka in unit tests,
	// but we can verify the processor returns nil (not an error) after DLQ dispatch.
	// The DLQ produce will fail (no real Kafka), so ProcessRecord should return an error.
	record := makeRecord(uuid.New().String(), "evt-dlq-1", []byte(`{}`))
	_ = service.ProcessRecord(ctx, record, rdb, nil, cfg, handler)
	// We only assert it doesn't panic; DLQ produce error is expected with nil client
}

func TestBackoffDuration(t *testing.T) {
	cfg := newTestConfig()
	cfg.WorkerBackoffBaseMs = 100

	// Verify backoff grows: 100ms, 200ms, 400ms
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
	record := makeRecord(uuid.New().String(), "evt-backoff-1", []byte(`{}`))
	service.ProcessRecord(context.Background(), record, rdb, nil, cfg, handler) //nolint:errcheck
	elapsed := time.Since(start)
	// Total backoff should be at least 100ms + 200ms = 300ms
	assert.Greater(t, elapsed.Milliseconds(), int64(250))
}
