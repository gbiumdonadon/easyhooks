"""Tests for Redis Streams event distribution with history support."""
import asyncio
import json
import uuid
from types import SimpleNamespace
from unittest.mock import patch
from uuid import UUID

import pytest
from httpx import AsyncClient
from httpx_ws import aconnect_ws

from src.config import settings
from src.services.event_streams import (
    publish_tenant_event,
    read_tenant_history,
    stream_tenant_events,
)
from src.worker import make_redis_streams_handler


async def _create_tenant(client: AsyncClient, admin_token: str, name: str):
    """Helper to create a tenant via admin API."""
    response = await client.post(
        "/admin/tenants",
        json={"name": name},
        headers={"Authorization": f"Bearer {admin_token}"},
    )
    assert response.status_code == 201
    data = response.json()
    return data["tenant_id"], data["secret_key"]


@pytest.mark.asyncio
async def test_publish_and_read_history(redis_client):
    """Test publishing events and reading history in correct order."""
    tenant_id = UUID("00000000-0000-0000-0000-000000000001")
    
    # Publish 5 events
    ids = []
    for i in range(5):
        payload = json.dumps({"event": f"test-{i}"}).encode()
        msg_id = await publish_tenant_event(redis_client, tenant_id, payload)
        ids.append(msg_id)
        await asyncio.sleep(0.01)  # Ensure different timestamps
    
    # Read last 3 events
    history = await read_tenant_history(redis_client, tenant_id, count=3)
    
    assert len(history) == 3
    
    # Should be in chronological order (oldest first)
    # Events 2, 3, 4 (last 3 of 0-4)
    for i, event in enumerate(history):
        expected_idx = i + 2
        assert event["id"] in ids
        payload = json.loads(event["payload"])
        assert payload["event"] == f"test-{expected_idx}"


@pytest.mark.asyncio
async def test_stream_only_new_events(redis_client):
    """Test that stream with $ only returns new events."""
    tenant_id = UUID("00000000-0000-0000-0000-000000000002")
    
    # Publish 2 old events
    for i in range(2):
        payload = json.dumps({"event": f"old-{i}"}).encode()
        await publish_tenant_event(redis_client, tenant_id, payload)
    
    # Start streaming with "$" (only new)
    received = []
    
    async def consumer():
        async with stream_tenant_events(redis_client, tenant_id, start_id="$") as messages:
            async for event in messages:
                received.append(json.loads(event["payload"]))
                if len(received) >= 2:
                    break
    
    consumer_task = asyncio.create_task(consumer())
    
    # Wait for stream to start
    await asyncio.sleep(0.1)
    
    # Publish 2 new events
    for i in range(2):
        payload = json.dumps({"event": f"new-{i}"}).encode()
        await publish_tenant_event(redis_client, tenant_id, payload)
        await asyncio.sleep(0.05)
    
    # Wait for consumer to receive them
    try:
        await asyncio.wait_for(consumer_task, timeout=2.0)
    except asyncio.TimeoutError:
        consumer_task.cancel()
    
    # Should only have new events, not old ones
    assert len(received) == 2
    assert received[0]["event"] == "new-0"
    assert received[1]["event"] == "new-1"


@pytest.mark.asyncio
async def test_stream_from_specific_id(redis_client):
    """Test streaming from a specific message ID."""
    tenant_id = UUID("00000000-0000-0000-0000-000000000003")
    
    # Publish 5 events and save IDs
    ids = []
    for i in range(5):
        payload = json.dumps({"event": f"event-{i}"}).encode()
        msg_id = await publish_tenant_event(redis_client, tenant_id, payload)
        ids.append(msg_id)
        await asyncio.sleep(0.01)
    
    # Stream from 3rd event (index 2)
    received = []
    
    async with stream_tenant_events(redis_client, tenant_id, start_id=ids[2], block_ms=500) as messages:
        async for event in messages:
            received.append(json.loads(event["payload"]))
            if len(received) >= 2:  # Should get events 3 and 4
                break
    
    # Should have events after ID[2]: events 3 and 4
    assert len(received) == 2
    assert received[0]["event"] == "event-3"
    assert received[1]["event"] == "event-4"


