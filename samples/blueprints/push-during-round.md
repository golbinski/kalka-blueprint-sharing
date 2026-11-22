---
kalka_reference: kalka-blueprint@v2
kalka_domain: kalka-core/sync@v1
repo: golbinski/kalka-blueprint-sharing
base_sha: 618f7edcaba5b7419069a76c039151fd26366718
---

# A push during a round is not lost

## Context

Releasing a version asks for a push: a round of the team two seconds later, so that teammates who are online get it at once. A round of a team that is already running is skipped, so two rounds never fetch the same documents twice. A push that arrived while a round ran was therefore dropped. The release still went out with the next periodic round, up to ten minutes later, and a reviewer waiting for it saw nothing.

## Scope

Touches the replication loop in `internal/core/replicate.go`: [pushTeam](gh:internal/core/replicate.go#L845-L850), [round](gh:internal/core/replicate.go#L942-L955) and the end of a round.

Does not change what a round sends, how many members it reaches, or the periodic round.

## Plan

1. A push sets the team's push flag and kicks a round after `PushDelay`, as today.
2. A round that starts clears the flag; a kick that finds a round running returns, as today.
3. A new `endRound` runs when a round finishes: if the flag was set again meanwhile, it kicks another round.
4. A daemon that is stopping or paused starts no new round; a paused one keeps the flag for its resume.

```mermaid
flowchart LR
  rel[Release] --> flag[Push flag set]
  flag -->|2 s| kick{Round running?}
  kick -->|no| run[Round clears the flag and runs]
  kick -->|yes| wait[Kick returns]
  run --> done[Round ends]
  wait --> done
  done -->|flag set again| kick
```

### The push path

```trace v2
lane business

step B1: The owner releases a version
  next B2
step B2: Online teammates get it within seconds
  next B3
step B3: A release during a sync is not held back

lane code

step C1: Set the push flag and kick after PushDelay
  next C2
  realizes B1
  ref gh:internal/core/replicate.go#L845-L850
step C2: One round per team at a time; it clears the flag as it starts
  next C3
  realizes B2
  ref gh:internal/core/replicate.go#L942-L955
  guards P2
  why two rounds of one team would fetch the same documents twice
step C3: When a round ends, kick again if the flag was set
  next C4
  realizes B3
  ref gh:internal/core/replicate.go#L930-L939
  guards sync#I5
  guards P1
step C4: No new round once the daemon is stopping
  ref gh:internal/core/replicate.go#L936-L938
  why shutdown waits for running rounds, so a new one would delay it
```

## Verification plan

```invariants v2
affects sync#I5

invariant P1: A release made while a round runs reaches an online teammate before the next periodic round
  verified-by gh:internal/core/replicate_test.go#L539-L589
  status new

invariant P2: Two rounds of one team never run at the same time
  why both would fetch the same documents
  verified-by Reviewer: read the round's start under repl.mu
```

- [TestPushDuringRound](gh:internal/core/replicate_test.go#L539-L589): a release made while a round runs reaches the teammate before the next periodic round could.
- Two quick releases in a row cause at most two rounds, not one per kick.
- Stopping the daemon during a round ends it within the drain timeout, with no round started after.
- Reviewer: run two daemons with `KALKA_TEST_LOG=1`, release during a slow round, and read the order of "round" lines.

## Critical sections

The push flag is cleared when a round **starts**, never when it ends. A push that arrives during a round sets it again, and that is what `endRound` sees. Clearing it in `endRound` would look tidier and would bring the lost push back. Both reads and writes of the flag happen under `repl.mu`.

## Operations

- Failure: a push is still lost somewhere. The periodic round every ten minutes delivers it; the Team page's *Sync now* does at once. The log shows "published … as version N" with no round after it.
- Rollback: revert the change. Nothing is stored, so there is nothing to migrate.
