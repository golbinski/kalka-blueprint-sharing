---
kalka_kind: domain
kalka_domain: sync
kalka_version: 1
repo: golbinski/kalka-blueprint-sharing
base_sha: 618f7edcaba5b7419069a76c039151fd26366718
---

# Sync domain

The shared language of kalka's sync: what the words in the team's blueprints mean, and what must always hold. Blueprints name the version they were written against with `kalka_domain: kalka-core/sync@v<N>`. The file says `kalka_version: 1`, as a released copy does, so `kalka check --domain` can read it from a repository.

## Glossary

```glossary v2
term blueprint: A Markdown file describing an implementation plan. Its body is the plan; a Review log at the end holds its entries.
  form blueprints
  alias design doc
  alias design docs
  context store

term entry: A signed comment, deviation or release in the review log. Entries are only ever appended.
  form entries
  context store

term owner: The one member who may change a document's plan. Everyone else adds entries.
  form owners
  see release
  context ownership

term release: The owner's signed entry that makes the plan as it stands version N. Edits not yet released do not travel.
  form releases
  context ownership

term team log: The signed operations of a team: members, owners, reassignments, transfer decisions. Replayed, it gives the roster.
  context team log

term replica: A member's copy of a team document. Every member holds one.
  form replicas
  context replication

term round: One pass of replication for a team: reach a few online members, compare inventories, send and fetch what differs.
  form rounds
  alias sync cycle
  context replication

term push: A round asked for because something changed here, run soon instead of at the next periodic round.
  form pushes
  context replication

term rendezvous: The server that introduces machines and relays traffic. It stores nothing and sees no content.
  context transport

term seed: A small blueprint for an idea split off from a plan whose scope was growing; it is kept until someone takes it up
  form seeds
  context store
```

## Parts

- **Store**: the folder, its index and its watcher. Knows nothing about teams.
- **Team log**: operations, replay, invites. Knows nothing about documents.
- **Replication**: rounds, inventories and merging. Uses both, and owns when documents travel.

## Invariants

```invariants v2
invariant I1: Only a release by the current owner changes the plan other members see
  why a plan edited by anyone else would be two plans with one name
  verified-by gh:internal/core/ownership_test.go

invariant I2: Every member who replays the same team log gets the same roster
  verified-by gh:internal/teamlog/replay_test.go

invariant I3: A merge never drops an entry either side holds
  why entries are the review; a lost one is a lost comment
  verified-by gh:internal/merge/merge_test.go

invariant I4: A team holds at most one document per domain
  verified-by gh:internal/core/domain_test.go

invariant I5: A push is always followed by a round that starts after it
  why the owner who releases expects online teammates to have it within seconds, not at the next periodic round
  verified-by gh:internal/core/replicate_test.go#L539-L589
```

## Team log operations

- `create-team {email, name}`
- `add-member {email, pubkey}`, with the invite it used
- `remove-member {email}`
- `add-owner {email}`, `remove-owner {email}`
- `reassign-document {doc, owner, from, after, plan}`
- `accept-transfer {doc, owner, transfer}`, `decline-transfer {doc, transfer}`
