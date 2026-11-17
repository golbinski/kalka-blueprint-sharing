---
kalka_kind: domain
kalka_domain: loans
kalka_version: 1
---

# Loans

The shared language of a lending library's loans desk, and what must always hold. This copy says `kalka_version: 1`, as a released version does, so `kalka check --domain` can read it from a repository: the plans next to it reference `lending/loans@v1`.

## Glossary

```glossary v1
- term: "member"
  definition: "Someone with a library card, who may borrow copies"
  forms: ["members"]
  aliases: ["patron", "patrons"]
- term: "copy"
  definition: "One physical item of a title, with its own barcode"
  forms: ["copies"]
  context: "catalogue"
- term: "loan"
  definition: "One copy lent to one member, from the day it leaves the desk until it is returned"
  forms: ["loans"]
  aliases: ["borrowing", "borrowings"]
- term: "due date"
  definition: "The day a loan ends unless it is renewed"
  forms: ["due dates"]
- term: "renewal"
  definition: "Moving a loan's due date on by one loan period, without the copy coming back"
  forms: ["renewals"]
  aliases: ["extension", "extensions"]
  see: ["hold"]
- term: "hold"
  definition: "A member's place in the queue for a title; the next copy returned is kept for the first hold"
  forms: ["holds"]
  aliases: ["reservation", "reservations"]
```

## Invariants

```invariants v1
invariants:
  - id: I1
    statement: "A copy is on at most one loan at a time"
    why: "the barcode names the copy; two loans of one copy mean one of them is wrong"
    verified_by: ["Test: the desk refuses to lend a copy already on loan"]
  - id: I2
    statement: "A loan is renewed at most twice"
    why: "other members wait for popular titles; two renewals keep a loan under nine weeks"
    verified_by: ["Test: the third renewal is refused"]
  - id: I3
    statement: "A loan is not renewed while another member has a hold on its title"
    why: "a hold is a promise to the next member"
    verified_by: ["Test: renewal with a waiting hold is refused"]
  - id: I4
    statement: "A member has at most ten loans"
    verified_by: ["Reviewer: the desk report lists no member over ten"]
```

## Parts

- **Desk**: lending, return and renewal at the counter.
- **Catalogue**: titles and copies. Knows nothing about members.
- **Notices**: the messages members get about due dates and holds.
