# 0025. The worker in Compose, and a stand-in for the provider

- Status: Accepted
- Date: 2026-11-23
- Amends: [0019](0019-container-image-and-the-one-command-stack.md) (Compose runs the worker and a stand-in provider)

## Context

The worker runs; Compose has to run it too, and it needs a provider to send to. The brief asks for webhook.site,
configured to answer `202`. A free webhook.site URL expires after seven days and stops accepting requests after
100 (50 by older terms): a URL committed today is dead when the repository is reviewed, and a load test exceeds it
in seconds. The owner chose among these:

| Option | A reviewer two weeks later | A load test | The brief |
|---|---|---|---|
| **A stand-in provider by default; webhook.site through `PROVIDER_URL`** | works with no setting | works | webhook.site shown through one setting |
| webhook.site required through `.env` | creates a URL before the worker starts | stops at 100 requests | met to the letter |
| The owner's webhook.site URL committed | the URL is gone | stops at 100 requests | met until it expires; anyone can read what it receives |

## Decision

- **Compose runs the worker** from the same image (`worker` command), after the migrations and once RabbitMQ is
  healthy, with `restart: unless-stopped`: a failure stops it and Docker restarts it (ADR 0009).
- **WireMock is the stand-in provider** (`wiremock/wiremock:3.13.2-3`), the industry's usual HTTP stub server, set up
  by JSON mappings in `deploy/wiremock/mappings` with no code of ours:
  - `POST /notify` answers `202` with the brief's body: `messageId` (a new UUID), `status: accepted`, `timestamp`;
  - `+905550000503` is always answered `503`, to show retries, the reaper and the circuit breaker;
  - `+905550000422` is answered `422`, to show a permanent failure.
  Its admin API (`http://localhost:8081/__admin/requests`) lists every request it received, headers included.
- **`PROVIDER_URL` chooses the provider.** Left out, the worker sends to the stand-in; set in `.env` to a webhook.site
  URL answering `202`, it sends there, as the brief asks. The URL is read as a secret: whoever has it can read what
  is sent to it.
- **`make smoke`** now waits up to 30 seconds for the worker to send the notification it created.

## Consequences

- `docker compose up` gives anyone a service that accepts, queues and sends notifications, offline, with no account.
- The circuit breaker and retries can be shown on demand with the two demonstration numbers.
- Sending to webhook.site is one line of `.env`; the README shows it with the owner's URL at the time of writing.

### Deliberately not done

| Not done | Why not |
|---|---|
| A stand-in written in Go, in this repository | A third binary to maintain for what a stub server does from a configuration file. |
| Health checks for the API and the worker | They come with the health block's `/healthz` and `/readyz`. |
| Randomly failing answers in the stand-in | Failures on demand, by number, make a demonstration repeatable. |
