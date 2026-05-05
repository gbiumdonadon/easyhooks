# Observability Stack for EasyHooks

This directory contains the complete observability implementation for the EasyHooks platform.

## 🎯 What Was Implemented

✅ **Complete observability stack** with Prometheus, Grafana, and Jaeger
✅ **Infrastructure monitoring** via exporters (Kafka, Redis, PostgreSQL)
✅ **Custom application metrics** for webhooks, retries, DLQ, idempotency
✅ **Distributed tracing** with OpenTelemetry across API → Kafka → Worker
✅ **Pre-configured Grafana dashboards** (5 dashboards)
✅ **Comprehensive documentation** (monitoring + tracing guides)
✅ **Automated tests** for observability features

## 📊 Architecture

```
┌─────────────┐
│  Go API     │──── /metrics ────┐
│  (Chi)      │                   │
└─────────────┘                   │
                                  ▼
┌─────────────┐            ┌──────────────┐
│  Kafka      │───────────▶│  Prometheus  │
│  Worker     │            │  (Metrics)   │
└─────────────┘            └──────────────┘
                                  │
┌─────────────────────────┐      │
│  Exporters:             │      │
│  - kafka-exporter       │──────┤
│  - redis-exporter       │      │
│  - postgres-exporter    │──────┘
└─────────────────────────┘      │
                                  ▼
                           ┌──────────────┐
                           │   Grafana    │
                           │ (Dashboards) │
                           └──────────────┘

┌─────────────┐
│  App/Worker │──── OTLP ────┐
│  (OTel SDK) │              │
└─────────────┘              ▼
                      ┌──────────────┐
                      │    Jaeger    │
                      │  (Tracing)   │
                      └──────────────┘
```

## 📁 Directory Structure

```
observability/
├── prometheus/
│   └── prometheus.yml          # Prometheus configuration
├── grafana/
│   ├── provisioning/
│   │   ├── datasources/
│   │   │   └── datasources.yml # Auto-provision Prometheus + Jaeger
│   │   └── dashboards/
│   │       └── dashboards.yml  # Dashboard provisioning config
│   └── dashboards/
│       ├── README.md           # Dashboard documentation
│       ├── 01-overview.json    # Main overview dashboard
│       └── 02-kafka.json       # Kafka metrics dashboard
└── README.md                   # This file
```

## 🚀 Quick Start

### 1. Start the Stack

```bash
docker compose up -d
```

All observability services start automatically:
- Prometheus: http://localhost:9090
- Grafana: http://localhost:3000 (admin/admin)
- Jaeger: http://localhost:16686

### 2. Access Dashboards

Open Grafana and navigate to "EasyHooks" folder in the sidebar.

### 3. Generate Test Traffic

```bash
# Create a tenant
curl -X POST http://localhost:8000/admin/tenants \
  -H "Authorization: Bearer $ADMIN_SEED_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test Tenant"}'

# Send webhooks
# (see main README for full examples)
```

### 4. View Metrics

- **Prometheus**: http://localhost:9090/graph
- **Grafana**: http://localhost:3000
- **Jaeger**: http://localhost:16686

## 📈 Key Metrics

### Critical Metrics to Monitor

| Metric | Description | Query | Healthy Range |
|--------|-------------|-------|---------------|
| **Kafka Lag** | Messages waiting to be processed | `kafka_consumergroup_lag` | < 100 |
| **Error Rate** | Failed webhooks (DLQ) | `rate(webhook_dlq_total[5m])` | < 1% |
| **Processing Duration p95** | 95th percentile latency | `histogram_quantile(0.95, rate(webhook_processing_duration_seconds_bucket[5m]))` | < 200ms |
| **Active WebSocket Connections** | Real-time connections | `websocket_connections_active` | - |

### All Available Metrics

#### Application Metrics

- `webhook_requests_total` — Total webhook requests (by tenant, status)
- `webhook_processing_duration_seconds` — Processing time histogram
- `webhook_retries_total` — Retry attempts (by attempt number)
- `webhook_dlq_total` — Events sent to Dead Letter Queue
- `idempotency_duplicates_total` — Duplicate events detected
- `websocket_connections_active` — Active WS connections (gauge)
- `websocket_messages_sent_total` — Messages sent via WebSocket
- `redis_operations_total` — Redis operations counter
- `kafka_produce_total` — Messages produced to Kafka
- `kafka_consume_total` — Messages consumed from Kafka
- `http_request_duration_seconds` — HTTP request latency
- `http_requests_total` — HTTP request counter

#### Infrastructure Metrics (from exporters)

- `kafka_consumergroup_lag` — **Most critical!** Consumer lag
- `kafka_consumergroup_current_offset` — Current offset position
- `redis_commands_total` — Redis commands/sec
- `redis_memory_used_bytes` — Redis memory usage
- `pg_stat_database_*` — PostgreSQL statistics

## 🔍 Distributed Tracing

### Trace Flow

A complete webhook trace includes:

1. `webhook.ingest` — API receives request
2. `webhook.validate_hmac` — HMAC validation
3. `webhook.produce_kafka` — Send to Kafka
4. `webhook.process` — Worker processes
5. `webhook.idempotency_check` — Check for duplicates
6. `webhook.business_handler` — Core processing
7. `webhook.publish_redis` — Publish to Redis
8. `websocket.send` — Deliver to client

### Using Jaeger

1. Open http://localhost:16686
2. Select service: `easyhooks` or `easyhooks-worker`
3. Search by:
   - Tenant ID: `tenant_id=xxx`
   - Event ID: `event_id=evt-001`
   - Errors: `error=true`
   - Duration: Min/Max filters

