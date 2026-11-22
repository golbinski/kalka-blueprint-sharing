---
kalka_kind: domain
kalka_domain: loans
kalka_version: 1
---

# Loans

The shared language of a lending library's loans desk, and what must always hold. This copy says `kalka_version: 1`, as a released version does, so `kalka check --domain` can read it from a repository: the plans next to it reference `lending/loans@v1`.

## Glossary

```glossary v2
term member: Someone with a library card, who may borrow copies
  form members
  alias patron
  alias patrons
  context desk

term copy: One physical item of a title, with its own barcode
  form copies
  context catalogue

term loan: One copy lent to one member, from the day it leaves the desk until it is returned
  form loans
  alias borrowing
  alias borrowings
  context desk

term due date: The day a loan ends unless it is renewed
  form due dates
  context desk

term renewal: Moving a loan's due date on by one loan period, without the copy coming back
  form renewals
  alias extension
  alias extensions
  see hold
  context desk

term hold: A member's place in the queue for a title; the next copy returned is kept for the first hold
  form holds
  alias reservation
  alias reservations
  context desk

term title: A book, film or other work the library holds, of which it has one or more copies
  form titles
  see copy
  context catalogue

term overdue loan: A loan whose due date has passed and whose copy is not back
  form overdue loans
  alias late loan
  alias late loans
  see due date
  context desk

term notice: A message the library sends a member about a loan or a hold
  form notices
  alias notification
  alias notifications
  context notices
```

## Invariants

```invariants v2
invariant I1: A copy is on at most one loan at a time
  why the barcode names the copy; two loans of one copy mean one of them is wrong
  verified-by Test: the desk refuses to lend a copy already on loan

invariant I2: A loan is renewed at most twice
  why other members wait for popular titles; two renewals keep a loan under nine weeks
  verified-by Test: the third renewal is refused

invariant I3: A loan is not renewed while another member has a hold on its title
  why a hold is a promise to the next member
  verified-by Test: renewal with a waiting hold is refused

invariant I4: A member has at most ten loans
  verified-by Reviewer: the desk report lists no member over ten

invariant I5: A member with an overdue loan cannot borrow another copy
  why the copy that is out is waiting for someone else
  verified-by Test: lending to a member with an overdue loan is refused

invariant I6: A member gets at most one notice a day about the same loan
  why a daily stream of messages is read as noise and the next one is ignored
  verified-by Test: two runs of the reminder job on one day send one notice
```

## Parts

- **Desk**: lending, return and renewal at the counter.
- **Catalogue**: titles and copies. Knows nothing about members.
- **Notices**: the messages members get about due dates and holds.
