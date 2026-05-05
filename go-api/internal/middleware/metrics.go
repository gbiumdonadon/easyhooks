package middleware

import (
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/easyhooks/easyhooks/internal/observability"
)

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Normalization patterns for stable Prometheus label cardinalities.
var normalizationPatterns = []struct {
	re          *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`/admin/tenants/[0-9a-f-]{36}`), "/admin/tenants/{tenant_id}"},
	{regexp.MustCompile(`/v1/webhooks/[0-9a-f-]{36}`), "/v1/webhooks/{tenant_id}"},
	{regexp.MustCompile(`/v1/tokens/[0-9a-f-]{36}`), "/v1/tokens/{tenant_id}"},
	{regexp.MustCompile(`/ws/events/[0-9a-f-]{36}`), "/ws/events/{tenant_id}"},
}

func normalizePath(path string) string {
	for _, p := range normalizationPatterns {
		path = p.re.ReplaceAllString(path, p.replacement)
	}
	return path
}

// HTTPMetrics records http_requests_total and http_request_duration_seconds.
// Skips /metrics to avoid recursive counting.
// HTTP request metrics middleware for Prometheus.
func HTTPMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()

		next.ServeHTTP(rw, r)

		duration := time.Since(start).Seconds()
		endpoint := normalizePath(r.URL.Path)
		statusCode := fmt.Sprintf("%d", rw.status)

		observability.HTTPRequestsTotal.WithLabelValues(r.Method, endpoint, statusCode).Inc()
		observability.HTTPRequestDuration.WithLabelValues(r.Method, endpoint, statusCode).Observe(duration)
	})
}
