from redis.asyncio import Redis
from src.config import settings

redis_pool = Redis.from_url(settings.REDIS_URL, decode_responses=False)


async def get_redis() -> Redis:
    yield redis_pool
