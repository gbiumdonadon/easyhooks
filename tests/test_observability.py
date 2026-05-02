"""Tests for observability features (metrics and tracing).

Validates that:
- Metrics endpoint returns Prometheus format
- Metrics are incremented correctly
- Tracing setup works without errors
- Context propagation is configured
"""
import re
import pytest
from httpx import AsyncClient
from src.main import app
from src.observability import metrics, tracing


@pytest.mark.asyncio
async def test_metrics_endpoint_exists():
    """Verify /metrics endpoint is accessible."""
    async with AsyncClient(app=app, base_url="http://test") as client:
        response = await client.get("/metrics")
    
    assert response.status_code == 200
    assert "text/plain" in response.headers["content-type"]


@pytest.mark.asyncio
async def test_metrics_prometheus_format():
    """Verify metrics are in Prometheus text format."""
    async with AsyncClient(app=app, base_url="http://test") as client:
        response = await client.get("/metrics")
    
    content = response.text
    
    assert "# HELP" in content
    assert "# TYPE" in content
    
    assert "webhook_requests_total" in content
    assert "webhook_processing_duration_seconds" in content
    assert "webhook_retries_total" in content
    assert "webhook_dlq_total" in content
    assert "idempotency_duplicates_total" in content
    assert "websocket_connections_active" in content
    assert "redis_operations_total" in content


def test_metrics_counter_increment():
    """Verify metrics counters can be incremented."""
    initial = metrics.webhook_requests_total._metrics.get(
        ("test-tenant", "success"), None
    )
    initial_value = initial._value._value if initial else 0
    
    metrics.webhook_requests_total.labels(
        tenant_id="test-tenant",
        status="success"
    ).inc()
    
    after = metrics.webhook_requests_total._metrics[("test-tenant", "success")]
    after_value = after._value._value
    
    assert after_value == initial_value + 1


def test_metrics_histogram_observe():
    """Verify histogram metrics can record observations."""
    metrics.webhook_processing_duration_seconds.labels(
        tenant_id="test-tenant"
    ).observe(0.123)
    
    metric = metrics.webhook_processing_duration_seconds._metrics[("test-tenant",)]
    
    assert metric._sum._value > 0
    assert metric._count._value > 0


def test_metrics_gauge_set():
    """Verify gauge metrics can be set."""
    metrics.websocket_connections_active.labels(
        tenant_id="test-tenant"
    ).set(42)
    
    metric = metrics.websocket_connections_active._metrics[("test-tenant",)]
    assert metric._value._value == 42
    
    metrics.websocket_connections_active.labels(
        tenant_id="test-tenant"
    ).inc()
    assert metric._value._value == 43
    
    metrics.websocket_connections_active.labels(
        tenant_id="test-tenant"
    ).dec()
    assert metric._value._value == 42


def test_tracing_setup_without_errors():
    """Verify tracing can be configured without errors."""
    try:
        tracing.setup_tracing(
            service_name="test-service",
            otlp_endpoint="http://localhost:4317",
            enabled=True,
            sample_rate=1.0,
        )
    except Exception as exc:
        pytest.fail(f"Tracing setup failed: {exc}")


def test_tracing_disabled_mode():
    """Verify tracing can be disabled."""
    try:
        tracing.setup_tracing(
            service_name="test-service",
            otlp_endpoint="http://localhost:4317",
            enabled=False,
        )
    except Exception as exc:
        pytest.fail(f"Tracing setup with disabled=True failed: {exc}")


def test_trace_span_context_manager():
    """Verify trace span context manager works."""
    try:
        with tracing.trace_span("test.operation", {"key": "value"}):
            pass
    except Exception as exc:
        pytest.fail(f"Trace span context manager failed: {exc}")


def test_trace_span_with_exception():
    """Verify trace span handles exceptions correctly."""
    with pytest.raises(ValueError):
        with tracing.trace_span("test.failing_operation"):
            raise ValueError("Expected test error")


