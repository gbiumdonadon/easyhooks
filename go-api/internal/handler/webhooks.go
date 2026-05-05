package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	goredis "github.com/redis/go-redis/v9"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/middleware"
	"github.com/easyhooks/easyhooks/internal/observability"
	"github.com/easyhooks/easyhooks/internal/streams"
)

// LoadShedder is the minimal contract IngestWebhook needs to decide whether
// the ingestion stream is saturated and new requests should be rejected with
// HTTP 429. Pass nil to disable load shedding (e.g. in unit tests).
type LoadShedder interface {
	ShouldShed() bool
}

// IngestWebhook handles POST /v1/webhooks/{tenant_id} by appending the event to
// the Redis Stream cfg.EventStreamKey for the worker to pick up. Protected by
// TenantAuth middleware. When shedder reports ShouldShed()==true, the request
// is rejected with 429 + Retry-After before any Redis round-trip.
func IngestWebhook(rdb *goredis.Client, cfg *config.Config, shedder LoadShedder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, ok := middleware.TenantFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "Unauthenticated")
			return
		}

		if shedder != nil && shedder.ShouldShed() {
			w.Header().Set("Retry-After", "5")
			observability.WebhookLoadShedTotal.WithLabelValues(tenant.TenantID.String()).Inc()
			observability.WebhookRequestsTotal.WithLabelValues(tenant.TenantID.String(), "shed").Inc()
			writeError(w, http.StatusTooManyRequests, "Ingestion queue saturated; retry later")
			return
		}

		eventID := strings.TrimSpace(r.Header.Get("X-Event-Id"))
		if eventID == "" {
			writeError(w, http.StatusBadRequest, "Missing required header X-Event-Id")
			observability.WebhookRequestsTotal.WithLabelValues(tenant.TenantID.String(), "error").Inc()
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Cannot read request body")
			return
		}

		if _, err := streams.Publish(r.Context(), rdb, cfg.EventStreamKey, tenant.TenantID.String(), eventID, body); err != nil {
			slog.Error("Failed to publish webhook to stream",
				"tenant_id", tenant.TenantID, "event_id", eventID, "error", err,
			)
			observability.StreamPublishTotal.WithLabelValues(cfg.EventStreamKey, "error").Inc()
			observability.WebhookRequestsTotal.WithLabelValues(tenant.TenantID.String(), "error").Inc()
			writeError(w, http.StatusInternalServerError, "Failed to queue webhook event")
			return
		}

		observability.StreamPublishTotal.WithLabelValues(cfg.EventStreamKey, "success").Inc()
		observability.WebhookRequestsTotal.WithLabelValues(tenant.TenantID.String(), "accepted").Inc()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":    "accepted",
			"tenant_id": tenant.TenantID.String(),
		})
	}
}
