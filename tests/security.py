"""Group 2: Security, Authentication & Isolation — TDD Tests"""
import hashlib
import hmac
import json
import uuid

import pytest
from httpx import AsyncClient


@pytest.mark.asyncio
async def test_should_return_401_when_no_credentials_provided(
    async_client: AsyncClient,
):
    tenant_id = uuid.uuid4()

    response = await async_client.post(
        f"/v1/webhooks/{tenant_id}",
        json={"event": "order.created", "data": {"order_id": 123}},
    )

    assert response.status_code == 401
    body = response.json()
    assert "detail" in body


@pytest.mark.asyncio
async def test_should_return_403_when_tenant_x_uses_valid_key_to_post_to_tenant_y(
    async_client: AsyncClient,
    admin_token: str,
):
    # Create Tenant X
    resp_x = await async_client.post(
        "/admin/tenants",
        json={"name": "Tenant X"},
        headers={"Authorization": f"Bearer {admin_token}"},
    )
    assert resp_x.status_code == 201
    tenant_x_secret = resp_x.json()["secret_key"]

    # Create Tenant Y
    resp_y = await async_client.post(
        "/admin/tenants",
        json={"name": "Tenant Y"},
        headers={"Authorization": f"Bearer {admin_token}"},
    )
    assert resp_y.status_code == 201
    tenant_y_id = resp_y.json()["tenant_id"]

    # Tenant X's secret is valid, but POST to Tenant Y's route
    response = await async_client.post(
        f"/v1/webhooks/{tenant_y_id}",
        json={"event": "order.created", "data": {"order_id": 456}},
        headers={"Authorization": f"Bearer {tenant_x_secret}"},
    )

    assert response.status_code == 403
    body = response.json()
    assert "detail" in body


@pytest.mark.asyncio
async def test_should_return_202_when_valid_tenant_posts_to_own_route(
    async_client: AsyncClient,
    admin_token: str,
):
    # Create a tenant
    resp = await async_client.post(
        "/admin/tenants",
        json={"name": "Valid Tenant"},
        headers={"Authorization": f"Bearer {admin_token}"},
    )
    assert resp.status_code == 201
    tenant_id = resp.json()["tenant_id"]
    tenant_secret = resp.json()["secret_key"]

    # POST to own route with valid credentials
    response = await async_client.post(
        f"/v1/webhooks/{tenant_id}",
        json={"event": "order.created", "data": {"order_id": 789}},
        headers={
            "Authorization": f"Bearer {tenant_secret}",
            "X-Event-Id": "evt-valid-tenant-01",
        },
    )

    assert response.status_code == 202
    body = response.json()
    assert body["tenant_id"] == tenant_id


@pytest.mark.asyncio
async def test_should_accept_valid_hmac_signature(
    async_client: AsyncClient,
    admin_token: str,
):
    resp = await async_client.post(
        "/admin/tenants",
        json={"name": "HMAC Tenant"},
        headers={"Authorization": f"Bearer {admin_token}"},
    )
    assert resp.status_code == 201
    tenant_id = resp.json()["tenant_id"]
    secret = resp.json()["secret_key"]

    body = json.dumps({"event": "order.created", "data": {"id": 1}}).encode()
    sig = "sha256=" + hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()

    response = await async_client.post(
        f"/v1/webhooks/{tenant_id}",
        content=body,
        headers={
            "X-Webhook-Signature": sig,
            "X-Event-Id": "evt-hmac-1",
            "Content-Type": "application/json",
        },
    )

    assert response.status_code == 202


@pytest.mark.asyncio
async def test_should_reject_invalid_hmac_signature(
    async_client: AsyncClient,
    admin_token: str,
):
    resp = await async_client.post(
        "/admin/tenants",
        json={"name": "HMAC Bad Tenant"},
        headers={"Authorization": f"Bearer {admin_token}"},
    )
    assert resp.status_code == 201
    tenant_id = resp.json()["tenant_id"]

    body = json.dumps({"event": "order.created"}).encode()
    sig = "sha256=" + "0" * 64

    response = await async_client.post(
        f"/v1/webhooks/{tenant_id}",
        content=body,
        headers={
            "X-Webhook-Signature": sig,
            "X-Event-Id": "evt-hmac-bad",
            "Content-Type": "application/json",
        },
    )

    assert response.status_code == 403
