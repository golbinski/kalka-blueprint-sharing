---
kalka_reference: kalka-blueprint@v1
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

```trace v1
lanes: [business, code]
nodes:
  - id: B1
    lane: business
    text: "An accepted document becomes an ordinary team document"
    next: [B2]
  - id: B2
    lane: business
    text: "Its owner's version 1 reaches the team"

  - id: C1
    lane: code
    text: "Write the file into the team's folder"
    realizes: [B1]
    ref: "gh:internal/core/transfer.go#L606-L613"
    next: [C2]
  - id: C2
    lane: code
    text: "Remove the pending copy once the file is written"
    change: removed
    ref: "gh:internal/core/transfer.go#L614"
    why: "a failed release would leave no copy to retry from"
    next: [C3]
  - id: C3
    lane: code
    text: "Release version 1 under the new owner"
    realizes: [B2]
    ref: "gh:internal/core/transfer.go#L620-L625"
    next:
      C4: "released"
      C5: "release failed"
  - id: C4
    lane: code
    text: "Remove the pending copy"
    change: new
    realizes: [B2]
    why: "the copy is needed until version 1 exists"
  - id: C5
    lane: code
    text: "Log the failure and keep the pending copy"
    change: modified
    realizes: [B2]
    ref: "gh:internal/core/transfer.go#L621-L623"
    back:
      C3: "next team log change or round"
```