## 📊 Grafana Dashboards

### Available Dashboards

1. **EasyHooks Overview** (`01-overview.json`)
   - System-wide metrics
   - Request rates
   - Processing latency (p50, p95, p99)
   - Error rates
   - Active connections

2. **Kafka Metrics** (`02-kafka.json`)
   - **Consumer lag** (critical!)
   - Throughput (produced vs consumed)
   - Offset progression
   - Partition metrics

3-5. **Additional dashboards** can be created in Grafana UI and exported to this directory.

### Creating Custom Dashboards

1. Open Grafana at http://localhost:3000
2. Create dashboard using visual editor
3. Export JSON: Dashboard settings → JSON Model
4. Save to `observability/grafana/dashboards/`
5. Restart Grafana or wait for auto-reload

## 🔧 Configuration

### Environment Variables

Set in `.env`:

```bash
# Observability
METRICS_ENABLED=true
TRACING_ENABLED=true
OTEL_SERVICE_NAME=easyhooks
OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4317
TRACING_SAMPLE_RATE=1.0  # 100% in dev, 0.1-0.2 in prod
```

### Prometheus Configuration

Edit `observability/prometheus/prometheus.yml` to:
- Change scrape intervals
- Add new targets
- Configure alerting rules
- Adjust retention

### Grafana Datasources

Edit `observability/grafana/provisioning/datasources/datasources.yml` to:
- Add new datasources
- Change endpoints
- Configure authentication

## 🧪 Testing

Run the Go unit tests (including handler and middleware coverage used by metrics):

```bash
cd go-api && go test ./...
```

Manual smoke checks:
- Metrics endpoint: `curl -sSf http://localhost:8000/metrics | head`
- Tracing: enable `TRACING_ENABLED=true` and inspect spans in Jaeger after sending a webhook

## 🚨 Alerting (Production)

For production, configure Prometheus alerts:

```yaml
# prometheus/alerts.yml
groups:
  - name: easyhooks_critical
    rules:
      - alert: HighKafkaLag
        expr: kafka_consumergroup_lag > 1000
        for: 5m
        annotations:
          summary: "Kafka lag too high"
          
      - alert: HighErrorRate
        expr: rate(webhook_dlq_total[5m]) / rate(kafka_consume_total[5m]) > 0.05
        for: 5m
        annotations:
          summary: "Error rate > 5%"
```

## 📚 Documentation

- **Main docs**: http://localhost:3001/observability/monitoring
- **Monitoring guide**: `docs/docs/observability/monitoring.md`
- **Tracing guide**: `docs/docs/observability/tracing.md`
- **Dashboard README**: `observability/grafana/dashboards/README.md`

## 🎓 Best Practices

### Development

- ✅ Keep sampling rate at 100% (`TRACING_SAMPLE_RATE=1.0`)
- ✅ Monitor dashboards during testing
- ✅ Use traces to debug issues
- ✅ Check metrics after code changes

### Production

- ⚠️ Lower sampling rate to 10-20% (`TRACING_SAMPLE_RATE=0.1`)
- ⚠️ Set up persistent storage for Prometheus/Jaeger
- ⚠️ Configure alerting for critical metrics
- ⚠️ Monitor Kafka lag continuously
- ⚠️ Review metrics daily for trends

## 🐛 Troubleshooting

### Metrics not appearing

1. Check Prometheus targets: http://localhost:9090/targets
2. Verify all targets show "UP"
3. Check app logs: `docker compose logs app`
4. Ensure metrics endpoint works: `curl http://localhost:8000/metrics`

### Dashboards empty

1. Verify Prometheus datasource in Grafana
2. Check time range (use "Last 1 hour")
3. Generate test traffic
4. Review queries in dashboard panels

### Traces not showing

1. Check tracing is enabled: `TRACING_ENABLED=true`
2. Verify Jaeger is running: `docker compose ps jaeger`
3. Check OTLP endpoint: `curl http://localhost:4317`
4. Look for errors in app logs

### High resource usage

**Prometheus:**
- Increase scrape interval: `scrape_interval: 30s`
- Reduce retention: `--storage.tsdb.retention.time=7d`

**Tracing:**
- Lower sampling rate: `TRACING_SAMPLE_RATE=0.1`
- Disable for health checks

**Grafana:**
- Reduce refresh rate on dashboards
- Limit dashboard time range

## 📦 Components Version

- Prometheus: v2.51.0
- Grafana: 10.4.0
- Jaeger: 1.55
- kafka-exporter: v1.7.0
- redis-exporter: v1.58.0
- postgres-exporter: v0.15.0

## 🤝 Contributing

When adding new metrics:

1. Define counters/histograms in `go-api/internal/observability/metrics.go` (or adjacent packages)
2. Instrument handlers/worker code
3. Add to Grafana dashboards
4. Document in `docs/docs/observability/monitoring.md`
5. Add or extend `go test` coverage under `go-api/`

When adding traces:

1. Use the OpenTelemetry Go SDK (`go.opentelemetry.io/otel`) in the API or worker
2. Add meaningful span attributes
3. Document expected spans in `docs/docs/observability/tracing.md`

## 📖 Further Reading

- [Prometheus Best Practices](https://prometheus.io/docs/practices/)
- [Grafana Dashboard Design](https://grafana.com/docs/grafana/latest/dashboards/)
- [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/)
- [Jaeger Architecture](https://www.jaegertracing.io/docs/latest/architecture/)

---

**Built with ❤️ for production-grade observability**
