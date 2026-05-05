package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// GenerateSecretKey returns a cryptographically secure random URL-safe base64 string.
func GenerateSecretKey(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashSecret hashes a plaintext secret using bcrypt with cost 12.
func HashSecret(raw string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(raw), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt hash: %w", err)
	}
	return string(hashed), nil
}

// VerifySecret compares a plaintext secret against its bcrypt hash.
func VerifySecret(raw, hashed string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(raw)) == nil
}

// VerifyHMACSignature validates the "sha256=<hex>" webhook signature header.
// Compatible with GitHub webhook format.
func VerifyHMACSignature(secretRaw string, body []byte, signature string) bool {
	sig := strings.TrimPrefix(signature, "sha256=")
	if sig == signature {
		return false // prefix missing
	}
	mac := hmac.New(sha256.New, []byte(secretRaw))
	mac.Write(body)
	expected := fmt.Sprintf("%x", mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// wsPayload is the signed WebSocket token payload.
type wsPayload struct {
	Sub string `json:"sub"`
	Exp int64  `json:"exp"`
}

func b64Encode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func b64Decode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func signBody(body, appSecretKey string) string {
	mac := hmac.New(sha256.New, []byte(appSecretKey))
	mac.Write([]byte(body))
	return b64Encode(mac.Sum(nil))
}

// CreateSignedToken creates a signed WebSocket token.
// Format: base64url(json_payload).base64url(hmac_sha256)
// CreateSignedToken builds body.signature HMAC token for WebSocket auth.
func CreateSignedToken(tenantID uuid.UUID, ttlSeconds int, appSecretKey string) string {
	payload := wsPayload{
		Sub: tenantID.String(),
		Exp: time.Now().Unix() + int64(ttlSeconds),
	}
	// json.Marshal produces compact JSON (no spaces between fields).
	jsonBytes, _ := json.Marshal(payload)
	body := b64Encode(jsonBytes)
	sig := signBody(body, appSecretKey)
	return body + "." + sig
}

// InvalidTokenError is returned by VerifySignedToken on any validation failure.
type InvalidTokenError struct {
	Reason string
}

func (e *InvalidTokenError) Error() string { return "invalid token: " + e.Reason }

// VerifySignedToken validates a signed WebSocket token and returns the tenant UUID.
func VerifySignedToken(token, appSecretKey string) (uuid.UUID, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return uuid.Nil, &InvalidTokenError{"malformed token"}
	}
	body, sig := parts[0], parts[1]

	expected := signBody(body, appSecretKey)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return uuid.Nil, &InvalidTokenError{"bad signature"}
	}

	rawPayload, err := b64Decode(body)
	if err != nil {
		return uuid.Nil, &InvalidTokenError{"malformed payload"}
	}
	var p wsPayload
	if err := json.Unmarshal(rawPayload, &p); err != nil {
		return uuid.Nil, &InvalidTokenError{"malformed payload"}
	}
	if time.Now().Unix() > p.Exp {
		return uuid.Nil, &InvalidTokenError{"expired"}
	}
	id, err := uuid.Parse(p.Sub)
	if err != nil {
		return uuid.Nil, &InvalidTokenError{"invalid sub"}
	}
	return id, nil
}
