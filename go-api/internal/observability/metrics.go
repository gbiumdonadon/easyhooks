package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	WebhookRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "webhook_requests_total",
			Help: "Total number of webhook requests received",
		},
		[]string{"tenant_id", "status"},
	)

	WebhookProcessingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "webhook_processing_duration_seconds",
			Help:    "Time spent processing webhook events in worker",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1.0, 2.5, 5.0, 7.5, 10.0},
		},
		[]string{"tenant_id"},
	)

	WebhookRetriesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "webhook_retries_total",
			Help: "Total number of webhook processing retries",
		},
		[]string{"tenant_id", "attempt"},
	)

	WebhookDLQTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "webhook_dlq_total",
			Help: "Total number of events sent to Dead Letter Queue",
		},
		[]string{"tenant_id", "error_type"},
	)

	IdempotencyDuplicatesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "idempotency_duplicates_total",
			Help: "Total number of duplicate events detected by idempotency check",
		},
		[]string{"tenant_id"},
	)

	WebSocketConnectionsActive = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "websocket_connections_active",
			Help: "Number of active WebSocket connections",
		},
		[]string{"tenant_id"},
	)

	WebSocketMessagesSentTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "websocket_messages_sent_total",
			Help: "Total number of messages sent via WebSocket",
		},
		[]string{"tenant_id"},
	)

	RedisOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "redis_operations_total",
			Help: "Total number of Redis operations",
		},
		[]string{"operation", "status"},
	)

	StreamConsumeTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "stream_consume_total",
			Help: "Total number of messages consumed from a Redis Stream consumer group",
		},
		[]string{"stream", "consumer_group"},
	)

	StreamPublishTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "stream_publish_total",
			Help: "Total number of messages published to a Redis Stream (success or error)",
		},
		[]string{"stream", "status"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
		},
		[]string{"method", "endpoint", "status_code"},
	)

	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status_code"},
	)

	LoadtestRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loadtest_requests_total",
			Help: "Total requests during load test (tagged for analysis)",
		},
		[]string{"tenant_id", "endpoint", "test_scenario"},
	)

	LoadtestErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loadtest_errors_total",
			Help: "Total errors during load test",
		},
		[]string{"tenant_id", "endpoint", "error_type", "test_scenario"},
	)

	WebSocketE2ELatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "websocket_e2e_latency_seconds",
			Help:    "End-to-end latency from webhook POST to WebSocket delivery",
			Buckets: []float64{0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0},
		},
		[]string{"tenant_id"},
	)

	WebSocketConnectionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "websocket_connection_duration_seconds",
			Help:    "Duration of WebSocket connections",
			Buckets: []float64{1, 5, 10, 30, 60, 300, 600, 1800, 3600},
		},
		[]string{"tenant_id"},
	)

	TenantIsolationLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tenant_isolation_latency_seconds",
			Help:    "Per-tenant latency for isolation testing",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
		},
		[]string{"tenant_id", "tenant_tier"},
	)

	WebhookLoadShedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "webhook_load_shed_total",
			Help: "Total number of webhook ingestion requests rejected with HTTP 429 due to queue backpressure",
		},
		[]string{"tenant_id"},
	)

	IngestQueueDepth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ingest_queue_depth",
			Help: "Last observed length of the ingestion stream (XLEN), polled by the queue-depth monitor",
		},
		[]string{"stream"},
	)

	IngestLoadSheddingActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "ingest_load_shedding_active",
			Help: "1 when the ingestion is currently shedding load (queue depth crossed the high watermark), 0 otherwise",
		},
	)

	WebSocketSubscriberDroppedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "websocket_subscriber_dropped_total",
			Help: "Total number of WebSocket subscribers disconnected by the fanout layer (e.g. slow consumer with full buffer)",
		},
		[]string{"tenant_id", "reason"},
	)

	easyhooksProfileInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "easyhooks_profile_info",
			Help: "Constant gauge (=1) labelled with the active EASYHOOKS_PROFILE; useful as a Grafana variable",
		},
		[]string{"profile"},
	)
)

// RecordProfileInfo sets easyhooks_profile_info{profile=<active>} to 1 so
// dashboards can pivot on the configured tier.
func RecordProfileInfo(profile string) {
	easyhooksProfileInfo.WithLabelValues(profile).Set(1)
}
