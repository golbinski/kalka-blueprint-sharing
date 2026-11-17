---
kalka_reference: kalka-blueprint@v1
kalka_domain: lending/loans@v1
---

# Remind members of overdue loans

## Context

A copy that comes back late has usually been forgotten, not kept. Today the desk phones members one at a time, and the calls stop whenever the desk is busy. This plan sends notices on a schedule instead, and keeps the phone for the loans that stay overdue longest.

## Scope

The notices context: a daily reminder job and its message text. The desk's lending screen and the catalogue do not change.

## Plan

Each morning the job lists the overdue loans. For a loan 1, 8 or 15 days past its due date, it sends the member a notice naming the title, the branch and the due date. A loan 22 days past goes on the desk's list for a call. Returning the copy ends the reminders.

```trace v1
lanes: [business, service, store]
nodes:
  - id: B1
    lane: business
    text: "A member is reminded when a loan is overdue"
    next: [B2]
  - id: B2
    lane: business
    text: "The reminders stop when the copy is back"
    next: [B3]
  - id: B3
    lane: business
    text: "The desk phones every member with an overdue loan"
    change: removed
    next: [B4]
  - id: B4
    lane: business
    text: "The desk phones a member whose loan stays overdue three weeks"
    change: new

  - id: S1
    lane: service
    text: "List the overdue loans each morning"
    change: new
    realizes: [B1]
    next: [S2]
  - id: S2
    lane: service
    text: "Pick the loans 1, 8 or 15 days past their due date"
    change: new
    realizes: [B1]
    guards: [N1]
    next: [S3]
  - id: S3
    lane: service
    text: "Send one notice for each, naming title, branch and due date"
    change: new
    realizes: [B1]
    guards: ["loans#I6", N2]
    back:
      S3: "while loans remain"
    next: [S4]
  - id: S4
    lane: service
    text: "Put loans 22 days overdue on the desk's call list"
    change: new
    realizes: [B4]
    guards: [N1]

  - id: D1
    lane: store
    text: "Read each overdue loan with its due date and last notice"
    change: new
    realizes: [S1, S2]
    next: [D2]
  - id: D2
    lane: store
    text: "Record each notice sent, with the loan and the day"
    change: new
    realizes: [S3]
    guards: ["loans#I6"]
    next: [D3]
  - id: D3
    lane: store
    text: "Add a loan to the call list"
    change: new
    realizes: [S4]
    next: [D4]
  - id: D4
    lane: store
    text: "Stop the reminders once the loan is returned"
    change: new
    realizes: [B2]
    why: "no service step is needed: a returned loan is no longer on the overdue list"
```

## Verification plan

```invariants v1
affects: ["loans#I6"]
invariants:
  - id: N1
    statement: "A loan 22 or more days overdue gets no notice, only a place on the call list"
    why: "a fourth message adds nothing the desk's call does not"
    verified_by: ["Test: run the job on a loan 22 days overdue and read the call list"]
    status: changed
  - id: N2
    statement: "A notice names the title, the branch and the due date, in the words of the loans domain"
    verified_by: ["Reviewer: read the notice text for each of the three days"]
    status: new
  - id: N3
    statement: "A member is phoned before a loan is a week overdue"
    status: removed
```

N3 is the practice this plan ends, listed with `status: removed` so reviewers see it go; kalka asks nothing of it.

## Critical sections

The job reads the day's notices before it sends, in one query, rather than asking per loan. It looks wasteful on a small branch and is what keeps the job under a minute at the central library.

## Operations

Watch the number of notices sent on the first day: it should match the loans 1, 8 and 15 days overdue, not all of them. Rolling back stops the job; notices already sent stay recorded.
