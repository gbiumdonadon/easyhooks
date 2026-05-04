"""OpenTelemetry tracing configuration for distributed tracing.

This module sets up:
- OTLP exporter to Jaeger
- Automatic instrumentation for FastAPI, SQLAlchemy, Redis, Kafka
- Custom span creation utilities
- Context propagation across services
"""
import logging
from contextlib import contextmanager
from typing import Any

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.instrumentation.redis import RedisInstrumentor
from opentelemetry.instrumentation.sqlalchemy import SQLAlchemyInstrumentor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.trace.sampling import TraceIdRatioBased

logger = logging.getLogger(__name__)

tracer = trace.get_tracer(__name__)


def setup_tracing(
    service_name: str,
    otlp_endpoint: str,
    enabled: bool = True,
    sample_rate: float = 1.0,
) -> None:
    """Configure OpenTelemetry tracing with OTLP exporter.
    
    Args:
        service_name: Name of the service for trace identification
        otlp_endpoint: OTLP endpoint URL (e.g., http://jaeger:4317)
        enabled: Whether tracing is enabled
        sample_rate: Sampling rate (0.0 to 1.0)
    """
    if not enabled:
        logger.info("Tracing is disabled")
        return

    try:
        resource = Resource.create({
            "service.name": service_name,
            "service.version": "0.1.0",
        })

        sampler = TraceIdRatioBased(sample_rate)
        provider = TracerProvider(resource=resource, sampler=sampler)
        
        otlp_exporter = OTLPSpanExporter(
            endpoint=otlp_endpoint,
            insecure=True,
        )
        
        span_processor = BatchSpanProcessor(otlp_exporter)
        provider.add_span_processor(span_processor)
        
        trace.set_tracer_provider(provider)
        
        logger.info(
            "OpenTelemetry tracing configured",
            extra={
                "service_name": service_name,
                "otlp_endpoint": otlp_endpoint,
                "sample_rate": sample_rate,
            },
        )
    except Exception as exc:
        logger.warning(f"Failed to configure tracing: {exc}")


def instrument_fastapi(app) -> None:
    """Instrument FastAPI application for automatic tracing.
    
    Args:
        app: FastAPI application instance
    """
    try:
        FastAPIInstrumentor.instrument_app(app)
        logger.info("FastAPI instrumentation enabled")
    except Exception as exc:
        logger.warning(f"Failed to instrument FastAPI: {exc}")


def instrument_redis() -> None:
    """Instrument Redis client for automatic tracing."""
    try:
        RedisInstrumentor().instrument()
        logger.info("Redis instrumentation enabled")
    except Exception as exc:
        logger.warning(f"Failed to instrument Redis: {exc}")


def instrument_sqlalchemy(engine) -> None:
    """Instrument SQLAlchemy engine for automatic tracing.
    
    Args:
        engine: SQLAlchemy engine instance
    """
    try:
        SQLAlchemyInstrumentor().instrument(engine=engine)
        logger.info("SQLAlchemy instrumentation enabled")
    except Exception as exc:
        logger.warning(f"Failed to instrument SQLAlchemy: {exc}")


@contextmanager
def trace_span(name: str, attributes: dict[str, Any] | None = None):
    """Create a custom trace span with optional attributes.
    
    Usage:
        with trace_span("webhook.validate_hmac", {"tenant_id": tenant_id}):
            # Your code here
            pass
    
    Args:
        name: Span name (use dot notation, e.g., "webhook.process")
        attributes: Optional dictionary of span attributes
    """
    span = tracer.start_span(name)
    
    if attributes:
        for key, value in attributes.items():
            span.set_attribute(key, str(value))
    
    try:
        yield span
    except Exception as exc:
        span.set_attribute("error", True)
        span.set_attribute("error.type", type(exc).__name__)
        span.set_attribute("error.message", str(exc))
        span.record_exception(exc)
        raise
    finally:
        span.end()


def add_span_attributes(**attributes: Any) -> None:
    """Add attributes to the current active span.
    
    Usage:
        add_span_attributes(tenant_id=tenant_id, event_id=event_id)
    
    Args:
        **attributes: Key-value pairs to add as span attributes
    """
    current_span = trace.get_current_span()
    if current_span.is_recording():
        for key, value in attributes.items():
            current_span.set_attribute(key, str(value))


def add_span_event(name: str, attributes: dict[str, Any] | None = None) -> None:
    """Add an event to the current active span.
    
    Args:
        name: Event name
        attributes: Optional dictionary of event attributes
    """
    current_span = trace.get_current_span()
    if current_span.is_recording():
        current_span.add_event(name, attributes or {})


def get_trace_context() -> dict[str, str]:
    """Get the current trace context for propagation.
    
    Returns:
        Dictionary with traceparent header for context propagation
    """
    current_span = trace.get_current_span()
    if current_span.is_recording():
        ctx = current_span.get_span_context()
        return {
            "traceparent": f"00-{format(ctx.trace_id, '032x')}-{format(ctx.span_id, '016x')}-{ctx.trace_flags:02x}"
        }
    return {}
