package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Namespace prefixes the service's own metrics, so they read apart from those of other services in one Prometheus.
const Namespace = "notifications"

// NewRegistry returns a Prometheus registry holding the Go runtime's and the process's own metrics. Each process
// registers its metrics on a registry of its own rather than the library's global one, so tests can make as many
// as they need.
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return reg
}

// MetricsHandler serves reg in Prometheus's text format, for Prometheus to scrape.
func MetricsHandler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg})
}
