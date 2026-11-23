# 27. Correlation across the queue

- Date: 2026-11-23
- Commits: this block's commit
- Decisions: [ADR 0028](../adr/0028-correlation-across-the-queue.md)

## Goal

One correlation ID finds the API's line about a request and the worker's lines about the sends it caused.

## Steps

1. Took stock: the notification already stored the ID (block 15), the relay already set the AMQP header (block 20),
   the consumer already logged under the stored ID after the claim (block 22), and the provider client already sent
   it (block 21). Left: the lines written before the claim, and a test of the whole way.
2. Decided that a notification keeps its creating request's ID through every retry, joined to a retry request by
   `notification_id` (ADR 0028), with no question left open: the roadmap already said the ID is stored on the
   notification.
3. Test first: a dead-lettered message logs under its header's ID, and a header that is not a valid ID, such as one
   holding a line break, is ignored. Seen failing, then `Consumer.handle` reads the header, checked with
   `observability.ValidCorrelationID`.
4. `TestCorrelationIDCrossesTheQueue` in `cmd/worker`: a notification created under `trace-me`, as the API creates
   it, reaches the provider with `X-Correlation-ID: trace-me`, and the worker's "attempt finished" line carries it.
   The behaviour it covers existed already, so the test passed at once; with the consumer's two correlation lines
   removed it failed, as it should.
5. ADR 0013 now points to 0028; the contract's introduction says where the ID goes after the request.

## Verification

`internal/delivery` and `cmd/worker` with `-race` green; `golangci-lint` 0 issues.

## Follow-ups

Phase 4 starts with block 28: the OpenAPI contract made detailed, Swagger UI, and a test that every route is in it.
