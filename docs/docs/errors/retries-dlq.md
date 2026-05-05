---
id: retries-dlq
title: Retries & DLQ
sidebar_position: 2
description: Retry policy, exponential backoff, and the Redis Streams Dead Letter Queue.
---

# Retries & Dead Letter Queue

## Retry policy

When the worker fails to process an event, it retries with exponential backoff
based on `WORKER_BACKOFF_BASE_MS` (default 100 ms). With `WORKER_MAX_RETRIES=3`
the timeline is:

- **Attempt 1** — immediate.
- **Attempt 2** — after 100 ms.
- **Attempt 3** — after 200 ms.

After all attempts fail the event is routed to the **Dead Letter Queue**
(`events:failed` Redis Stream).

## Idempotency

The worker uses Redis to ensure the same `event_id` is processed only once:

```
SET event_lock:{event_id} "1" NX EX 86400
```

If the SET fails (key already exists) the event is skipped — the duplicate is
counted in the `idempotency_duplicates_total` Prometheus metric.

## Dead Letter Queue

Failed events are appended to the `events:failed` stream (configurable via
`DLQ_STREAM_KEY`). Each entry carries the same fields as the original
`events:in` entry plus `x_original_error` with the last failure message:

| Field | Description |
| --- | --- |
| `tenant_id` | Original tenant UUID |
| `event_id` | Original `X-Event-Id` |
| `payload` | Raw request body |
| `x_original_error` | Error message from the last attempt |

### Inspecting the DLQ

```bash
# Count
docker compose exec redis redis-cli XLEN events:failed

# Read the last 10 entries
docker compose exec redis redis-cli XREVRANGE events:failed + - COUNT 10

# Tail in real time
docker compose exec redis redis-cli XREAD BLOCK 0 STREAMS events:failed '$'
```

## Common failure causes

- Redis connection lost (the worker's `XADD` to the per-tenant stream fails).
- Invalid payload format expected by the business handler.
- Network timeout to a downstream service when extended business handlers are
  plugged in.

## Recovery

To reprocess DLQ entries, replay them into `events:in` with a new `event_id`.
A minimal one-liner using `redis-cli` (Lua not required):

```bash
# WARNING: only run this on a paused worker — it bypasses retries
docker compose exec redis sh -c '
  redis-cli XRANGE events:failed - + COUNT 100 |
    awk "/tenant_id/{tid=\$2} /event_id/{eid=\$2} /payload/{print tid, eid, \$2}"
'
```

For production replay, build a small Go utility (or a `redis-cli` script) that
calls `XADD events:in` for each entry you want to retry, then `XDEL events:failed`
once successfully republished. The receiver should already deduplicate via the
existing idempotency lock — change the `event_id` if you intentionally want a
fresh attempt.

## Observability

| Metric | Use it for |
| --- | --- |
| `webhook_retries_total{attempt="2"}` | Spotting transient failures |
| `webhook_dlq_total{tenant_id,error_type}` | Per-tenant DLQ trend |
| `redis_stream_length{stream="events:failed"}` | DLQ depth |
| `redis_stream_group_pending{stream="events:in",group="webhook-workers"}` | Worker backlog (alert before users notice) |
