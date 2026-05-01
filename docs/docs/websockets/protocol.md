---
id: protocol
title: Protocol
sidebar_position: 2
description: WebSocket message format and protocol details.
---

# WebSocket Protocol

## Message Format

All messages are JSON-encoded strings. The platform only sends messages (server → client); clients don't send messages back.

### Event Message

```json
{
  "event": "order.created",
  "occurred_at": "2026-05-01T14:30:00Z",
  "data": {
    "order_id": "ord_123",
    "amount_cents": 4990
  }
}
```

The exact payload structure depends on what was sent to the ingestor.

## Connection Lifecycle

1. **Handshake** — Client connects with token in query string
2. **History** — Server sends last 50 events from Redis Stream (if any)
3. **Live** — Server sends new events as they're processed
4. **Keepalive** — Connection stays open indefinitely
5. **Close** — Client or server can close anytime

## Error Handling

- **401** — Missing or invalid token
- **403** — Token is for a different tenant
- **Connection closed** — Token expired, or server restart

Clients should reconnect with a new token if the connection drops.

## Isolation

Each tenant has its own isolated channel. Events from other tenants are never leaked.
