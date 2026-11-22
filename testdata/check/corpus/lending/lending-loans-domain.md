---
kalka_kind: domain
kalka_team: lending
kalka_domain: loans
kalka_version: 2
---
# Loans

The words of the lending desk, version 2.

## Glossary

```glossary v2
term loan: One item lent to one reader
  form loans
  alias borrowing

term reader: Someone with a library card
  form readers
  alias patron
```

## Invariants

```invariants v2
invariant I1: An item is on at most one loan at a time
  verified-by Test: lending an item already on loan is refused
```
