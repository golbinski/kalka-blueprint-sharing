---
kalka_reference: kalka-blueprint@v2
kalka_domain: kalka-core/sync@v1
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

```trace v2
lane business

step B1: An accepted document becomes an ordinary team document
  next B2
step B2: Its owner's version 1 reaches the team

lane code

step C1: Write the file into the team's folder
  next C2
  realizes B1
  ref gh:internal/core/transfer.go#L606-L613
step C2: Remove the pending copy once the file is written
  next C3
  ref gh:internal/core/transfer.go#L614
  why a failed release would leave no copy to retry from
  change removed
step C3: Release version 1 under the new owner
  next C4: released
  next C5: release failed
  realizes B2
  ref gh:internal/core/transfer.go#L620-L625
step C4: Remove the pending copy
  realizes B2
  why the copy is needed until version 1 exists
  change new
step C5: Log the failure and keep the pending copy
  back C3: next team log change or round
  realizes B2
  ref gh:internal/core/transfer.go#L621-L623
  change modified
```
