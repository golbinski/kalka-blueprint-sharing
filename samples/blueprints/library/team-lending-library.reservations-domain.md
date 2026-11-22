---
kalka_id: 5d2e8a10-3c6f-4b9e-a7d4-1f0e6b8c2a93
kalka_kind: domain
kalka_domain: library.reservations
kalka_parent: team-lending/library@v2
kalka_version: 1
kalka_owner: marek@example.org
---

# Reservations

The reservation queue subdomain. A domain name is a path in the tree (`library.reservations`) and starts with the parent's name. It pins `library@v2`; a new version of the parent bumps nothing here, it only shows "parent newer".

## Glossary

```glossary v2
term queue: The ordered list of open reservations for one title
  form queues
  see reservation

term queue head: The member whose reservation is first in the queue
  see queue
  see member

term hold: A copy set aside for the queue head after a return
  form holds
  form held
  see copy
  see queue head
```

## Invariants

`refines` names a term of a domain above this one. Without `refines loan`, R2 would narrow an inherited term without saying so.

```invariants v2
invariant R1: A copy on hold is lent only to the member it is held for
  why otherwise the queue head loses the copy they waited for
  verified-by gh:test/holds_test.py#L5-L60

invariant R2: A loan of a copy whose title has open reservations cannot be renewed
  refines loan
  why renewing would keep the queue waiting indefinitely
  verified-by gh:test/renewals_test.py#L12-L38
  verified-by Reviewer: try renewing with one open reservation, then with none
```
