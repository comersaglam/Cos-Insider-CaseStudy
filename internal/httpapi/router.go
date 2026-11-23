package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/health"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/httpx"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// NewRouter returns the service's HTTP handler: every module's routes, the probes, Prometheus's /metrics, and the
// contract with its documentation page, on one mux, requests no route matches answered as problems, all behind the middleware chain of ADR 0013 (correlation ID,
// metrics, access log, panic recovery). Mounting a module is one line here. The HTTP metrics are registered on reg.
func NewRouter(log *slog.Logger, reg *prometheus.Registry, probes *health.Handler, notifications *notification.Handler) http.Handler {
	h, _ := newRouter(log, reg, probes, notifications)
	return h
}

// routes is a ServeMux that keeps the pattern of every route registered through Handle, so that a test can hold
// them against the contract (ADR 0029).
type routes struct {
	*http.ServeMux
	patterns []string
}

// Handle registers handler for pattern and records the pattern.
func (r *routes) Handle(pattern string, handler http.Handler) {
	r.patterns = append(r.patterns, pattern)
	r.ServeMux.Handle(pattern, handler)
}

// newRouter is NewRouter, returning beside the handler the pattern of every route it serves.
func newRouter(log *slog.Logger, reg *prometheus.Registry, probes *health.Handler, notifications *notification.Handler) (http.Handler, []string) {
	mux := &routes{ServeMux: http.NewServeMux()}
	probes.RegisterRoutes(mux)
	notifications.RegisterRoutes(mux)
	mux.Handle("GET /metrics", observability.MetricsHandler(reg))
	mux.Handle("GET /openapi.yaml", http.HandlerFunc(serveSpec))
	mux.Handle("GET /docs", http.HandlerFunc(serveDocs))

	h := httpx.ProblemFallback(mux.ServeMux)
	h = httpx.Recover(log)(h)
	// Probes and scrapes come every few seconds; they are logged at debug level while they succeed.
	h = httpx.AccessLog(log, "/healthz", "/readyz", "/metrics")(h)
	h = httpx.NewHTTPMetrics(reg).Middleware(h)
	return httpx.Correlation(h), mux.patterns
}
