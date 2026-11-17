---
kalka_domain: lending/loans@v1
---
# Overdue reminders

## Plan

Send a reminder the day after a borrowing is due. A patron is a reader here, not an alias yet.

```trace v1
lanes: [business, code]
nodes:
  - id: B1
    lane: business
    text: "A reader with an overdue loan gets a reminder"
    guards: ["loans#I2"]
  - id: C1
    lane: code
    text: "remind() runs once a day"
    realizes: [B1]
    ref: "gh:jobs/remind.go#L1-L30"
```

## Verification plan

```invariants v1
invariants:
  - id: N1
    statement: "A reader gets at most one reminder a day"
```
