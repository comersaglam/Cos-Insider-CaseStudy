# Event-Driven Notification System

A Go service for the Insider One software engineer assessment: a notification API, queue workers that deliver
over SMS, e-mail and push through an external provider, and observability.

The working rules are in [CONTRIBUTING.md](CONTRIBUTING.md) and bind agents the same way they bind people:
@CONTRIBUTING.md

## For agents

- The owner is new to Go and must be able to explain every line. Before a design choice, explain the options
  with defined terms, a worked example and a pros/cons table, recommend one, then ask.
- After each block, write what Go concepts it used into `docs/learning/<block>.tr.md` (Turkish, git-ignored).
- Commit at the end of each finished block, after its tests pass, without asking. Never push.
- Never install a tool or dependency without the owner's approval.
