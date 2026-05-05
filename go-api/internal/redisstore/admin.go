// Package redisstore implements the minimal Redis-backed credential store that
// replaces the former PostgreSQL admin_users table.
//
// Only one bootstrap admin is supported (seeded from ADMIN_SEED_TOKEN). The
// hash is stored under the single key `admin:token_hash` and verified with
// bcrypt on every admin Bearer authentication.
//
// Tenant credentials continue to live in Redis as before:
//   - tenant_auth:{id}      bcrypt hash for Bearer auth
//   - tenant_hmac_key:{id}  raw secret for HMAC-SHA256 webhook signatures
package redisstore

import (
	"context"
	"errors"
	"fmt"

	goredis "github.com/redis/go-redis/v9"

	"github.com/easyhooks/easyhooks/internal/security"
)

// AdminTokenHashKey is the single Redis key that holds the bcrypt hash of the
// bootstrap admin Bearer token.
const AdminTokenHashKey = "admin:token_hash"

// ErrAdminNotSeeded is returned by VerifyAdmin when the seed step has not run
// yet and the admin key is missing.
var ErrAdminNotSeeded = errors.New("admin token hash not seeded")

// SeedSuperAdmin writes the bcrypt hash of rawToken under AdminTokenHashKey
// the first time the application starts. Subsequent calls are no-ops, so that
// rotating ADMIN_SEED_TOKEN at runtime does not silently change the password.
func SeedSuperAdmin(ctx context.Context, rdb *goredis.Client, rawToken string) error {
	exists, err := rdb.Exists(ctx, AdminTokenHashKey).Result()
	if err != nil {
		return fmt.Errorf("EXISTS %s: %w", AdminTokenHashKey, err)
	}
	if exists > 0 {
		return nil
	}
	hash, err := security.HashSecret(rawToken)
	if err != nil {
		return fmt.Errorf("hash admin token: %w", err)
	}
	if err := rdb.Set(ctx, AdminTokenHashKey, hash, 0).Err(); err != nil {
		return fmt.Errorf("SET %s: %w", AdminTokenHashKey, err)
	}
	return nil
}

// VerifyAdmin returns true when presented matches the seeded admin token. It
// returns ErrAdminNotSeeded if the bootstrap step has not run yet, and any
// underlying Redis error otherwise.
func VerifyAdmin(ctx context.Context, rdb *goredis.Client, presented string) (bool, error) {
	hash, err := rdb.Get(ctx, AdminTokenHashKey).Result()
	if errors.Is(err, goredis.Nil) {
		return false, ErrAdminNotSeeded
	}
	if err != nil {
		return false, fmt.Errorf("GET %s: %w", AdminTokenHashKey, err)
	}
	return security.VerifySecret(presented, hash), nil
}
