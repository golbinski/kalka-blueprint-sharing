---
kalka_id: 0b6f3c2e-9a41-4d7b-8e15-6c2a9f0d4e77
kalka_kind: domain
kalka_domain: library
kalka_version: 2
kalka_owner: ewa@example.org
---

# Library

The shared language of the lending library: the root of the team's domain tree. Every subdomain inherits these terms and invariants and may only extend them.

## Glossary

```glossary v2
term member: A person registered with the library who may borrow copies
  alias patron
  alias reader

term copy: One physical item of a title that can be lent
  form copies
  alias item
  alias book

term loan: A copy lent to one member for a fixed period
  form loans
  alias checkout
  see copy
  see member

term reservation: A member's request to borrow a title as soon as a copy is free
  form reservations
  alias hold request

term return: A member bringing a lent copy back
  form returns
  form returned
```

## Invariants

```invariants v2
invariant L1: A copy is on at most one loan at a time
  why two members cannot hold the same physical item
  verified-by gh:test/loans_test.py#L10-L44
```
