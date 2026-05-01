from fastapi import APIRouter, Depends
from sqlalchemy.ext.asyncio import AsyncSession
from redis.asyncio import Redis
from src.database import get_db
from src.redis_client import get_redis
from src.dependencies import get_current_admin
from src.models.admin_user import AdminUser
from src.schemas.tenant import TenantCreateRequest, TenantCreateResponse
from src.services.tenant_service import create_tenant

router = APIRouter(prefix="/admin", tags=["admin"])


@router.post("/tenants", status_code=201, response_model=TenantCreateResponse)
async def create_tenant_endpoint(
    payload: TenantCreateRequest,
    admin: AdminUser = Depends(get_current_admin),
    db: AsyncSession = Depends(get_db),
    redis: Redis = Depends(get_redis),
):
    tenant, raw_secret = await create_tenant(
        name=payload.name, admin_id=admin.id, db=db, redis=redis
    )
    return TenantCreateResponse(
        tenant_id=str(tenant.id),
        secret_key=raw_secret,
    )
