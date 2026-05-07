// Package config exposes runtime settings parsed from the process environment.
//
// Configuration is layered in three steps to give users predictable defaults
// while keeping every knob explicitly overridable:
//
//  1. Built-in defaults via `envDefault` tags (for fields that do not depend on
//     the chosen profile, e.g. retries, TTLs, OTEL endpoint).
//  2. Profile defaults (small / medium / large) applied by applyProfileDefaults
//     to fields that scale with the available memory budget.
//  3. Explicit env vars set by the user, which always win.
//
// The "custom" profile skips step 2 entirely; advanced operators are expected
// to set every memory-related env var themselves.
package config

import (
	"fmt"
	"log/slog"

	"github.com/caarlos0/env/v11"
)

// Profile names exposed via EASYHOOKS_PROFILE.
const (
	ProfileSmall  = "small"
	ProfileMedium = "medium"
	ProfileLarge  = "large"
	ProfileCustom = "custom"
)

// Config holds every runtime setting parsed from the process environment.
// Fields that scale with the chosen profile have no `envDefault` so that the
// zero value can be detected after parsing and replaced by the profile tier.
type Config struct {
	// --- Profile (drives the memory-related defaults below) ---
	Profile string `env:"EASYHOOKS_PROFILE" envDefault:"small"`

	// --- Redis (sole datastore) ---
	RedisURL      string `env:"REDIS_URL" envDefault:"redis://localhost:6379/0"`
	RedisPoolSize int    `env:"REDIS_POOL_SIZE"` // profile-driven

	// --- Bootstrap / security ---
	AdminSeedToken string `env:"ADMIN_SEED_TOKEN,required"`
	SecretKeyBytes int    `env:"SECRET_KEY_BYTES" envDefault:"32"`
	AppSecretKey   string `env:"APP_SECRET_KEY,required"`
	WSTokenTTL     int    `env:"WS_TOKEN_TTL_SECONDS" envDefault:"300"`
	AuthSessionTTL int    `env:"AUTH_SESSION_TTL_SECONDS" envDefault:"300"`

	// --- Event work-queue (Redis Streams) ---
	EventStreamKey  string `env:"EVENT_STREAM_KEY" envDefault:"events:in"`
	DLQStreamKey    string `env:"DLQ_STREAM_KEY" envDefault:"events:failed"`
	DLQStreamMaxLen int    `env:"DLQ_STREAM_MAX_LEN" envDefault:"10000"`
	ConsumerGroup   string `env:"CONSUMER_GROUP" envDefault:"webhook-workers"`
	StreamBlockMs   int    `env:"STREAM_BLOCK_MS" envDefault:"5000"`
	StreamCount     int64  `env:"STREAM_COUNT" envDefault:"32"`

	// --- Worker retry / idempotency ---
	WorkerMaxRetries    int `env:"WORKER_MAX_RETRIES" envDefault:"3"`
	WorkerBackoffBaseMs int `env:"WORKER_BACKOFF_BASE_MS" envDefault:"100"`
	IdempotencyTTL      int `env:"IDEMPOTENCY_TTL_SECONDS" envDefault:"86400"`

	// --- Per-tenant fan-out streams (consumed by WS handler) ---
	TenantEventsStreamPrefix string `env:"TENANT_EVENTS_STREAM_PREFIX" envDefault:"stream:tenant:"`
	StreamMaxLen             int    `env:"STREAM_MAX_LEN"`         // profile-driven
	StreamHistoryCount       int    `env:"STREAM_HISTORY_COUNT" envDefault:"50"`
	WSUseFanout              bool   `env:"WS_USE_FANOUT" envDefault:"true"`
	WSFanoutBufferSize       int    `env:"WS_FANOUT_BUFFER_SIZE"` // profile-driven

	// --- Memory / load shedding ---
	// GoMemLimitBytes is always derived from the profile; users override it via
	// the native GOMEMLIMIT env var (handled by the Go runtime, see
	// runtime/debug.SetMemoryLimit).
	//
	// IngestMaxQueueDepth is the high watermark on the consumer-group backlog
	// (lag + pending) of the ingestion stream, NOT on XLEN. Above it the API
	// rejects POST /v1/webhooks/* with 429 + Retry-After.
	//
	// IngestStreamMaxLen is purely a memory guard-rail on the physical size of
	// the events:in stream (XADD MAXLEN ~ N). It is decoupled from the load
	// shedder: backlog goes to zero whenever the worker keeps up, regardless
	// of how much history Redis still holds in the stream.
	GoMemLimitBytes       int64
	IngestMaxQueueDepth   int `env:"INGEST_MAX_QUEUE_DEPTH"` // profile-driven
	IngestStreamMaxLen    int `env:"INGEST_STREAM_MAX_LEN"`  // 0 → 2 * IngestMaxQueueDepth
	QueueDepthPollMs      int `env:"QUEUE_DEPTH_POLL_MS" envDefault:"1000"`
	QueueDepthLowWaterPct int `env:"QUEUE_DEPTH_LOW_WATER_PCT" envDefault:"80"`

	CORSOrigins string `env:"CORS_ORIGINS" envDefault:"http://localhost:3001,http://localhost:3000"`

	// --- Observability ---
	OTELEndpoint      string  `env:"OTEL_EXPORTER_OTLP_ENDPOINT" envDefault:"http://jaeger:4317"`
	OTELServiceName   string  `env:"OTEL_SERVICE_NAME" envDefault:"easyhooks"`
	MetricsEnabled    bool    `env:"METRICS_ENABLED" envDefault:"true"`
	TracingEnabled    bool    `env:"TRACING_ENABLED" envDefault:"true"`
	TracingSampleRate float64 `env:"TRACING_SAMPLE_RATE" envDefault:"1.0"`
}

