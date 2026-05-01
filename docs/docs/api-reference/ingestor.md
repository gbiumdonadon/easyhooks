---
id: ingestor
title: Ingestor
sidebar_position: 1
description: POST /v1/webhooks/{tenant_id} — public endpoint for event reception.
---

# Ingestor `POST /v1/webhooks/{tenant_id}`

The ingestor is the **only** public endpoint that accepts events from the client's system. It authenticates, persists to Kafka, and returns `202 Accepted` in a few milliseconds.

## URL

```
POST /v1/webhooks/{tenant_id}
```

`tenant_id` is the UUID returned in [`POST /admin/tenants`](../getting-started/authentication.md).

## Headers

### Authentication (required — choose **one** of the two methods)

- `X-Webhook-Signature: sha256=<hex>` — HMAC-SHA256 signature of the raw body. See [HMAC Security](./hmac-security.md). **Recommended** for external integrations.
- `Authorization: Bearer <secret_key>` — simple alternative (sends the secret in plaintext). Useful for manual testing or internal tooling.

> If both headers are sent, `X-Webhook-Signature` takes precedence.

### Other headers

- `X-Event-Id: <string>` (**required**) — unique event identifier. Used for idempotency: the same `event_id` sent twice is processed only once. UUID, ULID, or payload hash recommended.
- `Content-Type: application/json` (**required**) — only JSON is accepted.

## Body

Arbitrary JSON. The platform doesn't validate the schema; it only ensures it's parseable JSON.

```json
{
  "event": "order.created",
  "occurred_at": "2026-04-30T22:00:00Z",
  "data": {
    "order_id": "ord_123",
    "amount_cents": 4990,
    "currency": "BRL"
  }
}
```

## Responses

- `202 Accepted` — event accepted and queued in Kafka.

  ```json
  { "status": "accepted", "tenant_id": "f1a2b3c4-..." }
  ```

- `400 Bad Request` — `X-Event-Id` header missing or empty.
- `401 Unauthorized` — no credentials sent (`Authorization` and `X-Webhook-Signature` missing), or unknown tenant.
- `403 Forbidden` — invalid credentials (Bearer doesn't match the tenant) or incorrect HMAC signature.
- `422 Unprocessable Entity` — `tenant_id` is not a valid UUID, or body is not parseable JSON.

Full details in [Errors & DLQ → HTTP Codes](../errors/http-codes.md).

## Complete Example

```bash
BODY='{"event":"order.created","data":{"id":1}}'
SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" -hex | awk '{print $2}')"

curl -X POST "http://localhost:8000/v1/webhooks/$TENANT_ID" \
  -H "X-Webhook-Signature: $SIG" \
  -H "X-Event-Id: $(uuidgen)" \
  -H "Content-Type: application/json" \
  -d "$BODY"
```

## What happens after `202`

1. The API publishes to Kafka (`webhooks.inbound`) with headers `tenant_id` and `event_id`.
2. The `worker` consumes, validates idempotency via Redis (`SET event_lock:{event_id} NX EX`).
3. On success, publishes to Pub/Sub channel `tenant_events:{tenant_id}` — anyone connected via [WebSocket](../websockets/connection.md) receives it immediately.
4. On failure, up to `WORKER_MAX_RETRIES` attempts are made with exponential backoff; when exhausted, the message is routed to `webhooks.dlq` ([details](../errors/retries-dlq.md)).
