from fastapi import APIRouter, Depends
from pydantic import BaseModel

from src.config import settings
from src.dependencies import AuthenticatedTenant, get_authenticated_tenant
from src.services.ws_token import create_signed_token

router = APIRouter(prefix="/v1/tokens", tags=["tokens"])


class TokenResponse(BaseModel):
    token: str
    expires_in: int


@router.post("/{tenant_id}", response_model=TokenResponse)
async def issue_ws_token(
    tenant: AuthenticatedTenant = Depends(get_authenticated_tenant),
) -> TokenResponse:
    token = create_signed_token(
        tenant.tenant_id, ttl_seconds=settings.WS_TOKEN_TTL_SECONDS
    )
    return TokenResponse(token=token, expires_in=settings.WS_TOKEN_TTL_SECONDS)
