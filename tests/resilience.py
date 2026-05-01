"""Group 5: Resili\u00eancia e Tratamento de Erros (DLQ) \u2014 TDD Tests"""
import asyncio
import uuid
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

import pytest
from aiokafka import AIOKafkaConsumer, AIOKafkaProducer

from src.config import settings
from src.services.webhook_processor import process_record


async def _consume_one(bootstrap: str, topic: str, timeout: float = 30.0):
    consumer = AIOKafkaConsumer(
        topic,
        bootstrap_servers=bootstrap,
        group_id=f"test-{uuid.uuid4().hex}",
        auto_offset_reset="earliest",
        enable_auto_commit=False,
    )
    await consumer.start()
    try:
        async def _next():
            async for record in consumer:
                return record

        return await asyncio.wait_for(_next(), timeout=timeout)
    finally:
        await consumer.stop()


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
async def test_should_route_to_dlq_after_max_failures(
    kafka_real_producer,
    kafka_bootstrap,
    redis_client,
):
    business_handler = AsyncMock(side_effect=RuntimeError("boom"))

    dlq_producer = AIOKafkaProducer(bootstrap_servers=kafka_bootstrap)
    await dlq_producer.start()

    try:
        event_id = "evt-" + uuid.uuid4().hex
        await kafka_real_producer.send_and_wait(
            settings.KAFKA_WEBHOOK_TOPIC,
            value=b'{"k":"v"}',
            headers=[
                ("event_id", event_id.encode()),
                ("tenant_id", b"tenant-1"),
            ],
        )

        records = await _consume_n(
            kafka_bootstrap, settings.KAFKA_WEBHOOK_TOPIC, n=1
        )
        assert len(records) == 1

        await process_record(
            records[0],
            redis=redis_client,
            dlq_producer=dlq_producer,
            business_handler=business_handler,
        )

        assert business_handler.call_count == settings.WORKER_MAX_RETRIES

        dlq_record = await _consume_one(
            kafka_bootstrap, settings.KAFKA_DLQ_TOPIC
        )
        assert dlq_record is not None
        assert dlq_record.value == b'{"k":"v"}'

        dlq_headers = {k: v for k, v in dlq_record.headers}
        assert dlq_headers.get("tenant_id") == b"tenant-1"
        assert dlq_headers.get("event_id") == event_id.encode()
        assert "x-original-error" in dlq_headers
        assert b"boom" in dlq_headers["x-original-error"]
    finally:
        await dlq_producer.stop()


@pytest.mark.asyncio
async def test_should_apply_exponential_backoff_between_retries(redis_client):
    business_handler = AsyncMock(side_effect=RuntimeError("boom"))
    dlq_producer = AsyncMock()

    fake_record = SimpleNamespace(
        value=b'{"k":"v"}',
        headers=(
            ("event_id", b"evt-backoff-01"),
            ("tenant_id", b"tenant-1"),
        ),
    )

    sleep_calls: list[float] = []

    async def fake_sleep(delay: float) -> None:
        sleep_calls.append(delay)

    with patch("src.services.webhook_processor.asyncio.sleep", fake_sleep):
        await process_record(
            fake_record,
            redis=redis_client,
            dlq_producer=dlq_producer,
            business_handler=business_handler,
        )

    assert business_handler.call_count == settings.WORKER_MAX_RETRIES
    base_seconds = settings.WORKER_BACKOFF_BASE_MS / 1000.0
    expected = [base_seconds * (2 ** i) for i in range(settings.WORKER_MAX_RETRIES - 1)]
    assert sleep_calls == expected
    dlq_producer.send_and_wait.assert_awaited_once()
