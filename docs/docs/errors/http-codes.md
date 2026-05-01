---
id: http-codes
title: HTTP Status Codes
sidebar_position: 1
description: HTTP status codes returned by the API.
---

# HTTP Status Codes

## Success

- **200 OK** — Request successful, body contains data
- **201 Created** — Resource created successfully
- **202 Accepted** — Event accepted and queued

## Client Errors

- **400 Bad Request** — Malformed request (missing required header, invalid JSON)
- **401 Unauthorized** — Missing credentials
- **403 Forbidden** — Invalid credentials or cross-tenant access attempt
- **404 Not Found** — Resource doesn't exist
- **422 Unprocessable Entity** — Invalid UUID or non-parseable JSON

## Server Errors

- **500 Internal Server Error** — Unexpected server error
- **503 Service Unavailable** — Dependent service (Kafka, Redis, Postgres) unavailable

All error responses include a JSON body:

```json
{
  "detail": "Human-readable error message"
}
```
