# 0032. Templates

- Status: Accepted
- Date: 2026-11-24

## Context

The brief's bonus asks for templates with variable substitution. Three choices were the owner's; the worked
example below runs through them: a team creates `order-shipped` for SMS, `Hi {{.name}}, your order {{.order}} has
shipped.`, and a client sends a notification with its `templateId` and `{"name": "Ayşe", "order": "42"}`.

| Engine | The example | `{{range 200000000}}{{end}}` | Cost |
|---|---|---|---|
| **`text/template`, placeholders only** | renders | refused when the template is created | a walk of the parse tree |
| `text/template` in full | renders; `{{if}}`, `{{range}}`, functions too | accepted: 3.4 s of CPU per render, measured, and a render cannot be cancelled | none |
| A substitution of our own | renders, as `{{name}}` | no such construct | a parser of our own to write and test |

| Rendered | A missing variable | Stored content | The template changes later |
|---|---|---|---|
| **When the notification is created** | `422` at once | what is sent | accepted notifications keep their content |
| When it is sent | the notification fails in the worker | a reference; the sent text is known only later | waiting notifications are sent with the new text; deleting the template breaks them |

| Management | |
|---|---|
| **Full CRUD** | create, read, list, replace, delete |
| Immutable templates | a change is a new template, and clients move to its ID |

## Decision

- **Templates belong to the notification module**: rendering is a step of creating a notification, and a module of
  their own would need an interface back into this one. `templates` is a table of its own (`migrations/0008`).
- **A template** has a unique name (lower-case words joined by hyphens, at most 64 characters), a channel, and a
  body that follows the channel's content rules. **The body may hold only text and `{{.name}}` placeholders**: it
  is parsed with `text/template` and its parse tree is walked; a condition, loop, function, pipeline, variable,
  nested field, `{{.}}` or `{{define}}` is refused when the template is created or replaced. Rendering is then
  linear in the body and its values, and a value is inserted as it is, never rendered again. The API lists the
  variables a body needs.
- **`/v1/templates`**: `POST` (201, `Location`), `GET` (a keyset-paginated list, newest first), and
  `/v1/templates/{id}` with `GET`, `PUT` (replaces the whole template) and `DELETE` (204). A taken name is
  `409 template_name_taken`, detected from PostgreSQL's unique constraint, so two requests racing for one name
  cannot both win.
- **A notification from a template** gives `templateId` and `variables` instead of `content`; `channel` may be left
  out and, if given, must be the template's. It is rendered when the notification is created, in a create and in
  each batch item, a batch reading each template once. Every missing variable is named at once
  (`/variables`, `required`); an unknown template is `/templateId` `invalid`; the rendered content is then checked
  as written content is. The notification keeps its rendered content and `templateId`.
- **Idempotency**: the fingerprint is of the request as written, the template's ID and the variables sorted by
  name, so a repeat is the same request even after the template has changed. A request without a template keeps the
  fingerprint it had.
- **`templateId` on a notification is history, not a foreign key**: deleting a template leaves it in place.

## Consequences

- A template cannot slow the service down, whoever writes it.
- What a notification says is fixed when it is accepted; changing or deleting a template never changes what was sent
  or what waits.
- A notification's `/content` error may refer to text the client did not write, the rendered content; the problem's
  detail says so by its limit.

### Deliberately not done

| Not done | Why not |
|---|---|
| Naming a template by name in a notification | An ID does not change when a template is renamed; names are for people. |
| Conditions and loops in templates | Not asked for, and the reason rendering is safe. |
| HTML escaping | The provider receives plain text content; nothing renders it as HTML here. |
| Template versions | Notifications keep their rendered content, which is the history that matters. |
