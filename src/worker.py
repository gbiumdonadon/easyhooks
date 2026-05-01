"""Webhook worker entrypoint: `python -m src.worker`.

Consome mensagens de `webhooks.inbound`, aplica idempot\u00eancia via Redis,
delega ao `business_handler` com retry/backoff e roteia falhas para DLQ.
"""
import asyncio
import logging
import signal

from aiokafka import AIOKafkaConsumer, AIOKafkaProducer
from redis.asyncio import Redis

from src.config import settings
from src.services.event_streams import publish_tenant_event
from src.services.webhook_processor import process_record

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
logger = logging.getLogger("worker")


async def default_business_handler(record) -> None:
    logger.info(
        "Processed webhook event",
        extra={"value_size": len(record.value or b"")},
    )


def make_redis_streams_handler(redis):
    """Create a business handler that publishes events to Redis Streams.
    
    Events are persisted in tenant-specific streams with configurable history.
    WebSocket clients can retrieve historical events on connect.
    """
    async def handler(record) -> None:
        from uuid import UUID
        
        headers = {k: v for k, v in record.headers}
        tenant_id = headers["tenant_id"].decode()
        
        msg_id = await publish_tenant_event(
            redis,
            UUID(tenant_id),
            record.value,
        )
        
        logger.info(
            "Published event to stream",
            extra={"tenant_id": tenant_id, "stream_id": msg_id},
        )
    
    return handler


async def run(business_handler=None) -> None:
    consumer = AIOKafkaConsumer(
        settings.KAFKA_WEBHOOK_TOPIC,
        bootstrap_servers=settings.KAFKA_BOOTSTRAP_SERVERS,
        group_id=settings.KAFKA_CONSUMER_GROUP,
        enable_auto_commit=False,
        auto_offset_reset="earliest",
    )
    dlq_producer = AIOKafkaProducer(bootstrap_servers=settings.KAFKA_BOOTSTRAP_SERVERS)
    redis = Redis.from_url(settings.REDIS_URL, decode_responses=False)

    if business_handler is None:
        business_handler = make_redis_streams_handler(redis)

    stop_event = asyncio.Event()

    def _request_stop(*_args) -> None:
        logger.info("Shutdown signal received; stopping worker")
        stop_event.set()

    loop = asyncio.get_running_loop()
    for sig_name in ("SIGINT", "SIGTERM"):
        sig = getattr(signal, sig_name, None)
        if sig is None:
            continue
        try:
            loop.add_signal_handler(sig, _request_stop)
        except NotImplementedError:
            signal.signal(sig, _request_stop)

    await consumer.start()
    await dlq_producer.start()
    logger.info(
        "Worker started",
        extra={
            "topic": settings.KAFKA_WEBHOOK_TOPIC,
            "dlq_topic": settings.KAFKA_DLQ_TOPIC,
            "group_id": settings.KAFKA_CONSUMER_GROUP,
        },
    )

    try:
        while not stop_event.is_set():
            try:
                batch = await asyncio.wait_for(consumer.getmany(timeout_ms=1000), timeout=2.0)
            except asyncio.TimeoutError:
                continue

            for _tp, records in batch.items():
                for record in records:
                    try:
                        await process_record(
                            record,
                            redis=redis,
                            dlq_producer=dlq_producer,
                            business_handler=business_handler,
                        )
                    except Exception:
                        logger.exception("Unrecoverable error processing record")
                    finally:
                        await consumer.commit()
    finally:
        await consumer.stop()
        await dlq_producer.stop()
        await redis.aclose()
        logger.info("Worker stopped")


def main() -> None:
    asyncio.run(run())


if __name__ == "__main__":
    main()
