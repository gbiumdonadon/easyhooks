package config

import (
	"github.com/caarlos0/env/v11"
)

// Config holds every runtime setting parsed from the process environment.
// Defaults are tuned for local development; overriding via env vars is the
// expected production workflow.
type Config struct {
	// --- Redis (sole datastore) ---
	RedisURL      string `env:"REDIS_URL" envDefault:"redis://localhost:6379/0"`
	RedisPoolSize int    `env:"REDIS_POOL_SIZE" envDefault:"100"`

	// --- Bootstrap / security ---
	AdminSeedToken string `env:"ADMIN_SEED_TOKEN,required"`
	SecretKeyBytes int    `env:"SECRET_KEY_BYTES" envDefault:"32"`
	AppSecretKey   string `env:"APP_SECRET_KEY,required"`
	WSTokenTTL     int    `env:"WS_TOKEN_TTL_SECONDS" envDefault:"300"`
	AuthSessionTTL int    `env:"AUTH_SESSION_TTL_SECONDS" envDefault:"300"`

	// --- Event work-queue (Redis Streams) ---
	EventStreamKey string `env:"EVENT_STREAM_KEY" envDefault:"events:in"`
	DLQStreamKey   string `env:"DLQ_STREAM_KEY" envDefault:"events:failed"`
	ConsumerGroup  string `env:"CONSUMER_GROUP" envDefault:"webhook-workers"`
	StreamBlockMs  int    `env:"STREAM_BLOCK_MS" envDefault:"5000"`
	StreamCount    int64  `env:"STREAM_COUNT" envDefault:"32"`

	// --- Worker retry / idempotency ---
	WorkerMaxRetries    int `env:"WORKER_MAX_RETRIES" envDefault:"3"`
	WorkerBackoffBaseMs int `env:"WORKER_BACKOFF_BASE_MS" envDefault:"100"`
	IdempotencyTTL      int `env:"IDEMPOTENCY_TTL_SECONDS" envDefault:"86400"`

	// --- Per-tenant fan-out streams (consumed by WS handler) ---
	TenantEventsStreamPrefix string `env:"TENANT_EVENTS_STREAM_PREFIX" envDefault:"stream:tenant:"`
	StreamMaxLen             int    `env:"STREAM_MAX_LEN" envDefault:"1000"`
	StreamHistoryCount       int    `env:"STREAM_HISTORY_COUNT" envDefault:"50"`
	WSUseFanout              bool   `env:"WS_USE_FANOUT" envDefault:"true"`

	CORSOrigins string `env:"CORS_ORIGINS" envDefault:"http://localhost:3001,http://localhost:3000"`

	// --- Observability ---
	OTELEndpoint      string  `env:"OTEL_EXPORTER_OTLP_ENDPOINT" envDefault:"http://jaeger:4317"`
	OTELServiceName   string  `env:"OTEL_SERVICE_NAME" envDefault:"easyhooks"`
	MetricsEnabled    bool    `env:"METRICS_ENABLED" envDefault:"true"`
	TracingEnabled    bool    `env:"TRACING_ENABLED" envDefault:"true"`
	TracingSampleRate float64 `env:"TRACING_SAMPLE_RATE" envDefault:"1.0"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
