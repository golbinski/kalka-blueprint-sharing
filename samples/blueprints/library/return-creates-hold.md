---
kalka_id: 3e9b7a60-1f24-4d8e-b0c5-7a2d91e4f803
kalka_owner: marek@example.org
kalka_team: team-lending
kalka_issue: jira:DEV-123
kalka_domain: team-lending/library.reservations@v1
kalka_parent: 8d1c0f2a-4b7e-4c39-9a51-2e6f0b3d7c11
---

# Return creates a hold and notifies the queue head

## Scope

Returning a copy in the `loans` module, and a new `holds` module. Queue position (G3 of the epic) is out of scope.

## Acceptance criteria

Each criterion names the goal of the epic it realizes, `parent#<id>`.

```goals v2
criterion AC1: The queue head is notified exactly once
  realizes parent#G1
  given a copy whose title has two open reservations
  when the copy is returned
  then the queue head gets one email
  then the second member gets nothing

criterion AC2: A held copy is refused to other members
  realizes parent#G2
  given a copy on hold for member A
  when member B tries to borrow it
  then the loan is refused
  then the copy stays on hold
```

## Invariants

The plan adds no invariants of its own; it declares which domain and epic invariants it touches.

```invariants v2
affects library.reservations#R1
affects parent#E1
```

## Trace

```trace v2
lane business

step B1: A member returns a copy
  next B2
step B2: The copy is held for the queue head
  covers AC2
  next B3
step B3: The queue head is told
  covers AC1

lane code

step C1: Close the loan and free the copy
  realizes B1
  ref gh:src/loans/return.py#L18-L52
  guards library#L1
  next C2
step C2: Look up the queue for the title
  ref gh:src/holds/queue.py#L8-L30
  change new
  why the old return path never read reservations
  next C3: queue not empty
  next C5: queue empty
step C3: Create a hold for the queue head
  realizes B2
  change new
  guards library.reservations#R1
  next C4
step C4: Email the queue head unless already notified for this hold
  realizes B3
  change new
  guards parent#E1
step C5: Put the copy back on the shelf
  ref gh:src/loans/return.py#L54-L70
  why nobody is waiting, so the copy becomes available
```

One chain runs from the epic to the code: `parent#G1` ← `AC1` ← `B3` ← `C4`.

## Review log

### release · marek@example.org · 2026-11-20T09:30:00Z · § · rev:89598eac · id:6d90b9e8fb58e81ba3217d7f2bc35604 · sig:oIWI3fw7QgNgqP7AXfwsq81LBc4nqx4Com4APD0xziE-mY9hpY6SVv6wKkTPTmxd0bZQhO-SWHfvnYe7G9MgDg
v1 · plan:a69bd0a54bb2aa4096e8d77dacca5da724f17cb34f556520a3cae3f2aded98a3 · owner:marek@example.org

### approved · marek@example.org · 2026-11-20T11:30:00Z · § · rev:89598eac · id:70026de122237a30bd2411d6c854a95a · sig:vG5fbGgTUp-w06eSTAXEQKJQiJFuoS-Fh9_rr1yVBSqRWCp_b_lBhpcNTVd8CY12fElX5A7snByntH4tYy9HDQ
approved: v1 · plan:a69bd0a54bb2aa4096e8d77dacca5da724f17cb34f556520a3cae3f2aded98a3
Reviewed with the desk staff.
