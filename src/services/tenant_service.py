import logging
import uuid
from uuid import UUID
from sqlalchemy.ext.asyncio import AsyncSession
from redis.asyncio import Redis
from src.models.tenant import Tenant
from src.security import generate_secret_key, hash_secret

logger = logging.getLogger(__name__)


async def create_tenant(
    name: str, admin_id: UUID, db: AsyncSession, redis: Redis
) -> tuple[Tenant, str]:
    raw_secret = generate_secret_key()
    secret_hash = hash_secret(raw_secret)

    tenant = Tenant(
        id=uuid.uuid4(),
        name=name,
        secret_key_hash=secret_hash,
        created_by=admin_id,
    )
    db.add(tenant)
    await db.commit()
    await db.refresh(tenant)

    try:
        await redis.set(f"tenant_auth:{tenant.id}", secret_hash)
        await redis.set(f"tenant_hmac_key:{tenant.id}", raw_secret)
    except Exception:
        logger.warning("Failed to sync tenant %s credentials to Redis", tenant.id)

    return tenant, raw_secret
