package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	goredis "github.com/redis/go-redis/v9"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/observability"
	"github.com/easyhooks/easyhooks/internal/security"
	"github.com/easyhooks/easyhooks/internal/service"
)

const wsHeartbeatInterval = 30 * time.Second

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WSEvents handles GET /ws/events/{tenant_id}?token=...
// Authenticates via signed token, sends history, then streams live events.
func WSEvents(rdb *goredis.Client, cfg *config.Config, fanoutMgr *service.FanoutManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantIDStr := chi.URLParam(r, "tenant_id")
		tenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}

		// Validate token BEFORE upgrading the WebSocket connection
		token := r.URL.Query().Get("token")
		tokenTenant, err := security.VerifySignedToken(token, cfg.AppSecretKey)
		if err != nil || tokenTenant != tenantID {
			http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Warn("WebSocket upgrade failed", "tenant_id", tenantID, "error", err)
			return
		}
		defer conn.Close()

		observability.WebSocketConnectionsActive.WithLabelValues(tenantID.String()).Inc()
		defer observability.WebSocketConnectionsActive.WithLabelValues(tenantID.String()).Dec()

		// Send historical events
		history, err := service.ReadTenantHistory(r.Context(), rdb, cfg, tenantID, cfg.StreamHistoryCount)
		if err != nil {
			slog.Warn("Failed to read history", "tenant_id", tenantID, "error", err)
		}
		for _, ev := range history {
			if err := conn.WriteMessage(websocket.TextMessage, ev.Payload); err != nil {
				slog.Debug("Failed to write history event", "tenant_id", tenantID, "error", err)
				return
			}
			observability.WebSocketMessagesSentTotal.WithLabelValues(tenantID.String()).Inc()
		}

		// Context for goroutines; cancelled on conn close
		ctx := r.Context()
		done := make(chan struct{})

		// Goroutine: heartbeat pings every 30s
		go func() {
			ticker := time.NewTicker(wsHeartbeatInterval)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case t := <-ticker.C:
					msg, _ := json.Marshal(map[string]interface{}{"type": "ping", "timestamp": t.Unix()})
					if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
						return
					}
				}
			}
		}()

		// Goroutine: forward events from stream/fanout to WebSocket
		go func() {
			defer close(done)
			var events <-chan service.StreamEvent

			if cfg.WSUseFanout {
				fanout := fanoutMgr.GetFanout(rdb, cfg, tenantID)
				ch, unsub := fanout.Subscribe()
				defer unsub()
				events = ch
			} else {
				events = service.StreamTenantEvents(ctx, rdb, cfg, tenantID, "$")
			}

			for {
				select {
				case ev, ok := <-events:
					if !ok {
						return
					}
					if err := conn.WriteMessage(websocket.TextMessage, ev.Payload); err != nil {
						return
					}
					observability.WebSocketMessagesSentTotal.WithLabelValues(tenantID.String()).Inc()
				case <-ctx.Done():
					return
				}
			}
		}()

		// Block reading from client (detect disconnect)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}
}
