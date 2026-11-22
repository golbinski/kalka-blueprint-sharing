---
kalka_domain: loans
---
# Holds

## The hold path

```trace v2
lane desk

step D1: A reader places a hold on a book that is out

lane code

step C1: Queue the hold
  next C2
  realizes D1
  ref gh:src/holds/queue.py#L5-L30
step C2: Cap the queue at twenty readers
  ref gh:src/holds/queue.py#L31-L44
```
