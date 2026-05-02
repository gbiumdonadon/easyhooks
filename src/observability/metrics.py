"""Prometheus metrics for EasyHooks platform.

This module defines custom metrics for monitoring:
- Webhook ingestion and processing
- Kafka consumer lag (via kafka-exporter)
- Retry/DLQ rates
- Idempotency checks
- WebSocket connections
- Redis operations
"""
from prometheus_client import Counter, Gauge, Histogram, generate_latest, CONTENT_TYPE_LATEST

webhook_requests_total = Counter(
    'webhook_requests_total',
    'Total number of webhook requests received',
    ['tenant_id', 'status']
)

webhook_processing_duration_seconds = Histogram(
    'webhook_processing_duration_seconds',
    'Time spent processing webhook events in worker',
    ['tenant_id'],
    buckets=(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1.0, 2.5, 5.0, 7.5, 10.0)
)

webhook_retries_total = Counter(
    'webhook_retries_total',
    'Total number of webhook processing retries',
    ['tenant_id', 'attempt']
)

webhook_dlq_total = Counter(
    'webhook_dlq_total',
    'Total number of events sent to Dead Letter Queue',
    ['tenant_id', 'error_type']
)

idempotency_duplicates_total = Counter(
    'idempotency_duplicates_total',
    'Total number of duplicate events detected by idempotency check',
    ['tenant_id']
)

websocket_connections_active = Gauge(
    'websocket_connections_active',
    'Number of active WebSocket connections',
    ['tenant_id']
)

websocket_messages_sent_total = Counter(
    'websocket_messages_sent_total',
    'Total number of messages sent via WebSocket',
    ['tenant_id']
)

redis_operations_total = Counter(
    'redis_operations_total',
    'Total number of Redis operations',
    ['operation', 'status']
)

kafka_produce_total = Counter(
    'kafka_produce_total',
    'Total number of messages produced to Kafka',
    ['topic', 'status']
)

kafka_consume_total = Counter(
    'kafka_consume_total',
    'Total number of messages consumed from Kafka',
    ['topic', 'consumer_group']
)

http_request_duration_seconds = Histogram(
    'http_request_duration_seconds',
    'HTTP request duration in seconds',
    ['method', 'endpoint', 'status_code'],
    buckets=(0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0)
)

http_requests_total = Counter(
    'http_requests_total',
    'Total number of HTTP requests',
    ['method', 'endpoint', 'status_code']
)

# Load Testing Specific Metrics
loadtest_requests_total = Counter(
    'loadtest_requests_total',
    'Total requests during load test (tagged for analysis)',
    ['tenant_id', 'endpoint', 'test_scenario']
)

loadtest_errors_total = Counter(
    'loadtest_errors_total',
    'Total errors during load test',
    ['tenant_id', 'endpoint', 'error_type', 'test_scenario']
)

websocket_e2e_latency_seconds = Histogram(
    'websocket_e2e_latency_seconds',
    'End-to-end latency from webhook POST to WebSocket delivery',
    ['tenant_id'],
    buckets=(0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0)
)

websocket_connection_duration_seconds = Histogram(
    'websocket_connection_duration_seconds',
    'Duration of WebSocket connections',
    ['tenant_id'],
    buckets=(1, 5, 10, 30, 60, 300, 600, 1800, 3600)
)

tenant_isolation_latency_seconds = Histogram(
    'tenant_isolation_latency_seconds',
    'Per-tenant latency for isolation testing',
    ['tenant_id', 'tenant_tier'],
    buckets=(0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0)
)


def get_metrics() -> tuple[bytes, str]:
    """Generate Prometheus metrics in text format.
    
    Returns:
        Tuple of (metrics_data, content_type)
    """
    return generate_latest(), CONTENT_TYPE_LATEST