@pytest.mark.asyncio
async def test_maxlen_trimming(redis_client, monkeypatch):
    """Test that stream respects MAXLEN trimming."""
    monkeypatch.setattr(settings, "STREAM_MAX_LEN", 10)
    
    tenant_id = UUID("00000000-0000-0000-0000-000000000004")
    
    # Publish 20 events (more than MAXLEN)
    for i in range(20):
        payload = json.dumps({"event": f"event-{i}"}).encode()
        await publish_tenant_event(redis_client, tenant_id, payload)
    
    # Read all available history
    history = await read_tenant_history(redis_client, tenant_id, count=100)
    
    # Should have ~10 events (approximate trimming with ~)
    # Allow some flexibility since trimming is approximate
    assert len(history) <= 12, f"Expected ~10 events, got {len(history)}"
    assert len(history) >= 8, f"Expected at least 8 events after trimming, got {len(history)}"
    
    # Oldest event should be from later in the sequence
    first_event = json.loads(history[0]["payload"])
    event_num = int(first_event["event"].split("-")[1])
    assert event_num >= 8, f"Expected oldest event >= 8, got {event_num}"


@pytest.mark.asyncio
async def test_websocket_receives_history_on_connect(
    async_client: AsyncClient,
    admin_token: str,
    redis_client,
):
    """Integration test: WebSocket receives historical events on connect."""
    from src.services.ws_token import create_signed_token
    
    # Create tenant
    tenant_id, _ = await _create_tenant(async_client, admin_token, "History Test")
    tid_uuid = UUID(tenant_id)
    
    # Publish 3 events directly to stream
    for i in range(3):
        payload = json.dumps({"event": f"historical-{i}"}).encode()
        await publish_tenant_event(redis_client, tid_uuid, payload)
    
    # Connect WebSocket
    token = create_signed_token(tid_uuid, ttl_seconds=60)
    
    async with aconnect_ws(
        f"http://testserver/ws/events/{tenant_id}?token={token}",
        async_client,
    ) as ws:
        # Should receive 3 historical events immediately
        messages = []
        for _ in range(3):
            try:
                msg = await asyncio.wait_for(ws.receive_text(), timeout=1.0)
                messages.append(json.loads(msg))
            except asyncio.TimeoutError:
                break
        
        assert len(messages) == 3
        for i in range(3):
            assert messages[i]["event"] == f"historical-{i}"
        
        # Now publish a new event
        payload = json.dumps({"event": "new-live"}).encode()
        await publish_tenant_event(redis_client, tid_uuid, payload)
        
        # Should receive the new event via stream
        try:
            new_msg = await asyncio.wait_for(ws.receive_text(), timeout=2.0)
            new_data = json.loads(new_msg)
            assert new_data["event"] == "new-live"
        except asyncio.TimeoutError:
            pytest.fail("Did not receive new event via stream")


@pytest.mark.asyncio
async def test_multiple_tenants_isolated(redis_client):
    """Test that tenant streams are isolated from each other."""
    tenant_a = UUID("aaaaaaaa-0000-0000-0000-000000000000")
    tenant_b = UUID("bbbbbbbb-0000-0000-0000-000000000000")
    
    # Publish events for both tenants
    for i in range(3):
        payload_a = json.dumps({"tenant": "A", "event": i}).encode()
        payload_b = json.dumps({"tenant": "B", "event": i}).encode()
        await publish_tenant_event(redis_client, tenant_a, payload_a)
        await publish_tenant_event(redis_client, tenant_b, payload_b)
    
    # Read history for tenant A
    history_a = await read_tenant_history(redis_client, tenant_a, count=10)
    assert len(history_a) == 3
    for event in history_a:
        data = json.loads(event["payload"])
        assert data["tenant"] == "A"
    
    # Read history for tenant B
    history_b = await read_tenant_history(redis_client, tenant_b, count=10)
    assert len(history_b) == 3
    for event in history_b:
        data = json.loads(event["payload"])
        assert data["tenant"] == "B"


@pytest.mark.asyncio
async def test_default_business_handler_publishes_to_stream(redis_client):
    """Test that the worker's default handler publishes to streams."""
    tenant_uuid = UUID("00000000-0000-0000-0000-000000000099")
    
    record = SimpleNamespace(
        value=b'{"event":"x"}',
        headers=(("event_id", b"e-stream-1"), ("tenant_id", str(tenant_uuid).encode())),
    )
    
    handler = make_redis_streams_handler(redis_client)
    await handler(record)
    
    # Verify event was added to stream
    from src.services.event_streams import tenant_stream_key
    
    stream_key = tenant_stream_key(tenant_uuid)
    
    # Read from stream
    results = await redis_client.xrange(stream_key, "-", "+")
    
    assert len(results) >= 1
    # Get last message
    _msg_id, fields = results[-1]
    payload = fields.get(b"payload") or fields.get("payload")
    assert payload == b'{"event":"x"}'
