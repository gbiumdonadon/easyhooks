package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/easyhooks/easyhooks/internal/config"
)

// StreamEvent holds a single event from a Redis Stream.
type StreamEvent struct {
	ID      string
	Payload []byte
}

// TenantStreamKey returns the Redis Stream key for a given tenant.
func TenantStreamKey(tenantID uuid.UUID, prefix string) string {
	return fmt.Sprintf("%s%s", prefix, tenantID)
}

// PublishTenantEvent publishes an event to the tenant's Redis Stream.
// Uses XADD MAXLEN ~ for approximate capping (better performance than exact).
func PublishTenantEvent(ctx context.Context, rdb *goredis.Client, cfg *config.Config, tenantID uuid.UUID, payload []byte) (string, error) {
	streamKey := TenantStreamKey(tenantID, cfg.TenantEventsStreamPrefix)
	msgID, err := rdb.XAdd(ctx, &goredis.XAddArgs{
		Stream: streamKey,
		MaxLen: int64(cfg.StreamMaxLen),
		Approx: true,
		Values: map[string]interface{}{"payload": payload},
	}).Result()
	if err != nil {
		return "", fmt.Errorf("XADD to stream %s: %w", streamKey, err)
	}
	return msgID, nil
}

// ReadTenantHistory reads up to count recent events in chronological order (oldest first).
// Uses XREVRANGE then reverses for tenant event history.
func ReadTenantHistory(ctx context.Context, rdb *goredis.Client, cfg *config.Config, tenantID uuid.UUID, count int) ([]StreamEvent, error) {
	streamKey := TenantStreamKey(tenantID, cfg.TenantEventsStreamPrefix)
	results, err := rdb.XRevRangeN(ctx, streamKey, "+", "-", int64(count)).Result()
	if err != nil {
		return nil, fmt.Errorf("XREVRANGE on %s: %w", streamKey, err)
	}

	events := make([]StreamEvent, 0, len(results))
	for i := len(results) - 1; i >= 0; i-- { // reverse for chronological order
		msg := results[i]
		payload, ok := msg.Values["payload"]
		if !ok {
			continue
		}
		var payloadBytes []byte
		switch v := payload.(type) {
		case string:
			payloadBytes = []byte(v)
		case []byte:
			payloadBytes = v
		default:
			continue
		}
		events = append(events, StreamEvent{ID: msg.ID, Payload: payloadBytes})
	}
	return events, nil
}

// StreamTenantEvents streams new events from a tenant's Redis Stream using XREAD BLOCK.
// Sends events to the returned channel until ctx is cancelled.
func StreamTenantEvents(ctx context.Context, rdb *goredis.Client, cfg *config.Config, tenantID uuid.UUID, startID string) <-chan StreamEvent {
	bufSize := cfg.WSFanoutBufferSize
	if bufSize <= 0 {
		bufSize = 100
	}
	ch := make(chan StreamEvent, bufSize)
	streamKey := TenantStreamKey(tenantID, cfg.TenantEventsStreamPrefix)

	go func() {
		defer close(ch)
		currentID := startID

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			results, err := rdb.XRead(ctx, &goredis.XReadArgs{
				Streams: []string{streamKey, currentID},
				Block:   5 * time.Second,
			}).Result()
			if err != nil {
				if err == context.Canceled || err == context.DeadlineExceeded {
					return
				}
				// nil result on timeout — normal, retry
				continue
			}

			for _, stream := range results {
				for _, msg := range stream.Messages {
					payload, ok := msg.Values["payload"]
					if !ok {
						continue
					}
					var payloadBytes []byte
					switch v := payload.(type) {
					case string:
						payloadBytes = []byte(v)
					case []byte:
						payloadBytes = v
					default:
						continue
					}
					currentID = msg.ID
					select {
					case ch <- StreamEvent{ID: msg.ID, Payload: payloadBytes}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return ch
}
