package handler

import (
	"encoding/json"
	"net/http"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/middleware"
	"github.com/easyhooks/easyhooks/internal/security"
)

// IssueToken handles POST /v1/tokens/{tenant_id}.
// Protected by TenantAuth middleware.
func IssueToken(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, ok := middleware.TenantFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "Unauthenticated")
			return
		}

		token := security.CreateSignedToken(tenant.TenantID, cfg.WSTokenTTL, cfg.AppSecretKey)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"token":      token,
			"expires_in": cfg.WSTokenTTL,
		})
	}
}
