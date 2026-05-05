package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/db/queries"
	"github.com/easyhooks/easyhooks/internal/security"
)

// contextKey is an unexported type to avoid collisions in context.
type contextKey int

const (
	tenantKey  contextKey = iota
	rawBodyKey            // stores []byte of request body after first read
)

// AuthenticatedTenant is injected into request context after successful auth.
type AuthenticatedTenant struct {
	TenantID uuid.UUID
}

// TenantFromContext returns the AuthenticatedTenant stored by TenantAuth middleware.
func TenantFromContext(ctx context.Context) (AuthenticatedTenant, bool) {
	t, ok := ctx.Value(tenantKey).(AuthenticatedTenant)
	return t, ok
}

func writeAuthError(w http.ResponseWriter, code int, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"detail": detail}) //nolint:errcheck
}

// AdminAuth verifies admin Bearer tokens against the DB (bcrypt).
func AdminAuth(store *queries.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				writeAuthError(w, http.StatusUnauthorized, "Missing or invalid authorization header")
				return
			}
			token := strings.TrimPrefix(auth, "Bearer ")

			admins, err := store.GetAllAdminUsers(r.Context())
			if err != nil {
				writeAuthError(w, http.StatusInternalServerError, "Internal error")
				return
			}
			for _, admin := range admins {
				if security.VerifySecret(token, admin.TokenHash) {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeAuthError(w, http.StatusForbidden, "Invalid admin credentials")
		})
	}
}

// TenantAuth validates tenant credentials (HMAC or Bearer) and injects AuthenticatedTenant.
// Resolves the authenticated tenant from context (Bearer + tenant id).
func TenantAuth(rdb *goredis.Client, cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantIDStr := chi.URLParam(r, "tenant_id")
			tenantID, err := uuid.Parse(tenantIDStr)
			if err != nil {
				writeAuthError(w, http.StatusBadRequest, "Invalid tenant_id")
				return
			}

			// Read body once and restore it so handlers can still read it.
			body, err := io.ReadAll(r.Body)
			if err != nil {
				writeAuthError(w, http.StatusBadRequest, "Cannot read request body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			ctx := context.WithValue(r.Context(), rawBodyKey, body)

			var tenant AuthenticatedTenant
			hmacSig := r.Header.Get("X-Webhook-Signature")
			if hmacSig != "" {
				tenant, err = authenticateViaHMAC(ctx, tenantID, hmacSig, body, rdb)
			} else if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				rawSecret := strings.TrimPrefix(auth, "Bearer ")
				tenant, err = authenticateViaBearer(ctx, tenantID, rawSecret, rdb, cfg)
			} else {
				writeAuthError(w, http.StatusUnauthorized, "Missing authentication credentials")
				return
			}
			if err != nil {
				if ae, ok := err.(*authErr); ok {
					writeAuthError(w, ae.code, ae.message)
				} else {
					writeAuthError(w, http.StatusInternalServerError, "Internal error")
				}
				return
			}

			ctx = context.WithValue(ctx, tenantKey, tenant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type authErr struct {
	code    int
	message string
}

func (e *authErr) Error() string { return e.message }

// authenticateViaHMAC validates X-Webhook-Signature against the raw secret in Redis.
func authenticateViaHMAC(ctx context.Context, tenantID uuid.UUID, signature string, body []byte, rdb *goredis.Client) (AuthenticatedTenant, error) {
	key := fmt.Sprintf("tenant_hmac_key:%s", tenantID)
	cachedKey, err := rdb.Get(ctx, key).Result()
	if err == goredis.Nil {
		return AuthenticatedTenant{}, &authErr{http.StatusUnauthorized, "Unknown tenant"}
	}
	if err != nil {
		return AuthenticatedTenant{}, &authErr{http.StatusInternalServerError, "Internal error"}
	}
	if !security.VerifyHMACSignature(cachedKey, body, signature) {
		return AuthenticatedTenant{}, &authErr{http.StatusForbidden, "Invalid HMAC signature"}
	}
	return AuthenticatedTenant{TenantID: tenantID}, nil
}

// authenticateViaBearer uses a two-level Redis cache to avoid repeated bcrypt calls.
// Bearer session authentication via Redis cache and bcrypt fallback.
func authenticateViaBearer(ctx context.Context, tenantID uuid.UUID, rawSecret string, rdb *goredis.Client, cfg *config.Config) (AuthenticatedTenant, error) {
	// Level 1: fast session cache — avoids bcrypt on every request
	shortHash := fmt.Sprintf("%x", sha256.Sum256([]byte(rawSecret)))[:16]
	sessionKey := fmt.Sprintf("auth_session:%s:%s", tenantID, shortHash)
	if exists, _ := rdb.Exists(ctx, sessionKey).Result(); exists > 0 {
		return AuthenticatedTenant{TenantID: tenantID}, nil
	}

	// Level 2: bcrypt verify against cached hash
	authKey := fmt.Sprintf("tenant_auth:%s", tenantID)
	cachedHash, err := rdb.Get(ctx, authKey).Result()
	if err == goredis.Nil {
		return AuthenticatedTenant{}, &authErr{http.StatusUnauthorized, "Unknown tenant"}
	}
	if err != nil {
		return AuthenticatedTenant{}, &authErr{http.StatusInternalServerError, "Internal error"}
	}
	if !security.VerifySecret(rawSecret, cachedHash) {
		return AuthenticatedTenant{}, &authErr{http.StatusForbidden, "Invalid credentials for this tenant"}
	}

	// Populate session cache
	rdb.Set(ctx, sessionKey, "1", time.Duration(cfg.AuthSessionTTL)*time.Second) //nolint:errcheck
	return AuthenticatedTenant{TenantID: tenantID}, nil
}
