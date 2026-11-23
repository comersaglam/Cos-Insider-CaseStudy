# 0034. Distributed tracing

- Status: Accepted
- Date: 2026-11-24
- Builds on: [0013](0013-platform-conventions.md) (logs gain `trace_id` beside the correlation ID),
  [0028](0028-correlation-across-the-queue.md) (what crosses the queue)

## Context

The brief's last bonus asks for distributed tracing. A notification crosses three hops that no single process sees:
the API's request, the relay that publishes it later from the outbox, and the consumer that sends it, perhaps
several times over minutes of retries. The correlation ID already joins their log lines; a trace adds when each step
started, how long it took, and where the time went. Two choices were the owner's.

Worked example: a notification accepted at 10:00:00.000 should show as one timeline: the API's 202 after 5 ms, the
relay's publish, the consumer's processing, and the provider's call of 120 ms; a notification the provider keeps
answering 503 should show each attempt in the same timeline, with the waits between them.

| Scope | The example | Cost |
|---|---|---|
| **The whole chain, one trace** | one timeline from the request to the last attempt | a column for the trace, an AMQP header carrier |
| Each process on its own | the API's trace and the worker's apart, joined only by the correlation ID | none |
| The whole chain and every SQL query | as above, with a span per query | another dependency, or a tracer of our own, and much noise |

| Backend in Compose | |
|---|---|
| **Jaeger 2 on its own** | one container, OTLP straight in, its UI on port 16686 |
| An OpenTelemetry Collector and Jaeger | the usual production topology; one more container and its configuration |
| Grafana Tempo | traces beside the metrics in Grafana, which comes only in block 35 |

## Decision

- **OpenTelemetry** (`go.opentelemetry.io/otel` and its SDK 1.47.0, the OTLP/HTTP exporter, `otelhttp` 0.72.0),
  set up by `observability.SetupTracing` in each process as `notifications-api` and `notifications-worker`.
- **Export is opt-in**: spans go to `OTEL_EXPORTER_OTLP_ENDPOINT` (Compose sets `http://jaeger:4318`) and nowhere
  when it is unset, as every stand-in here is turned on by its own setting. `OTEL_TRACES_SAMPLER` chooses the
  sampling, every trace by default. Spans are flushed within five seconds when a process stops.
- **Trace context always propagates**, exported or not, in the W3C `traceparent` header. The service names its one
  propagator (`observability.Propagator`) instead of OpenTelemetry's global, so tests and processes behave the same.
- **The chain**:
  - the API's request span (`otelhttp`), named by method and route like the metrics, continuing a client's
    `traceparent`; probes and scrapes are not traced;
  - the request's `traceparent` is kept on the notification (`migrations/0010`), because the outbox hands it to
    another process later;
  - the relay publishes each message in a producer span that continues it, and puts that span in the message's
    headers (`rabbitmq.HeaderCarrier`);
  - the consumer processes each message in a consumer span that continues the headers' trace, records the outcome
    on it, and marks failures;
  - the provider call is a client span (`otelhttp`), and the provider receives `traceparent`.
  A retry is published from the same stored `traceparent`, so every attempt joins the notification's trace.
- **Logs carry `trace_id` and `span_id`** beside `correlation_id`, so a log line leads to its span and back.
- **Jaeger 2.23.0** in Compose keeps traces in memory and shows them at `http://localhost:16686`.

## Consequences

- One trace shows a notification from the request to its last attempt; live, a notification answered 503 showed
  three attempts 2.6 s and 11.6 s apart in one timeline of the API's and the worker's spans.
- The provider's latency, the time a message waits in RabbitMQ, and the time a retry waits are visible per
  notification, not only as aggregates.
- The OTLP exporter links parts of gRPC into the binaries (its retry logic uses gRPC's status types), though it sends
  over HTTP.
- A notification's trace can last as long as its retries, up to about half an hour with the default policy.

### Deliberately not done

| Not done | Why not |
|---|---|
| Spans for SQL queries | Noise for this service; query latency is in the metrics and the logs. |
| A Collector | OTLP is the standard; moving to one changes an address, not the code. |
| Returning the trace ID to clients | The correlation ID is the client's handle; the trace is for operators. |
| Persistent trace storage | Jaeger in memory suits a local stack; production would choose its own backend. |
