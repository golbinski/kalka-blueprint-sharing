---
kalka_reference: kalka-blueprint@v2
kalka_domain: lending/loans@v1
---

# Remind members of overdue loans

## Context

A copy that comes back late has usually been forgotten, not kept. Today the desk phones members one at a time, and the calls stop whenever the desk is busy. This plan sends notices on a schedule instead, and keeps the phone for the loans that stay overdue longest.

## Scope

The notices context: a daily reminder job and its message text. The desk's lending screen and the catalogue do not change.

## Plan

Each morning the job lists the overdue loans. For a loan 1, 8 or 15 days past its due date, it sends the member a notice naming the title, the branch and the due date. A loan 22 days past goes on the desk's list for a call. Returning the copy ends the reminders.

```trace v2
lane business

step B1: A member is reminded when a loan is overdue
  next B2
step B2: The reminders stop when the copy is back
  next B3
step B3: The desk phones every member with an overdue loan
  next B4
  change removed
step B4: The desk phones a member whose loan stays overdue three weeks
  change new

lane service

step S1: List the overdue loans each morning
  next S2
  realizes B1
  change new
step S2: Pick the loans 1, 8 or 15 days past their due date
  next S3
  realizes B1
  guards N1
  change new
step S3: Send one notice for each, naming title, branch and due date
  next S4
  back S3: while loans remain
  realizes B1
  guards loans#I6
  guards N2
  change new
step S4: Put loans 22 days overdue on the desk's call list
  realizes B4
  guards N1
  change new

lane store

step D1: Read each overdue loan with its due date and last notice
  next D2
  realizes S1
  realizes S2
  change new
step D2: Record each notice sent, with the loan and the day
  next D3
  realizes S3
  guards loans#I6
  change new
step D3: Add a loan to the call list
  next D4
  realizes S4
  change new
step D4: Stop the reminders once the loan is returned
  realizes B2
  why no service step is needed: a returned loan is no longer on the overdue list
  change new
```

## Verification plan

```invariants v2
affects loans#I6

invariant N1: A loan 22 or more days overdue gets no notice, only a place on the call list
  why a fourth message adds nothing the desk's call does not
  verified-by Test: run the job on a loan 22 days overdue and read the call list
  status changed

invariant N2: A notice names the title, the branch and the due date, in the words of the loans domain
  verified-by Reviewer: read the notice text for each of the three days
  status new

invariant N3: A member is phoned before a loan is a week overdue
  status removed
```

N3 is the practice this plan ends, listed with `status: removed` so reviewers see it go; kalka asks nothing of it.

## Critical sections

The job reads the day's notices before it sends, in one query, rather than asking per loan. It looks wasteful on a small branch and is what keeps the job under a minute at the central library.

## Operations

Watch the number of notices sent on the first day: it should match the loans 1, 8 and 15 days overdue, not all of them. Rolling back stops the job; notices already sent stay recorded.
