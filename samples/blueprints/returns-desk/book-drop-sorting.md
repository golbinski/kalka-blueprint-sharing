---
reference: kalka-blueprint@v1
---

# Book drop sorting

A blueprint for a second team (`returns-desk`), to try copying and moving documents between teams.

## Context

Items returned through the outside book drop pile up overnight. In the morning one person sorts them into three carts: hold shelf, other branches, and the stacks.

## Scope

The returns screen of the desk app and the `ItemReturned` event. Not the catalogue, not loans.

## Plan

1. Scanning an item at the returns screen shows a large colour: green for the hold shelf, blue for another branch, grey for the stacks.
2. The screen keeps a count per cart, so the sorter sees when a cart is full.
3. Items for another branch get a routing slip printed on the spot.

## Verification plan

- An item with a hold at this branch always shows green, even when it belongs to another branch.
- Scanning the same item twice does not count it twice.

## Operations

- Failure: the slip printer is offline; the screen says so and keeps sorting.
