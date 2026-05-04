package config

import (
	"github.com/caarlos0/env/v11"
)

type Config struct {
	DatabaseURL         string  `env:"DATABASE_URL,required"`
	DatabasePoolSize    int32   `env:"DATABASE_POOL_SIZE" envDefault:"20"`
	DatabaseMaxOverflow int32   `env:"DATABASE_MAX_OVERFLOW" envDefault:"10"`
	RedisURL            string  `env:"REDIS_URL" envDefault:"redis://localhost:6379/0"`
	RedisPoolSize       int     `env:"REDIS_POOL_SIZE" envDefault:"100"`
	AdminSeedToken      string  `env:"ADMIN_SEED_TOKEN,required"`
	SecretKeyBytes      int     `env:"SECRET_KEY_BYTES" envDefault:"32"`

	KafkaBootstrapServers string `env:"KAFKA_BOOTSTRAP_SERVERS" envDefault:"localhost:9092"`
	KafkaWebhookTopic     string `env:"KAFKA_WEBHOOK_TOPIC" envDefault:"webhooks.inbound"`
	KafkaDLQTopic         string `env:"KAFKA_DLQ_TOPIC" envDefault:"webhooks.dlq"`
	KafkaConsumerGroup    string `env:"KAFKA_CONSUMER_GROUP" envDefault:"webhook-workers"`

	WorkerMaxRetries    int `env:"WORKER_MAX_RETRIES" envDefault:"3"`
	WorkerBackoffBaseMs int `env:"WORKER_BACKOFF_BASE_MS" envDefault:"100"`
	IdempotencyTTL      int `env:"IDEMPOTENCY_TTL_SECONDS" envDefault:"86400"`

	AppSecretKey   string `env:"APP_SECRET_KEY,required"`
	WSTokenTTL     int    `env:"WS_TOKEN_TTL_SECONDS" envDefault:"300"`
	AuthSessionTTL int    `env:"AUTH_SESSION_TTL_SECONDS" envDefault:"300"`

	TenantEventsStreamPrefix string `env:"TENANT_EVENTS_STREAM_PREFIX" envDefault:"stream:tenant:"`
	StreamMaxLen             int    `env:"STREAM_MAX_LEN" envDefault:"1000"`
	StreamHistoryCount       int    `env:"STREAM_HISTORY_COUNT" envDefault:"50"`
	WSUseFanout              bool   `env:"WS_USE_FANOUT" envDefault:"true"`

	CORSOrigins string `env:"CORS_ORIGINS" envDefault:"http://localhost:3001,http://localhost:3000"`

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
