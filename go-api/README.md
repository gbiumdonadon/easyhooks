# EasyHooks Go API and worker

Two binaries built from the same module:

- `cmd/api` — HTTP server (Chi). Admin routes, webhook ingest, WS token issuer,
  WebSocket fan-out endpoint. On startup it seeds the bootstrap admin token
  (`admin:token_hash`) and ensures the work-queue consumer group via
  `XGROUP CREATE events:in webhook-workers $ MKSTREAM`.
- `cmd/worker` — Redis Streams consumer. Loops over `XREADGROUP > events:in`,
  applies idempotency + retry, fans out to `stream:tenant:{uuid}`, routes
  permanent failures to `events:failed`, then `XACK`s the original entry.

## Internal layout

```
internal/
├── config/        Env parsing (caarlos0/env)
├── handler/       HTTP handlers (admin/CreateTenant, IngestWebhook, IssueToken, WSEvents)
├── middleware/    AdminAuth (Redis-backed) + TenantAuth (HMAC / Bearer)
├── observability/ Prometheus counters/histograms + OTel tracing helpers
├── redis/         Client constructor (REDIS_URL / pool size)
├── redisstore/    SeedSuperAdmin / VerifyAdmin (bcrypt over admin:token_hash)
├── security/      bcrypt hashing + HMAC-SHA256 signature verification
├── service/       Per-tenant streams, fan-out manager and the worker pipeline
└── streams/       Work-queue layer (Publish / Reader / Ack / PublishDLQ / EnsureGroup)
```

## Tests

```bash
go test ./...
go test -race ./...
go test -cover ./...
```

The suite is fully self-contained — `miniredis` stands in for Redis. There is no
need to start any external service.

## Running outside Docker (optional)

```bash
docker compose up -d redis      # only Redis is required

export REDIS_URL=redis://localhost:6379/0
export ADMIN_SEED_TOKEN=<secure-random>
export APP_SECRET_KEY=<secure-random>

go run ./cmd/api &
go run ./cmd/worker
```

The API will idempotently seed the admin token and create the consumer group on
first start; the worker can be re-run safely as many times as needed.
