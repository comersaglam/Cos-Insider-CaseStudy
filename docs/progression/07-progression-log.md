# 07. Progression log

- Date: 2026-11-23
- Commits: `4c1d2fc`
- Decisions: none

## Goal

Keep an English, block-by-block record of how the project is built, next to the ADRs and the commit history.

## Steps

1. Defined what the log holds and what it leaves to other records: decisions stay in the ADRs, code changes in
   the commit messages; the log keeps each block's goal, steps, verification and follow-ups.
2. Created `docs/progression/README.md` with the index and a template, numbered files instead of the letters used
   in conversation, and a rule for the commit hash a file cannot contain about itself.
3. Backfilled blocks 01–06 from the conversation and the git history.
4. Added the rule to `CONTRIBUTING.md`: every block is logged in the commit that finishes it.

## Verification

Every commit hash in the index was checked against `git log`. No code changed, so no tests ran.

## Follow-ups

The first implementation block: the `notification` domain, tests first.
