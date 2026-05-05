package middleware

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"time"

	"github.com/easyhooks/easyhooks/internal/observability"
)

// responseWriter wraps http.ResponseWriter to capture the status code while
// preserving the optional interfaces the standard library and gorilla/websocket
// rely on (Hijacker for WebSocket upgrades, Flusher for streaming responses).
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack lets the underlying ResponseWriter take over the connection, which is
// required for WebSocket upgrades. Without this, gorilla/websocket fails with
// "response does not implement http.Hijacker" and the client sees code 1006.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("underlying ResponseWriter does not implement http.Hijacker")
	}
	return h.Hijack()
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
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
