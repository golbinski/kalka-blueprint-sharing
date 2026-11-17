---
kalka_kind: domain
kalka_team: lending
kalka_domain: loans
kalka_version: 1
---
# Loans

The words of the lending desk, version 1.

## Glossary

```glossary v1
- term: "loan"
  definition: "One item lent to one reader"
  forms: ["loans"]
  aliases: ["borrowing"]
- term: "reader"
  definition: "Someone with a library card"
  forms: ["readers"]
```

## Invariants

```invariants v1
invariants:
  - id: I1
    statement: "An item is on at most one loan at a time"
    verified_by: ["Test: lending an item already on loan is refused"]
  - id: I2
    statement: "A reader holds at most twelve loans"
    verified_by: ["Test: the thirteenth loan is refused"]
```
