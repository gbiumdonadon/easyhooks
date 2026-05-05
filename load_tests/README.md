# EasyHooks load tests (Grafana k6)

Scripts in `k6/scenarios/` drive HTTP and WebSocket traffic against the API. Tenant credentials are stored in **`.tenant_pool.json`** (gitignored) as a JSON array: `[{ "tenant_id", "secret_key" }, ...]`.

## Prerequisites

- Running EasyHooks stack (`docker compose up -d` from repo root).
- `LOADTEST_ADMIN_TOKEN` (same value as `ADMIN_SEED_TOKEN`) for creating tenants.
- **curl** and **jq** for `scripts/create_tenant_pool.sh`.
- **k6** locally, *or* Docker using `load_tests/Dockerfile` / Compose.

## Quick start

From repo root:

```bash
export LOADTEST_ADMIN_TOKEN="$ADMIN_SEED_TOKEN"
export LOADTEST_API_BASE_URL=http://localhost:8000

cd load_tests
bash scripts/create_tenant_pool.sh
k6 run k6/scenarios/baseline.js
```

### Docker (init + k6)

```bash
# Creates / refreshes pool via loadtest-init (Alpine + curl)
docker compose -f load_tests/docker-compose.loadtest.yml run --rm loadtest-init

# Run a scenario (service name is `k6`; image entrypoint is already `k6`)
docker compose -f load_tests/docker-compose.loadtest.yml run --rm --no-deps \
  -e LOADTEST_API_BASE_URL=http://host.docker.internal:8000 \
  -e TENANT_POOL_FILE=/load_tests/.tenant_pool.json \
  k6 run k6/scenarios/throughput.js
```

On Linux, set `LOADTEST_API_BASE_URL` to a URL reachable from the container (often `http://172.17.0.1:8000` or your host IP instead of `host.docker.internal`).

## Scenarios

| Script | Purpose |
| --- | --- |
| `k6/scenarios/baseline.js` | Ramp VUs, webhook mix + warmup |
| `k6/scenarios/throughput.js` | `constant-arrival-rate` (tune `K6_TARGET_RPS`, `K6_DURATION`) |
| `k6/scenarios/websocket_scale.js` | Many WS connections (`K6_WS_VUS`, `K6_DURATION`) |
| `k6/scenarios/multi_tenant.js` | Spread load across pool |
| `k6/scenarios/stress.js` | Aggressive ramp |
| `k6/scenarios/custom_scenario.js` | Env-tunable (`K6_CUSTOM_TARGET`, `K6_CUSTOM_SLEEP`) |

## Environment variables

| Variable | Description |
| --- | --- |
| `LOADTEST_API_BASE_URL` | API base URL (no trailing slash) |
| `LOADTEST_ADMIN_TOKEN` | Bearer for `POST /admin/tenants` (init image) |
| `LOADTEST_TENANT_COUNT` | Tenants to create (default `50`) |
| `LOADTEST_TENANT_PREFIX` | Name prefix (default `loadtest`) |
| `TENANT_POOL_FILE` | Path to JSON pool (Docker: `/load_tests/.tenant_pool.json`) |

## Grafana

The **EasyHooks Load Test** dashboard shows **application** Prometheus metrics (`http_requests_total`, latency histograms, etc.) while k6 runs. k6’s own metrics are printed to the terminal (or can be wired to remote write separately).

## Make targets (repo root)

See root `Makefile`: `make loadtest-local`, `loadtest-tenants-create`, `loadtest-build`, etc.
