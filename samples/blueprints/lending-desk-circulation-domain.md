---
kind: domain
domain: circulation
---

# Circulation domain

The shared language of the lending desk: what the words in our blueprints mean, and what must always hold. Blueprints name the version they were written against with `domain: lending-desk/circulation@v<N>`.

## Glossary

| Term | Meaning |
|---|---|
| Member | A person with a library card in good standing. A card that has lapsed is not a member until it is renewed. |
| Item | One physical copy: a book, a disc, a board game. Two copies of one title are two items. |
| Loan | An item checked out to a member, from the moment it is lent until it is returned at any branch. |
| Due date | The last day of a loan. It moves only by a renewal. |
| Renewal | Extending a loan by one loan period. At most two per loan. |
| Hold | A member's request for the next available copy of a title. |
| Hold shelf | Where items waiting for the member who placed the hold are kept, at the pickup branch. |
| Pickup window | How long an item stays on the hold shelf: 7 days from the notice. |

## Bounded contexts

- **Catalogue**: titles and items, and where each item is. Knows nothing about members.
- **Circulation**: loans, renewals, holds and the hold shelf. Owns the due date.
- **Notices**: what members are told and when, by email or text. Never changes a loan.

## Invariants

1. An item is on at most one loan at a time.
2. An item on the hold shelf is reserved for exactly one hold.
3. A loan with an open hold on its title cannot be renewed.
4. A due date never moves backwards.

## Domain events

- `LoanStarted {item, member, due}`
- `LoanRenewed {item, member, due}`
- `ItemReturned {item, branch}`
- `HoldPlaced {title, member, pickup_branch}`
- `HoldReady {hold, item, expires}`
- `HoldExpired {hold, item}`
