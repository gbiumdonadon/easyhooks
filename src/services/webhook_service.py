import json
from uuid import UUID

from src.config import settings


def _build_message_headers(tenant_id: UUID | str, event_id: str) -> list[tuple[str, bytes]]:
    return [
        ("tenant_id", str(tenant_id).encode("utf-8")),
        ("event_id", event_id.encode("utf-8")),
    ]


async def produce_webhook_message(
    producer,
    tenant_id: UUID,
    event_id: str,
    payload: dict,
) -> None:
    message_value = json.dumps(payload).encode("utf-8")
    headers = _build_message_headers(tenant_id, event_id)

    # Fire-and-forget: don't wait for ACK to reduce latency
    # Idempotency is handled by the worker (event_lock in Redis)
    await producer.send(
        topic=settings.KAFKA_WEBHOOK_TOPIC,
        value=message_value,
        headers=headers,
    )
