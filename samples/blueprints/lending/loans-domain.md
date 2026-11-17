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
  context: "desk"
- term: "copy"
  definition: "One physical item of a title, with its own barcode"
  forms: ["copies"]
  context: "catalogue"
- term: "loan"
  definition: "One copy lent to one member, from the day it leaves the desk until it is returned"
  forms: ["loans"]
  aliases: ["borrowing", "borrowings"]
  context: "desk"
- term: "due date"
  definition: "The day a loan ends unless it is renewed"
  forms: ["due dates"]
  context: "desk"
- term: "renewal"
  definition: "Moving a loan's due date on by one loan period, without the copy coming back"
  forms: ["renewals"]
  aliases: ["extension", "extensions"]
  see: ["hold"]
  context: "desk"
- term: "hold"
  definition: "A member's place in the queue for a title; the next copy returned is kept for the first hold"
  forms: ["holds"]
  aliases: ["reservation", "reservations"]
  context: "desk"
- term: "title"
  definition: "A book, film or other work the library holds, of which it has one or more copies"
  forms: ["titles"]
  context: "catalogue"
  see: ["copy"]
- term: "overdue loan"
  definition: "A loan whose due date has passed and whose copy is not back"
  forms: ["overdue loans"]
  aliases: ["late loan", "late loans"]
  context: "desk"
  see: ["due date"]
- term: "notice"
  definition: "A message the library sends a member about a loan or a hold"
  forms: ["notices"]
  aliases: ["notification", "notifications"]
  context: "notices"
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
  - id: I5
    statement: "A member with an overdue loan cannot borrow another copy"
    context: "desk"
    why: "the copy that is out is waiting for someone else"
    verified_by: ["Test: lending to a member with an overdue loan is refused"]
  - id: I6
    statement: "A member gets at most one notice a day about the same loan"
    context: "notices"
    why: "a daily stream of messages is read as noise and the next one is ignored"
    verified_by: ["Test: two runs of the reminder job on one day send one notice"]
```

## Parts

- **Desk**: lending, return and renewal at the counter.
- **Catalogue**: titles and copies. Knows nothing about members.
- **Notices**: the messages members get about due dates and holds.
