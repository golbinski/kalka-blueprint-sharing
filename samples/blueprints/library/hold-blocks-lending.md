---
kalka_id: 6a4d2c81-7e3f-4b05-9c1a-58d0e2f4b6a7
kalka_owner: olga@example.org
kalka_team: team-lending
kalka_issue: jira:DEV-124
kalka_domain: team-lending/library.reservations@v1
kalka_parent: 8d1c0f2a-4b7e-4c39-9a51-2e6f0b3d7c11
---

# The desk refuses to lend a held copy

## Scope

The lending desk in the `loans` module: when a copy on hold is scanned for another member, the loan is refused. A sibling of DEV-123 under the epic DEV-100; together they claim its goal G2.

## Acceptance criteria

```goals v2
criterion AC1: The desk refuses a held copy to anyone but the queue head
  realizes parent#G2
  given a copy on hold for member A
  when the desk scans it for member B
  then the loan is refused with the name of the hold
```

## Invariants

```invariants v2
affects library.reservations#R1
```

## Trace

```trace v2
lane business

step B1: The desk scans a copy for a member
  covers AC1
  next B2
step B2: A held copy is refused to anyone else

lane code

step C1: Check for a hold before opening the loan
  realizes B1
  realizes B2
  ref gh:src/loans/desk.py#L12-L40
  guards library.reservations#R1
```

## Review log

### release · olga@example.org · 2026-11-21T14:00:00Z · § · rev:34dafda5 · id:48e6da1095652d65a7ce275bf492a0ad · sig:4aFkuTsTyIax19VlPsSPo6tdJuA5ymiaCQAfs6If2g1lk9op12YQqRYTkgtO5HO9RSHvrv4Bfjadr-KTXyneAg
v1 · plan:9bf29722e6d04043ccb3ee6c29b0baddfbda99873efc28571197ca148bda7644 · owner:olga@example.org

### approved · olga@example.org · 2026-11-21T16:00:00Z · § · rev:34dafda5 · id:cd788dba347ce3e8986e160b4af9cf8a · sig:KGDC9myanUrxwYYz_I1iTWx_qWwArZclxdoOqWUAjnGH2CZgoq1Tyf-H9tGzYJgRKnIR18e6TDEFmoFyf0pXDg
approved: v1 · plan:9bf29722e6d04043ccb3ee6c29b0baddfbda99873efc28571197ca148bda7644
Reviewed with the desk staff.
