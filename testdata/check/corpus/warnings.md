---
kalka_domain: loans
---
# Holds

## The hold path

```trace v1
lanes: [desk, code]
nodes:
  - id: D1
    lane: desk
    text: "A reader places a hold on a book that is out"
  - id: C1
    lane: code
    text: "Queue the hold"
    realizes: [D1]
    ref: "gh:src/holds/queue.py#L5-L30"
    next: [C2]
  - id: C2
    lane: code
    text: "Cap the queue at twenty readers"
    ref: "gh:src/holds/queue.py#L31-L44"
```
