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
	"github.com/easyhooks/easyhooks/internal/security"
)

func newTestConfig() *config.Config {
	return &config.Config{
		DatabaseURL:              "postgres://test:test@localhost/test",
		AdminSeedToken:           "test-admin",
		AppSecretKey:             "test-app-key",
		KafkaBootstrapServers:    "localhost:9092",
		KafkaWebhookTopic:        "webhooks.inbound",
		KafkaDLQTopic:            "webhooks.dlq",
		KafkaConsumerGroup:       "test-workers",
		WorkerMaxRetries:         3,
		WorkerBackoffBaseMs:      100,
		IdempotencyTTL:           86400,
		TenantEventsStreamPrefix: "stream:tenant:",
		StreamMaxLen:             1000,
		StreamHistoryCount:       50,
		WSTokenTTL:               300,
		AuthSessionTTL:           300,
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

// seedTenantInRedis sets up Redis keys for a tenant as CreateTenant would.
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
