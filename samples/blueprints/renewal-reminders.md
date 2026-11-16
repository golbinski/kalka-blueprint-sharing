---
reference: kalka-blueprint@v1
domain: lending-desk/circulation@v1
---

# Renewal reminders

## Context

Members ask for a reminder before a loan is due, with a link to renew it when they can.

## Scope

The Notices context only: a new reminder notice. Renewal itself is unchanged.

## Plan

- Three days before the due date, send a reminder listing the member's loans due that day.
- Say for each loan whether it can be renewed. It cannot be when the title has an open hold (circulation invariant 3) or after two renewals.
- One reminder per member per day, however many loans fall due.
