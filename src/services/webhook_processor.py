import asyncio
import logging
from dataclasses import dataclass

from src.config import settings

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
    return bool(
        await redis.set(
            f"event_lock:{event_id}",
            b"1",
            ex=settings.IDEMPOTENCY_TTL_SECONDS,
            nx=True,
        )
    )


def _backoff_seconds(attempt: int) -> float:
    return (settings.WORKER_BACKOFF_BASE_MS / 1000.0) * (2 ** (attempt - 1))


def _build_dlq_headers(envelope: WebhookEnvelope, error: BaseException) -> list[tuple[str, bytes]]:
    return [
        ("tenant_id", envelope.tenant_id.encode()),
        ("event_id", envelope.event_id.encode()),
        ("x-original-error", str(error).encode()),
    ]


async def _dispatch_to_dlq(dlq_producer, envelope: WebhookEnvelope, error: BaseException) -> None:
    await dlq_producer.send_and_wait(
        topic=settings.KAFKA_DLQ_TOPIC,
        value=envelope.payload,
        headers=_build_dlq_headers(envelope, error),
    )
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

    if not await _acquire_idempotency_lock(redis, envelope.event_id):
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
            await business_handler(record)
            return
        except Exception as exc:
            last_exc = exc
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
                await asyncio.sleep(_backoff_seconds(attempt))

    assert last_exc is not None
    await _dispatch_to_dlq(dlq_producer, envelope, last_exc)
