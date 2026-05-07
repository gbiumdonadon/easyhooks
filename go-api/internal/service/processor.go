package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/observability"
	"github.com/easyhooks/easyhooks/internal/streams"
)

// WebhookEnvelope holds the parsed content of a Redis Stream entry that
// represents a single inbound webhook event.
type WebhookEnvelope struct {
	EventID  string
	TenantID string
	Payload  []byte
}

// BusinessHandler is a function that processes a webhook envelope.
type BusinessHandler func(ctx context.Context, envelope WebhookEnvelope) error

// ExtractEnvelope parses tenant_id, event_id and payload from a Redis Stream
// XREADGROUP entry. The payload field may come back as either string or []byte
// depending on the redis client's serialization choice.
func ExtractEnvelope(msg goredis.XMessage) (WebhookEnvelope, error) {
	tenantID, err := stringField(msg.Values, streams.FieldTenantID)
	if err != nil {
		return WebhookEnvelope{}, err
	}
	eventID, err := stringField(msg.Values, streams.FieldEventID)
	if err != nil {
		return WebhookEnvelope{}, err
	}
	payload, err := payloadBytes(msg.Values, streams.FieldPayload)
	if err != nil {
		return WebhookEnvelope{}, err
	}
	return WebhookEnvelope{
		EventID:  eventID,
		TenantID: tenantID,
		Payload:  payload,
	}, nil
}

func stringField(values map[string]interface{}, key string) (string, error) {
	v, ok := values[key]
	if !ok {
		return "", fmt.Errorf("missing %s field", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s field has unexpected type %T", key, v)
	}
	return s, nil
}

func payloadBytes(values map[string]interface{}, key string) ([]byte, error) {
	v, ok := values[key]
	if !ok {
		return nil, fmt.Errorf("missing %s field", key)
	}
	switch t := v.(type) {
	case string:
		return []byte(t), nil
	case []byte:
		return t, nil
	default:
		return nil, fmt.Errorf("%s field has unexpected type %T", key, v)
	}
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

// ProcessMessage applies idempotency check, business handler with retry/backoff,
// and DLQ routing to a single Redis Stream entry. The caller is responsible
// for XACK after this function returns.
func ProcessMessage(ctx context.Context, msg goredis.XMessage, rdb *goredis.Client, cfg *config.Config, handler BusinessHandler) error {
	tracer := observability.Tracer("easyhooks.worker")

	envelope, err := ExtractEnvelope(msg)
	if err != nil {
		return fmt.Errorf("extract envelope: %w", err)
	}

	ctx, span := tracer.Start(ctx, "webhook.process")
	span.SetAttributes(
		attribute.String("tenant_id", envelope.TenantID),
		attribute.String("event_id", envelope.EventID),
		attribute.String("stream_id", msg.ID),
	)
	defer span.End()

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

	return dispatchToDLQ(ctx, rdb, cfg, envelope, lastErr)
}

// dispatchToDLQ appends the failed event to the DLQ stream with the original
// error message attached as a diagnostic field.
func dispatchToDLQ(ctx context.Context, rdb *goredis.Client, cfg *config.Config, envelope WebhookEnvelope, origErr error) error {
	tracer := observability.Tracer("easyhooks.worker")
	_, span := tracer.Start(ctx, "webhook.dispatch_to_dlq")
	defer span.End()

	if _, err := streams.PublishDLQ(ctx, rdb, cfg.DLQStreamKey, envelope.TenantID, envelope.EventID, envelope.Payload, origErr, int64(cfg.DLQStreamMaxLen)); err != nil {
		return fmt.Errorf("publish to DLQ stream: %w", err)
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

// MakeRedisStreamsHandler returns a BusinessHandler that fans the event out to
// the per-tenant Redis Stream consumed by the WebSocket layer.
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
