# 01. Working rules

- Date: 2026-11-23
- Commits: `054e9b4`
- Decisions: none recorded as ADRs; the rules themselves are in `CONTRIBUTING.md`

## Goal

Agree how every change in this repository is made before writing code, reusing the proven rules of an earlier
project and adapting them to a Go service.

## Steps

1. Reviewed the earlier project's working rules and selected twenty that apply here: work in blocks, tests first,
   commit messages that explain the code, surgical changes, built-ins first, one clock, one error envelope,
   no external calls in tests, PII-safe logging, and others.
2. Adapted the rules that were specific to another stack (layering, JSON naming, pagination, error contract) and
   dropped those that do not apply (frontend, deployment, team rules).
3. Decided three points that differ from the earlier project:
   - comments follow Go convention (doc comments on every package and exported identifier, "why" comments inside
     functions);
   - documentation changes in the same commit as the code it describes;
   - AI assistance is transparent: `CLAUDE.md` is tracked and commits carry a co-author trailer.
4. Chose the commit subject format of the Go project itself: `package: what now holds`.
5. Wrote `CONTRIBUTING.md` (the rules), `CLAUDE.md` (imports the rules, adds agent-only ones) and `.gitignore`
   (Turkish working copies `*.tr.md` and the assessment brief stay local).
6. Initialised the git repository.

## Verification

No code yet; the files were reviewed by the owner.

## Follow-ups

The Go-specific way of working: block 02.
