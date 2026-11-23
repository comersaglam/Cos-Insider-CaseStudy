# 32. Templates

- Date: 2026-11-24
- Commits: `fffbbb0`
- Decisions: [ADR 0032](../adr/0032-templates.md)

## Goal

Templates with variable substitution, the brief's second optional bonus, that no template can make slow.

## Steps

1. Measured `text/template` first: `{{range 200000000}}{{end}}` burns 3.4 s of CPU with no output, so capping the
   output is no defence. Asked the owner, who chose placeholders only, rendering when the notification is
   created, and full CRUD.
2. 32a, the domain, tests first: names, channels and bodies validated; the parse tree allows only text and
   `{{.name}}` (19 cases, the measured loop among them); the variables a body needs; every missing variable named;
   a value never rendered again.
3. 32b, storage, tests first: `migrations/0008` (`templates`, `template_id` on notifications as history); create,
   read, replace, delete, a taken name from the unique constraint on create and on rename, a keyset list.
4. 32c, the service and `/v1/templates`, tests first: 201 with `Location`, 204 on delete, 409
   `template_name_taken`, 422 field errors, a list taking only `limit` and `cursor`.
5. 32d, notifications from templates, tests first: rendering in a create and in each batch item, the template read
   once per batch; an unknown template, a missing variable, content beside a template, another channel, variables
   without a template, rendered content too long; the fingerprint of the request as written, whatever the order of
   its variables. End to end through the API: create, use, replace (the notification keeps its content), delete
   (the notification keeps its `templateId`).
6. 32e, the contract: the five operations, `template_name_taken`, the two forms of a create (`oneOf`), with examples
   collected from the running stack.

## Verification

`internal/notification`, `internal/httpapi`, `api/contract` and `cmd/api` with `-race` green, every answer checked
against the contract. One run of `cmd/api` failed after 63 s right after `make down`; its output was not kept, and
three runs after it passed in 5 to 7 s. Watched in the full suite.

## Follow-ups

Block 33, real-time status updates over WebSocket.
