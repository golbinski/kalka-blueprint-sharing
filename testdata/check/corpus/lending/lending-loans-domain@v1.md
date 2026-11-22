---
kalka_kind: domain
kalka_team: lending
kalka_domain: loans
kalka_version: 1
---
# Loans

The words of the lending desk, version 1.

## Glossary

```glossary v2
term loan: One item lent to one reader
  form loans
  alias borrowing

term reader: Someone with a library card
  form readers
```

## Invariants

```invariants v2
invariant I1: An item is on at most one loan at a time
  verified-by Test: lending an item already on loan is refused

invariant I2: A reader holds at most twelve loans
  verified-by Test: the thirteenth loan is refused
```
