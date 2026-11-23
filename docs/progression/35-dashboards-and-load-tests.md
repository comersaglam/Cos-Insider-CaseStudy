# 35. Dashboards and load tests

- Date: 2026-11-24
- Commits: this block's commit
- Decisions: [ADR 0035](../adr/0035-dashboards-and-load-tests.md)

## Goal

See the service at work in Grafana under load, with screenshots for the README; scaled down by the owner to one or
two small seeded runs.

## Steps

1. Prometheus 3.16.0 and Grafana 13.2.6 in Compose, provisioned from `deploy/`: data sources, and a 13-panel
   dashboard generated as JSON, opened as Grafana's home page. Both scrape targets came up healthy.
2. `scripts/loadtest`, the standard library only, tests first for the workload: one seed, one workload; the shares of
   channels, priorities and failing recipients.
3. A surprise in the tool: the first run reported 6000 notifications drained in 2.7 s. `encoding/json/v2` matches
   member names case-sensitively, and two fields lacked their tags, so every count read 0. A test of the summary's
   reading failed, then passed with the tags; the run was repeated.
4. Run 1, 6000 healthy notifications: 99.3, 98.4 and 98.6 sent a second on SMS, e-mail and push, in parallel.
5. Run 2, 3000 with 15% of SMS answered 503 and 5% 422: e-mail and push drained; SMS stopped. Its breaker opened
   and every half-open probe failed. The database showed why: all 51 high-priority SMS left were retries of the
   number answered 503, ahead of 968 healthy normal and low SMS. Recorded in ADR 0035; the fix waits for the owner.
6. Screenshots of both runs in Grafana, and of a retried notification's trace in Jaeger, in `docs/images` and the
   README; the breaker panel became a state timeline after the first screenshot showed it on a 0 to 100 axis.

## Verification

`scripts/loadtest` tests green; `docker compose config` valid; Prometheus's targets up; the dashboard served as
provisioned; `golangci-lint` clean.

## Follow-ups

- The owner's decision on the livelock (the breaker's trip and probe, or the retries' priority).
- The D2 architecture diagram, then the final review.
