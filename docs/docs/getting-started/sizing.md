---
id: sizing
title: Capacity planning
sidebar_position: 3
description: Choose an EasyHooks profile (small / medium / large) and understand how each tier behaves under load — measured numbers, memory footprint and graceful degradation.
---

# Capacity planning

EasyHooks ships three pre-tuned **profiles** that scale memory limits, Redis pool size, per-tenant stream caps, fanout buffers and ingestion backpressure together. Pick one based on the container memory budget you can give it.

> **Behavioural guarantee.** EasyHooks prioritises server integrity. Under
> extreme load it prefers to **reject new requests with HTTP 429** rather
> than crash the service via OOM. The numbers below reflect that contract.

## Profiles at a glance

| Profile | Recommended container | `GOMEMLIMIT` | `STREAM_MAX_LEN` | `REDIS_POOL_SIZE` | `WS_FANOUT_BUFFER_SIZE` | `INGEST_MAX_QUEUE_DEPTH` |
| --- | --- | --- | --- | --- | --- | --- |
| `small`  | 256 MB | 200 MiB | 1 000  | 50  | 100 | 5 000  |
| `medium` | 512 MB | 450 MiB | 5 000  | 100 | 256 | 25 000 |
| `large`  | 1 GB   | 900 MiB | 10 000 | 200 | 512 | 50 000 |
| `custom` | (yours)| (env)   | (env)  | (env) | (env) | (env)|

Activate a profile via `EASYHOOKS_PROFILE` in `.env`. Any individual env var still wins — set `STREAM_MAX_LEN=20000` on top of `EASYHOOKS_PROFILE=large` and the override is honoured.

## Measured behaviour under saturation

The numbers below come from running `load_tests/scripts/run_capacity_benchmark.ps1` on a single dev box (Windows + Docker Desktop, 100 k6 VUs, 30 s sustained, single tenant, payload ≈ 100 B). The k6 workload exceeds every profile's accept rate on purpose so we can compare backpressure behaviour.

| Profile | Container cap | Total req/s offered | 202 Accepted (30 s) | 429 Shed (30 s) | p95 ingest latency | App RSS |
| --- | --- | --- | --- | --- | --- | --- |
| `small`  | 256 MB | ~4 700 | 5 446  | 135 812 | 1.58 ms | ~26 MiB |
| `medium` | 512 MB | ~4 700 | 28 827 | 112 370 | 1.58 ms | ~26 MiB |
| `large`  | 1 GB   | ~4 700 | 52 169 | 88 903  | 1.66 ms | ~27 MiB |

Reading the table:

- **All three profiles stayed up** — no OOM, no crash, no panics. The server kept answering, just with more 429s on smaller tiers.
- **p95 ingest latency stays sub-2 ms** even at 4 700 req/s offered, on every profile. The 429 path is cheap (atomic read + early return).
- **Accepted volume scales with the queue headroom**: the larger the `INGEST_MAX_QUEUE_DEPTH`, the bigger the burst the API absorbs before backpressure engages.
- **Resident memory stays far below `GOMEMLIMIT`** at this load. The headroom is deliberate — Go's GC works hard to keep us under the limit before the container hits its OS-level cap.

## Why so much headroom?

`GOMEMLIMIT` is **not** a hard ceiling. The Go runtime will exceed it briefly when refusing to grow a goroutine stack would mean a panic. The recommended ratio is therefore `GOMEMLIMIT ≈ 0.8 × container_mem` so the runtime has room to manoeuvre before the OOM killer fires. That is why `small` ships with `GOMEMLIMIT=200MiB` for a 256 MB container, `medium` with 450 MiB / 512 MB, and `large` with 900 MiB / 1 GB.

## Tuning knobs

When the defaults need adjusting, override individual env vars (the profile keeps every other field). The most common ones:

- `INGEST_MAX_QUEUE_DEPTH` — raise it if your worker drains fast and you can afford more burst absorption; lower it if you want sharper 429s under sustained load.
- `QUEUE_DEPTH_LOW_WATER_PCT` — hysteresis. Default 80 % (we re-open ingestion only when `XLEN events:in` drops to 80 % of the high watermark). Set to `100` to release as soon as we dip below high (more flapping); `50` to hold the brake longer.
- `WS_FANOUT_BUFFER_SIZE` — raise it for tenants with many WS clients per stream. When this buffer fills, the slow subscriber is dropped and the metric `websocket_subscriber_dropped_total{reason="buffer_full"}` increments — a clear signal you need a bigger profile or faster clients.
- `GOMEMLIMIT` — advanced override. The native Go env var takes precedence over the profile-derived value. Use it if you need a non-standard ratio (e.g. running alongside a sidecar).

## Reproducing these numbers

```bash
# From the repo root, with Docker Desktop running.
powershell -ExecutionPolicy Bypass `
  -File load_tests/scripts/run_capacity_benchmark.ps1 `
  -DurationSec 30 -Vus 100
```

The script:

1. Builds the app/worker images.
2. For each profile, recreates `app` and `worker` with the matching `mem_limit`.
3. Drains `events:in` so each tier starts from zero.
4. Runs `k6 run --vus 100 --duration 30s k6/scenarios/throughput.js`.
5. Writes per-profile JSON summaries to `load_tests/reports/capacity/` and prints a markdown table.

Run it on your own infrastructure to get numbers calibrated for your CPU, network and Redis disk. Mileage will vary — the **shape** of the curve (small sheds first, large absorbs more burst, p95 stays low) should hold regardless.

## Observability

Three Prometheus metrics let you track the load-shedding behaviour live:

- `webhook_load_shed_total{tenant_id}` — counter of 429-rejected requests.
- `ingest_queue_depth{stream}` — current consumer-group backlog (`lag + pending`) of `events:in`, sampled via `XINFO GROUPS` every `QUEUE_DEPTH_POLL_MS`. This is the actual unprocessed work — not `XLEN` — so it goes back to zero whenever the worker is in sync.
- `ingest_load_shedding_active` — 0/1 gauge reflecting the hysteresis state. Alert on `== 1 for 1m` to be paged when the queue is actually saturated, not on transient blips.

For the WebSocket layer:

- `websocket_subscriber_dropped_total{tenant_id,reason}` — slow subscribers being disconnected. A non-zero rate means you should consider a larger profile or investigate the consumer side.

A constant gauge `easyhooks_profile_info{profile}` exposes the active tier so you can pivot Grafana variables on it.

## What's intentionally not done (yet)

- **`sync.Pool`** for the ingestion path. The current allocation pattern is well within `GOMEMLIMIT` for the measured load. We will revisit if profiling shows the `io.ReadAll(body)` path becomes hot.
- **Dynamic backpressure based on runtime memory pressure.** EasyHooks is opinionated: it picks a static high watermark (queue depth) instead of guessing whether the host is "stressed". This keeps the behaviour predictable across hosts and easy to reason about during incidents.
