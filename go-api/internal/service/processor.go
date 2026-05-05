package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/attribute"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/observability"
)

// WebhookEnvelope holds the parsed content of a Kafka webhook record.
type WebhookEnvelope struct {
	EventID  string
	TenantID string
	Payload  []byte
}

// BusinessHandler is a function that processes a webhook envelope.
type BusinessHandler func(ctx context.Context, envelope WebhookEnvelope) error

// ExtractEnvelope parses tenant_id and event_id from Kafka record headers.
func ExtractEnvelope(record *kgo.Record) (WebhookEnvelope, error) {
	headers := make(map[string][]byte, len(record.Headers))
	for _, h := range record.Headers {
		headers[h.Key] = h.Value
	}
	eventID, ok := headers["event_id"]
	if !ok {
		return WebhookEnvelope{}, fmt.Errorf("missing event_id header")
	}
	tenantID, ok := headers["tenant_id"]
	if !ok {
		return WebhookEnvelope{}, fmt.Errorf("missing tenant_id header")
	}
	return WebhookEnvelope{
		EventID:  string(eventID),
		TenantID: string(tenantID),
		Payload:  record.Value,
	}, nil
}

// acquireIdempotencyLock attempts to set the idempotency key (SET NX EX).
// Returns true if the lock was acquired (first time), false if duplicate.
func acquireIdempotencyLock(ctx context.Context, rdb *goredis.Client, cfg *config.Config, eventID string) (bool, error) {
	key := fmt.Sprintf("event_lock:%s", eventID)
	ok, err := rdb.SetNX(ctx, key, "1", time.Duration(cfg.IdempotencyTTL)*time.Second).Result()
	if err != nil {
		return false, fmt.Errorf("SET NX idempotency key: %w", err)
	}
	observability.RedisOperationsTotal.WithLabelValues("idempotency_check", "success").Inc()
	return ok, nil
}

// backoffDuration returns exponential backoff for a given attempt (1-based).
// Exponential backoff: base_ms * 2^(attempt-1)
func backoffDuration(cfg *config.Config, attempt int) time.Duration {
	ms := float64(cfg.WorkerBackoffBaseMs) * math.Pow(2, float64(attempt-1))
	return time.Duration(ms) * time.Millisecond
}

// ProcessRecord applies idempotency check, business handler with retry/backoff, and DLQ routing.
// Kafka consumer record processing pipeline.
func ProcessRecord(ctx context.Context, record *kgo.Record, rdb *goredis.Client, dlqClient *kgo.Client, cfg *config.Config, handler BusinessHandler) error {
	tracer := observability.Tracer("easyhooks.worker")

	envelope, err := ExtractEnvelope(record)
	if err != nil {
		return fmt.Errorf("extract envelope: %w", err)
	}

	ctx, span := tracer.Start(ctx, "webhook.process")
	span.SetAttributes(
		attribute.String("tenant_id", envelope.TenantID),
		attribute.String("event_id", envelope.EventID),
	)
	defer span.End()

	// Idempotency check
	acquired, err := acquireIdempotencyLock(ctx, rdb, cfg, envelope.EventID)
	if err != nil {
		return err
	}
	if !acquired {
		observability.IdempotencyDuplicatesTotal.WithLabelValues(envelope.TenantID).Inc()
		slog.Info("Ignored duplicated event", "event_id", envelope.EventID, "tenant_id", envelope.TenantID)
		return nil
	}

	start := time.Now()
	var lastErr error
	for attempt := 1; attempt <= cfg.WorkerMaxRetries; attempt++ {
		_, handlerSpan := tracer.Start(ctx, "webhook.business_handler")
		handlerSpan.SetAttributes(
			attribute.String("tenant_id", envelope.TenantID),
			attribute.String("event_id", envelope.EventID),
			attribute.Int("attempt", attempt),
		)

		err := handler(ctx, envelope)
		handlerSpan.End()

		if err == nil {
			duration := time.Since(start).Seconds()
			observability.WebhookProcessingDuration.WithLabelValues(envelope.TenantID).Observe(duration)
			return nil
		}

		lastErr = err
		observability.WebhookRetriesTotal.WithLabelValues(envelope.TenantID, fmt.Sprintf("%d", attempt)).Inc()
		slog.Warn("Business handler failed; will retry",
			"event_id", envelope.EventID,
			"tenant_id", envelope.TenantID,
			"attempt", attempt,
			"max_retries", cfg.WorkerMaxRetries,
			"error", err,
		)
		if attempt < cfg.WorkerMaxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoffDuration(cfg, attempt)):
			}
		}
	}

	// All retries exhausted — send to DLQ
	return dispatchToDLQ(ctx, dlqClient, cfg, envelope, lastErr)
}

// dispatchToDLQ sends the event to the DLQ topic with diagnostic headers.
func dispatchToDLQ(ctx context.Context, dlqClient *kgo.Client, cfg *config.Config, envelope WebhookEnvelope, origErr error) error {
	if dlqClient == nil {
		return fmt.Errorf("DLQ client is nil, cannot dispatch event %s", envelope.EventID)
	}

	tracer := observability.Tracer("easyhooks.worker")
	_, span := tracer.Start(ctx, "webhook.dispatch_to_dlq")
	defer span.End()

	record := &kgo.Record{
		Topic: cfg.KafkaDLQTopic,
		Value: envelope.Payload,
		Headers: []kgo.RecordHeader{
			{Key: "tenant_id", Value: []byte(envelope.TenantID)},
			{Key: "event_id", Value: []byte(envelope.EventID)},
			{Key: "x-original-error", Value: []byte(origErr.Error())},
		},
	}
	if err := dlqClient.ProduceSync(ctx, record).FirstErr(); err != nil {
		return fmt.Errorf("produce to DLQ: %w", err)
	}

	observability.WebhookDLQTotal.WithLabelValues(envelope.TenantID, fmt.Sprintf("%T", origErr)).Inc()
	slog.Error("Routed event to DLQ after exhausting retries",
		"event_id", envelope.EventID,
		"tenant_id", envelope.TenantID,
		"error", origErr,
		"max_retries", cfg.WorkerMaxRetries,
	)
	return nil
}

// MakeRedisStreamsHandler returns a BusinessHandler that publishes events to Redis Streams.
func MakeRedisStreamsHandler(rdb *goredis.Client, cfg *config.Config) BusinessHandler {
	return func(ctx context.Context, envelope WebhookEnvelope) error {
		tenantID, err := uuid.Parse(envelope.TenantID)
		if err != nil {
			return fmt.Errorf("parse tenant_id: %w", err)
		}
		msgID, err := PublishTenantEvent(ctx, rdb, cfg, tenantID, envelope.Payload)
		if err != nil {
			return err
		}
		slog.Info("Published event to stream", "tenant_id", envelope.TenantID, "stream_id", msgID)
		return nil
	}
}
