package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/db/queries"
	"github.com/easyhooks/easyhooks/internal/security"
)

// CreateTenantResult holds the newly created tenant and its plaintext secret key.
type CreateTenantResult struct {
	Tenant    *queries.Tenant
	SecretKey string
}

// CreateTenant creates a new tenant, hashes its secret, persists to DB, and caches in Redis.
// Mirrors Python's create_tenant in tenant_service.py.
func CreateTenant(ctx context.Context, store *queries.Store, rdb *goredis.Client, cfg *config.Config, name string, adminID uuid.UUID) (CreateTenantResult, error) {
	rawSecret, err := security.GenerateSecretKey(cfg.SecretKeyBytes)
	if err != nil {
		return CreateTenantResult{}, fmt.Errorf("generate secret key: %w", err)
	}

	secretHash, err := security.HashSecret(rawSecret)
	if err != nil {
		return CreateTenantResult{}, fmt.Errorf("hash secret: %w", err)
	}

	tenantID := uuid.New()
	tenant, err := store.CreateTenant(ctx, tenantID, name, secretHash, adminID)
	if err != nil {
		return CreateTenantResult{}, fmt.Errorf("create tenant in DB: %w", err)
	}

	// Cache credentials in Redis for fast auth lookups
	authKey := fmt.Sprintf("tenant_auth:%s", tenant.ID)
	hmacKey := fmt.Sprintf("tenant_hmac_key:%s", tenant.ID)
	if err := rdb.Set(ctx, authKey, secretHash, 0).Err(); err != nil {
		slog.Warn("Failed to sync tenant auth hash to Redis", "tenant_id", tenant.ID, "error", err)
	}
	if err := rdb.Set(ctx, hmacKey, rawSecret, 0).Err(); err != nil {
		slog.Warn("Failed to sync tenant HMAC key to Redis", "tenant_id", tenant.ID, "error", err)
	}

	return CreateTenantResult{Tenant: tenant, SecretKey: rawSecret}, nil
}
