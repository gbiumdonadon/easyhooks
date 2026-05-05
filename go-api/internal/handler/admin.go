package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	goredis "github.com/redis/go-redis/v9"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/service"
)

// createTenantRequest is intentionally permissive: the optional `name` field is
// accepted for backwards compatibility with existing tooling (e.g. the load
// test bootstrap script) but is not persisted in the Redis-only store.
type createTenantRequest struct {
	Name string `json:"name,omitempty"`
}

// CreateTenant handles POST /admin/tenants. Protected by AdminAuth middleware.
func CreateTenant(rdb *goredis.Client, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createTenantRequest
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "Invalid request body")
				return
			}
		}

		result, err := service.CreateTenant(r.Context(), rdb, cfg)
		if err != nil {
			slog.Error("Failed to create tenant", "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to create tenant")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"tenant_id":  result.TenantID.String(),
			"secret_key": result.SecretKey,
			"name":       req.Name,
		})
	}
}

func writeError(w http.ResponseWriter, code int, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"detail": detail})
}
