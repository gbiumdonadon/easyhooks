---
id: retries-dlq
title: Retries & DLQ
sidebar_position: 2
description: Retry policy, exponential backoff, and Dead Letter Queue.
---

# Retries & Dead Letter Queue

## Retry Policy

When the worker fails to process an event, it retries with exponential backoff:

- **Attempt 1** — immediate
- **Attempt 2** — after 100ms
- **Attempt 3** — after 300ms

After 3 failures, the event goes to the **Dead Letter Queue** (`webhooks.dlq`).

## Idempotency

The worker uses Redis to ensure the same `event_id` is processed only once:

```
SET event_lock:{event_id} "1" NX EX 86400
```

If the SET fails (key already exists), the event is skipped.

## Dead Letter Queue

Failed events are published to `webhooks.dlq` with headers:

- `x-tenant-id` — original tenant
- `x-event-id` — original event ID
- `x-original-error` — error message from last attempt
- `x-retry-count` — number of attempts made

### Inspecting the DLQ

```bash
docker compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 \
  --topic webhooks.dlq \
  --from-beginning \
  --property print.headers=true
```

## Common Failure Causes

- Redis connection lost
- Invalid payload structure expected by business handler
- Network timeout to external service

## Recovery

To reprocess DLQ messages, consume them and republish to `webhooks.inbound` with a new `event_id`.
