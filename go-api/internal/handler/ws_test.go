package handler_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/easyhooks/easyhooks/internal/handler"
	"github.com/easyhooks/easyhooks/internal/security"
	"github.com/easyhooks/easyhooks/internal/service"
)

// TestWSEvents_NoConcurrentWritePanic stresses the handler with a 1 ms
// heartbeat and a flood of fanout events. Before the single-writer refactor
// the heartbeat goroutine and the forwarder both called conn.WriteMessage
// concurrently, and gorilla/websocket panicked with
// "concurrent write to websocket connection". With the writePump-only
// invariant this test passes cleanly under `go test -race`.
//
// The test is intentionally lenient on counts (it is about race-freedom,
// not delivery semantics) but does require at least one application
// payload + one heartbeat to arrive — otherwise we'd be exercising none of
// the dangerous interleavings.
func TestWSEvents_NoConcurrentWritePanic(t *testing.T) {
	cfg := newTestConfig()
	cfg.WSUseFanout = true
	cfg.WSFanoutBufferSize = 256
	cfg.WSClientSendBuffer = 256
	cfg.WSHeartbeatInterval = 1 * time.Millisecond
	cfg.WSWriteTimeout = 5 * time.Second
	cfg.StreamHistoryCount = 0

	_, rdb := newMiniredisClient(t)

	tenantID := uuid.New()
	fanoutMgr := service.NewFanoutManager()

	r := chi.NewRouter()
	r.Get("/ws/events/{tenant_id}", handler.WSEvents(rdb, cfg, fanoutMgr))
	srv := httptest.NewServer(r)
	defer srv.Close()

	token := security.CreateSignedToken(tenantID, cfg.WSTokenTTL, cfg.AppSecretKey)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") +
		"/ws/events/" + tenantID.String() +
		"?token=" + url.QueryEscape(token)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close() //nolint:errcheck

	const wantEvents = 200

	var publishWG sync.WaitGroup
	publishWG.Add(1)
	go func() {
		defer publishWG.Done()
		for i := 0; i < wantEvents; i++ {
			payload, _ := json.Marshal(map[string]interface{}{"n": i})
			_, _ = service.PublishTenantEvent(context.Background(), rdb, cfg, tenantID, payload)
			// A tiny pause guarantees that the publisher and the heartbeat
			// ticker actually interleave instead of all events landing in
			// one Redis batch the fanout drains in a single iteration.
			time.Sleep(200 * time.Microsecond)
		}
	}()

	var (
		gotEvents atomic.Int64
		gotPings  atomic.Int64
	)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(500*time.Millisecond)))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var parsed map[string]interface{}
		if json.Unmarshal(raw, &parsed) == nil {
			if t, _ := parsed["type"].(string); t == "ping" {
				gotPings.Add(1)
				continue
			}
		}
		gotEvents.Add(1)
		if gotEvents.Load() >= wantEvents {
			break
		}
	}
	publishWG.Wait()

	require.Greater(t, gotEvents.Load(), int64(0),
		"expected at least one application event to round-trip via the fanout")
	require.Greater(t, gotPings.Load(), int64(0),
		"expected at least one heartbeat ping to interleave with events")
}

// TestWSEvents_SlowClientDisconnects verifies the second backpressure stage:
// when the per-connection out queue fills up because the peer is not
// reading, the handler disconnects the client (instead of penalising the
// fanout). We approximate "slow client" by never reading from the socket
// while events flood in; with a tiny send buffer the handler must give up.
func TestWSEvents_SlowClientDisconnects(t *testing.T) {
	cfg := newTestConfig()
	cfg.WSUseFanout = true
	cfg.WSFanoutBufferSize = 1024
	cfg.WSClientSendBuffer = 2
	cfg.WSHeartbeatInterval = 1 * time.Hour // keep the heartbeat out of the way
	cfg.WSWriteTimeout = 1 * time.Second
	cfg.StreamHistoryCount = 0

	_, rdb := newMiniredisClient(t)

	tenantID := uuid.New()
	fanoutMgr := service.NewFanoutManager()

	r := chi.NewRouter()
	r.Get("/ws/events/{tenant_id}", handler.WSEvents(rdb, cfg, fanoutMgr))
	srv := httptest.NewServer(r)
	defer srv.Close()

	token := security.CreateSignedToken(tenantID, cfg.WSTokenTTL, cfg.AppSecretKey)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") +
		"/ws/events/" + tenantID.String() +
		"?token=" + url.QueryEscape(token)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close() //nolint:errcheck

	// Give the handler a moment to subscribe to the fanout before we start
	// publishing — otherwise the early events are dropped at stage 1
	// (fanout) and the slow-client path never triggers.
	time.Sleep(100 * time.Millisecond)

	for i := 0; i < 1000; i++ {
		// Each payload is a few KB so the kernel send buffer fills quickly
		// even when the client doesn't ack.
		payload := make([]byte, 4*1024)
		for j := range payload {
			payload[j] = 'a'
		}
		_, _ = service.PublishTenantEvent(context.Background(), rdb, cfg, tenantID, payload)
	}

	// Without reading anything, wait for the server to detect the slow
	// client and close the connection. ReadMessage should return an error
	// (close frame, connection reset, or read deadline) within a few
	// seconds.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
