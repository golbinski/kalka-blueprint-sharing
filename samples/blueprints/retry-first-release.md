---
reference: kalka-blueprint@v1
domain: kalka-core/sync@v1
repo: golbinski/kalka-blueprint-sharing
base_sha: 618f7edcaba5b7419069a76c039151fd26366718
---

# Retry version 1 after an accepted transfer

## Context

Accepting a document from another team installs it in this team's folder and then releases version 1 under the new owner ([install](gh:internal/core/transfer.go#L573-L626)). If that release fails, for example because the document is busy in a sync, the file stays in the folder with no release, and nothing tries again: the pending copy it came from was already removed.

## Scope

Transfers only: [afterTransfers](gh:internal/core/transfer.go#L525-L559) and `install`. Releasing in general is unchanged.

## Plan

- Keep the pending copy until version 1 is released, not only until the file is written.
- In `afterTransfers`, treat an installed file with no release, whose accepted owner is the local user, as an install still to finish, and release it.
- Log the failed attempt and leave the next one to the next team log change or round.
