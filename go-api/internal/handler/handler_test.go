package handler_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/easyhooks/easyhooks/internal/config"
	"github.com/easyhooks/easyhooks/internal/handler"
	appmiddleware "github.com/easyhooks/easyhooks/internal/middleware"
	"github.com/easyhooks/easyhooks/internal/redisstore"
	"github.com/easyhooks/easyhooks/internal/security"
	"github.com/easyhooks/easyhooks/internal/streams"
)

func newTestConfig() *config.Config {
	return &config.Config{
		AdminSeedToken:           "test-admin",
		AppSecretKey:             "test-app-key",
		EventStreamKey:           "events:in",
		DLQStreamKey:             "events:failed",
		ConsumerGroup:            "test-workers",
		StreamBlockMs:            100,
		StreamCount:              10,
		WorkerMaxRetries:         3,
		WorkerBackoffBaseMs:      100,
		IdempotencyTTL:           86400,
		TenantEventsStreamPrefix: "stream:tenant:",
		StreamMaxLen:             1000,
		StreamHistoryCount:       50,
		WSTokenTTL:               300,
		AuthSessionTTL:           300,
		SecretKeyBytes:           32,
	}
}

func newMiniredisClient(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return mr, client
}

// seedTenantInRedis sets up Redis keys for a tenant as service.CreateTenant would.
func seedTenantInRedis(t *testing.T, rdb *redis.Client, tenantID uuid.UUID, rawSecret string) {
	t.Helper()
	hash, err := security.HashSecret(rawSecret)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, rdb.Set(ctx, "tenant_auth:"+tenantID.String(), hash, 0).Err())
	require.NoError(t, rdb.Set(ctx, "tenant_hmac_key:"+tenantID.String(), rawSecret, 0).Err())
}

// --- Health ---

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.Health(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "healthy", resp["status"])
	assert.Equal(t, "easyhooks", resp["service"])
}

// --- Token issuance ---

func TestIssueToken_ValidTenant(t *testing.T) {
	cfg := newTestConfig()
	_, rdb := newMiniredisClient(t)
	tenantID := uuid.New()
	rawSecret := "valid-secret"
	seedTenantInRedis(t, rdb, tenantID, rawSecret)

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.TenantAuth(rdb, cfg))
		r.Post("/v1/tokens/{tenant_id}", handler.IssueToken(cfg))
	})

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/tokens/"+tenantID.String(), body)
	req.Header.Set("Authorization", "Bearer "+rawSecret)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.NotEmpty(t, resp["token"])
	assert.Equal(t, float64(300), resp["expires_in"])
}

func TestIssueToken_InvalidCredentials(t *testing.T) {
	cfg := newTestConfig()
	_, rdb := newMiniredisClient(t)
	tenantID := uuid.New()
	seedTenantInRedis(t, rdb, tenantID, "correct-secret")

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.TenantAuth(rdb, cfg))
		r.Post("/v1/tokens/{tenant_id}", handler.IssueToken(cfg))
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/tokens/"+tenantID.String(), bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer wrong-secret")

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestIssueToken_MissingAuth(t *testing.T) {
	cfg := newTestConfig()
	_, rdb := newMiniredisClient(t)
	tenantID := uuid.New()

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.TenantAuth(rdb, cfg))
		r.Post("/v1/tokens/{tenant_id}", handler.IssueToken(cfg))
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/tokens/"+tenantID.String(), nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

// --- Auth middleware ---

func TestTenantAuth_HMACSignature(t *testing.T) {
	cfg := newTestConfig()
	_, rdb := newMiniredisClient(t)
	tenantID := uuid.New()
	rawSecret := "hmac-secret"
	seedTenantInRedis(t, rdb, tenantID, rawSecret)

	reached := false
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.TenantAuth(rdb, cfg))
		r.Post("/v1/tokens/{tenant_id}", func(w http.ResponseWriter, r *http.Request) {
			reached = true
			w.WriteHeader(http.StatusOK)
		})
	})

	body := []byte(`{"payload":"data"}`)
	mac := hmac.New(sha256.New, []byte(rawSecret))
	mac.Write(body)
	sig := "sha256=" + fmt.Sprintf("%x", mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/v1/tokens/"+tenantID.String(), bytes.NewReader(body))
	req.Header.Set("X-Webhook-Signature", sig)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, reached)
}

// --- Admin auth (Redis-backed) ---

func TestAdminAuth_AcceptsSeededToken(t *testing.T) {
	_, rdb := newMiniredisClient(t)
	require.NoError(t, redisstore.SeedSuperAdmin(context.Background(), rdb, "super-secret"))

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.AdminAuth(rdb))
		r.Get("/admin/ping", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
	req.Header.Set("Authorization", "Bearer super-secret")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestAdminAuth_RejectsWrongToken(t *testing.T) {
	_, rdb := newMiniredisClient(t)
	require.NoError(t, redisstore.SeedSuperAdmin(context.Background(), rdb, "super-secret"))

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.AdminAuth(rdb))
		r.Get("/admin/ping", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestAdminAuth_NotSeeded(t *testing.T) {
	_, rdb := newMiniredisClient(t)

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.AdminAuth(rdb))
		r.Get("/admin/ping", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
}

// --- CreateTenant ---

func TestCreateTenant_StoresKeysAndReturnsCredentials(t *testing.T) {
	cfg := newTestConfig()
	_, rdb := newMiniredisClient(t)
	require.NoError(t, redisstore.SeedSuperAdmin(context.Background(), rdb, "admin-token"))

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.AdminAuth(rdb))
		r.Post("/admin/tenants", handler.CreateTenant(rdb, cfg))
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/tenants", bytes.NewBufferString(`{"name":"acme"}`))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	tenantID := resp["tenant_id"]
	require.NotEmpty(t, tenantID)
	require.NotEmpty(t, resp["secret_key"])
	assert.Equal(t, "acme", resp["name"])

	ctx := context.Background()
	hash, err := rdb.Get(ctx, "tenant_auth:"+tenantID).Result()
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	raw, err := rdb.Get(ctx, "tenant_hmac_key:"+tenantID).Result()
	require.NoError(t, err)
	assert.Equal(t, resp["secret_key"], raw)
}

// --- IngestWebhook ---

func TestIngestWebhook_PublishesToEventStream(t *testing.T) {
	cfg := newTestConfig()
	_, rdb := newMiniredisClient(t)
	tenantID := uuid.New()
	rawSecret := "tenant-secret"
	seedTenantInRedis(t, rdb, tenantID, rawSecret)

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.TenantAuth(rdb, cfg))
		r.Post("/v1/webhooks/{tenant_id}", handler.IngestWebhook(rdb, cfg))
	})

	body := []byte(`{"event":"x"}`)
	mac := hmac.New(sha256.New, []byte(rawSecret))
	mac.Write(body)
	sig := "sha256=" + fmt.Sprintf("%x", mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/"+tenantID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Event-Id", "evt-1")
	req.Header.Set("X-Webhook-Signature", sig)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusAccepted, rr.Code)

	entries, err := rdb.XRange(context.Background(), cfg.EventStreamKey, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, tenantID.String(), entries[0].Values[streams.FieldTenantID])
	assert.Equal(t, "evt-1", entries[0].Values[streams.FieldEventID])
	assert.Equal(t, string(body), entries[0].Values[streams.FieldPayload])
}
