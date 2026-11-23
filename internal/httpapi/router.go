package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/httpx"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// NewRouter returns the service's HTTP handler: every module's routes and Prometheus's /metrics on one mux,
// requests no route matches answered as problems, all behind the middleware chain of ADR 0013 (correlation ID,
// metrics, access log, panic recovery). Mounting a module is one line here. The HTTP metrics are registered on reg.
func NewRouter(log *slog.Logger, reg *prometheus.Registry, notifications *notification.Handler) http.Handler {
	mux := http.NewServeMux()
	notifications.RegisterRoutes(mux)
	mux.Handle("GET /metrics", observability.MetricsHandler(reg))

	h := httpx.ProblemFallback(mux)
	h = httpx.Recover(log)(h)
	h = httpx.AccessLog(log)(h)
	h = httpx.NewHTTPMetrics(reg).Middleware(h)
	return httpx.Correlation(h)
}
