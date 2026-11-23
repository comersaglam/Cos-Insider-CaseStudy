# Architecture Decision Records

Each significant decision is recorded as one numbered file, in the format of Michael Nygard's ADRs: the context
that forced the decision, the decision itself, and its consequences, including what was deliberately left out.
A decision is never edited away: when it changes, a new record supersedes it and the old one says so.

Every decision follows the industry standard where one exists; a departure from it states why.

| ADR | Title | Status |
|---|---|---|
| [0001](0001-go-toolchain-and-libraries.md) | Go toolchain and libraries | Accepted |
| [0002](0002-testing-strategy.md) | Testing strategy | Accepted |

## Template

```markdown
# NNNN. Title

- Status: Proposed | Accepted | Superseded by NNNN
- Date: YYYY-MM-DD

## Context
What forces the decision: requirements, constraints, the options considered.

## Decision
What we do, stated in the present tense.

## Consequences
What becomes easier, what becomes harder, and what we deliberately do not do, each with its reason.
```
