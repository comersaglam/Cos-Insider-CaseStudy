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

// NewRouter returns the service's HTTP handler: every module's routes, the probes and Prometheus's /metrics on one
// mux, requests no route matches answered as problems, all behind the middleware chain of ADR 0013 (correlation ID,
// metrics, access log, panic recovery). Mounting a module is one line here. The HTTP metrics are registered on reg.
func NewRouter(log *slog.Logger, reg *prometheus.Registry, probes *health.Handler, notifications *notification.Handler) http.Handler {
	mux := http.NewServeMux()
	probes.RegisterRoutes(mux)
	notifications.RegisterRoutes(mux)
	mux.Handle("GET /metrics", observability.MetricsHandler(reg))

	h := httpx.ProblemFallback(mux)
	h = httpx.Recover(log)(h)
	// Probes and scrapes come every few seconds; they are logged at debug level while they succeed.
	h = httpx.AccessLog(log, "/healthz", "/readyz", "/metrics")(h)
	h = httpx.NewHTTPMetrics(reg).Middleware(h)
	return httpx.Correlation(h)
}
