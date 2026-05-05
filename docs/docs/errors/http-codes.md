---
id: http-codes
title: HTTP Status Codes
sidebar_position: 1
description: HTTP status codes returned by the API.
---

# HTTP Status Codes

## Success

- **200 OK** — Request successful, body contains data.
- **201 Created** — Resource created successfully (e.g. a new tenant).
- **202 Accepted** — Event accepted and appended to the `events:in` Redis Stream.

## Client errors

- **400 Bad Request** — Malformed request: invalid `tenant_id` UUID, missing `X-Event-Id` header or invalid JSON body.
- **401 Unauthorized** — Missing credentials, or admin token has not been seeded yet (`Admin not provisioned`).
- **403 Forbidden** — Invalid credentials, wrong HMAC signature, or cross-tenant access attempt.
- **404 Not Found** — Resource doesn't exist.

## Server errors

- **500 Internal Server Error** — Unexpected server error (for example an XADD failure on `events:in`).
- **503 Service Unavailable** — Redis is unreachable or the admin bootstrap step has not finished yet.

All error responses include a JSON body:

```json
{
  "detail": "Human-readable error message"
}
```

All error responses include a JSON body:

```json
{
  "detail": "Human-readable error message"
}
```
