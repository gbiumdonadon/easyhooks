"""Group 3: Ingestion & Messaging — TDD Tests"""
import json

import pytest
from unittest.mock import AsyncMock
from httpx import AsyncClient


@pytest.mark.asyncio
async def test_should_produce_message_to_kafka_with_tenant_header(
    async_client: AsyncClient,
    admin_token: str,
    kafka_mock: AsyncMock,
):
    # Create a tenant
    resp = await async_client.post(
        "/admin/tenants",
        json={"name": "Kafka Tenant"},
        headers={"Authorization": f"Bearer {admin_token}"},
    )
    assert resp.status_code == 201
    tenant_id = resp.json()["tenant_id"]
    tenant_secret = resp.json()["secret_key"]

    # POST a valid webhook
    webhook_payload = {
        "event": "payment.completed",
        "data": {"payment_id": "pay_123", "amount": 99.99},
    }

    response = await async_client.post(
        f"/v1/webhooks/{tenant_id}",
        json=webhook_payload,
        headers={
            "Authorization": f"Bearer {tenant_secret}",
            "X-Event-Id": "evt-tenant-header-01",
        },
    )

    # HTTP 202 Accepted
    assert response.status_code == 202

    # Kafka producer was called exactly once
    kafka_mock.send.assert_called_once()

    call_kwargs = kafka_mock.send.call_args

    # Topic should be the configured webhook topic
    assert call_kwargs.kwargs.get("topic") == "webhooks.inbound"

    # Message value should contain the webhook payload
    sent_value = call_kwargs.kwargs.get("value")
    assert sent_value is not None
    parsed_value = json.loads(sent_value)
    assert parsed_value["event"] == "payment.completed"
    assert parsed_value["data"]["payment_id"] == "pay_123"

    # tenant_id MUST be in Kafka headers, NOT in the body
    sent_headers = call_kwargs.kwargs.get("headers")
    assert sent_headers is not None

    header_dict = {k: v for k, v in sent_headers}
    assert "tenant_id" in header_dict
    assert header_dict["tenant_id"] == tenant_id.encode()


@pytest.mark.asyncio
async def test_should_return_400_when_x_event_id_missing(
    async_client: AsyncClient,
    admin_token: str,
):
    resp = await async_client.post(
        "/admin/tenants",
        json={"name": "No Event Id Tenant"},
        headers={"Authorization": f"Bearer {admin_token}"},
    )
    assert resp.status_code == 201
    tenant_id = resp.json()["tenant_id"]
    tenant_secret = resp.json()["secret_key"]

    response = await async_client.post(
        f"/v1/webhooks/{tenant_id}",
        json={"event": "order.created", "data": {"order_id": 1}},
        headers={"Authorization": f"Bearer {tenant_secret}"},
    )

    assert response.status_code == 400
    assert "detail" in response.json()


@pytest.mark.asyncio
async def test_should_propagate_event_id_to_kafka_headers(
    async_client: AsyncClient,
    admin_token: str,
    kafka_mock: AsyncMock,
):
    resp = await async_client.post(
        "/admin/tenants",
        json={"name": "Event Id Tenant"},
        headers={"Authorization": f"Bearer {admin_token}"},
    )
    assert resp.status_code == 201
    tenant_id = resp.json()["tenant_id"]
    tenant_secret = resp.json()["secret_key"]

    event_id = "evt-abc-123"
    response = await async_client.post(
        f"/v1/webhooks/{tenant_id}",
        json={"event": "order.created", "data": {"order_id": 2}},
        headers={
            "Authorization": f"Bearer {tenant_secret}",
            "X-Event-Id": event_id,
        },
    )

    assert response.status_code == 202
    kafka_mock.send.assert_called_once()

    sent_headers = kafka_mock.send.call_args.kwargs.get("headers")
    assert sent_headers is not None
    header_dict = {k: v for k, v in sent_headers}
    assert header_dict.get("event_id") == event_id.encode()
    assert header_dict.get("tenant_id") == tenant_id.encode()
