# 34. Distributed tracing

- Date: 2026-11-24
- Commits: `b566214`
- Decisions: [ADR 0034](../adr/0034-distributed-tracing.md)

## Goal

One trace per notification across the API, the outbox, RabbitMQ, the consumer and the provider, the brief's last
optional bonus.

## Steps

1. Asked the owner, who approved OpenTelemetry (otel and its SDK 1.47.0, the OTLP/HTTP exporter, otelhttp 0.72.0),
   the whole chain in one trace, and Jaeger 2 on its own in Compose.
2. 34a, tests first: `SetupTracing` exports nothing without an endpoint and OTLP/HTTP to `/v1/traces` with one; log
   lines inside a span carry `trace_id` and `span_id`.
3. 34b, tests first: `httpx.Trace` names the span by method and route, continues a caller's trace, skips probes. A
   surprise: otelhttp names the span again from the matched pattern through the formatter, overwriting a name set
   inside; the formatter now uses the route, and the route goes on as `http.route` after the router has run.
4. One W3C propagator, named rather than global, so a trace crosses processes whether spans are exported or not and
   tests need no global state.
5. 34c, tests first: the notification keeps the request's `traceparent` (`migrations/0010`); the relay publishes each
   message in a producer span and puts it in the headers (`rabbitmq.HeaderCarrier`, and `Message.Headers`); the
   consumer continues it in a consumer span with the outcome; the provider call is a client span and carries
   `traceparent`. The worker's end-to-end test sees the creating trace reach the provider.
6. 34d: Jaeger 2.23.0 in Compose, `OTEL_EXPORTER_OTLP_ENDPOINT` for the API and the worker. Live, a notification
   answered 503 showed its attempts 2.6 s and 11.6 s apart in one trace of both services, in Jaeger's API and UI,
   and the worker's log lines carried its `trace_id`.

## Verification

Every package touched with `-race` green; `make verify` and `golangci-lint` clean. The binaries now link parts of
gRPC through the OTLP exporter.

## Follow-ups

Block 35: Prometheus and Grafana in Compose, load tests at several scales and seeds, screenshots for the README.
