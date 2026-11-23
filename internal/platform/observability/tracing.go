package observability

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// Propagator carries a trace across processes in the W3C traceparent header, over HTTP and in RabbitMQ messages'
// headers (ADR 0034). The service uses it by name rather than through OpenTelemetry's global, so a trace crosses
// processes whether or not spans are exported.
func Propagator() propagation.TextMapPropagator {
	return propagation.TraceContext{}
}

// SetupTracing makes OpenTelemetry the process's tracer provider (ADR 0034). Spans are exported only when endpoint,
// the base URL of an OTLP/HTTP receiver such as http://jaeger:4318, is not empty; without it spans are not recorded,
// but trace context still propagates. The sampler follows OTEL_TRACES_SAMPLER, sampling every trace by default. The
// returned function flushes and stops the export; the process calls it on the way out.
func SetupTracing(ctx context.Context, service, endpoint string) (shutdown func(context.Context) error, err error) {
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(strings.TrimSuffix(endpoint, "/")+"/v1/traces"))
	if err != nil {
		return nil, fmt.Errorf("create the trace exporter: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(service))),
	)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}
