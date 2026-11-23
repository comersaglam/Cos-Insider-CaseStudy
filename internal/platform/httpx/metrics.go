package httpx

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// HTTPMetrics counts requests and measures how long they take, by method, route and status.
type HTTPMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewHTTPMetrics registers the HTTP metrics on reg.
func NewHTTPMetrics(reg prometheus.Registerer) *HTTPMetrics {
	m := &HTTPMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: observability.Namespace, Name: "http_requests_total",
			Help: "HTTP requests answered, by method, route and status.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: observability.Namespace, Name: "http_request_duration_seconds",
			Help: "How long HTTP requests take to answer, by method and route.", Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
	}
	reg.MustRegister(m.requests, m.duration)
	return m
}

// Middleware measures every request. The route is the matched pattern, such as /v1/notifications/{id}, never the
// path, and an unknown method counts as OTHER: labels take a bounded set of values, since each new value is a new
// time series for Prometheus to keep. It must wrap the mux directly or through middleware that passes the request
// on unchanged, since the mux records the pattern on the request it is given.
func (m *HTTPMetrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		route := routeOf(r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		method := knownMethod(r.Method)
		m.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
		m.duration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
	})
}

// knownMethod returns m when it is a standard HTTP method, and OTHER otherwise.
func knownMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
		http.MethodOptions, http.MethodConnect, http.MethodTrace:
		return m
	}
	return "OTHER"
}

// unmatched is the route of a request no pattern matched.
const unmatched = "unmatched"

// routeOf returns the path part of the pattern the router matched r with, such as /v1/notifications/{id}, once the
// router has run, or unmatched: a bounded set of values, never a path with an ID in it.
func routeOf(r *http.Request) string {
	if _, path, ok := strings.Cut(r.Pattern, " "); ok {
		return path
	}
	if r.Pattern != "" {
		return r.Pattern
	}
	return unmatched
}
