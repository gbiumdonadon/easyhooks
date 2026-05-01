from aiokafka import AIOKafkaProducer
from src.config import settings


class KafkaProducerManager:
    def __init__(self):
        self._producer: AIOKafkaProducer | None = None

    async def start(self):
        self._producer = AIOKafkaProducer(
            bootstrap_servers=settings.KAFKA_BOOTSTRAP_SERVERS,
        )
        await self._producer.start()

    async def stop(self):
        if self._producer:
            await self._producer.stop()
            self._producer = None

    @property
    def producer(self) -> AIOKafkaProducer:
        if self._producer is None:
            raise RuntimeError("Kafka producer not started")
        return self._producer


kafka_manager = KafkaProducerManager()


async def get_kafka_producer():
    yield kafka_manager.producer
