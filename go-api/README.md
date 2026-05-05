# EasyHooks Go API and worker

- `cmd/api` — HTTP server (Chi): admin routes, webhook ingest, WS tokens, WebSocket fan-out.
- `cmd/worker` — Kafka consumer: idempotency, retries, DLQ, Redis pub/sub.

## Tests

```bash
go test ./...
go test -race ./...
```

Integration tests with full Kafka/Postgres are optional; see the root `README.md` for the Docker-based workflow.
