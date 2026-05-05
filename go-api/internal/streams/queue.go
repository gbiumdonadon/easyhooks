// Package streams implements the work-queue layer on top of Redis Streams.
//
// Two streams are used:
//   - cfg.EventStreamKey (default "events:in") — global ingestion queue consumed
//     by the worker via consumer group cfg.ConsumerGroup. Replaces the previous
//     Kafka topic webhooks.inbound.
//   - cfg.DLQStreamKey (default "events:failed") — events that exhausted retries.
//     Replaces the previous Kafka topic webhooks.dlq.
//
// Each entry carries fields tenant_id, event_id and payload (bytes).
package streams

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Field names used in every stream entry. The DLQ adds x_original_error.
const (
	FieldTenantID      = "tenant_id"
	FieldEventID       = "event_id"
	FieldPayload       = "payload"
	FieldOriginalError = "x_original_error"
)

// EnsureGroup creates the consumer group on stream if it does not exist yet.
// It calls XGROUP CREATE ... MKSTREAM with id "$" so that only new messages are
// delivered. Returns nil if the group already exists (BUSYGROUP).
func EnsureGroup(ctx context.Context, rdb *goredis.Client, stream, group string) error {
	if err := rdb.XGroupCreateMkStream(ctx, stream, group, "$").Err(); err != nil {
		if strings.Contains(err.Error(), "BUSYGROUP") {
			return nil
		}
		return fmt.Errorf("create consumer group %s on %s: %w", group, stream, err)
	}
	return nil
}

// Publish appends a webhook event to the ingestion stream and returns the new
// stream entry id (e.g. "1700000000000-0").
func Publish(ctx context.Context, rdb *goredis.Client, stream, tenantID, eventID string, payload []byte) (string, error) {
	id, err := rdb.XAdd(ctx, &goredis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{
			FieldTenantID: tenantID,
			FieldEventID:  eventID,
			FieldPayload:  payload,
		},
	}).Result()
	if err != nil {
		return "", fmt.Errorf("XADD %s: %w", stream, err)
	}
	return id, nil
}

// PublishDLQ appends a permanently failed event to the DLQ stream, including
// the original error message for diagnostics.
func PublishDLQ(ctx context.Context, rdb *goredis.Client, stream, tenantID, eventID string, payload []byte, origErr error) (string, error) {
	values := map[string]interface{}{
		FieldTenantID: tenantID,
		FieldEventID:  eventID,
		FieldPayload:  payload,
	}
	if origErr != nil {
		values[FieldOriginalError] = origErr.Error()
	}
	id, err := rdb.XAdd(ctx, &goredis.XAddArgs{
		Stream: stream,
		Values: values,
	}).Result()
	if err != nil {
		return "", fmt.Errorf("XADD DLQ %s: %w", stream, err)
	}
	return id, nil
}

// Reader consumes messages from a stream as part of a consumer group.
// One Reader per worker instance; Consumer must be unique within the group.
type Reader struct {
	rdb      *goredis.Client
	Stream   string
	Group    string
	Consumer string
	Block    int
	Count    int64
}

// NewReader constructs a Reader. blockMs is the XREADGROUP block timeout; count
// caps the batch size returned by each Read.
func NewReader(rdb *goredis.Client, stream, group, consumer string, blockMs int, count int64) *Reader {
	if count <= 0 {
		count = 32
	}
	return &Reader{
		rdb:      rdb,
		Stream:   stream,
		Group:    group,
		Consumer: consumer,
		Block:    blockMs,
		Count:    count,
	}
}

// Read blocks up to r.Block milliseconds waiting for new messages. Returns an
// empty slice on timeout (no error). Returns ctx.Err() when the context is done.
func (r *Reader) Read(ctx context.Context) ([]goredis.XMessage, error) {
	streams, err := r.rdb.XReadGroup(ctx, &goredis.XReadGroupArgs{
		Group:    r.Group,
		Consumer: r.Consumer,
		Streams:  []string{r.Stream, ">"},
		Count:    r.Count,
		Block:    time.Duration(r.Block) * time.Millisecond,
	}).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("XREADGROUP %s: %w", r.Stream, err)
	}
	if len(streams) == 0 {
		return nil, nil
	}
	return streams[0].Messages, nil
}

// Ack acknowledges one or more pending messages so that the consumer group
// stops tracking them.
func (r *Reader) Ack(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := r.rdb.XAck(ctx, r.Stream, r.Group, ids...).Err(); err != nil {
		return fmt.Errorf("XACK %s/%s: %w", r.Stream, r.Group, err)
	}
	return nil
}
