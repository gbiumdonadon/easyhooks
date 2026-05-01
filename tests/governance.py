"""Group 1: Governance and Administrative Setup — TDD Tests"""
import uuid
import pytest
from httpx import AsyncClient
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select
from redis.asyncio import Redis

from src.models.tenant import Tenant
from src.security import verify_secret


# =============================================================================
# Test 1.1: should_create_tenant_and_generate_secure_credentials
# =============================================================================

@pytest.mark.asyncio
async def test_should_create_tenant_and_generate_secure_credentials(
    async_client: AsyncClient,
    admin_token: str,
    db_session: AsyncSession,
):
    response = await async_client.post(
        "/admin/tenants",
        json={"name": "Acme Corp"},
        headers={"Authorization": f"Bearer {admin_token}"},
    )

    assert response.status_code == 201
    body = response.json()

    # Must return a valid UUID tenant_id
    assert "tenant_id" in body
    uuid.UUID(body["tenant_id"])  # raises ValueError if not valid UUID

    # Must return a high-entropy secret_key (>= 43 chars base64url from 32 bytes)
    assert "secret_key" in body
    assert len(body["secret_key"]) >= 43

    # The secret_key in the database must be stored as a hash, not plaintext
    result = await db_session.execute(
        select(Tenant).where(Tenant.id == uuid.UUID(body["tenant_id"]))
    )
    tenant = result.scalar_one_or_none()
    assert tenant is not None
    assert tenant.secret_key_hash != body["secret_key"]
    assert verify_secret(body["secret_key"], tenant.secret_key_hash) is True


# =============================================================================
# Test 1.2: should_sync_tenant_credentials_to_redis_on_creation
# =============================================================================

@pytest.mark.asyncio
async def test_should_sync_tenant_credentials_to_redis_on_creation(
    async_client: AsyncClient,
    admin_token: str,
    redis_client,
):
    response = await async_client.post(
        "/admin/tenants",
        json={"name": "Globex Corporation"},
        headers={"Authorization": f"Bearer {admin_token}"},
    )
    assert response.status_code == 201
    body = response.json()
    tenant_id = body["tenant_id"]

    # Redis must have the key tenant_auth:<tenant_id>
    cached_value = await redis_client.get(f"tenant_auth:{tenant_id}")
    assert cached_value is not None

    # The cached value should be the bcrypt hash (starts with $2b$)
    cached_hash = cached_value.decode()
    assert cached_hash.startswith("$2b$")

    # The hash in Redis must verify against the raw secret_key
    assert verify_secret(body["secret_key"], cached_hash) is True


# =============================================================================
# Test 1.3: should_reject_tenant_creation_without_admin_privileges
# =============================================================================

@pytest.mark.asyncio
async def test_should_reject_tenant_creation_without_admin_privileges(
    async_client: AsyncClient,
):
    # Case 1: No authorization header at all -> 401
    response = await async_client.post(
        "/admin/tenants",
        json={"name": "Unauthorized Inc"},
    )
    assert response.status_code == 401


@pytest.mark.asyncio
async def test_should_reject_tenant_creation_with_invalid_token(
    async_client: AsyncClient,
):
    # Case 2: Invalid/random token -> 403
    response = await async_client.post(
        "/admin/tenants",
        json={"name": "Fake Token Corp"},
        headers={"Authorization": "Bearer totally-invalid-token"},
    )
    assert response.status_code == 403


@pytest.mark.asyncio
async def test_tenant_cannot_create_another_tenant(
    async_client: AsyncClient,
    admin_token: str,
):
    # Create a tenant first
    resp = await async_client.post(
        "/admin/tenants",
        json={"name": "Tenant A"},
        headers={"Authorization": f"Bearer {admin_token}"},
    )
    assert resp.status_code == 201
    tenant_secret = resp.json()["secret_key"]

    # Try to use tenant's secret_key as bearer token to create another tenant
    response = await async_client.post(
        "/admin/tenants",
        json={"name": "Tenant B Created By Tenant A"},
        headers={"Authorization": f"Bearer {tenant_secret}"},
    )
    assert response.status_code in (401, 403)
