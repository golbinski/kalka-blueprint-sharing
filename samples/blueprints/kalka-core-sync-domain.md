---
kind: domain
domain: sync
---

# Sync domain

The shared language of kalka's sync: what the words in the team's blueprints mean, and what must always hold. Blueprints name the version they were written against with `domain: kalka-core/sync@v<N>`.

## Glossary

| Term | Meaning |
|---|---|
| Blueprint | A Markdown file describing an implementation plan. Its body is the plan; a `## Review log` at the end holds its entries. |
| Entry | A signed comment, deviation or release in the review log. Entries are only ever appended. |
| Owner | The one member who may change a document's plan. Everyone else adds entries. |
| Release | The owner's signed entry that makes the plan as it stands version N. Edits not yet released do not travel. |
| Team log | The signed operations of a team: members, owners, reassignments, transfer decisions. Replayed, it gives the roster. |
| Replica | A member's copy of a team document. Every member holds one. |
| Round | One pass of replication for a team: reach a few online members, compare inventories, send and fetch what differs. |
| Push | A round asked for because something changed here, run soon instead of at the next periodic round. |
| Rendezvous | The server that introduces machines and relays traffic. It stores nothing and sees no content. |

## Parts

- **Store**: the folder, its index and its watcher. Knows nothing about teams.
- **Team log**: operations, replay, invites. Knows nothing about documents.
- **Replication**: rounds, inventories and merging. Uses both, and owns when documents travel.

## Invariants

1. Only a release by the current owner changes the plan other members see.
2. Every member who replays the same team log gets the same roster.
3. A merge never drops an entry either side holds.
4. A team holds at most one document per domain.

## Team log operations

- `create-team {email, name}`
- `add-member {email, pubkey}`, with the invite it used
- `remove-member {email}`
- `add-owner {email}`, `remove-owner {email}`
- `reassign-document {doc, owner, from, after, plan}`
- `accept-transfer {doc, owner, transfer}`, `decline-transfer {doc, transfer}`
