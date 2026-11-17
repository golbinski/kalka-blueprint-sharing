---
kalka_reference: kalka-blueprint@v1
kalka_domain: lending/loans@v1
---

# Members renew loans online

## Context

Members renew by phone or at the desk; the desk spends an hour a day on it. This plan lets a member renew from their account page, with the same rules as the desk.

## Scope

The account page and the renewal service. Notices and the catalogue stay as they are.

## Plan

A member opens their loans, picks one and renews it. The renewal service applies the rules of the loans domain and moves the due date on; the account page shows the new due date, or why the renewal was refused.

```trace v1
lanes: [business, code]
nodes:
  - id: B1
    lane: business
    text: "A member picks a loan on their account page"
    next: [B2]
  - id: B2
    lane: business
    text: "The renewal is refused if the title has a hold or the loan was renewed twice"
    next:
      B3: "allowed"
    guards: ["loans#I2", "loans#I3"]
  - id: B3
    lane: business
    text: "The due date moves on and the member sees it"
    guards: [N1]

  - id: C1
    lane: code
    text: "renew(loan) checks holds, then the renewal count"
    realizes: [B2]
    next: [C2]
  - id: C2
    lane: code
    text: "renew() writes the new due date and an audit line in one transaction"
    realizes: [B3]
    guards: [N1, N3]
```

## Verification plan

```invariants v1
affects: ["loans#I2", "loans#I3"]
invariants:
  - id: N1
    statement: "An online renewal and a desk renewal of the same loan give the same due date"
    verified_by: ["Test: renew from the account page and at the desk, compare"]
    status: new
  - id: N2
    statement: "A member sees why a renewal was refused, in the words of the loans domain"
    verified_by: ["Reviewer: read every refusal message on the account page"]
    status: new
  - id: N3
    statement: "Every renewal leaves exactly one audit line"
    status: new
```

N2 is guarded by no step yet: the refusal message is written in the account page, which the trace does not show. N3 has no test yet; the audit table has none.

## Critical sections

The renewal count is read and written in one transaction, so two renewals at the same moment cannot both pass the check.

## Operations

Watch the refusal rate in the first week: a sudden rise means the hold check reads the wrong title. Rolling back hides the button; renewals already made stay.