// profileDefaults captures the memory-scaled tier values applied to any
// matching Config field that came back zeroed from env.Parse.
type profileDefaults struct {
	GoMemLimitBytes     int64
	StreamMaxLen        int
	RedisPoolSize       int
	WSFanoutBufferSize  int
	IngestMaxQueueDepth int
}

const mib = 1024 * 1024

// profilesTable lists the recommended values for each named profile. Keep the
// table in sync with the "Capacity planning" section of the README.
var profilesTable = map[string]profileDefaults{
	ProfileSmall: {
		GoMemLimitBytes:     200 * mib,
		StreamMaxLen:        1000,
		RedisPoolSize:       50,
		WSFanoutBufferSize:  100,
		IngestMaxQueueDepth: 5000,
	},
	ProfileMedium: {
		GoMemLimitBytes:     450 * mib,
		StreamMaxLen:        5000,
		RedisPoolSize:       100,
		WSFanoutBufferSize:  256,
		IngestMaxQueueDepth: 25000,
	},
	ProfileLarge: {
		GoMemLimitBytes:     900 * mib,
		StreamMaxLen:        10000,
		RedisPoolSize:       200,
		WSFanoutBufferSize:  512,
		IngestMaxQueueDepth: 50000,
	},
}

// Load reads every supported env var, validates the chosen profile and applies
// the matching tier defaults to fields that came back zeroed.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	if err := applyProfileDefaults(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// applyProfileDefaults validates cfg.Profile and fills any zero-valued
// memory-related field with the matching tier value. Setting an env var to
// `0` is intentionally treated as "not set" so that the profile still wins —
// this is documented in the .env.example file.
func applyProfileDefaults(cfg *Config) error {
	switch cfg.Profile {
	case ProfileSmall, ProfileMedium, ProfileLarge:
		t := profilesTable[cfg.Profile]
		if cfg.GoMemLimitBytes == 0 {
			cfg.GoMemLimitBytes = t.GoMemLimitBytes
		}
		if cfg.StreamMaxLen == 0 {
			cfg.StreamMaxLen = t.StreamMaxLen
		}
		if cfg.RedisPoolSize == 0 {
			cfg.RedisPoolSize = t.RedisPoolSize
		}
		if cfg.WSFanoutBufferSize == 0 {
			cfg.WSFanoutBufferSize = t.WSFanoutBufferSize
		}
		if cfg.IngestMaxQueueDepth == 0 {
			cfg.IngestMaxQueueDepth = t.IngestMaxQueueDepth
		}
		if cfg.IngestStreamMaxLen == 0 {
			cfg.IngestStreamMaxLen = cfg.IngestMaxQueueDepth * 2
		}
	case ProfileCustom:
		// "custom" is opt-in for advanced operators: nothing is auto-filled,
		// but we surface a warning if essential fields are missing so a typo
		// does not silently disable load shedding.
		if cfg.GoMemLimitBytes == 0 || cfg.StreamMaxLen == 0 || cfg.RedisPoolSize == 0 ||
			cfg.WSFanoutBufferSize == 0 || cfg.IngestMaxQueueDepth == 0 {
			slog.Warn("EASYHOOKS_PROFILE=custom but one or more memory-related env vars are unset",
				"gomemlimit_bytes", cfg.GoMemLimitBytes,
				"stream_max_len", cfg.StreamMaxLen,
				"redis_pool_size", cfg.RedisPoolSize,
				"ws_fanout_buffer_size", cfg.WSFanoutBufferSize,
				"ingest_max_queue_depth", cfg.IngestMaxQueueDepth,
			)
		}
		// Even in custom mode we derive the physical stream cap from the load
		// shedder threshold when the operator did not set it explicitly. This
		// keeps INGEST_STREAM_MAX_LEN safe-by-default without forcing every
		// custom deployment to think about it.
		if cfg.IngestStreamMaxLen == 0 && cfg.IngestMaxQueueDepth > 0 {
			cfg.IngestStreamMaxLen = cfg.IngestMaxQueueDepth * 2
		}
	default:
		return fmt.Errorf("invalid EASYHOOKS_PROFILE %q (expected small, medium, large or custom)", cfg.Profile)
	}

	if cfg.QueueDepthLowWaterPct < 0 || cfg.QueueDepthLowWaterPct > 100 {
		return fmt.Errorf("invalid QUEUE_DEPTH_LOW_WATER_PCT %d (expected 0..100)", cfg.QueueDepthLowWaterPct)
	}
	return nil
}
