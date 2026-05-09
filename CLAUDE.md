# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

All Go commands run from the `go-api/` directory.

```bash
# Build
go build ./...
go build -o /app/api ./cmd/api
go build -o /app/worker ./cmd/worker

# Test (all packages use miniredis — no live Redis required)
go test ./...
go test ./internal/handler/...         # a single package
go test -run TestIngestWebhook ./...   # a single test

# No linter config exists; use go vet
go vet ./...
```

Docker (from repo root):
```bash
docker compose up -d                   # start Redis + api + worker + docs
docker compose build                   # rebuild after Go changes
```

Load tests (from repo root, requires `LOADTEST_ADMIN_TOKEN`):
```bash
make loadtest-validate                 # check environment
make loadtest-local                    # baseline scenario
cd load_tests && bash run-loadtest.sh e2e   # e2e combined scenario
```

## Architecture

EasyHooks is a **webhook fanout system** where Redis Streams are the sole datastore (no Kafka, no SQL). Two binaries ship in the same Docker image:

- **`cmd/api`** — HTTP server (chi router, port 8000). Accepts webhook events, manages tenants, issues WebSocket tokens, serves WebSocket streams.
- **`cmd/worker`** — Consumer loop. Reads from `events:in` via XREADGROUP, fans out to per-tenant streams, handles retries + DLQ.

### Data flow

```
POST /v1/webhooks/{tenant} → events:in (global stream)
                                 ↓
                            worker XREADGROUP
                                 ↓
                      stream:tenant:{id}  (per-tenant stream)
                                 ↓
                    GET /ws/events/{tenant} (WebSocket, XREAD BLOCK)
```

### Redis key space

| Pattern | Type | TTL | Purpose |
|---|---|---|---|
| `admin:token_hash` | String | none | bcrypt hash of bootstrap admin token |
| `tenant_auth:{id}` | String | none | bcrypt hash for tenant Bearer auth |
| `tenant_hmac_key:{id}` | String | none | HMAC-SHA256 secret for signature verify |
| `events:in` | Stream | MAXLEN cap | global ingestion queue |
| `events:failed` | Stream | MAXLEN 10k | dead-letter queue |
| `stream:tenant:{id}` | Stream | MAXLEN cap | per-tenant history for WS clients |
| `event_lock:{event_id}` | String | 1h | idempotency deduplication |
| `auth_session:{id}:{hash}` | String | 5m | two-level Bearer auth cache (avoids bcrypt per-req) |

Redis runs with `--maxmemory 85mb --maxmemory-policy volatile-lru`: keys with TTL (idempotency + auth caches) are evicted automatically under memory pressure; permanent keys (credentials, streams) are never evicted.

### Config & profiles

`internal/config/config.go` parses all env vars. `EASYHOOKS_PROFILE` (small/medium/large/custom) drives memory-scaled defaults — pool sizes, stream max lengths, queue watermarks. Custom profile requires all memory-related vars explicitly.

### Load shedding

`internal/streams/monitor.go` — `QueueDepthMonitor` polls XINFO GROUPS every 1s and exposes an atomic `ShouldShed()` bool. The hot path in `handler/webhooks.go` reads that atomic (zero Redis round-trips) and returns 429 when the consumer-group backlog (`lag + pending`) exceeds the high watermark. Hysteresis prevents flapping. XLEN is intentionally not used as the signal — XACK doesn't decrement XLEN.

### Fan-out optimisation

`internal/service/fanout.go` — `FanoutManager` coalesces multiple WebSocket subscribers on the same tenant into a single XREAD goroutine. Without it each WS connection would issue its own XREAD BLOCK.

### Testing

Tests use `github.com/alicebob/miniredis/v2` in place of a real Redis server. No integration test infrastructure needed. See `internal/handler/handler_test.go` for the `newMiniredisClient` helper pattern used across test files.
