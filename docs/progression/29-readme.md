# 29. README

- Date: 2026-11-23
- Commits: `dfa42c1`

## Goal

A README that gets a reviewer from a clone to a running, tested service through the make targets, then explains the
API, how it works and how it meets the brief.

## Steps

1. Wrote `README.md` in the order the owner asked: quick start (`make up`, `make smoke`, the addresses), running the
   tests (what each kind covers), the make targets, then curl examples of every operation, webhook.site, how a
   notification travels with links to the ADRs behind each step, how the brief is met, observability,
   configuration, the layout, where decisions and history live, and what is left out on purpose.
2. Checked its claims against the code: recipients masked and content never logged, the defaults of every setting
   listed, the stand-in provider's demonstration numbers, the tests' isolation.
3. `docs/requirements.md` now links the decisions behind the requirements that had none yet, and the README.
4. The roadmap records that the D2 diagram and the screenshots come after block 35, as the owner asked.

## Verification

Every relative link in the README and `docs/` resolves. The commands in the quick start are the ones verified live in
blocks 26 and 28 (`make up` healthy in 27 s, `make smoke` passing).

## Follow-ups

Block 30, CI on GitHub Actions; the README's diagram and screenshots in the closing work.
