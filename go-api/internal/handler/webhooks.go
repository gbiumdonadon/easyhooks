package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/kafka"
	"github.com/easyhooks/easyhooks/internal/middleware"
	"github.com/easyhooks/easyhooks/internal/observability"
)

// IngestWebhook handles POST /v1/webhooks/{tenant_id}.
// Protected by TenantAuth middleware.
func IngestWebhook(producer *kgo.Client, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, ok := middleware.TenantFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "Unauthenticated")
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

		if err := kafka.ProduceWebhookMessage(r.Context(), producer, cfg.KafkaWebhookTopic, tenant.TenantID.String(), eventID, body); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to queue webhook event")
			observability.WebhookRequestsTotal.WithLabelValues(tenant.TenantID.String(), "error").Inc()
			return
		}

		observability.WebhookRequestsTotal.WithLabelValues(tenant.TenantID.String(), "accepted").Inc()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"status":    "accepted",
			"tenant_id": tenant.TenantID.String(),
		})
	}
}
