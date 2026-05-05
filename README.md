# Easyhooks

[🇧🇷 Portuguese version](README.pt-br.md)

Multi-tenant platform for **ingestion, idempotent processing, and real-time distribution** of webhooks. Built with Go (Chi) + Kafka + Redis + PostgreSQL, featuring WebSocket pub/sub for push delivery to end clients.

> **Full product documentation:** Check the Docusaurus site at `http://localhost:3001` (starts via `docker compose up -d`). Content organized in Quick Start, API Reference, WebSockets, and Error Handling sections.

---

## Table of Contents

- [Architecture](#architecture)
- [Stack](#stack)
- [Prerequisites](#prerequisites)
- [Quick Start (5 minutes)](#quick-start-5-minutes)
- [URLs and Ports](#urls-and-ports)
- [Environment Variables](#environment-variables)
- [Observability](#observability)
- [Testing](#testing)
- [Documentation](#documentation)
- [Contributing](#contributing)
- [License](#license)
- [Disclaimer](#disclaimer)

---

## Architecture

```mermaid
flowchart LR
    Admin[Admin] -->|"POST /admin/tenants"| API[Go API (Chi)]
    Client[Client] -->|"POST /v1/webhooks/:id<br/>+ HMAC"| API
    API -->|"http_requests_total\nhttp_request_duration_seconds"| Prometheus[(Prometheus)]
    API -->|"webhooks.inbound"| Kafka[(Kafka)]
    Kafka --> Worker[Worker]
    Worker -->|"PUBLISH<br/>tenant_events:id"| Redis[(Redis Pub/Sub)]
    Worker -->|"failure 3x"| DLQ[(webhooks.dlq)]
    Client -->|"POST /v1/tokens/:id"| API
    Client -->|"WS /ws/events/:id"| API
    Redis --> API
    API -->|"send_text"| Client
    Prometheus --> Grafana[Grafana]
    K6[k6] -->|"load test"| API
```

- **`app`** — Go/Chi: Admin API, webhook ingestor, WS token issuer, WebSocket endpoint, HTTP metrics middleware.
- **`worker`** — Dedicated Kafka consumer: idempotency (Redis), exponential retry, DLQ, and pub/sub.
- **`docs`** — Docusaurus site (Nginx serving static files).
- **`db`** — PostgreSQL 16 (tenants, admins).
- **`redis`** — Redis 7 (credential cache, idempotency locks, pub/sub).
- **`kafka`** — Kafka 3.7 (KRaft, single-broker for dev).
- **`prometheus`** / **`grafana`** / **`jaeger`** — Full observability stack.

---

## Stack

| Layer | Technology |
| --- | --- |
| Language | Go 1.24 |
| Web framework | Chi (`go-chi/chi`) + `net/http` stdlib |
| Database driver / Migrations | `pgx/v5` + `golang-migrate` |
| Messaging | Apache Kafka (`twmb/franz-go`) |
| Cache / Streams | Redis 7 (`go-redis/v9`) |
| Database | PostgreSQL 16 |
| Observability | Prometheus + Grafana + Jaeger (OpenTelemetry) |
| Load Testing | Grafana k6 (HTTP + WebSocket scenarios under `load_tests/k6/`) |
| Tests | `testing` stdlib + `testify` + `miniredis` |
| Docs | Docusaurus 3 (Node 20 build → Nginx Alpine runtime) |
| Infra | Docker Compose |

---

## Prerequisites

- **Docker** ≥ 24 + **Docker Compose** v2.
- (Optional for running outside Docker) **Go 1.24+**, **Node 20+**.
- (Optional, recommended) **`make`**, **`curl`**, **`jq`**, **`openssl`**.

For Windows, use **WSL2** or **Docker Desktop**. Commands below are portable (PowerShell, bash, and zsh).

---

## Quick Start (5 minutes)

### 1. Clone and set up environment

```bash
git clone https://github.com/gbiumdonadon/easyhooks.git
cd easyhooks

# Copy environment template and configure
cp .env.example .env
```

**Important:** Edit `.env` and set secure values for:
- `POSTGRES_PASSWORD`
- `ADMIN_SEED_TOKEN`
- `APP_SECRET_KEY`
- `GRAFANA_ADMIN_PASSWORD`

Generate secure random values:

```bash
# On Linux/macOS/WSL
openssl rand -hex 32

# On Windows PowerShell
[Convert]::ToBase64String((1..32 | ForEach-Object { Get-Random -Maximum 256 }))
```

### 2. Start the stack

```bash
docker compose up -d
docker compose ps        # confirm all services are healthy
```

First startup takes ~2-3 minutes (Go build + base images download). Subsequent starts are almost instant.

### 3. Verify everything is running

- API: <http://localhost:8000/health> (health check).
- Documentation: <http://localhost:3001>.
- Redis: `docker compose exec redis redis-cli ping` → `PONG`.
- Kafka: `docker compose exec kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --list`.

### 4. Create your first tenant

```bash
curl -X POST http://localhost:8000/admin/tenants \
  -H "Authorization: Bearer <YOUR_ADMIN_SEED_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"name":"Acme Inc."}'
```

Response:

```json
{
  "tenant_id": "f1a2b3c4-d5e6-7890-abcd-ef0123456789",
  "secret_key": "a-very-long-base64url-secret-..."
}
```

> The `secret_key` is shown **only once**. Save it securely.

### 5. Send your first event

```bash
export TENANT_ID="<the tenant_id from response>"
export SECRET="<the secret_key from response>"

BODY='{"event":"order.created","data":{"id":1}}'
SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" -hex | awk '{print $2}')"

curl -i -X POST "http://localhost:8000/v1/webhooks/$TENANT_ID" \
  -H "X-Webhook-Signature: $SIG" \
  -H "X-Event-Id: evt-001" \
  -H "Content-Type: application/json" \
  -d "$BODY"
```

Expected response: `HTTP/1.1 202 Accepted`.

### 6. Watch the worker processing

```bash
docker compose logs -f worker
```

You'll see something like:

```
INFO  Acquired idempotency lock for event_id=evt-001 ...
INFO  Published event to tenant channel tenant_events:f1a2b3c4-...
```

For details (HMAC, WebSocket, DLQ, examples in other languages), see the documentation at <http://localhost:3001>.

---

## URLs and Ports

| Service | Local URL | Internal Port | Description |
| --- | --- | --- | --- |
| API | <http://localhost:8000/health> | 8000 | Health check (no bundled Swagger in distroless image) |
| API root | <http://localhost:8000/> | 8000 | Go API (Chi) |
| Metrics | <http://localhost:8000/metrics> | 8000 | Prometheus metrics |
| Documentation | <http://localhost:3001> | 80 | Docusaurus site (Nginx) |
| **Grafana** | <http://localhost:3000> | 3000 | Dashboards (credentials from `.env`) |
| **Prometheus** | <http://localhost:9090> | 9090 | Metrics & queries |
| **Jaeger** | <http://localhost:16686> | 16686 | Distributed tracing |
| PostgreSQL | localhost:5432 | 5432 | user from .env |
| Redis | localhost:6379 | 6379 | no auth (dev) |
| Kafka | localhost:9092 | 9092 | PLAINTEXT listener |

---

## Environment Variables

All variables can be configured via `.env` file in the project root. Copy `.env.example` to `.env` and customize.

| Variable | Description | Example |
| --- | --- | --- |
| `DATABASE_URL` | PostgreSQL connection string | `postgres://webhooks:password@db:5432/webhooks?sslmode=disable` |
| `POSTGRES_USER` | PostgreSQL username | `webhooks` |
| `POSTGRES_PASSWORD` | PostgreSQL password | `change-this-password` |
| `POSTGRES_DB` | PostgreSQL database name | `webhooks` |
| `REDIS_URL` | Redis connection string | `redis://redis:6379/0` |
| `KAFKA_BOOTSTRAP_SERVERS` | Kafka broker addresses | `kafka:9092` |
| `KAFKA_WEBHOOK_TOPIC` | Inbound webhook topic | `webhooks.inbound` |
| `KAFKA_DLQ_TOPIC` | Dead letter queue topic | `webhooks.dlq` |
| `KAFKA_CONSUMER_GROUP` | Consumer group ID | `webhook-workers` |
| `WORKER_MAX_RETRIES` | Max retries before DLQ | `3` |
| `WORKER_BACKOFF_BASE_MS` | Exponential backoff base (ms) | `100` |
| `IDEMPOTENCY_TTL_SECONDS` | Idempotency lock TTL | `86400` |
| `ADMIN_SEED_TOKEN` | Bootstrap admin token **(MUST CHANGE)** | *(generated)* |
| `APP_SECRET_KEY` | WS token signing key **(MUST CHANGE)** | *(generated)* |
| `WS_TOKEN_TTL_SECONDS` | WebSocket token TTL | `300` |
| `TENANT_EVENTS_CHANNEL_PREFIX` | Pub/Sub channel prefix | `tenant_events:` |
| `TENANT_EVENTS_STREAM_PREFIX` | Redis stream prefix | `stream:tenant:` |
| `STREAM_MAX_LEN` | Max stream length | `1000` |
| `STREAM_HISTORY_COUNT` | History count on connect | `50` |
| `CORS_ORIGINS` | Allowed CORS origins | `http://localhost:3001,http://localhost:3000` |
| `SECRET_KEY_BYTES` | Tenant secret entropy | `32` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OpenTelemetry OTLP endpoint | `http://jaeger:4317` |
| `OTEL_SERVICE_NAME` | Service name for tracing | `easyhooks` |
| `METRICS_ENABLED` | Enable Prometheus metrics | `true` |
| `TRACING_ENABLED` | Enable distributed tracing | `true` |
| `TRACING_SAMPLE_RATE` | Tracing sampling rate (0.0–1.0) | `1.0` |
| `GRAFANA_ADMIN_USER` | Grafana admin username | `admin` |
| `GRAFANA_ADMIN_PASSWORD` | Grafana admin password **(MUST CHANGE)** | *(generated)* |
| `GRAFANA_SERVER_ROOT_URL` | Grafana public root URL | `http://localhost:3000` |
| `LOADTEST_ADMIN_TOKEN` | Admin token for load tests (mirrors `ADMIN_SEED_TOKEN`) | *(same as ADMIN_SEED_TOKEN)* |
| `LOADTEST_API_BASE_URL` | Target URL for load tests | `http://localhost:8000` |

> **Production:** Always set `ADMIN_SEED_TOKEN`, `APP_SECRET_KEY`, and `GRAFANA_ADMIN_PASSWORD` via a secret manager. Rotate before promoting. Adjust `TRACING_SAMPLE_RATE` to 0.1–0.2 to reduce overhead.

---

## Observability

EasyHooks includes comprehensive observability with **metrics**, **dashboards**, and **distributed tracing**.

### Quick Access

- **Grafana**: <http://localhost:3000> (credentials: `GRAFANA_ADMIN_USER` / `GRAFANA_ADMIN_PASSWORD` from `.env`) — Pre-configured dashboards
- **Prometheus**: <http://localhost:9090> — Metrics and queries
- **Jaeger**: <http://localhost:16686> — Distributed tracing UI

### Key Metrics

#### 1. Kafka Consumer Lag ⚠️ **MOST CRITICAL**

Shows how many messages are waiting to be processed.

```promql
kafka_consumergroup_lag{consumergroup="webhook-workers"}
```

- **Healthy**: < 100 messages
- **Warning**: 100-500 messages
- **Critical**: > 1000 messages

**If lag is high:** Scale worker horizontally or investigate processing bottlenecks.

#### 2. Error Rate (DLQ)

Percentage of webhooks that failed after all retries.

```promql
rate(webhook_dlq_total[5m]) / rate(kafka_consume_total[5m])
```

- **Healthy**: < 1%
- **Warning**: 1-5%
- **Critical**: > 5%

#### 3. Processing Duration

Time to process each webhook (p95).

```promql
histogram_quantile(0.95, rate(webhook_processing_duration_seconds_bucket[5m]))
```

- **Good**: < 200ms
- **Acceptable**: 200-500ms
- **Slow**: > 500ms

#### 4. HTTP Request Rate

Total incoming requests processed by the API, tagged by endpoint and status code.

```promql
sum(rate(http_requests_total[1m])) by (endpoint, status_code)
```

#### 5. Active WebSocket Connections

Real-time connections per tenant.

```promql
websocket_connections_active
```

### Grafana Dashboards

Three pre-configured dashboards are automatically provisioned:

1. **EasyHooks Overview** — System-wide health: webhook RPS, p95 latency, WebSocket connections, DLQ error rate
2. **Kafka Metrics** — Consumer lag, offsets, throughput
3. **EasyHooks Load Test** — HTTP request rate per endpoint, latency percentiles (p50/p95/p99), error rate by status code, total requests counter

### Distributed Tracing

View complete request flows from API → Kafka → Worker → Redis → WebSocket:

1. Open Jaeger: <http://localhost:16686>
2. Select service: `easyhooks` or `easyhooks-worker`
3. Click "Find Traces"
4. Explore waterfall view for latency breakdown

**Example trace spans:**
- `webhook.ingest` — API receives webhook
- `webhook.validate_hmac` — HMAC signature validation
- `webhook.produce_kafka` — Send to Kafka
- `webhook.process` — Worker processing
- `webhook.idempotency_check` — Duplicate detection
- `webhook.publish_redis` — Pub/sub distribution
- `websocket.send` — Client delivery

### Troubleshooting with Observability

| Problem | Check |
| --- | --- |
| Slow webhooks | Jaeger traces → Find longest span |
| High error rate | Grafana DLQ dashboard → Error types |
| Worker falling behind | Grafana Kafka dashboard → Consumer lag |
| Client not receiving | Jaeger → Look for missing `websocket.send` span |

### Production Recommendations

1. **Set up alerts** for critical metrics (lag, error rate)
2. **Lower sampling rate**: `TRACING_SAMPLE_RATE=0.1` (10%)
3. **Use persistent storage** for Prometheus/Jaeger
4. **Monitor trends** daily, not just current values
5. **Correlate** metrics with deployments and incidents

For detailed documentation, see <http://localhost:3001/observability/monitoring>

---

## Testing

Automated tests live under **`go-api/`** and use the Go `testing` package plus `testify` and `miniredis` where Redis behaviour is needed.

```bash
cd go-api
go test ./...

# Optional: race detector and coverage
go test -race ./...
go test -cover ./...
```

> Integration-style tests that spin up Kafka/Postgres in Docker are not wired in CI yet; use the Docker Compose stack and manual checks, or extend `go test` with something like Testcontainers when you need full-stack parity.

---

## Documentation

The project includes comprehensive documentation via Docusaurus.

### Viewing Documentation

```bash
docker compose up -d docs
# Open http://localhost:3001
```

### Editing Documentation

Documentation files are in `docs/docs/` (pure markdown with Docusaurus frontmatter).

#### Hot reload in dev mode

```bash
cd docs
npm install        # first time only
npm start          # http://localhost:3000 with hot reload
```

#### Validate production build

```bash
cd docs
npm run build      # generates docs/build/
npm run serve      # serves docs/build/ at http://localhost:3000
```

---

## Load Testing

EasyHooks ships with a **Grafana k6** suite under `load_tests/k6/`. Tenant pools are created with shell + `curl`/`jq` (see `load_tests/scripts/create_tenant_pool.sh`).

### Quick run

```bash
# From repo root — stack must be up; set tokens in .env or export
export LOADTEST_ADMIN_TOKEN="$ADMIN_SEED_TOKEN"
export LOADTEST_API_BASE_URL=http://localhost:8000

cd load_tests
./scripts/create_tenant_pool.sh
k6 run k6/scenarios/baseline.js
```

With Docker (no local k6 install):

```bash
docker compose -f load_tests/docker-compose.loadtest.yml run --rm loadtest-init
docker compose -f load_tests/docker-compose.loadtest.yml run --rm k6 run k6/scenarios/baseline.js
```

### Available scenarios

| Scenario | Script | Purpose |
| --- | --- | --- |
| Baseline | `k6/scenarios/baseline.js` | Normal-load ramp |
| Throughput | `k6/scenarios/throughput.js` | Higher sustained RPS |
| WebSocket scale | `k6/scenarios/websocket_scale.js` | Many concurrent WS clients |
| Multi-tenant | `k6/scenarios/multi_tenant.js` | Spread load across tenant pool |
| Stress | `k6/scenarios/stress.js` | Aggressive ramp toward saturation |
| Custom | `k6/scenarios/custom_scenario.js` | Env-tunable smoke / custom mix |

### Metrics in Grafana

While a test is running, open the **EasyHooks Load Test** dashboard at <http://localhost:3000/d/loadtest-overview> to see **application** Prometheus metrics (the dashboard does not ingest k6’s own metrics):

- Request rate per endpoint (RPS)
- Latency percentiles (p50 / p95 / p99)
- HTTP error rate by status code
- Kafka consumer lag (leading indicator of saturation)

### Load Ramp-Up Guide

| Goal | Users | Ramp Up | Expected RPS |
| --- | --- | --- | --- |
| Baseline check | 20 | 5/s | ~40 RPS |
| Moderate stress | 100 | 10/s | ~200 RPS |
| High stress | 500 | 20/s | ~1 000 RPS |
| Saturation search | 2 000 | 20/s | ~3 000+ RPS |

> **Rule of thumb:** Keep ramp-up rate ≤ 20 users/second to avoid connection-reset storms. Watch Kafka consumer lag — if it grows unboundedly, the pipeline is saturating.

See `load_tests/README.md` for the full guide (Docker Compose, `make loadtest-*`, and environment variables).

---

## Contributing

### Tests

1. Add or extend `_test.go` files next to the code under `go-api/`.
2. Run `go test ./...` from `go-api/` until green.
3. Refactor while keeping tests passing.

### Code standards (Go)

- Prefer small, focused packages under `go-api/internal/`.
- Use `context.Context` for cancellation on I/O paths.
- Run `go fmt` / `golangci-lint` if configured in your environment.

### Before opening a PR

```bash
cd go-api && go test ./...
cd docs && npm run build
```

---

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

```
Copyright © 2026 Gustavo Bium Donadon
```

---

## Disclaimer

This project is provided "as is" for study and free use purposes. While it implements production-ready patterns (idempotency, retry logic, DLQ, multi-tenancy), it is primarily intended for educational purposes and as a starting point for webhook infrastructure implementations.

**Use in production at your own risk.** Always conduct thorough security audits, load testing, and customize the system to your specific requirements before deploying to production environments.

---

## Resources

- **Product Documentation:** <http://localhost:3001> (after `docker compose up -d docs`)
- **API health:** <http://localhost:8000/health>

---

**Built with Go, Kafka, Redis, and PostgreSQL**
