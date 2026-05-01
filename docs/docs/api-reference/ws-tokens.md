---
id: ws-tokens
title: WebSocket Tokens
sidebar_position: 3
description: How to obtain ephemeral tokens for WebSocket authentication.
---

# WebSocket Tokens

To connect to the WebSocket endpoint `/ws/events/{tenant_id}`, you must first obtain an ephemeral token from the API.

## Endpoint

```
POST /v1/tokens/{tenant_id}
```

## Authentication

Use the tenant's `secret_key` (obtained at tenant creation):

```
Authorization: Bearer <secret_key>
```

## Response

```json
{
  "token": "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9..."
}
```

The token is valid for 5 minutes (`WS_TOKEN_TTL_SECONDS`).

## Usage

Use the token as a query parameter when connecting to the WebSocket:

```javascript
const ws = new WebSocket(`ws://localhost:8000/ws/events/${tenantId}?token=${token}`);
```

See [WebSockets → Connection](../websockets/connection.md) for complete examples.
