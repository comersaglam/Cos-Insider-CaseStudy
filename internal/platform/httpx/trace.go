package httpx

import (
	"net/http"
	"slices"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// Trace starts a server span for each request (ADR 0034), continuing the caller's trace when the request carries a
// traceparent. Once the router has matched a pattern, otelhttp names the span by it through the formatter below,
// and the span gets the route as http.route: the method and route, as the metrics label them, never a path with an
// ID in it. Requests to the quiet paths, probes and scrapes, are not traced.
func Trace(quiet ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		withRoute := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			if route := routeOf(r); route != unmatched {
				trace.SpanFromContext(r.Context()).SetAttributes(semconv.HTTPRoute(route))
			}
		})
		return otelhttp.NewHandler(withRoute, "http.server",
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				if route := routeOf(r); route != unmatched {
					return r.Method + " " + route
				}
				return r.Method
			}),
			otelhttp.WithFilter(func(r *http.Request) bool { return !slices.Contains(quiet, r.URL.Path) }),
			otelhttp.WithPropagators(observability.Propagator()),
		)
	}
}
