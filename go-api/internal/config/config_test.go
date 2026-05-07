package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setRequiredSecrets seeds the env vars marked as `required` so that env.Parse
// does not fail. Tests that need additional vars set them on top of these.
func setRequiredSecrets(t *testing.T) {
	t.Setenv("ADMIN_SEED_TOKEN", "test-admin-token")
	t.Setenv("APP_SECRET_KEY", "test-app-key")
}

func TestLoad_DefaultsToSmallProfile(t *testing.T) {
	setRequiredSecrets(t)

	cfg, err := Load()
	require.NoError(t, err)

	expected := profilesTable[ProfileSmall]
	assert.Equal(t, ProfileSmall, cfg.Profile)
	assert.Equal(t, expected.GoMemLimitBytes, cfg.GoMemLimitBytes)
	assert.Equal(t, expected.StreamMaxLen, cfg.StreamMaxLen)
	assert.Equal(t, expected.RedisPoolSize, cfg.RedisPoolSize)
	assert.Equal(t, expected.WSFanoutBufferSize, cfg.WSFanoutBufferSize)
	assert.Equal(t, expected.IngestMaxQueueDepth, cfg.IngestMaxQueueDepth)
	assert.Equal(t, expected.IngestMaxQueueDepth*2, cfg.IngestStreamMaxLen,
		"IngestStreamMaxLen should default to 2x IngestMaxQueueDepth")
}

func TestLoad_MediumProfileAppliesTier(t *testing.T) {
	setRequiredSecrets(t)
	t.Setenv("EASYHOOKS_PROFILE", "medium")

	cfg, err := Load()
	require.NoError(t, err)

	expected := profilesTable[ProfileMedium]
	assert.Equal(t, expected.GoMemLimitBytes, cfg.GoMemLimitBytes)
	assert.Equal(t, expected.StreamMaxLen, cfg.StreamMaxLen)
	assert.Equal(t, expected.RedisPoolSize, cfg.RedisPoolSize)
	assert.Equal(t, expected.WSFanoutBufferSize, cfg.WSFanoutBufferSize)
	assert.Equal(t, expected.IngestMaxQueueDepth, cfg.IngestMaxQueueDepth)
}

func TestLoad_LargeProfileAppliesTier(t *testing.T) {
	setRequiredSecrets(t)
	t.Setenv("EASYHOOKS_PROFILE", "large")

	cfg, err := Load()
	require.NoError(t, err)

	expected := profilesTable[ProfileLarge]
	assert.Equal(t, expected.GoMemLimitBytes, cfg.GoMemLimitBytes)
	assert.Equal(t, expected.StreamMaxLen, cfg.StreamMaxLen)
	assert.Equal(t, expected.RedisPoolSize, cfg.RedisPoolSize)
	assert.Equal(t, expected.WSFanoutBufferSize, cfg.WSFanoutBufferSize)
	assert.Equal(t, expected.IngestMaxQueueDepth, cfg.IngestMaxQueueDepth)
}

func TestLoad_ExplicitEnvOverridesProfile(t *testing.T) {
	setRequiredSecrets(t)
	t.Setenv("EASYHOOKS_PROFILE", "large")
	t.Setenv("STREAM_MAX_LEN", "20000")
	t.Setenv("REDIS_POOL_SIZE", "150")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 20000, cfg.StreamMaxLen, "explicit STREAM_MAX_LEN should win over profile")
	assert.Equal(t, 150, cfg.RedisPoolSize, "explicit REDIS_POOL_SIZE should win over profile")
	// Other fields still come from the profile.
	assert.Equal(t, profilesTable[ProfileLarge].WSFanoutBufferSize, cfg.WSFanoutBufferSize)
	assert.Equal(t, profilesTable[ProfileLarge].IngestMaxQueueDepth, cfg.IngestMaxQueueDepth)
}

func TestLoad_CustomProfileSkipsTier(t *testing.T) {
	setRequiredSecrets(t)
	t.Setenv("EASYHOOKS_PROFILE", "custom")
	t.Setenv("STREAM_MAX_LEN", "777")
	t.Setenv("REDIS_POOL_SIZE", "33")
	t.Setenv("WS_FANOUT_BUFFER_SIZE", "64")
	t.Setenv("INGEST_MAX_QUEUE_DEPTH", "1234")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, ProfileCustom, cfg.Profile)
	assert.Equal(t, 777, cfg.StreamMaxLen)
	assert.Equal(t, 33, cfg.RedisPoolSize)
	assert.Equal(t, 64, cfg.WSFanoutBufferSize)
	assert.Equal(t, 1234, cfg.IngestMaxQueueDepth)
	// GoMemLimitBytes was not provided and custom does not auto-fill: caller
	// is expected to handle this (the WARN log is emitted).
	assert.Equal(t, int64(0), cfg.GoMemLimitBytes)
}

func TestLoad_InvalidProfileFails(t *testing.T) {
	setRequiredSecrets(t)
	t.Setenv("EASYHOOKS_PROFILE", "huge")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid EASYHOOKS_PROFILE")
}

func TestLoad_InvalidLowWaterPctFails(t *testing.T) {
	setRequiredSecrets(t)
	t.Setenv("QUEUE_DEPTH_LOW_WATER_PCT", "150")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "QUEUE_DEPTH_LOW_WATER_PCT")
}

func TestLoad_IngestStreamMaxLenExplicitOverride(t *testing.T) {
	setRequiredSecrets(t)
	t.Setenv("INGEST_STREAM_MAX_LEN", "12345")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 12345, cfg.IngestStreamMaxLen,
		"explicit INGEST_STREAM_MAX_LEN should win over the 2x default")
}

func TestLoad_IngestStreamMaxLenBelowQueueDepthIsAllowed(t *testing.T) {
	setRequiredSecrets(t)
	// The load shedder no longer reads XLEN, so INGEST_STREAM_MAX_LEN is
	// purely a memory guard-rail — it can sit below INGEST_MAX_QUEUE_DEPTH
	// without breaking the shedder. The ratio is just a sizing decision.
	t.Setenv("INGEST_STREAM_MAX_LEN", "100")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 100, cfg.IngestStreamMaxLen)
	assert.Equal(t, profilesTable[ProfileSmall].IngestMaxQueueDepth, cfg.IngestMaxQueueDepth)
}

func TestLoad_CustomProfileDerivesIngestStreamMaxLen(t *testing.T) {
	setRequiredSecrets(t)
	t.Setenv("EASYHOOKS_PROFILE", "custom")
	t.Setenv("INGEST_MAX_QUEUE_DEPTH", "1000")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 2000, cfg.IngestStreamMaxLen,
		"custom profile should still derive IngestStreamMaxLen as 2x IngestMaxQueueDepth when unset")
}
