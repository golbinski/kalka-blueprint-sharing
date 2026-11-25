---
kalka_id: 8d1c0f2a-4b7e-4c39-9a51-2e6f0b3d7c11
kalka_owner: ewa@example.org
kalka_team: team-lending
kalka_issue: jira:DEV-100
kalka_domain: team-lending/library.reservations@v1
kalka_breakdown: decomposed
---

# Holds and notification on return

When a copy comes back, the first person in the queue should get it and be told. This epic is a plan node: its goals are realized by the leaves below it, and its invariants hold in every one of them. `kalka_breakdown: decomposed` says the split into leaves was made in kalka.

## Goals

```goals v2
goal G1: The queue head learns that a copy is waiting for them
goal G2: A held copy is not lent to anyone else
goal G3: Members can see their queue position
```

`G3` has no leaf yet. That is a warning, whether the epic is a draft or approved: approving a plan says its review is done, not that every goal is served.

## Invariants

```invariants v2
invariant E1: A member is notified at most once per hold
  why duplicate emails were the top complaint about the old system
  verified-by gh:test/notify_test.py#L20-L75
  status new
```
