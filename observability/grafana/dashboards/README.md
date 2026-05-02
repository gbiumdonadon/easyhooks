# Grafana Dashboards for EasyHooks

This directory contains pre-configured Grafana dashboards for monitoring the EasyHooks platform.

## Available Dashboards

### 1. EasyHooks Overview (01-overview.json)
Main dashboard showing system-wide metrics:
- Webhook requests per second
- Processing duration (p50, p95, p99)
- Error rates
- Active WebSocket connections
- System health status

### 2. Kafka Metrics (02-kafka.json)
Critical Kafka monitoring:
- **Consumer Lag** (most important metric!)
- Messages produced vs consumed
- Offset progression by partition
- Consumer group rebalancing events

### 3. Worker Processing (03-worker.json)
Worker-specific metrics:
- Events processed vs failed
- Retry distribution (1st, 2nd, 3rd attempts)
- DLQ rate and error types
- Idempotency duplicate detection
- Processing duration histograms

### 4. Infrastructure (04-infrastructure.json)
Infrastructure components:
- Redis: Commands/sec, latency, memory usage, connections
- PostgreSQL: Active connections, queries/sec, cache hit ratio
- Kafka: Disk usage, broker status

### 5. WebSockets (05-websockets.json)
Real-time distribution metrics:
- Active connections by tenant
- Messages sent per second
- Connection/disconnection rates
- Message delivery latency

## Creating Dashboards

The dashboards are provisioned automatically from this directory. To create or update:

1. **Using Grafana UI:**
   - Open Grafana at http://localhost:3000
   - Create/edit dashboard visually
   - Export JSON (Dashboard settings → JSON Model)
   - Save to this directory

2. **Key Queries to Use:**

### Webhook Request Rate
```promql
rate(http_requests_total{endpoint="/v1/webhooks"}[5m])
```

### Kafka Consumer Lag (CRITICAL!)
```promql
kafka_consumergroup_lag{consumergroup="webhook-workers"}
```

### Processing Duration p95
```promql
histogram_quantile(0.95, rate(webhook_processing_duration_seconds_bucket[5m]))
```

### DLQ Rate
```promql
rate(webhook_dlq_total[5m])
```

### Retry Distribution
```promql
sum by (attempt) (rate(webhook_retries_total[5m]))
```

### Active WebSocket Connections
```promql
websocket_connections_active
```

### Redis Operations Rate
```promql
rate(redis_operations_total[5m])
```

## Dashboard Tips

1. **Use template variables** for tenant_id to filter metrics
2. **Set appropriate time ranges** (last 1h for ops, last 24h for trends)
3. **Configure alerts** for critical metrics (lag > 1000, error rate > 5%)
4. **Add annotations** for deployments and incidents
5. **Use color thresholds** (green/yellow/red) for quick status checks

## Quick Start

After starting the stack, dashboards are automatically available at:
http://localhost:3000 (admin/admin)

They will be in the "EasyHooks" folder in the left sidebar.
