import asyncio
import logging
import time
from dataclasses import dataclass

from src.config import settings
from src.observability import metrics, tracing

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class WebhookEnvelope:
    event_id: str
    tenant_id: str
    payload: bytes


def _extract_envelope(record) -> WebhookEnvelope:
    headers = {k: v for k, v in record.headers}
    return WebhookEnvelope(
        event_id=headers["event_id"].decode(),
        tenant_id=headers["tenant_id"].decode(),
        payload=record.value,
    )


async def _acquire_idempotency_lock(redis, event_id: str) -> bool:
    with tracing.trace_span("webhook.idempotency_check", {"event_id": event_id}):
        result = bool(
            await redis.set(
                f"event_lock:{event_id}",
                b"1",
                ex=settings.IDEMPOTENCY_TTL_SECONDS,
                nx=True,
            )
        )
        metrics.redis_operations_total.labels(
            operation="idempotency_check",
            status="success"
        ).inc()
        return result


def _backoff_seconds(attempt: int) -> float:
    return (settings.WORKER_BACKOFF_BASE_MS / 1000.0) * (2 ** (attempt - 1))


def _build_dlq_headers(envelope: WebhookEnvelope, error: BaseException) -> list[tuple[str, bytes]]:
    return [
        ("tenant_id", envelope.tenant_id.encode()),
        ("event_id", envelope.event_id.encode()),
        ("x-original-error", str(error).encode()),
    ]


async def _dispatch_to_dlq(dlq_producer, envelope: WebhookEnvelope, error: BaseException) -> None:
    with tracing.trace_span("webhook.dispatch_to_dlq", {
        "tenant_id": envelope.tenant_id,
        "event_id": envelope.event_id,
        "error_type": type(error).__name__,
    }):
        await dlq_producer.send_and_wait(
            topic=settings.KAFKA_DLQ_TOPIC,
            value=envelope.payload,
            headers=_build_dlq_headers(envelope, error),
        )
        
        metrics.webhook_dlq_total.labels(
            tenant_id=envelope.tenant_id,
            error_type=type(error).__name__,
        ).inc()
        
        logger.error(
            "Routed event to DLQ after exhausting retries",
            extra={
                "event_id": envelope.event_id,
                "tenant_id": envelope.tenant_id,
                "error": str(error),
                "max_retries": settings.WORKER_MAX_RETRIES,
            },
        )


async def process_record(record, redis, dlq_producer, business_handler):
    envelope = _extract_envelope(record)
    
    with tracing.trace_span("webhook.process", {
        "tenant_id": envelope.tenant_id,
        "event_id": envelope.event_id,
    }):
        start_time = time.time()
        
        if not await _acquire_idempotency_lock(redis, envelope.event_id):
            metrics.idempotency_duplicates_total.labels(
                tenant_id=envelope.tenant_id
            ).inc()
            
            logger.info(
                "Ignored duplicated event",
                extra={
                    "event_id": envelope.event_id,
                    "tenant_id": envelope.tenant_id,
                },
            )
            return

        last_exc: BaseException | None = None
        for attempt in range(1, settings.WORKER_MAX_RETRIES + 1):
            try:
                with tracing.trace_span("webhook.business_handler", {
                    "tenant_id": envelope.tenant_id,
                    "event_id": envelope.event_id,
                    "attempt": attempt,
                }):
                    await business_handler(record)
                
                duration = time.time() - start_time
                metrics.webhook_processing_duration_seconds.labels(
                    tenant_id=envelope.tenant_id
                ).observe(duration)
                
                tracing.add_span_attributes(status="success", duration_seconds=duration)
                return
            except Exception as exc:
                last_exc = exc
                
                metrics.webhook_retries_total.labels(
                    tenant_id=envelope.tenant_id,
                    attempt=str(attempt),
                ).inc()
                
                tracing.add_span_event("retry", {
                    "attempt": str(attempt),
                    "error_type": type(exc).__name__,
                    "error_message": str(exc),
                })
                
                logger.warning(
                    "Business handler failed; will retry",
                    extra={
                        "event_id": envelope.event_id,
                        "tenant_id": envelope.tenant_id,
                        "attempt": attempt,
                        "max_retries": settings.WORKER_MAX_RETRIES,
                        "error": str(exc),
                    },
                )
                if attempt < settings.WORKER_MAX_RETRIES:
                    backoff = _backoff_seconds(attempt)
                    tracing.add_span_attributes(backoff_seconds=backoff)
                    await asyncio.sleep(backoff)

        assert last_exc is not None
        await _dispatch_to_dlq(dlq_producer, envelope, last_exc)
