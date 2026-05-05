package security_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/easyhooks/easyhooks/internal/security"
)

func TestGenerateSecretKey(t *testing.T) {
	key, err := security.GenerateSecretKey(32)
	require.NoError(t, err)
	assert.NotEmpty(t, key)
	assert.False(t, strings.Contains(key, "="), "should be unpadded base64url")

	key2, _ := security.GenerateSecretKey(32)
	assert.NotEqual(t, key, key2, "keys should be random")
}

func TestHashAndVerifySecret(t *testing.T) {
	raw := "my-super-secret"
	hash, err := security.HashSecret(raw)
	require.NoError(t, err)
	assert.True(t, security.VerifySecret(raw, hash))
	assert.False(t, security.VerifySecret("wrong-secret", hash))
}

func buildHMACSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + fmt.Sprintf("%x", mac.Sum(nil))
}

func TestVerifyHMACSignature(t *testing.T) {
	body := []byte(`{"event":"push"}`)
	secret := "webhook-secret-key"

	tests := []struct {
		name      string
		signature string
		valid     bool
	}{
		{"valid signature", buildHMACSignature(secret, body), true},
		{"wrong secret", buildHMACSignature("wrong-secret", body), false},
		{"missing sha256= prefix", "deadbeef", false},
		{"empty signature", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := security.VerifyHMACSignature(secret, body, tt.signature)
			assert.Equal(t, tt.valid, result)
		})
	}
}

func TestSignedToken_CreateAndVerify(t *testing.T) {
	tenantID := uuid.New()
	appKey := "test-secret-key"

	token := security.CreateSignedToken(tenantID, 300, appKey)
	assert.NotEmpty(t, token)
	assert.True(t, strings.Contains(token, "."), "token should be body.sig format")

	gotID, err := security.VerifySignedToken(token, appKey)
	require.NoError(t, err)
	assert.Equal(t, tenantID, gotID)
}

func TestSignedToken_WrongKey(t *testing.T) {
	tenantID := uuid.New()
	token := security.CreateSignedToken(tenantID, 300, "correct-key")
	_, err := security.VerifySignedToken(token, "wrong-key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bad signature")
}

func TestSignedToken_Expired(t *testing.T) {
	tenantID := uuid.New()
	// TTL of -1 creates an already-expired token
	token := security.CreateSignedToken(tenantID, -1, "key")
	time.Sleep(10 * time.Millisecond)
	_, err := security.VerifySignedToken(token, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestSignedToken_Malformed(t *testing.T) {
	_, err := security.VerifySignedToken("notavalidtoken", "key")
	assert.Error(t, err)

	_, err = security.VerifySignedToken("", "key")
	assert.Error(t, err)
}
