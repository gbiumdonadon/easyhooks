package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/easyhooks/easyhooks/internal/service"
)

func TestPublishAndReadHistory(t *testing.T) {
	cfg := newTestConfig()
	_, rdb := newMiniredisClient(t)
	ctx := context.Background()

	tenantID := uuid.New()
	payloads := [][]byte{
		[]byte(`{"event":"first"}`),
		[]byte(`{"event":"second"}`),
		[]byte(`{"event":"third"}`),
	}

	for _, p := range payloads {
		_, err := service.PublishTenantEvent(ctx, rdb, cfg, tenantID, p)
		require.NoError(t, err)
	}

	history, err := service.ReadTenantHistory(ctx, rdb, cfg, tenantID, 10)
	require.NoError(t, err)
	assert.Len(t, history, 3)

	// Should be in chronological order (oldest first)
	assert.Equal(t, payloads[0], history[0].Payload)
	assert.Equal(t, payloads[1], history[1].Payload)
	assert.Equal(t, payloads[2], history[2].Payload)
}

func TestReadHistory_Empty(t *testing.T) {
	cfg := newTestConfig()
	_, rdb := newMiniredisClient(t)

	history, err := service.ReadTenantHistory(context.Background(), rdb, cfg, uuid.New(), 10)
	require.NoError(t, err)
	assert.Empty(t, history)
}

func TestReadHistory_LimitedCount(t *testing.T) {
	cfg := newTestConfig()
	_, rdb := newMiniredisClient(t)
	ctx := context.Background()
	tenantID := uuid.New()

	for i := 0; i < 5; i++ {
		service.PublishTenantEvent(ctx, rdb, cfg, tenantID, []byte(`{"n":1}`)) //nolint:errcheck
	}

	history, err := service.ReadTenantHistory(ctx, rdb, cfg, tenantID, 3)
	require.NoError(t, err)
	assert.Len(t, history, 3)
}

func TestStreamTenantEvents_ReceivesPublishedEvent(t *testing.T) {
	cfg := newTestConfig()
	_, rdb := newMiniredisClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tenantID := uuid.New()
	events := service.StreamTenantEvents(ctx, rdb, cfg, tenantID, "0")

	// Publish after starting stream
	go func() {
		time.Sleep(50 * time.Millisecond)
		service.PublishTenantEvent(context.Background(), rdb, cfg, tenantID, []byte(`{"hello":"world"}`)) //nolint:errcheck
	}()

	select {
	case ev := <-events:
		assert.Equal(t, []byte(`{"hello":"world"}`), ev.Payload)
	case <-ctx.Done():
		t.Fatal("timed out waiting for event")
	}
}

func TestTenantStreamKey(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	key := service.TenantStreamKey(id, "stream:tenant:")
	assert.Equal(t, "stream:tenant:550e8400-e29b-41d4-a716-446655440000", key)
}
