from redis.asyncio import ConnectionPool, Redis
from src.config import settings

# Configure explicit connection pool with max_connections limit
_connection_pool = ConnectionPool.from_url(
    settings.REDIS_URL,
    max_connections=settings.REDIS_POOL_SIZE,
    decode_responses=False,
)

redis_pool = Redis(connection_pool=_connection_pool)


async def get_redis() -> Redis:
    yield redis_pool
