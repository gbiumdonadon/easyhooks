from pydantic_settings import BaseSettings


class Settings(BaseSettings):
    DATABASE_URL: str
    REDIS_URL: str = "redis://localhost:6379/0"
    ADMIN_SEED_TOKEN: str
    SECRET_KEY_BYTES: int = 32
    KAFKA_BOOTSTRAP_SERVERS: str = "localhost:9092"
    KAFKA_WEBHOOK_TOPIC: str = "webhooks.inbound"
    KAFKA_DLQ_TOPIC: str = "webhooks.dlq"
    KAFKA_CONSUMER_GROUP: str = "webhook-workers"
    WORKER_MAX_RETRIES: int = 3
    WORKER_BACKOFF_BASE_MS: int = 100
    IDEMPOTENCY_TTL_SECONDS: int = 86400
    APP_SECRET_KEY: str
    WS_TOKEN_TTL_SECONDS: int = 300
    TENANT_EVENTS_CHANNEL_PREFIX: str = "tenant_events:"
    TENANT_EVENTS_STREAM_PREFIX: str = "stream:tenant:"
    STREAM_MAX_LEN: int = 1000
    STREAM_HISTORY_COUNT: int = 50
    CORS_ORIGINS: str = "http://localhost:3001,http://localhost:3000"
    OTEL_EXPORTER_OTLP_ENDPOINT: str = "http://jaeger:4317"
    OTEL_SERVICE_NAME: str = "easyhooks"
    METRICS_ENABLED: bool = True
    TRACING_ENABLED: bool = True
    TRACING_SAMPLE_RATE: float = 1.0

    model_config = {"env_file": ".env", "extra": "ignore"}


settings = Settings()
