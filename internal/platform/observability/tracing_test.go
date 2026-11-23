package observability_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// restoreTracing puts the global tracer provider back after a test that sets it.
func restoreTracing(t *testing.T) {
	t.Helper()
	tp := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(tp) })
}

// TestTracingExportsOnlyWithAnEndpoint: without an endpoint nothing is exported; with one, ended spans reach it over
// OTLP/HTTP when the provider shuts down.
func TestTracingExportsOnlyWithAnEndpoint(t *testing.T) {
	restoreTracing(t)
	var posts atomic.Int32
	var path, contentType atomic.Value
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		path.Store(r.URL.Path)
		contentType.Store(r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	shutdown, err := observability.SetupTracing(t.Context(), "notifications-test", "")
	if err != nil {
		t.Fatal(err)
	}
	_, span := otel.Tracer("test").Start(t.Context(), "off")
	span.End()
	if err := shutdown(t.Context()); err != nil || posts.Load() != 0 {
		t.Fatalf("without an endpoint: shutdown %v, %d exports", err, posts.Load())
	}

	shutdown, err = observability.SetupTracing(t.Context(), "notifications-test", collector.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, span = otel.Tracer("test").Start(t.Context(), "on")
	span.End()
	if err := shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if posts.Load() == 0 || path.Load() != "/v1/traces" || contentType.Load() != "application/x-protobuf" {
		t.Errorf("exports %d to %v as %v; want OTLP/HTTP to /v1/traces", posts.Load(), path.Load(), contentType.Load())
	}
}

// TestLoggerAddsTheTraceOfTheContext: a line logged inside a span carries its trace and span IDs, so the logs of a
// trace can be found from Jaeger and the other way round.
func TestLoggerAddsTheTraceOfTheContext(t *testing.T) {
	tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	sid, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid}))

	var buf bytes.Buffer
	log := observability.NewLogger(&buf, slog.LevelInfo)
	log.InfoContext(ctx, "inside")
	log.InfoContext(context.Background(), "outside")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	var inside, outside map[string]any
	if err := json.Unmarshal(lines[0], &inside); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(lines[1], &outside); err != nil {
		t.Fatal(err)
	}
	if inside["trace_id"] != tid.String() || inside["span_id"] != sid.String() {
		t.Errorf("inside a span: %v", inside)
	}
	if _, ok := outside["trace_id"]; ok {
		t.Errorf("outside a span: %v", outside)
	}
}
