---
kalka_domain: lending/loans@v1
---
# Overdue reminders

## Plan

Send a reminder the day after a borrowing is due. A patron is a reader here, not an alias yet.

```trace v2
lane business

step B1: A reader with an overdue loan gets a reminder
  guards loans#I2

lane code

step C1: remind() runs once a day
  realizes B1
  ref gh:jobs/remind.go#L1-L30
```

## Verification plan

```invariants v2
invariant N1: A reader gets at most one reminder a day
```
