from pydantic import BaseModel, Field


class TenantCreateRequest(BaseModel):
    name: str = Field(..., min_length=1, max_length=255)


class TenantCreateResponse(BaseModel):
    tenant_id: str
    secret_key: str
