# Observability stack for EasyHooks

This directory holds the optional observability stack: Prometheus, Grafana,
Jaeger and `redis-exporter`. It is shipped as a separate Docker Compose file so
production deployments can opt in (or pick a different observability surface)
without paying the cost in development.

## Quick start

```bash
docker compose up -d                                                      # app stack
docker compose -f docker-compose.yml -f docker-compose.monitoring.yml up -d  # + monitoring
```

- Prometheus: <http://localhost:9090>
- Grafana: <http://localhost:3000> (creds from `.env`)
- Jaeger: <http://localhost:16686>

## Architecture

```
┌─────────────┐
│   Go API    │──── /metrics (Prometheus) ──┐
│   (Chi)     │                              │
└─────────────┘                              ▼
                                       ┌─────────────┐
┌─────────────┐                        │ Prometheus  │
│ Go Worker   │──── OTLP traces ──┐    │ (TSDB)      │
└─────────────┘                    │    └──────┬──────┘
                                   │           │
┌─────────────────────┐            │           ▼
│ redis-exporter      │────────────┼────▶ ┌─────────────┐
│  (XLEN, XPENDING…)  │            │      │   Grafana   │
└─────────────────────┘            │      │ (Dashboards)│
                                   ▼      └─────────────┘
                            ┌─────────────┐
                            │   Jaeger    │
                            │ (Tracing)   │
                            └─────────────┘
```

Both `easyhooks` (API) and `easyhooks-worker` push OTLP spans to the Jaeger
collector. Prometheus scrapes `app:8000/metrics` and `redis-exporter:9121` —
the latter is configured (`REDIS_EXPORTER_CHECK_STREAMS=events:in,events:failed`)
to expose `redis_stream_length` and `redis_stream_group_pending` for the work
queue and DLQ streams.

## Directory layout

```
observability/
├── prometheus/
│   └── prometheus.yml          # Prometheus configuration
├── grafana/
│   ├── provisioning/
│   │   ├── datasources/        # Auto-provisions Prometheus + Jaeger
│   │   └── dashboards/         # Dashboard provisioning config
│   └── dashboards/
│       ├── README.md
│       ├── 01-overview.json    # System-wide overview
│       ├── 02-streams.json     # Redis Streams work queue & DLQ
│       └── 03-loadtest.json    # Load testing dashboard
└── README.md                   # This file
```

## Key metrics to watch

| Metric | Purpose | Healthy |
| --- | --- | --- |
| `redis_stream_group_pending{stream="events:in",group="webhook-workers"}` | Worker backlog | < 100 |
| `redis_stream_length{stream="events:failed"}` | DLQ depth | flat / 0 |
| `rate(stream_consume_total[5m])` | Worker throughput | matches publish rate |
| `rate(webhook_dlq_total[5m]) / rate(stream_consume_total[5m])` | DLQ ratio | < 1 % |
| `histogram_quantile(0.95, rate(webhook_processing_duration_seconds_bucket[5m]))` | Processing p95 | < 200 ms |
| `websocket_connections_active` | WS connections per tenant | n/a |

### Application metrics (exposed on `/metrics`)

- `webhook_requests_total` — by tenant + status
- `webhook_processing_duration_seconds` — histogram
- `webhook_retries_total` — by attempt
- `webhook_dlq_total` — DLQ counter
- `idempotency_duplicates_total` — duplicates dropped
- `websocket_connections_active`, `websocket_messages_sent_total`,
  `websocket_e2e_latency_seconds`
- `redis_operations_total` — per operation/status
- `stream_publish_total` — XADD on the work queue (success/error)
- `stream_consume_total` — XREADGROUP on the work queue
- `http_request_duration_seconds`, `http_requests_total`

## Distributed tracing

Spans across the request flow:

1. `webhook.ingest` — API receives the request
2. `webhook.validate_hmac` — HMAC validation
3. `webhook.publish_stream` — `XADD events:in`
4. `webhook.process` — Worker picks up the entry
5. `webhook.idempotency_check` — `SET NX event_lock:{event_id}`
6. `webhook.business_handler` — Fan-out / business logic
7. `webhook.publish_redis` — `XADD stream:tenant:{uuid}`
8. `webhook.dispatch_to_dlq` — Only when retries are exhausted
9. `websocket.send` — Delivery to the client

Filter in Jaeger by `tenant_id`, `event_id` or `error=true`.

## Configuration

### Prometheus

`observability/prometheus/prometheus.yml` defines two scrape jobs:

- `easyhooks-api` → `app:8000/metrics`
- `redis-exporter` → `redis-exporter:9121`

Add new jobs (alertmanager, additional exporters) by appending to the file and
reloading Prometheus (`POST /-/reload` or restart the container).

### Grafana

Datasources and dashboards are auto-provisioned from
`observability/grafana/provisioning/`. Edit dashboards in the UI, export the
JSON model, save it back into `observability/grafana/dashboards/` and restart
the container — they are picked up on the next reload.

### Application toggles

```bash
METRICS_ENABLED=true
TRACING_ENABLED=true
OTEL_SERVICE_NAME=easyhooks
OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4317
TRACING_SAMPLE_RATE=1.0   # 100% in dev, 0.1–0.2 in production
```

## Alerting (production)

Suggested starting rules:

```yaml
groups:
  - name: easyhooks_critical
    rules:
      - alert: HighWorkerBacklog
        expr: redis_stream_group_pending{stream="events:in",group="webhook-workers"} > 1000
        for: 5m
        annotations:
          summary: "Worker backlog above 1k pending entries"

      - alert: HighDLQRatio
        expr: rate(webhook_dlq_total[5m]) / rate(stream_consume_total[5m]) > 0.05
        for: 5m
        annotations:
          summary: "DLQ ratio above 5%"

      - alert: DLQGrowing
        expr: increase(redis_stream_length{stream="events:failed"}[10m]) > 0
        for: 10m
        annotations:
          summary: "Events keep landing in events:failed"
```

## Troubleshooting

### Metrics missing

1. Check Prometheus targets at <http://localhost:9090/targets> — both
   `easyhooks-api` and `redis-exporter` should be `UP`.
2. `curl http://localhost:8000/metrics` from your host to confirm the API is
   exposing them.

### Stream metrics empty

`redis-exporter` only collects per-stream metrics for the keys passed in
`REDIS_EXPORTER_CHECK_STREAMS`. Streams that have not received any entry yet
will simply be missing — push at least one event and they appear.

### Traces missing

1. `TRACING_ENABLED=true` is set.
2. Jaeger is running (`docker compose ps jaeger`).
3. The application can reach `OTEL_EXPORTER_OTLP_ENDPOINT` — both API and
   worker log a warning if init fails.

## Versions

- Prometheus 2.51.0
- Grafana 10.4.0
- Jaeger 1.55
- redis-exporter 1.58.0

## Further reading

- [Prometheus best practices](https://prometheus.io/docs/practices/)
- [Grafana dashboard design](https://grafana.com/docs/grafana/latest/dashboards/)
- [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/)
- [Redis Streams reference](https://redis.io/docs/data-types/streams/)
