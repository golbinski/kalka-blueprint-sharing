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

```glossary v1
- term: "blueprint"
  definition: "A Markdown file describing an implementation plan. Its body is the plan; a Review log at the end holds its entries."
  forms: ["blueprints"]
  aliases: ["design doc", "design docs"]
  context: "store"
- term: "entry"
  definition: "A signed comment, deviation or release in the review log. Entries are only ever appended."
  forms: ["entries"]
  context: "store"
- term: "owner"
  definition: "The one member who may change a document's plan. Everyone else adds entries."
  forms: ["owners"]
  see: ["release"]
  context: "ownership"
- term: "release"
  definition: "The owner's signed entry that makes the plan as it stands version N. Edits not yet released do not travel."
  forms: ["releases"]
  context: "ownership"
- term: "team log"
  definition: "The signed operations of a team: members, owners, reassignments, transfer decisions. Replayed, it gives the roster."
  context: "team log"
- term: "replica"
  definition: "A member's copy of a team document. Every member holds one."
  forms: ["replicas"]
  context: "replication"
- term: "round"
  definition: "One pass of replication for a team: reach a few online members, compare inventories, send and fetch what differs."
  forms: ["rounds"]
  aliases: ["sync cycle"]
  context: "replication"
- term: "push"
  definition: "A round asked for because something changed here, run soon instead of at the next periodic round."
  forms: ["pushes"]
  context: "replication"
- term: "rendezvous"
  definition: "The server that introduces machines and relays traffic. It stores nothing and sees no content."
  context: "transport"
- term: "seed"
  definition: "A small blueprint for an idea split off from a plan whose scope was growing; it is kept until someone takes it up"
  forms: ["seeds"]
  context: "store"
```

## Parts

- **Store**: the folder, its index and its watcher. Knows nothing about teams.
- **Team log**: operations, replay, invites. Knows nothing about documents.
- **Replication**: rounds, inventories and merging. Uses both, and owns when documents travel.

## Invariants

```invariants v1
invariants:
  - id: I1
    statement: "Only a release by the current owner changes the plan other members see"
    context: "ownership"
    why: "a plan edited by anyone else would be two plans with one name"
    verified_by: ["gh:internal/core/ownership_test.go"]
  - id: I2
    statement: "Every member who replays the same team log gets the same roster"
    context: "team log"
    verified_by: ["gh:internal/teamlog/replay_test.go"]
  - id: I3
    statement: "A merge never drops an entry either side holds"
    context: "replication"
    why: "entries are the review; a lost one is a lost comment"
    verified_by: ["gh:internal/merge/merge_test.go"]
  - id: I4
    statement: "A team holds at most one document per domain"
    verified_by: ["gh:internal/core/domain_test.go"]
  - id: I5
    statement: "A push is always followed by a round that starts after it"
    context: "replication"
    why: "the owner who releases expects online teammates to have it within seconds, not at the next periodic round"
    verified_by: ["gh:internal/core/replicate_test.go#L539-L589"]
```

## Team log operations

- `create-team {email, name}`
- `add-member {email, pubkey}`, with the invite it used
- `remove-member {email}`
- `add-owner {email}`, `remove-owner {email}`
- `reassign-document {doc, owner, from, after, plan}`
- `accept-transfer {doc, owner, transfer}`, `decline-transfer {doc, transfer}`
