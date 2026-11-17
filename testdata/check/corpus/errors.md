---
kalka_id: overdue-plan
kalka_owner: ida@library.example
---
# Renewals

## The renewal path

```trace v1
lanes: [desk, code]
nodes:
  - id: D1
    lane: desk
    text: "The reader asks to renew a loan"
    next: [D9]
  - id: C1
    lane: code
    text: "Extend the due date"
    realizes: [D1]
    ref: "gh:src/loans/renew.py#L10-L40"
```
