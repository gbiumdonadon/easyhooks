import uuid
from unittest.mock import AsyncMock

import pytest
import pytest_asyncio
from aiokafka import AIOKafkaProducer
from aiokafka.admin import AIOKafkaAdminClient, NewTopic
from httpx import AsyncClient
from httpx_ws.transport import ASGIWebSocketTransport
from sqlalchemy.ext.asyncio import create_async_engine, async_sessionmaker
import fakeredis.aioredis

from src.config import settings
from src.main import app
from src.database import get_db
from src.redis_client import get_redis
from src.services.kafka_producer import get_kafka_producer
from src.models.base import Base
from src.models.admin_user import AdminUser
from src.security import hash_secret

TEST_DATABASE_URL = "sqlite+aiosqlite:///./test.db"


@pytest_asyncio.fixture(scope="session")
async def test_engine():
    engine = create_async_engine(TEST_DATABASE_URL, echo=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.drop_all)
        await conn.run_sync(Base.metadata.create_all)
    yield engine
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.drop_all)
    await engine.dispose()


@pytest_asyncio.fixture
async def db_session(test_engine):
    session_factory = async_sessionmaker(test_engine, expire_on_commit=False)
    async with session_factory() as session:
        yield session
        await session.rollback()


@pytest_asyncio.fixture
async def redis_client():
    client = fakeredis.aioredis.FakeRedis(decode_responses=False)
    await client.flushdb()
    yield client
    await client.flushdb()
    await client.aclose()


@pytest_asyncio.fixture
async def admin_token(db_session):
    raw_token = "test-admin-token-" + uuid.uuid4().hex
    admin = AdminUser(
        id=uuid.uuid4(),
        username=f"testadmin-{uuid.uuid4().hex[:8]}",
        token_hash=hash_secret(raw_token),
        role="admin",
    )
    db_session.add(admin)
    await db_session.commit()
    return raw_token


@pytest_asyncio.fixture
async def kafka_mock():
    mock = AsyncMock()
    mock.send = AsyncMock()
    mock.send_and_wait = AsyncMock()
    return mock


@pytest.fixture(scope="session")
def kafka_container():
    """Sobe um container real de Kafka via testcontainers (escopo de sess\u00e3o)."""
    from testcontainers.kafka import KafkaContainer

    container = KafkaContainer("confluentinc/cp-kafka:7.6.0")
    container.start()
    try:
        yield container
    finally:
        container.stop()


@pytest.fixture(scope="session")
def kafka_bootstrap(kafka_container):
    return kafka_container.get_bootstrap_server()


@pytest_asyncio.fixture
async def kafka_topics(kafka_bootstrap):
    """Garante que os t\u00f3picos webhooks.inbound e webhooks.dlq existem,
    limpos por teste (recriados a cada chamada)."""
    admin = AIOKafkaAdminClient(bootstrap_servers=kafka_bootstrap)
    await admin.start()
    try:
        existing = await admin.list_topics()
        to_delete = [
            t for t in (settings.KAFKA_WEBHOOK_TOPIC, settings.KAFKA_DLQ_TOPIC)
            if t in existing
        ]
        if to_delete:
            await admin.delete_topics(to_delete)
            # Aguardar dele\u00e7\u00e3o propagar
            import asyncio
            for _ in range(20):
                current = await admin.list_topics()
                if not any(t in current for t in to_delete):
                    break
                await asyncio.sleep(0.1)

        await admin.create_topics([
            NewTopic(
                name=settings.KAFKA_WEBHOOK_TOPIC,
                num_partitions=1,
                replication_factor=1,
            ),
            NewTopic(
                name=settings.KAFKA_DLQ_TOPIC,
                num_partitions=1,
                replication_factor=1,
            ),
        ])
        yield (settings.KAFKA_WEBHOOK_TOPIC, settings.KAFKA_DLQ_TOPIC)
    finally:
        await admin.close()


@pytest_asyncio.fixture
async def kafka_real_producer(kafka_bootstrap, kafka_topics):
    producer = AIOKafkaProducer(bootstrap_servers=kafka_bootstrap)
    await producer.start()
    try:
        yield producer
    finally:
        await producer.stop()


@pytest_asyncio.fixture
async def async_client(db_session, redis_client, kafka_mock):
    async def override_get_db():
        yield db_session

    async def override_get_redis():
        yield redis_client

    async def override_get_kafka_producer():
        yield kafka_mock

    app.dependency_overrides[get_db] = override_get_db
    app.dependency_overrides[get_redis] = override_get_redis
    app.dependency_overrides[get_kafka_producer] = override_get_kafka_producer

    transport = ASGIWebSocketTransport(app=app)
    await transport.__aenter__()
    client = AsyncClient(transport=transport, base_url="http://testserver")
    try:
        yield client
    finally:
        try:
            await client.aclose()
        except RuntimeError as exc:
            # anyio cancel scope cross-task warning when pytest-asyncio
            # runs setup and teardown in different tasks; transport is
            # already closed at this point so we can safely swallow it.
            if "different task" not in str(exc):
                raise
        app.dependency_overrides.clear()
