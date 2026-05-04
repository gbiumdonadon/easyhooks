package handler

import (
	"encoding/json"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Health handles GET /health.
func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy", "service": "easyhooks"}) //nolint:errcheck
}

// Metrics exposes Prometheus metrics at GET /metrics.
func Metrics() http.Handler {
	return promhttp.Handler()
}
