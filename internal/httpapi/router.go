package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/httpx"
)

// NewRouter returns the service's HTTP handler: every module's routes on one mux, requests no route matches
// answered as problems, all behind the middleware chain of ADR 0013 (correlation ID, access log, panic recovery).
// Mounting a module is one line here.
func NewRouter(log *slog.Logger, notifications *notification.Handler) http.Handler {
	mux := http.NewServeMux()
	notifications.RegisterRoutes(mux)

	h := httpx.ProblemFallback(mux)
	h = httpx.Recover(log)(h)
	h = httpx.AccessLog(log)(h)
	return httpx.Correlation(h)
}
