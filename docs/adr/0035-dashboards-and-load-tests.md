# 0035. Dashboards and load tests

- Status: Accepted
- Date: 2026-11-24
- Builds on: [0026](0026-metrics.md) (the metrics), [0034](0034-distributed-tracing.md) (the traces)

## Context

The owner asked for the metrics to be seen in Grafana and for load tests whose graphs show the service at work,
saved as screenshots for the README. A full stress test campaign was estimated at three to six hours; the owner
scaled it down to one or two small seeded runs that show the service can be measured.

## Decision

- **Prometheus 3.16.0** in Compose scrapes the API's and the worker's `/metrics` every five seconds; its UI is on
  `127.0.0.1:9091`, since the worker's operations port already has 9090.
- **Grafana 13.2.6** starts provisioned from `deploy/grafana`: Prometheus and Jaeger as data sources, and one
  dashboard, *Notifications*, as the home page, open to anonymous viewers on this machine. Its panels answer the
  brief's questions: sent per second by channel (the rate limit), attempts by outcome, waiting notifications by
  status, RabbitMQ queue depths with the dead-letter queue, provider latency at p50, p95 and p99, the rate limiter's
  wait, each channel's circuit breaker as a state timeline, sends in flight, the API's latency by route, and four
  headline figures.
- **`scripts/loadtest`** (the standard library only; `make load`) sends a seeded burst through `/v1/batches`: half
  SMS, 30% e-mail, 20% push; 20% high, 60% normal, 20% low priority; and, when asked, a share of SMS to the stand-in
  provider's numbers that answer 503 and 422. One seed is one workload, so a run can be repeated and compared. It
  then follows `/v1/metrics` until nothing is queued or sending, and reports the rate measured per channel.
- **Two runs**, their screenshots in `docs/images`:

  | Run | Workload | Result |
  |---|---|---|
  | 1 | `N=6000 SEED=1`, every recipient healthy | accepted in 0.59 s; drained in 30.6 s (SMS), 17.9 s (e-mail) and 12.5 s (push), in parallel: 98.7, 97.8 and 97.9 a second from the start, 100 a second at the peak |
  | 2 | `N=3000 SEED=2 UNAVAILABLE=0.15 REJECTED=0.05` | e-mail and push drained; 94.6% of the attempts succeeded; the SMS channel stopped: its breaker opened and stayed open (below) |

  The screenshots come from a stack started on empty volumes, so no earlier data shows in them. A first run against a
  stand-in provider just started was slower (p99 latency 800 ms while its JVM warmed up, sends in flight at the
  concurrency limit); the run shown follows it.

## Consequences

- The dashboard shows the rate limit holding at 100 a second per channel under a burst 30 times larger, priorities
  draining high first, and the queue emptying as expected.
- **Run 2 found a livelock.** Retries of notifications the provider keeps refusing are queued when due, together, so
  they fail five in a row and open the SMS breaker. Every probe in half-open is then such a retry: healthy
  high-priority SMS had been sent first, and a retried high-priority message outranks the healthy normal and low
  ones waiting. Each probe fails, the breaker reopens for 30 seconds, and 968 healthy SMS waited behind 51 failing
  ones. With up to eight attempts each, one per 30 seconds, it would take hours to clear. The tests had not found it:
  they never combine priorities, retries and the breaker under load. Fixed by
  [ADR 0036](0036-retries-and-the-breaker-under-load.md): retries at the lowest priority, and a breaker that opens on
  a failure ratio.

### Deliberately not done

| Not done | Why not |
|---|---|
| Hours-long stress and soak tests | Scaled down by the owner; the tool runs larger workloads with `N`. |
| Alerting rules | No one to page on a local stack; the dashboard's figures are where rules would start. |
| A load test tool (k6, Vegeta) | A program of ours, standard library only, needs nothing installed and knows the API's batches and summary. |
