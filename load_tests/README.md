# EasyHooks load tests (Grafana k6)

Scripts in `k6/scenarios/` drive HTTP and WebSocket traffic against the API.
Tenant credentials are stored in **`.tenant_pool.json`** (gitignored) as a JSON
array: `[{ "tenant_id", "secret_key" }, ...]`.

## Prerequisites

- Running EasyHooks stack (`docker compose up -d` from repo root). Only the
  application stack is needed — the optional monitoring compose file is **not**
  a prerequisite.
- `LOADTEST_ADMIN_TOKEN` (same value as `ADMIN_SEED_TOKEN`) for creating
  tenants.
- **curl** and **jq** for `scripts/create_tenant_pool.sh`.
- **k6** locally, *or* Docker using `load_tests/Dockerfile` / Compose.

## Quick start

```bash
docker compose up -d
docker compose -f load_tests/docker-compose.loadtest.yml run --rm k6 \
  run k6/scenarios/baseline.js
```

The `loadtest-init` step (which provisions the tenant pool) runs automatically
before `k6` starts, since the compose file declares it as a dependency.

### Run from the host (no Docker for k6)

```bash
export LOADTEST_ADMIN_TOKEN="$ADMIN_SEED_TOKEN"
export LOADTEST_API_BASE_URL=http://localhost:8000

cd load_tests
bash scripts/create_tenant_pool.sh
k6 run k6/scenarios/baseline.js
```

## Scenarios

| Script | Purpose |
| --- | --- |
| `k6/scenarios/baseline.js` | Ramp VUs, webhook mix |
| `k6/scenarios/throughput.js` | `constant-arrival-rate` (tune `K6_TARGET_RPS`, `K6_DURATION`) |
| `k6/scenarios/websocket_scale.js` | Many WS connections (`K6_WS_VUS`, `K6_DURATION`) |
| `k6/scenarios/multi_tenant.js` | Spread load across the pool |
| `k6/scenarios/custom_scenario.js` | Env-tunable (`K6_CUSTOM_TARGET`, `K6_CUSTOM_SLEEP`) |
| `k6/scenarios/combined_baseline_ws.js` | Publisher HTTP + consumidores WS no mesmo run, mede latência fim-a-fim de entrega via `ws_event_latency_ms` (`K6_WS_VUS`, `K6_PUB_VUS`, `K6_WS_WARMUP`, `K6_PUB_DURATION`, `K6_TOTAL_DURATION`) |
| `k6/scenarios/failure_tracking.js` | Injeta uma fração configurável (`LOADTEST_FAILURE_RATIO`, default 10%) de requisições inválidas (HMAC errada, sem `X-Event-Id`, tenant inexistente) e grava cada falha observada no stream Redis `loadtest:dlq` para análise posterior. Veja [Investigando falhas](#investigando-falhas-loadtestdlq) |

## Environment variables

| Variable | Description |
| --- | --- |
| `LOADTEST_API_BASE_URL` | API base URL (no trailing slash). Defaults to `http://app:8000` inside Docker. |
| `LOADTEST_ADMIN_TOKEN` | Bearer for `POST /admin/tenants` (init image) |
| `LOADTEST_TENANT_COUNT` | Tenants to create (default `50`) |
| `LOADTEST_TENANT_PREFIX` | Name prefix (default `loadtest`) |
| `TENANT_POOL_FILE` | Path to JSON pool (Docker: `/load_tests/.tenant_pool.json`) |
| `LOADTEST_REDIS_URL` | Redis a partir do container k6 (default `redis://redis:6379/0`). Usado por `failure_tracking.js`. |
| `LOADTEST_DLQ_STREAM` | Stream onde o k6 grava falhas (default `loadtest:dlq`). |
| `LOADTEST_DLQ_MAX_LEN` | Cap aproximado do stream `loadtest:dlq` (`MAXLEN ~`, default `50000`). |
| `LOADTEST_FAILURE_RATIO` | Fração de requisições intencionalmente inválidas no `failure_tracking.js` (default `0.10`). |
| `LOADTEST_WORKER_DLQ_STREAM` | DLQ do worker observada (default `events:failed`). XLEN é amostrado em setup/teardown. |

## Investigando falhas (`loadtest:dlq`)

O cenário `failure_tracking.js` mantém um *load-test DLQ* dedicado em
`loadtest:dlq` (separado da DLQ do worker `events:failed`). Toda resposta
não-202, timeout ou erro de transporte vira uma entrada no stream com:

`scenario`, `kind` (`bad_signature` | `missing_event_id` | `unknown_tenant` |
`valid` | …), `injected` (`true`/`false`), `tenant_id`, `event_id`, `status`,
`error`, `error_code`, `classification`, `latency_ms`, `body_excerpt`, `vu`,
`iter`, `ts`.

Inspecione depois do run com:

```bash
docker compose exec redis redis-cli XLEN loadtest:dlq
docker compose exec redis redis-cli XREVRANGE loadtest:dlq + - COUNT 20
docker compose exec redis redis-cli XINFO STREAM loadtest:dlq
# DLQ "real" do worker (eventos que esgotaram retries)
docker compose exec redis redis-cli XLEN events:failed
docker compose exec redis redis-cli XREVRANGE events:failed + - COUNT 20
```

Para limpar o stream entre runs:

```bash
docker compose exec redis redis-cli DEL loadtest:dlq
```

O stream é capado em `LOADTEST_DLQ_MAX_LEN` (default `50000`) via
`XADD ... MAXLEN ~ N`, mesmo padrão usado em
[`go-api/internal/streams/queue.go`](../go-api/internal/streams/queue.go).

## What to watch while running

- **Application metrics** are scraped by Prometheus when the optional
  monitoring stack is up:
  ```bash
  docker compose -f docker-compose.yml -f docker-compose.monitoring.yml up -d
  ```
  Then open the **EasyHooks Load Test** Grafana dashboard at
  <http://localhost:3000>. The `Stream Pending Backlog` panel surfaces
  `redis_stream_group_pending{stream="events:in",group="webhook-workers"}`
  while load is sustained — if it grows unbounded, the worker is saturating.
- **Quick CLI checks** (no monitoring stack required):
  ```bash
  docker compose exec redis redis-cli XLEN events:in
  docker compose exec redis redis-cli XPENDING events:in webhook-workers
  docker compose exec redis redis-cli XLEN events:failed
  ```

## Make targets (repo root)

See root `Makefile`: `make loadtest-local`, `loadtest-tenants-create`,
`loadtest-build`, etc.
