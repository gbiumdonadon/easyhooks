import asyncio
import hashlib
from dataclasses import dataclass
from uuid import UUID

from fastapi import Depends, Header, HTTPException, Request
from redis.asyncio import Redis
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select

from src.config import settings
from src.database import get_db
from src.redis_client import get_redis
from src.models.admin_user import AdminUser
from src.security import verify_hmac_signature, verify_secret


async def get_current_admin(
    authorization: str | None = Header(None),
    db: AsyncSession = Depends(get_db),
) -> AdminUser:
    if not authorization or not authorization.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="Missing or invalid authorization header")

    token = authorization.removeprefix("Bearer ").strip()

    result = await db.execute(select(AdminUser))
    admins = result.scalars().all()

    for admin in admins:
        if await asyncio.to_thread(verify_secret, token, admin.token_hash):
            return admin

    raise HTTPException(status_code=403, detail="Invalid admin credentials")


@dataclass(frozen=True)
class AuthenticatedTenant:
    tenant_id: UUID


async def _authenticate_via_bearer(
    tenant_id: UUID, raw_secret: str, redis: Redis
) -> AuthenticatedTenant:
    # Try session cache first to avoid expensive bcrypt verification
    secret_hash = hashlib.sha256(raw_secret.encode()).hexdigest()[:16]
    session_key = f"auth_session:{tenant_id}:{secret_hash}"
    
    if await redis.exists(session_key):
        return AuthenticatedTenant(tenant_id=tenant_id)
    
    # Fallback: verify with bcrypt (in thread pool to avoid blocking event loop)
    cached_hash = await redis.get(f"tenant_auth:{tenant_id}")
    if cached_hash is None:
        raise HTTPException(status_code=401, detail="Unknown tenant")
    hash_str = cached_hash.decode() if isinstance(cached_hash, bytes) else cached_hash
    if not await asyncio.to_thread(verify_secret, raw_secret, hash_str):
        raise HTTPException(status_code=403, detail="Invalid credentials for this tenant")
    
    # Cache successful authentication for AUTH_SESSION_TTL_SECONDS
    await redis.set(session_key, b"1", ex=settings.AUTH_SESSION_TTL_SECONDS)
    return AuthenticatedTenant(tenant_id=tenant_id)


async def _authenticate_via_hmac(
    tenant_id: UUID, signature: str, body: bytes, redis: Redis
) -> AuthenticatedTenant:
    """Valida 'X-Webhook-Signature' = 'sha256=<hex>' = HMAC-SHA256(secret, body).

    Usa a chave Redis 'tenant_hmac_key:{id}' que armazena o secret bruto
    necessário para recomputar o HMAC."""
    cached_key = await redis.get(f"tenant_hmac_key:{tenant_id}")
    if cached_key is None:
        raise HTTPException(status_code=401, detail="Unknown tenant")
    secret_raw = cached_key.decode() if isinstance(cached_key, bytes) else cached_key
    if not verify_hmac_signature(secret_raw, body, signature):
        raise HTTPException(status_code=403, detail="Invalid HMAC signature")
    return AuthenticatedTenant(tenant_id=tenant_id)


async def get_authenticated_tenant(
    tenant_id: UUID,
    request: Request,
    authorization: str | None = Header(None),
    x_webhook_signature: str | None = Header(None),
    redis: Redis = Depends(get_redis),
) -> AuthenticatedTenant:
    if x_webhook_signature:
        body = await request.body()
        return await _authenticate_via_hmac(
            tenant_id, x_webhook_signature, body, redis
        )

    if authorization and authorization.startswith("Bearer "):
        raw_secret = authorization.removeprefix("Bearer ").strip()
        return await _authenticate_via_bearer(tenant_id, raw_secret, redis)

    raise HTTPException(status_code=401, detail="Missing authentication credentials")
