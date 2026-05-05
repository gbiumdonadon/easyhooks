# Grafana Dashboards for EasyHooks

Pre-configured dashboards for monitoring the Redis-only EasyHooks platform.

## Available Dashboards

### 1. EasyHooks Overview (`01-overview.json`)
System-wide health view:
- Webhook requests per second (HTTP)
- Processing duration (p50, p95, p99)
- DLQ ratio (`webhook_dlq_total / stream_consume_total`)
- Active WebSocket connections
- Worker error rates

### 2. Redis Streams Metrics (`02-streams.json`)
The replacement for the old Kafka dashboard, focused on the work queue:
- **Worker Backlog** — `redis_stream_group_pending{stream="events:in",group="webhook-workers"}` (single most important metric: how many entries the worker has not acked yet).
- **Stream Throughput** — published vs consumed rate (`stream_publish_total` / `stream_consume_total`).
- **Stream Length (XLEN)** — `redis_stream_length{stream="events:in"}` and `events:failed` (DLQ).

### 3. Load Test (`03-loadtest.json`)
Throughput and latency view used while running k6 scenarios. The "Stream Pending Backlog" panel surfaces XPENDING for the ingestion stream during sustained load.

## How dashboards are loaded

Dashboards are mounted into Grafana by `docker-compose.monitoring.yml`. They are
provisioned automatically from this directory. To create or update one:

1. Edit visually in Grafana at <http://localhost:3000>.
2. Export the dashboard JSON (Dashboard settings → JSON Model).
3. Save back to this directory and restart the `grafana` service.

## Useful PromQL snippets

### Worker backlog (CRITICAL)

```promql
redis_stream_group_pending{stream="events:in", group="webhook-workers"}
```

### Stream length (queue depth)

```promql
redis_stream_length{stream="events:in"}
redis_stream_length{stream="events:failed"}
```

### DLQ rate

```promql
rate(webhook_dlq_total[5m])
```

### Throughput

```promql
rate(stream_publish_total{stream="events:in",status="success"}[5m])
rate(stream_consume_total{stream="events:in"}[5m])
```

### Processing latency p95

```promql
histogram_quantile(0.95, rate(webhook_processing_duration_seconds_bucket[5m]))
```

### Active WebSocket connections

```promql
websocket_connections_active
```

## Tips

1. Filter by tenant with template variables when relevant.
2. Set alerts on `redis_stream_group_pending > 1000` (worker stalled) and on
   `rate(webhook_dlq_total[5m]) > 0.05` (sustained DLQ traffic).
3. Use annotations for deployments and load tests — pairs nicely with the
   `loadtest_requests_total` series.

## Quick start

```bash
docker compose -f docker-compose.yml -f docker-compose.monitoring.yml up -d
# Grafana: http://localhost:3000 (default admin/${GRAFANA_ADMIN_PASSWORD})
# Prometheus: http://localhost:9090
# Jaeger: http://localhost:16686
```