def test_add_span_attributes():
    """Verify adding attributes to current span."""
    try:
        with tracing.trace_span("test.operation"):
            tracing.add_span_attributes(
                tenant_id="test-tenant",
                event_id="evt-123",
                custom_field="value",
            )
    except Exception as exc:
        pytest.fail(f"Adding span attributes failed: {exc}")


def test_add_span_event():
    """Verify adding events to current span."""
    try:
        with tracing.trace_span("test.operation"):
            tracing.add_span_event("checkpoint", {"step": "validation"})
            tracing.add_span_event("retry", {"attempt": 2})
    except Exception as exc:
        pytest.fail(f"Adding span event failed: {exc}")


def test_get_trace_context():
    """Verify trace context can be retrieved."""
    context = tracing.get_trace_context()
    
    assert isinstance(context, dict)


def test_metrics_get_metrics_function():
    """Verify get_metrics returns correct format."""
    data, content_type = metrics.get_metrics()
    
    assert isinstance(data, bytes)
    assert isinstance(content_type, str)
    assert "text/plain" in content_type
    
    decoded = data.decode('utf-8')
    assert "# HELP" in decoded
    assert "# TYPE" in decoded


@pytest.mark.asyncio
async def test_metrics_not_in_openapi_schema():
    """Verify /metrics endpoint is excluded from OpenAPI docs."""
    async with AsyncClient(app=app, base_url="http://test") as client:
        response = await client.get("/openapi.json")
    
    openapi_spec = response.json()
    paths = openapi_spec.get("paths", {})
    
    assert "/metrics" not in paths


def test_all_metric_types_defined():
    """Verify all expected metric types are defined."""
    assert hasattr(metrics, 'webhook_requests_total')
    assert hasattr(metrics, 'webhook_processing_duration_seconds')
    assert hasattr(metrics, 'webhook_retries_total')
    assert hasattr(metrics, 'webhook_dlq_total')
    assert hasattr(metrics, 'idempotency_duplicates_total')
    assert hasattr(metrics, 'websocket_connections_active')
    assert hasattr(metrics, 'websocket_messages_sent_total')
    assert hasattr(metrics, 'redis_operations_total')
    assert hasattr(metrics, 'kafka_produce_total')
    assert hasattr(metrics, 'kafka_consume_total')
    assert hasattr(metrics, 'http_request_duration_seconds')
    assert hasattr(metrics, 'http_requests_total')


def test_metric_labels_are_correct():
    """Verify metrics have correct label names."""
    assert metrics.webhook_requests_total._labelnames == ('tenant_id', 'status')
    assert metrics.webhook_processing_duration_seconds._labelnames == ('tenant_id',)
    assert metrics.webhook_retries_total._labelnames == ('tenant_id', 'attempt')
    assert metrics.webhook_dlq_total._labelnames == ('tenant_id', 'error_type')
    assert metrics.idempotency_duplicates_total._labelnames == ('tenant_id',)
    assert metrics.websocket_connections_active._labelnames == ('tenant_id',)
    assert metrics.redis_operations_total._labelnames == ('operation', 'status')


@pytest.mark.asyncio
async def test_metrics_format_is_parseable():
    """Verify metrics output can be parsed."""
    async with AsyncClient(app=app, base_url="http://test") as client:
        response = await client.get("/metrics")
    
    lines = response.text.split('\n')
    
    help_pattern = re.compile(r'^# HELP \w+ .+$')
    type_pattern = re.compile(r'^# TYPE \w+ (counter|gauge|histogram|summary)$')
    metric_pattern = re.compile(r'^\w+(\{[^}]*\})?\s+[\d.]+(\s+\d+)?$')
    
    for line in lines:
        if not line or line.startswith('#'):
            if line.startswith('# HELP'):
                assert help_pattern.match(line), f"Invalid HELP line: {line}"
            elif line.startswith('# TYPE'):
                assert type_pattern.match(line), f"Invalid TYPE line: {line}"
        else:
            assert metric_pattern.match(line), f"Invalid metric line: {line}"
