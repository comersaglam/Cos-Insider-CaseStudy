# 36. Closing: the diagram and a final review

- Date: 2026-11-24
- Commits: this block's commit

## Goal

The architecture drawn for the README, and the delivery reviewed before it is handed over.

## Steps

1. The architecture in D2 (`docs/architecture.d2`, `make diagram`, D2 0.7.2 through `go run`), rendered to one SVG
   with a light and a dark theme and its icons embedded, so GitHub shows it in either theme without fetching
   anything. Five layouts were tried: left to right with ELK and dagre was 2400 to 3900 pixels wide, too small to
   read at the README's width; top to bottom, with the worker drawn on both sides of RabbitMQ and the edges back
   written with `<-`, reads as one path. Checked at twice the resolution, edge by edge.
2. The owner asked whether the arrows point the right way: Grafana's pointed at Prometheus and Jaeger, who calls
   whom, while every other arrow shows where data goes. They now point at Grafana, the convention is written down,
   and the provider's own delivery to recipients is drawn, dashed, since the owner asked whether messages come
   back from the provider (they do not: it only receives). The owner also asked for the architecture as text,
   drawn without the D2 source; it agreed with the diagram except for that one direction, and goes under the
   diagram in the README.
3. Reviewing the screenshots found three misleading panels: stacked series showed a zero series' colour on top
   (successes read as transient failures, queued as sending), and the limiter's p95 read 23 ms where the wait is
   10 ms, an artefact of the 10 and 25 ms buckets. The panels now draw lines, the limiter's mean, and the success
   rate over the range shown.
4. Comparing Prometheus with PostgreSQL for run 2 found a real bug: 22 permanent failures counted as 0. A counter
   series created by its first failure hides it from `rate()` and `increase()`. The consumer now creates every
   channel's series at zero (`Metrics.start`), test first; afterwards Prometheus and PostgreSQL agreed (21 and 21
   permanent, 83 and 82 transient). ADR 0026 records it.
5. Both load runs repeated on a stack started on empty volumes and the new screenshots taken; the first run against
   a just-started stand-in was slower while its JVM warmed up, and the run shown follows it.
6. The README tried as on a clean machine: a fresh clone as its own Compose project on empty volumes; `make up` was
   healthy in 48 s, `make smoke` passed, `make load` ran.
7. The whole suite, one package at a time with `-race`: 16 packages green in 165 s; `make verify` and the linters
   clean; every relative link resolves; no TODO left.

## Follow-ups

- The breaker livelock found in block 35: the owner chose retries at the lowest priority and a breaker that opens on
  a failure ratio; block 37.
- An independent code review is left to the owner's `/code-review`.
