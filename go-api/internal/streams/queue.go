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
//
// maxLenApprox caps the physical size of the stream via XADD ... MAXLEN ~ N.
// XACK only removes entries from the consumer group's pending list; the entries
// themselves stay in the stream forever unless trimmed. Without a cap, XLEN
// grows unboundedly across runs and the load shedder (which reads XLEN) ends
// up stuck in shedding mode forever. Use cfg.IngestStreamMaxLen for production
// callers; pass 0 in tests to keep the legacy untrimmed behavior.
func Publish(ctx context.Context, rdb *goredis.Client, stream, tenantID, eventID string, payload []byte, maxLenApprox int64) (string, error) {
	args := &goredis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{
			FieldTenantID: tenantID,
			FieldEventID:  eventID,
			FieldPayload:  payload,
		},
	}
	if maxLenApprox > 0 {
		// Approx (~) is O(1) amortized — Redis trims whole macro-nodes at the
		// stream's tail rather than scanning entry by entry. The actual XLEN
		// after the call may sit slightly above maxLenApprox (a handful of
		// entries) which is fine for our use case.
		args.MaxLen = maxLenApprox
		args.Approx = true
	}
	id, err := rdb.XAdd(ctx, args).Result()
	if err != nil {
		return "", fmt.Errorf("XADD %s: %w", stream, err)
	}
	return id, nil
}

// PublishDLQ appends a permanently failed event to the DLQ stream, including
// the original error message for diagnostics.
//
// maxLenApprox caps the DLQ stream the same way Publish caps events:in
// (XADD ... MAXLEN ~ N). The DLQ has much lower write pressure (only events
// that exhausted retries) but it is still unbounded by default and operators
// rarely XDEL/XTRIM it manually, so the same protection applies. Pass 0 to
// disable trimming (legacy behavior, useful in tests).
func PublishDLQ(ctx context.Context, rdb *goredis.Client, stream, tenantID, eventID string, payload []byte, origErr error, maxLenApprox int64) (string, error) {
	values := map[string]interface{}{
		FieldTenantID: tenantID,
		FieldEventID:  eventID,
		FieldPayload:  payload,
	}
	if origErr != nil {
		values[FieldOriginalError] = origErr.Error()
	}
	args := &goredis.XAddArgs{
		Stream: stream,
		Values: values,
	}
	if maxLenApprox > 0 {
		args.MaxLen = maxLenApprox
		args.Approx = true
	}
	id, err := rdb.XAdd(ctx, args).Result()
	if err != nil {
		return "", fmt.Errorf("XADD DLQ %s: %w", stream, err)
	}
	return id, nil
}

// TrimMaxLenApprox runs an explicit XTRIM ... MAXLEN ~ N on stream so an
// already-saturated instance can recover after upgrading. Without this, an
// instance that crossed the load shedder high-water mark before the MAXLEN
// fix would stay stuck in shedding mode forever — the shedder blocks the very
// XADDs that would otherwise exercise the new cap.
//
// Returns the number of entries trimmed (best-effort, may be approximate).
// maxLenApprox <= 0 is a no-op. Errors are returned to the caller; this is
// intentionally not best-effort because a startup-time trim failure is worth
// surfacing in the logs.
func TrimMaxLenApprox(ctx context.Context, rdb *goredis.Client, stream string, maxLenApprox int64) (int64, error) {
	if maxLenApprox <= 0 {
		return 0, nil
	}
	trimmed, err := rdb.XTrimMaxLenApprox(ctx, stream, maxLenApprox, 0).Result()
	if err != nil {
		return 0, fmt.Errorf("XTRIM %s MAXLEN ~ %d: %w", stream, maxLenApprox, err)
	}
	return trimmed, nil
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
