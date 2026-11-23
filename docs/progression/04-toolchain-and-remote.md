# 04. Toolchain and remote

- Date: 2026-11-23
- Commits: none (this block changed no file)
- Decisions: none

## Goal

Have the tools the decisions require installed, and the repository on GitHub.

## Steps

1. Installed Go, golangci-lint and sqlc with Homebrew.
2. Verified the versions: Go 1.27.4, golangci-lint 2.15.0, sqlc 1.31.1, Docker 29.0.1, Docker Compose 2.40.3.
3. Connected the empty GitHub repository as `origin` and pushed `main`.

## Verification

Each tool answered its version command; `git ls-remote` showed the remote empty before the push, and `main`
tracks `origin/main` after it.

## Follow-ups

The delivery and retry design: block 05.
