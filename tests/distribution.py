"""Group 6: Distribui\u00e7\u00e3o Segura via WebSockets \u2014 TDD Tests"""
import asyncio
import json
import uuid
from types import SimpleNamespace
from uuid import UUID

import pytest
from httpx import AsyncClient
from httpx_ws import WebSocketDisconnect, aconnect_ws

from src.services.ws_token import (
    InvalidToken,
    create_signed_token,
    verify_signed_token,
)
from src.services.event_streams import publish_tenant_event
from src.worker import make_redis_streams_handler


def test_should_create_and_verify_signed_token():
    tid = uuid.uuid4()
    token = create_signed_token(tid, ttl_seconds=60)
    assert verify_signed_token(token) == tid


def test_should_reject_expired_token():
    token = create_signed_token(uuid.uuid4(), ttl_seconds=-1)
    with pytest.raises(InvalidToken):
        verify_signed_token(token)


def test_should_reject_tampered_token():
    token = create_signed_token(uuid.uuid4(), ttl_seconds=60)
    payload, sig = token.split(".")
    with pytest.raises(InvalidToken):
        verify_signed_token(payload + "." + "x" * len(sig))


def test_should_reject_malformed_token():
    with pytest.raises(InvalidToken):
        verify_signed_token("not-a-valid-token")


async def _create_tenant(client: AsyncClient, admin_token: str, name: str):
    resp = await client.post(
        "/admin/tenants",
        json={"name": name},
        headers={"Authorization": f"Bearer {admin_token}"},
    )
    assert resp.status_code == 201
    return resp.json()["tenant_id"], resp.json()["secret_key"]


@pytest.mark.asyncio
async def test_should_issue_ws_token_for_authenticated_tenant(
    async_client: AsyncClient,
    admin_token: str,
):
    tenant_id, secret = await _create_tenant(async_client, admin_token, "WS Tenant")

    resp = await async_client.post(
        f"/v1/tokens/{tenant_id}",
        headers={"Authorization": f"Bearer {secret}"},
    )

    assert resp.status_code == 200
    body = resp.json()
    assert "token" in body
    assert "expires_in" in body
    assert verify_signed_token(body["token"]) == UUID(tenant_id)


@pytest.mark.asyncio
async def test_should_return_401_when_issuing_token_without_credentials(
    async_client: AsyncClient,
):
    resp = await async_client.post(f"/v1/tokens/{uuid.uuid4()}")
    assert resp.status_code == 401


@pytest.mark.asyncio
async def test_should_return_403_when_issuing_token_with_wrong_tenant_secret(
    async_client: AsyncClient,
    admin_token: str,
):
    tenant_a_id, _ = await _create_tenant(async_client, admin_token, "Tenant A")
    _, secret_b = await _create_tenant(async_client, admin_token, "Tenant B")

    resp = await async_client.post(
        f"/v1/tokens/{tenant_a_id}",
        headers={"Authorization": f"Bearer {secret_b}"},
    )

    assert resp.status_code == 403


@pytest.mark.asyncio
async def test_should_reject_websocket_connection_for_wrong_tenant(
    async_client: AsyncClient,
    admin_token: str,
):
    tenant_a_id, _ = await _create_tenant(async_client, admin_token, "Tenant A WS")
    tenant_b_id, _ = await _create_tenant(async_client, admin_token, "Tenant B WS")

    token_a = create_signed_token(UUID(tenant_a_id), ttl_seconds=60)

    with pytest.raises(WebSocketDisconnect) as exc_info:
        async with aconnect_ws(
            f"http://testserver/ws/events/{tenant_b_id}?token={token_a}",
            async_client,
        ) as ws:
            await ws.receive_text()

    assert exc_info.value.code == 1008


@pytest.mark.asyncio
async def test_should_accept_websocket_connection_for_matching_tenant(
    async_client: AsyncClient,
    admin_token: str,
):
    tenant_id, _ = await _create_tenant(async_client, admin_token, "Tenant Match WS")
    token = create_signed_token(UUID(tenant_id), ttl_seconds=60)

    async with aconnect_ws(
        f"http://testserver/ws/events/{tenant_id}?token={token}",
        async_client,
    ) as ws:
        assert ws is not None


@pytest.mark.asyncio
async def test_should_reject_websocket_connection_without_token(
    async_client: AsyncClient,
    admin_token: str,
):
    tenant_id, _ = await _create_tenant(async_client, admin_token, "Tenant No Token")

    with pytest.raises(WebSocketDisconnect) as exc_info:
        async with aconnect_ws(
            f"http://testserver/ws/events/{tenant_id}",
            async_client,
        ) as ws:
            await ws.receive_text()

    assert exc_info.value.code == 1008


@pytest.mark.asyncio
async def test_should_deliver_stream_event_to_connected_websocket(
    async_client: AsyncClient,
    admin_token: str,
    redis_client,
):
    tenant_id, _ = await _create_tenant(async_client, admin_token, "Tenant Stream")
    token = create_signed_token(UUID(tenant_id), ttl_seconds=60)

    async with aconnect_ws(
        f"http://testserver/ws/events/{tenant_id}?token={token}",
        async_client,
    ) as ws:
        # Wait for WebSocket to be ready and receive any history
        await asyncio.sleep(0.1)
        
        # Publish event to stream
        await publish_tenant_event(
            redis_client,
            UUID(tenant_id),
            b'{"event":"order.created"}',
        )
        
        # Should receive the event
        msg = await asyncio.wait_for(ws.receive_text(), timeout=2.0)
        assert json.loads(msg) == {"event": "order.created"}


@pytest.mark.asyncio
async def test_should_not_leak_other_tenant_stream_event(
    async_client: AsyncClient,
    admin_token: str,
    redis_client,
):
    tenant_a_id, _ = await _create_tenant(async_client, admin_token, "Tenant Iso A")
    tenant_b_id, _ = await _create_tenant(async_client, admin_token, "Tenant Iso B")
    token_a = create_signed_token(UUID(tenant_a_id), ttl_seconds=60)

    async with aconnect_ws(
        f"http://testserver/ws/events/{tenant_a_id}?token={token_a}",
        async_client,
    ) as ws:
        await asyncio.sleep(0.2)
        
        # Publish to tenant B's stream
        await publish_tenant_event(
            redis_client,
            UUID(tenant_b_id),
            b'{"event":"leak.attempt"}',
        )

        # Tenant A should not receive tenant B's event
        with pytest.raises(asyncio.TimeoutError):
            await asyncio.wait_for(ws.receive_text(), timeout=0.5)


@pytest.mark.asyncio
async def test_default_business_handler_publishes_to_tenant_stream(
    redis_client,
):
    record = SimpleNamespace(
        value=b'{"event":"x"}',
        headers=(("event_id", b"e-stream-1"), ("tenant_id", b"00000000-0000-0000-0000-000000000099")),
    )

    handler = make_redis_streams_handler(redis_client)
    await handler(record)

    # Verify event was added to stream
    from src.services.event_streams import tenant_stream_key
    
    stream_key = tenant_stream_key("00000000-0000-0000-0000-000000000099")
    results = await redis_client.xrange(stream_key, "-", "+")
    
    assert len(results) >= 1
    _msg_id, fields = results[-1]
    payload = fields.get(b"payload") or fields.get("payload")
    assert payload == b'{"event":"x"}'
