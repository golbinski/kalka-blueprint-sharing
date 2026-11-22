---
kalka_id: overdue-plan
kalka_owner: ida@library.example
---
# Renewals

## The renewal path

```trace v2
lane desk

step D1: The reader asks to renew a loan
  next D9

lane code

step C1: Extend the due date
  realizes D1
  ref gh:src/loans/renew.py#L10-L40
```
