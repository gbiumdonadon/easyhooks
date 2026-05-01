"""Group 4: Processamento e Idempot\u00eancia (O Worker) \u2014 TDD Tests"""
import asyncio
import uuid
from unittest.mock import AsyncMock

import pytest
from aiokafka import AIOKafkaConsumer

from src.config import settings
from src.services.webhook_processor import process_record


async def _consume_n(bootstrap: str, topic: str, n: int, timeout: float = 30.0):
    consumer = AIOKafkaConsumer(
        topic,
        bootstrap_servers=bootstrap,
        group_id=f"test-{uuid.uuid4().hex}",
        auto_offset_reset="earliest",
        enable_auto_commit=False,
    )
    await consumer.start()
    try:
        records = []

        async def _collect():
            async for record in consumer:
                records.append(record)
                if len(records) >= n:
                    break

        await asyncio.wait_for(_collect(), timeout=timeout)
        return records
    finally:
        await consumer.stop()


@pytest.mark.asyncio
async def test_should_skip_already_processed_event(
    kafka_real_producer,
    kafka_bootstrap,
    redis_client,
):
    business_handler = AsyncMock()
    dlq_producer = AsyncMock()

    event_id = "evt-" + uuid.uuid4().hex
    headers = [
        ("event_id", event_id.encode()),
        ("tenant_id", b"tenant-1"),
    ]

    for _ in range(2):
        await kafka_real_producer.send_and_wait(
            settings.KAFKA_WEBHOOK_TOPIC,
            value=b'{"x":1}',
            headers=headers,
        )

    records = await _consume_n(
        kafka_bootstrap, settings.KAFKA_WEBHOOK_TOPIC, n=2
    )
    assert len(records) == 2

    for record in records:
        await process_record(
            record,
            redis=redis_client,
            dlq_producer=dlq_producer,
            business_handler=business_handler,
        )

    assert business_handler.call_count == 1
    dlq_producer.send_and_wait.assert_not_called()
