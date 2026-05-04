package handler

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/db/queries"
	"github.com/easyhooks/easyhooks/internal/service"
)

type createTenantRequest struct {
	Name string `json:"name"`
}

// CreateTenant handles POST /admin/tenants.
// Protected by AdminAuth middleware.
func CreateTenant(store *queries.Store, rdb *goredis.Client, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createTenantRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			writeError(w, http.StatusBadRequest, "Invalid request body — 'name' is required")
			return
		}

		// Use a fixed admin ID placeholder; real admin ID lookup is not in scope for this handler
		// (admin context not injected — using zero UUID as created_by placeholder consistent with seeded admin)
		adminID := uuid.Nil
		result, err := service.CreateTenant(r.Context(), store, rdb, cfg, req.Name, adminID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to create tenant")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"tenant_id":  result.Tenant.ID.String(),
			"secret_key": result.SecretKey,
		})
	}
}

func writeError(w http.ResponseWriter, code int, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"detail": detail}) //nolint:errcheck
}
