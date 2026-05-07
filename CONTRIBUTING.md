# Contributing to EasyHooks

[🇧🇷 Versão em Português](CONTRIBUTING.pt-br.md)

First of all, thank you for your interest in contributing to **EasyHooks**!
This project aims to be a high-performance, multi-tenant platform for webhook
ingestion, idempotent processing and real-time distribution — and your help
keeps it sharp.

Because performance and reliability are first-class goals, we follow a few
guidelines to keep the codebase clean, fast and predictable.

---

## Table of Contents

- [How can you contribute?](#-how-can-you-contribute)
- [Development Workflow](#-development-workflow)
- [Pull Request Standards](#-pull-request-standards)
- [Code Standards](#-code-standards)
- [Code of Conduct](#-code-of-conduct)

---

## 🛠 How can you contribute?

Beyond reviews and test suggestions, here are the main ways you can help:

### 1. Code review and optimization

We constantly chase **lower latency** and **lower memory usage**. If you spot
a bottleneck — hot loops, unnecessary allocations, lock contention or
suboptimal Redis pipelining — open an Issue or send a PR.

Particularly welcome:

- Reducing per-request allocations on the ingest path (`/v1/webhooks/:id`).
- Tuning Redis client usage (pipelines, `XADD`/`XREADGROUP` batching).
- Improving the worker's idempotency lock and retry/backoff logic.
- WebSocket fan-out efficiency (per-tenant streams and subscriber buffers).

### 2. Test scenarios and benchmarks

EasyHooks must be resilient. Contributions are welcome in:

- **Load tests** — new scenarios under `load_tests/k6/` (Grafana k6).
- **Chaos engineering** — scenarios that exercise resilience when Redis
  becomes slow, drops connections, or recovers from a restart.
- **Edge cases** — unit tests (`go-api/**/*_test.go`) for malformed payloads,
  HMAC mismatches, idempotency races, network instability and DLQ paths.
- **Capacity benchmarks** — extending `load_tests/scripts/run_capacity_benchmark.ps1`
  or adding equivalent shell scripts.

### 3. Documentation and examples

- Improving `README.md`, `README.pt-br.md` and the Docusaurus site under `docs/`.
- New "Getting Started" guides or tutorials.
- Conceptual SDK examples in different languages (Node.js, Python, Go, etc.)
  showing how to sign and send webhooks (HMAC) or consume the WebSocket
  fan-out.

### 4. Issue triage

Helping reproduce bugs reported by other users, labelling issues and
confirming whether problems still exist on `main` keeps the backlog healthy.

---

## 🚀 Development Workflow

### Local Setup

1. **Fork** the repository.
2. **Clone** your fork:
   ```bash
   git clone https://github.com/<your-username>/easyhooks.git
   cd easyhooks
   ```
3. **Set up the environment**:
   ```bash
   cp .env.example .env
   # Edit .env and set ADMIN_SEED_TOKEN and APP_SECRET_KEY (see README.md)
   ```
4. **Bring up the dependencies** (Redis + API + worker + docs):
   ```bash
   docker compose up -d
   docker compose ps
   ```
5. **Verify the stack**:
   - API health: <http://localhost:8000/health>
   - Redis: `docker compose exec redis redis-cli ping` → `PONG`
6. **Make sure all current tests pass** before changing anything:
   ```bash
   cd go-api
   go test ./...
   go test -race ./...
   ```

### Optional: observability and load tests

```bash
# Observability stack (Prometheus, Grafana, Jaeger, redis-exporter)
docker compose -f docker-compose.yml -f docker-compose.monitoring.yml up -d

# Load tests (Grafana k6)
docker compose -f load_tests/docker-compose.loadtest.yml run --rm k6 \
  run k6/scenarios/baseline.js
```

See `load_tests/README.md` for the full load-testing guide.

---

## 📦 Pull Request Standards

When opening a PR, please:

- **Keep it atomic.** A PR should fix one bug or add one feature. Split large
  refactors into reviewable chunks.
- **Write a clear description.** Explain the **why** of the change, not just
  the **what**. Link the related issue when applicable.
- **Provide performance evidence** when the change is an optimization. Include
  before/after numbers from `go test -bench`, k6 summaries (e.g.
  `load_tests/reports/baseline-summary.json`), or Grafana screenshots from the
  `EasyHooks Load Test` dashboard.
- **Add or update tests.** New behaviour should be covered by `_test.go`
  files under `go-api/`. The unit suite uses `miniredis`, so it does not
  require a live Redis instance.
- **Run the full test suite locally**:
  ```bash
  cd go-api && go test ./... && go test -race ./...
  ```
- **Rebuild the docs** if you touched anything under `docs/`:
  ```bash
  cd docs && npm run build
  ```
- **Update the README** (both `README.md` and `README.pt-br.md`) when you
  change user-facing behaviour, env vars or capacity profiles.

---

## 📏 Code Standards

### Idempotency

Every change to the message flow must preserve the **idempotent** nature of
the server. The worker relies on `SET NX event_lock:<event-id>` to deduplicate
events; do not bypass or weaken this guarantee. Always honour the
`X-Event-Id` header on the ingestion path.

### Logs and observability

- Use **structured logs** (matching the existing logger). Include
  `tenant_id`, `event_id` and `stream_id` whenever they are available.
- Don't log secrets (HMAC keys, bearer tokens, full payloads of unknown size).
- Add or update **Prometheus metrics** when you introduce a new code path
  worth measuring (latency histograms, counters for retries/DLQ, etc.).
- Add **OpenTelemetry spans** for new operations that cross service
  boundaries (HTTP → Redis → Worker → WebSocket).

### Error handling

Don't swallow errors. Choose the right strategy for the context:

- **Retry with exponential backoff** for transient failures (configured via
  `WORKER_BACKOFF_BASE_MS` / `WORKER_MAX_RETRIES`).
- **Dead Letter Queue** (`events:failed`) for permanent failures. Always
  include the `x_original_error` field so operators can investigate.
- **HTTP 4xx/5xx** with explicit error codes for client-facing failures.
  Never leak internal stack traces to clients.

### Backpressure and capacity

The server prioritises integrity over throughput. Under saturation it should
**reject new requests with HTTP 429** rather than crash. Any change that
affects the ingest path must respect the load shedder
(`INGEST_MAX_QUEUE_DEPTH`, `QUEUE_DEPTH_LOW_WATER_PCT`) and the existing
capacity profiles (`small` / `medium` / `large` / `custom`).

### Go style

- Follow `gofmt` / `goimports`. CI may reject unformatted code.
- Prefer the standard library plus the existing dependencies (`chi`,
  `go-redis/v9`, `testify`, `miniredis`). Avoid pulling in new heavy
  dependencies without prior discussion.
- Avoid premature abstractions; mirror the patterns already in `go-api/`.

### Comments

Comments should explain **non-obvious intent, trade-offs or constraints**.
Do not add narrating comments like `// increment counter` — let the code
speak for itself.

---

## ⚖️ Code of Conduct

Be respectful and collaborative. We're all here to learn and to build
something useful for the community. Assume good intent, give constructive
feedback, and prefer concrete suggestions over vague criticism.

### Conventional Commits

We use **Conventional Commits** for commit messages and PR titles:

| Prefix | When to use |
| --- | --- |
| `feat:` | new user-facing feature |
| `fix:` | bug fix |
| `perf:` | performance improvement (include numbers in the PR) |
| `refactor:` | code change that neither fixes a bug nor adds a feature |
| `test:` | adding or updating tests |
| `docs:` | documentation only |
| `chore:` | tooling, build, dependencies, CI |

Example:

```
perf: reduce per-request allocations on /v1/webhooks/:id
```

---

Thanks again for contributing! If you're unsure about the scope of a change,
open a draft PR or an issue first — we're happy to discuss the approach
before you invest time in the implementation.
