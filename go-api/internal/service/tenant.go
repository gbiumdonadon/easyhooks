package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/security"
)

// CreateTenantResult holds the newly provisioned tenant id and the plaintext
// secret key that the caller must hand back to the client (it is never
// retrievable again — only the bcrypt hash is stored).
type CreateTenantResult struct {
	TenantID  uuid.UUID
	SecretKey string
}

// CreateTenant generates a tenant id and a fresh secret, then stores both
// auth artifacts in Redis:
//   - tenant_auth:{id}     bcrypt hash for Bearer auth
//   - tenant_hmac_key:{id} raw secret for HMAC-SHA256 webhook signatures
//
// Both keys are persisted with no TTL so that they survive Redis restarts as
// long as AOF/RDB persistence is enabled. They are the canonical source of
// truth for tenant credentials.
func CreateTenant(ctx context.Context, rdb *goredis.Client, cfg *config.Config) (CreateTenantResult, error) {
	rawSecret, err := security.GenerateSecretKey(cfg.SecretKeyBytes)
	if err != nil {
		return CreateTenantResult{}, fmt.Errorf("generate secret key: %w", err)
	}
	secretHash, err := security.HashSecret(rawSecret)
	if err != nil {
		return CreateTenantResult{}, fmt.Errorf("hash secret: %w", err)
	}

	tenantID := uuid.New()
	authKey := fmt.Sprintf("tenant_auth:%s", tenantID)
	hmacKey := fmt.Sprintf("tenant_hmac_key:%s", tenantID)

	if err := rdb.Set(ctx, authKey, secretHash, 0).Err(); err != nil {
		return CreateTenantResult{}, fmt.Errorf("SET %s: %w", authKey, err)
	}
	if err := rdb.Set(ctx, hmacKey, rawSecret, 0).Err(); err != nil {
		// Best-effort cleanup so we don't leave a half-provisioned tenant.
		_ = rdb.Del(ctx, authKey).Err()
		return CreateTenantResult{}, fmt.Errorf("SET %s: %w", hmacKey, err)
	}

	return CreateTenantResult{TenantID: tenantID, SecretKey: rawSecret}, nil
}
