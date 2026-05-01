---
id: connection
title: Connection
sidebar_position: 1
description: How to connect to the WebSocket endpoint and receive events.
---

# WebSocket Connection

## 1. Obtain a token

First, get an ephemeral token (valid for 5 minutes):

```bash
curl -X POST http://localhost:8000/v1/tokens/$TENANT_ID \
  -H "Authorization: Bearer $SECRET"
```

Response:

```json
{
  "token": "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9..."
}
```

## 2. Connect to WebSocket

Use the token as a query parameter:

```javascript
const ws = new WebSocket(`ws://localhost:8000/ws/events/${tenantId}?token=${token}`);

ws.onopen = () => console.log('Connected!');
ws.onmessage = (event) => console.log('Received:', event.data);
ws.onerror = (error) => console.error('Error:', error);
ws.onclose = () => console.log('Disconnected');
```

## 3. Receive Events

When an event is processed by the worker, it's published to the tenant's channel and delivered to all connected WebSocket clients:

```json
{
  "event": "order.created",
  "data": {"id": 1}
}
```

## Python Example

```python
import asyncio
import websockets
import requests

# Get token
response = requests.post(
    f"http://localhost:8000/v1/tokens/{tenant_id}",
    headers={"Authorization": f"Bearer {secret}"}
)
token = response.json()["token"]

# Connect
async def listen():
    uri = f"ws://localhost:8000/ws/events/{tenant_id}?token={token}"
    async with websockets.connect(uri) as ws:
        async for message in ws:
            print(f"Received: {message}")

asyncio.run(listen())
```

See [Protocol](./protocol.md) for message format details.
