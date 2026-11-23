package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// recordSpans makes the global tracer provider record ended spans, for the test's length.
func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(tp) })
	return rec
}

func TestTraceNamesTheSpanByItsRouteAndContinuesTheCallersTrace(t *testing.T) {
	spans := recordSpans(t)
	mux := http.NewServeMux()
	mux.Handle("GET /v1/notifications/{id}", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	mux.Handle("GET /healthz", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h := Trace("/healthz")(mux)

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/notifications/0193a8f2", nil)
	r.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	h.ServeHTTP(httptest.NewRecorder(), r)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))

	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("%d spans, want one: the probe is not traced", len(ended))
	}
	s := ended[0]
	if s.Name() != "GET /v1/notifications/{id}" || s.SpanKind() != trace.SpanKindServer {
		t.Errorf("span %q of kind %v", s.Name(), s.SpanKind())
	}
	if s.SpanContext().TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" || s.Parent().SpanID().String() != "00f067aa0ba902b7" {
		t.Errorf("trace %s, parent %s; want the caller's", s.SpanContext().TraceID(), s.Parent().SpanID())
	}
	found := false
	for _, a := range s.Attributes() {
		if a.Key == "http.route" && a.Value.AsString() == "/v1/notifications/{id}" {
			found = true
		}
	}
	if !found {
		t.Errorf("no http.route among %v", s.Attributes())
	}
}
