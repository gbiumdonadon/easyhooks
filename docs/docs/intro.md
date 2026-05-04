---
id: intro
title: Overview
slug: /
sidebar_position: 0
description: Overview of Easyhooks — multi-tenant ingestion, idempotent processing, and real-time distribution.
---

# Easyhooks

Multi-tenant platform for **ingestion, idempotent processing, and real-time distribution** of webhooks. Designed to receive events from multiple clients, guarantee *at-least-once* delivery (with internal exactly-once), and push processed events to frontends and downstream systems via WebSocket.

## High-Level Architecture

```mermaid
flowchart LR
    Admin[Admin] -->|"POST /admin/tenants"| API[Go API (Chi)]
    API -->|"tenant_id + secret"| Admin
    Client[Client] -->|"POST /v1/webhooks/:id<br/>+ HMAC"| API
    API -->|"webhooks.inbound"| Kafka[(Kafka)]
    Kafka --> Worker[Worker]
    Worker -->|"PUBLISH<br/>tenant_events:id"| Redis[(Redis Pub/Sub)]
    Worker -->|"3x failure"| DLQ[(webhooks.dlq)]
    Client -->|"POST /v1/tokens/:id"| API
    API -->|"WS token"| Client
    Client -->|"WS /ws/events/:id"| API
    Redis --> API
    API -->|"send_text"| Client
```

## Components

- **API (`app`)** — Go/Chi; exposes Admin API, webhook ingestor, WS token issuer, and WebSocket endpoint.
- **Worker** — Dedicated Kafka consumer; applies idempotency (Redis), exponential retry, DLQ, and Pub/Sub publishing.
- **Postgres** — Persistence for tenants and admins.
- **Redis** — Credentials cache (`tenant_auth:{id}`, `tenant_hmac_key:{id}`), idempotency locks (`event_lock:{event_id}`), and Pub/Sub channels (`tenant_events:{id}`).
- **Kafka** — Event buffer: `webhooks.inbound` (input) + `webhooks.dlq` (Dead Letter Queue).

## Guarantees

- **Isolated Multi-tenancy** — Each tenant has unique credentials and its own Pub/Sub channel. Cross-tenant attempts are rejected with `403`.
- **Idempotency** — Same `X-Event-Id` sent N times is processed only once.
- **Resilience** — Up to 3 processing attempts with exponential backoff; terminal failures go to DLQ with preserved headers.
- **Real-time** — Processed events are published to Redis Pub/Sub and delivered to connected WebSocket clients in milliseconds.
- **Security** — HMAC-SHA256 over raw body + ephemeral tokens for WebSocket (HMAC of `APP_SECRET_KEY`).

## Getting Started

1. **[Quick Start → Authentication](./getting-started/authentication.md)** — create a tenant and get credentials.
2. **[Quick Start → First Event](./getting-started/first-event.md)** — send your first webhook in ~3 minutes.
3. **[API Reference → Ingestor](./api-reference/ingestor.md)** — complete reference of the main endpoint.
4. **[API Reference → HMAC Security](./api-reference/hmac-security.md)** — implement secure signing with examples in Python, Node, bash, and Go.
5. **[WebSockets → Connection](./websockets/connection.md)** — receive real-time events.
6. **[Errors & DLQ → Retries](./errors/retries-dlq.md)** — understand retry policy and how to inspect the DLQ.

## Stack

- **Language:** Go 1.24
- **Framework:** Chi (`go-chi/chi`) + `net/http` stdlib
- **Database driver / Migrations:** `pgx/v5` + `golang-migrate`
- **Messaging:** Apache Kafka (`twmb/franz-go`)
- **Cache / Streams:** Redis 7+ (`go-redis/v9`)
- **Database:** PostgreSQL 16+
- **Tests:** `testing` stdlib + `testify` + `miniredis`
- **Infrastructure:** Docker Compose
