---
name: kalka-blueprint
description: How to write a blueprint (a Markdown implementation plan reviewed with kalka) that agents can act on safely. Use when writing, extending or implementing a blueprint, or when recording where an implementation departed from one.
kalka_reference: kalka-blueprint@v1
---

# Writing blueprints agents can use

A *blueprint* is one Markdown file describing an implementation plan. People and agents write it with their own tools; kalka passes it between teammates for review and collects their comments in a `## Review log` at the end of the file.

This reference describes what a blueprint useful to agents usually contains. It is guidance, not a template you must fill: a short plan for a small change may need two of these sections, and kalka accepts any Markdown. The examples come from an invented parcel-delivery service.

## Opting in

Add one line to the blueprint's frontmatter:

```yaml
kalka_reference: kalka-blueprint@v1
```

kalka then shows soft hints, such as "no verification plan" (rules `H001` to `H005`), which anyone can dismiss. Documents without this line get no hints. Hints are information: they never block a sync and never fail `kalka check`.

## Rules for agents working on a blueprint

- **The body belongs to the owner.** The frontmatter `kalka_owner:` names them. If you are not acting for the owner, do not edit the body: add a comment instead (`kalka comment`).
- **Never edit the review log by hand.** Entries are signed and hashed; an edited entry becomes invalid. Add entries with `kalka comment <file> --anchor <slug> -m "<text>"`.
- **Leave unknown frontmatter alone.** Keys you do not understand belong to someone's pipeline.
- **Stay inside the scope.** Work that does not fit becomes a seed, not a larger change (see "When to split off a seed").
- **Do not "clean up" critical sections.** They look wrong on purpose. Read their reasoning first and ask a human.
- **Trace node ids are permanent.** Comments are attached to them; keep them when you change a trace (see "Trace blocks").

## The sections

Use these headings (any level) so that people, agents and kalka's hints find them. Each one says why it helps an agent.

### Domain reference

Which domain document, and which version of it, the plan was written against. Put it in the frontmatter, and repeat it in prose if the plan relies on particular terms:

```yaml
kalka_domain: parcels/delivery@v7
```

Write the plan in the glossary's terms. Where the domain lists a word as an alias ("package" for "parcel"), use the term: `kalka check` warns about each alias it finds, with the term to write instead (`G020`). Name the domain's invariants the plan touches in `affects` (see "Verification plan").

*Why agents benefit:* the domain document defines the team's words (what a "consignment" is, when a parcel counts as "delivered"). An agent that reads the right version uses the same names and respects the same invariants as the rest of the code. kalka shows when a newer version exists, so the plan's author can decide whether it matters.

### Scope

The bounded contexts, modules and interfaces the change is expected to touch, and anything it must not touch.

> Touches the `redelivery` module and the `DeliveryAttempt` table. Does not change the courier app's API or the tracking page.

*Why agents benefit:* an explicit boundary lets an agent tell "part of this plan" from "a new idea". When the work turns out to need more, the agent stops and proposes a seed instead of quietly growing the change, which keeps the review the reviewers agreed to.

### Verification plan

Invariants that must hold, how each is verified, and what a human reviewer must specifically check, as an `invariants v1` block. Write it while planning, not while reviewing.

````markdown
```invariants v1
affects: ["delivery#I3"]
invariants:
  - id: N1
    statement: "A parcel is never scheduled for more than three redelivery attempts"
    verified_by: ["gh:test/redelivery_test.py#L40-L90"]
    status: new
  - id: N2
    statement: "A recipient who reschedules during an attempt in progress gets the new slot, not a duplicate"
    verified_by: ["Reviewer: check the migration on a copy of last week's attempts table; it rewrites 2 million rows"]
    status: new
```
````

- Keys: `id` and `statement` always; `why`, `context`, `verified_by` and `status` when they apply. One key per line; text in double quotes.
- `statement` is one sentence saying what holds, not what the code does.
- `verified_by` lists how each is checked: a reference to the test (`gh:…`), or a check a person makes, starting `Reviewer:`. An invariant without one is a gap `kalka check` reports (`I031`).
- `status` says what the plan does to it: `new`, `changed` or `removed`. Leave it out for an invariant the plan keeps as it is.
- `affects` names the domain's invariants the plan touches, as `<domain>#<id>`, from the version in `kalka_domain:`.
- Ids are permanent, as for trace steps: comments attach to them (`§verification-plan#N1`).
- In a trace, the steps that keep an invariant true list it under `guards` (see "Trace blocks"). An invariant no step guards is a gap (`I030`).

*Why agents benefit:* an agent cannot know which properties matter most to the team. A verification plan written up front turns them into tests the agent writes first, and tells the reviewer where to spend their attention instead of rereading everything.

### Critical sections

Places where the design deliberately departs from the clean model, for example for performance, compatibility or an external constraint, with the reasoning.

> The attempt counter is denormalized onto the parcel row. Joining the attempts table on every scan of the sorting belt was too slow (p99 above the belt's 40 ms budget). Keep the two in sync in the same transaction; do not replace the counter with a join.

*Why agents benefit:* agents are trained to remove duplication and restore the "proper" design. Here that would reintroduce the problem the design avoids. Naming these places, and requiring human review there, prevents a well-meant cleanup from undoing a decision.

### Operations

How the change fails in production, what to watch, and how to roll it back.

> - Failure: the scheduler stops creating attempts. Watch `redelivery_attempts_created` per hour; alert below 50 % of the same hour last week.
> - Rollback: switch off the `redelivery-v2` flag. The migration is additive, so no schema rollback is needed.

*Why agents benefit:* an agent implementing the plan adds the metrics, logs and flags operations will need, instead of leaving them for later. A reviewer can check that the rollback really works without the author present.

### Deviation log

Where the implementation departed from the plan as written. This is not a section in the body: it is the set of `deviation` entries in the review log, which kalka keeps in order and never loses.

Record a deviation as soon as it happens:

```sh
kalka comment plan.md --anchor scope --kind deviation \
  -m "Also changed the courier app's reschedule endpoint: it validated the old three-slot rule."
```

*Why agents benefit:* the plan stays the agreed reference, and the log says what changed and why. The next agent, or the reviewer, sees both, instead of a plan silently rewritten to match the code.

## Trace blocks

A trace block shows one process at two or more levels (lanes), most abstract first, and links the levels. kalka draws it and lists where the levels differ. Write one when a plan describes a pipeline or algorithm in words and the reader needs to find it in the code.

*Why agents benefit:* the next agent finds the code for each step of the plan without searching, and the reviewer sees at once which code steps the plan never mentioned.

````markdown
```trace v1
lanes: [business, code]
nodes:
  - id: B1
    lane: business
    text: "The courier scans the parcel at the door"
    next: [B2]
  - id: B2
    lane: business
    text: "The recipient is told it has arrived"

  - id: C1
    lane: code
    text: "Store the delivery scan"
    realizes: [B1]
    ref: "gh:src/scan/handler.py#L30-L77"
    next: [C2]
  - id: C2
    lane: code
    text: "Send the arrival message"
    realizes: [B2]
    ref: "gh:src/notify/arrival.py#L9-L40"
```
````

**Format**

- Fence the block as `trace v1`, never as `yaml`.
- One key per line for every node. Never write a node as `{id: …, text: …}`.
- Put `text`, `why`, `ref` and labels in double quotes. Inside them write `\"` for a quote and `\\` for a backslash.
- Keys: `id`, `lane`, `text` always; `next`, `back`, `realizes`, `ref`, `why`, `change`, `guards` when they apply. Do not invent other keys; a key of your own must start with `x-`.
- List the nodes lane by lane, each lane in the order things happen.

**Content**

- Each lane describes the *same* process. The top lane is how a user or the business would tell it; the bottom lane is what the code does. Add a middle lane only if the reader needs it.
- `text` is one short line saying what happens. Keep it under 100 characters.
- Describe the code as it is, including the steps nobody asked for: guards, retries, caches, batching, clean-up. Do not leave a code step out to make the lanes match, and do not add `realizes` to a step that implements nothing above it. The differences are what the reviewer needs.
- A step that realizes nothing gets a `why`.
- Every node in the bottom lane gets a `ref` to the lines it describes, unless it is `change: new`.
- `next` names what follows in the same lane. For a branch, use the labelled form and say when each edge is taken:

  ```yaml
  next:
    C4: "third failed attempt"
    C5: "otherwise"
  ```

- A loop or retry goes under `back`, on the node where the flow returns, with a label saying when: `back: {C1: "while pages remain"}` or the block form. `next` must never lead back to an earlier node.
- `realizes` names nodes in lanes above. In three lanes, link each node to the lane directly above where you can; do not also link it to the top lane.
- A step that keeps an invariant true lists it under `guards`: the document's own by id, a domain's as `<domain>#<id>`, such as `guards: [N1, "delivery#I3"]`. Put it on the step where the property is enforced, in the lowest lane that shows it.
- When the trace describes a plan, mark what the plan does: `change: new`, `change: modified` or `change: removed`. Leave unchanged steps unmarked. Keep removed steps in the flow where they are today.
- Aim for 20 nodes or fewer in a block; 40 is the limit. For a larger process, put an overview block under one heading and one block per phase under its own heading, and repeat the last step of a phase as the first step of the next.

**Ids are permanent**

Reviewers' comments are attached to node ids. When you change an existing trace:

- read the existing block first and keep the id of every step that is still the same step, even if its text changes or it moves;
- give a new step an id that was never used in that block; do not renumber the others to make room;
- never give a deleted step's id to a different step;
- use a different prefix per lane and per block under the same heading (`B1…`, `C1…`), so that two blocks never share an id.

**Check**

Run `kalka check <file> --json` after writing. Fix every error (the block is shown as plain code until you do) and read every warning. Then read the divergence list: each item should be something true about the code. If one is an artefact of how you wrote the trace, fix the trace; if it is real, leave it, and make sure the node's `why` explains it.

Renaming the heading above a block detaches the comments on its steps, as it does for comments on a section.

## Domain documents

A domain document (`kalka_kind: domain`) holds a team's shared language and what must always hold. Blueprints reference one released version of it with `kalka_domain: <team>/<domain>@v<N>`, and kalka links their text against that version.

**Glossary.** Define the terms in one or more `glossary v1` blocks:

````markdown
```glossary v1
- term: "parcel"
  definition: "One item the depot accepts, labelled and tracked on its own"
  forms: ["parcels"]
  aliases: ["package", "packages"]
  see: ["delivery attempt"]
- term: "delivery attempt"
  definition: "One visit by a courier to hand a parcel over"
  forms: ["delivery attempts"]
```
````

- `term` is the word the team uses, `definition` one sentence a reader sees on hover. `forms` are other forms of the same word (plurals); `aliases` are words people also use that the plan should not (each use is a warning). `context` names the bounded context; `see` names related terms of the same glossary.
- A word names one entry: the same term, form or alias in two entries is an error.
- Matching ignores case and works on whole words; a term interrupted by markup does not match. Code, link targets and the frontmatter are never matched.

**A plan with no domain.** A blueprint without `kalka_domain:` may define its own words in a `glossary v1` block, and its own invariants in an `invariants v1` block, with the same syntax. kalka links and checks the plan against them as it would against a domain's. Once the plan names a domain, move its terms into the domain document: a blueprint is linked against one glossary, and its own block is then shown as code (`G008`).

**Invariants.** List them in an `invariants v1` block, as in a plan but without `status` or `affects`. Ids are referenced from blueprints as `<domain>#<id>`; never reuse the id of a removed invariant for a different one. When the owner releases a version that removes an invariant or adds an alias, kalka lists the held blueprints that rely on it.

## When to split off a seed

A *seed* is a small blueprint (`kalka_status: seed`) holding an idea split off from a plan that is growing. Split one off when:

- the work needs a module outside the plan's scope;
- a deviation turns out to be a feature of its own;
- a reviewer's comment raises something worth doing, but not now.

In kalka, "Create seed" on a deviation entry, or `kalka seed <file> --from <entry id> --title "<title>"`, writes the seed with its problem and why it was split off, and records a `budded` entry in the parent so the two stay linked. The seed waits in the team's pre-backlog until its owner promotes it.

## Check before handing over

Run `kalka check --json <file>` before you hand a blueprint over, and again after every change:

```sh
kalka check --json plan.md
```

Each diagnostic has a rule id, a severity, a line and a message saying what to write instead; some carry a `fix` with the replacement text. Fix every error. Read every warning: fix it, or make sure the plan says why it stays. Information needs nothing. `kalka check --explain <id>` says what a rule means and why it exists. kalka never rewrites the file for you.

The exit code is 0 when there is nothing to fix, 1 for errors, 2 for warnings only. When the plan references a domain version (`kalka_domain:`) that is not in the local kalka folder, point `--domain` at the domain document; otherwise the glossary and invariant rules report that they could not check.

### Rules

<!-- rules -->

#### Frontmatter (F)

| Rule | Severity | What it means |
|---|---|---|
| `F001` | error | **Frontmatter never closed.** The file opens frontmatter with `---` and never closes it, so all of it is read as body. |
| `F002` | error | **kalka_id is not a UUID.** `kalka_id:` is not a version 4 UUID. |
| `F003` | error | **Shared blueprint has no owner.** The document has a `kalka_id:` but no `kalka_owner:`. |
| `F004` | error | **Owner is not an email address.** `kalka_owner:` is not an email address. |
| `F005` | info | **Local blueprint.** The document has no `kalka_id:` yet: it is local and gets one on first publish. |
| `F010` | warning | **Domain name missing or malformed.** A domain document (`kalka_kind: domain`) has no valid `kalka_domain:` name: lower-case letters, digits and dashes. |
| `F011` | warning | **Domain version is not a number.** `kalka_version:` of a domain document is not a positive number. |

#### Structure and the review log (B)

| Rule | Severity | What it means |
|---|---|---|
| `B000` | error | **File cannot be read.** The file cannot be read, is not UTF-8 text kalka can parse, or is over the limits (2 MiB). |
| `B001` | error | **Body ends inside an open block.** The body ends inside an unclosed code fence or HTML block. |
| `B002` | warning | **Second review log heading.** A second `## Review log` heading is ordinary body text; only the first is the review log. |
| `B003` | warning | **Unparsed text in the review log.** Text in the review log is not an entry; it is kept as it is but takes no part in review. |
| `B010` | warning | **Edited entry.** An entry's id no longer matches its content: it was edited after it was written. |
| `B011` | warning | **Entry appears twice.** The same entry appears more than once in the log. |
| `B012` | error | **Entry too large.** An entry's body is larger than 64 KiB. |
| `B013` | error | **Entry opens a fence it never closes.** An entry body opens a code fence and never closes it. |
| `B014` | warning | **Heading inside an entry.** Headings inside an entry must be level 4 or deeper. |
| `B015` | warning | **Comment on a removed section.** An entry is anchored to a section, trace step or invariant that is no longer in the body. |
| `B016` | info | **Comment on an earlier version.** An entry was written against an earlier text of the body. |

#### Trace blocks (T)

| Rule | Severity | What it means |
|---|---|---|
| `T001` | error | **Trace YAML does not parse.** The content of the trace block is not well-formed YAML. |
| `T002` | error | **YAML feature not allowed.** Anchors, aliases, explicit tags, merge keys or more than one YAML document. |
| `T003` | error | **Key twice in one mapping.** A key appears twice in one mapping. |
| `T004` | error | **Block is not a mapping.** The trace block is not a mapping with lanes and nodes. |
| `T005` | error | **Unknown trace version.** The fence names a version this kalka does not know, or its second word is not a version. |
| `T010` | error | **No lanes.** `lanes` is missing or is not a list of texts. |
| `T011` | error | **Wrong number of lanes.** Fewer than 2 or more than 5 lanes. |
| `T012` | error | **Bad lane name.** A lane name is empty, longer than 40 characters, or listed twice. |
| `T020` | error | **No nodes.** `nodes` is missing, is not a list, or is empty. |
| `T021` | error | **Node is not a mapping.** An entry of `nodes` is not a block mapping. |
| `T022` | error | **Too many nodes.** More than 40 nodes. |
| `T030` | error | **Bad node id.** `id` is missing or is not a letter followed by letters, digits, - and _ (at most 32). |
| `T031` | error | **Node id used twice.** Two nodes have the same id. |
| `T032` | error | **Unknown lane.** `lane` is missing or is not one of `lanes`. |
| `T033` | error | **Node has no text.** `text` is missing or empty. |
| `T034` | error | **Value has the wrong shape.** A value is not of its key's form: a text where a list is expected, a list where a text is, or the retired `{to: …, label: …}` edge. |
| `T040` | error | **Unknown node named.** `next`, `back` or `realizes` names an id that is not a node of the block. |
| `T041` | error | **Edge across lanes.** `next` or `back` names a node in another lane. |
| `T042` | error | **Node follows itself.** `next` names the node itself. |
| `T043` | error | **Cycle in next.** The `next` edges form a cycle. |
| `T044` | error | **back goes forward.** `back` names a node that is neither the node itself nor one before it. |
| `T045` | error | **realizes names a lower lane.** `realizes` names a node that is not in a lane above. |
| `T046` | error | **Unknown change.** `change` is not `new`, `modified` or `removed`. |
| `T101` | warning | **Trace fence without version.** The fence has no version; the block is read as v1. |
| `T102` | warning | **Unknown key.** A key that is not part of the language and does not start with `x-`; it is ignored. |
| `T103` | warning | **Comment after a value.** A YAML comment follows a value on the same line. |
| `T104` | warning | **Id named twice.** An id is named twice in one `next`, `back` or `realizes`, or under both `next` and `back`. |
| `T105` | warning | **Redundant realizes.** A node realizes a node directly and also through another node it realizes. |
| `T106` | warning | **Empty lane.** A lane has no nodes. |
| `T107` | warning | **Lane in pieces.** The nodes of a lane fall into groups that no `next` or `back` edge joins. |
| `T108` | warning | **Many nodes.** More than 20 nodes. |
| `T109` | warning | **Text too long.** `text` longer than 100 characters, `why` longer than 300, or a label longer than 40. |
| `T110` | warning | **Step without why.** A live node outside the first lane realizes nothing and has no `why`. |
| `T111` | warning | **Ids differ only in case.** Two ids differ only in letter case. |
| `T112` | warning | **ref is not scheme:payload.** `ref` is not of the form `scheme:payload`, or contains white space. |
| `T113` | warning | **Some branches unlabelled.** A node has several `next` edges and only some have a label. |
| `T120` | warning | **Node id shared under one heading.** Two trace blocks under the same heading both have a node with this id. |
| `T121` | warning | **Trace without a heading.** The block has no heading above it, so its steps cannot be commented on. |
| `T122` | warning | **YAML block that looks like a trace.** A block fenced as `yaml` has a root mapping with both `lanes` and `nodes`. |

#### Glossary (G)

| Rule | Severity | What it means |
|---|---|---|
| `G001` | error | **Glossary YAML does not parse.** The content of a glossary block is not valid YAML. |
| `G002` | error | **Glossary uses a YAML feature that is not allowed.** The block uses anchors, aliases, tags, merge keys or several YAML documents. |
| `G003` | error | **Glossary key written twice.** One entry has the same key twice. |
| `G004` | error | **Glossary entry has the wrong shape.** The block is not a list of entries, an entry is not a block mapping, or a value is not a text or a list of texts. |
| `G005` | error | **Glossary version not known.** The fence names a version of the glossary language this kalka does not read. |
| `G006` | warning | **Glossary fence has no version.** The fence says `glossary` without a version; it is read as v1. |
| `G007` | warning | **Unknown glossary key.** An entry has a key that is not term, definition, aliases, forms, context or see, and does not start with `x-`. |
| `G008` | warning | **Glossary in a blueprint that names a domain.** A blueprint with `kalka_domain:` has a glossary block of its own; it is shown as code and links nothing. A blueprint that names no domain may keep its own glossary. |
| `G010` | error | **Glossary entry has no term.** An entry has no `term:`. |
| `G011` | error | **Term has no definition.** An entry has a term but no `definition:`. |
| `G012` | error | **A word names two glossary entries.** A term, form or alias is the same (ignoring case) as a term, form or alias of another entry. |
| `G013` | error | **Term too long.** A term is longer than 60 characters. |
| `G014` | warning | **Too many terms.** The glossary has more than 500 terms; the rest are not read. |
| `G015` | warning | **Definition too long.** A definition is longer than 400 characters. |
| `G016` | warning | **see names an unknown term.** `see:` names a word that is not a term of the same glossary. |
| `G020` | warning | **Alias used instead of the term.** The text uses an alias of a glossary term; the diagnostic offers the term as a replacement. |
| `G030` | info | **Release would make held blueprints use an alias.** Shown to a domain owner releasing a new version: a held blueprint uses, as a word, an alias the new version introduces. |

#### Invariants (I)

| Rule | Severity | What it means |
|---|---|---|
| `I001` | error | **Invariants YAML does not parse.** The content of an invariants block is not valid YAML. |
| `I002` | error | **Invariants use a YAML feature that is not allowed.** The block uses anchors, aliases, tags, merge keys or several YAML documents. |
| `I003` | error | **Invariants key written twice.** One mapping has the same key twice. |
| `I004` | error | **Invariants block has the wrong shape.** The block is not a mapping with an `invariants:` list, an invariant is not a block mapping, a value has the wrong type, or `affects` names something that is not `<domain>#<id>`. |
| `I005` | error | **Invariants version not known.** The fence names a version of the invariants language this kalka does not read. |
| `I006` | warning | **Invariants fence has no version.** The fence says `invariants` without a version; it is read as v1. |
| `I007` | warning | **Unknown invariants key.** A key is not one of the language's and does not start with `x-`. |
| `I008` | warning | **Blueprint-only key in a domain document.** A domain document uses `affects` or `status`, which only a blueprint has; they are ignored. |
| `I010` | error | **Invariant id is not valid.** An id is missing, does not start with a letter, uses characters other than letters, digits, `-` and `_`, or is longer than 32 characters. |
| `I011` | error | **Invariant id used twice.** Two invariants in one document have the same id. |
| `I012` | error | **Invariant has no statement.** An invariant has an id but no `statement:`. |
| `I013` | error | **Invariant status not known.** `status:` is not `new`, `changed` or `removed`. |
| `I014` | warning | **Too many invariants.** The document has more than 100 invariants; the rest are not read. |
| `I015` | warning | **Statement too long.** A statement is longer than 300 characters. |
| `I016` | warning | **Invariant id is also a trace step id.** An invariant and a trace step under the same heading share an id, so the anchor `§slug#id` names both. |
| `I020` | error | **Invariant reference does not resolve.** A `guards` or `affects` entry names an invariant that does not exist: an unknown id, a domain other than the one `kalka_domain:` names, or a domain reference with no `kalka_domain:` at all. |
| `I030` | warning | **No trace step guards the invariant.** The document has a trace, and no step lists the invariant (one it introduces or affects) under `guards`. |
| `I031` | warning | **Invariant not verified.** An invariant has no `verified_by`. |
| `I040` | info | **Referenced invariant removed in a newer domain version.** A blueprint references a domain invariant that a newer version of the domain no longer has. Also shown to the domain owner releasing that version, listing the held blueprints concerned. |

#### References to other documents (R)

| Rule | Severity | What it means |
|---|---|---|
| `R001` | info | **Domain version not here, can't check.** The domain version `kalka_domain:` names is not available, so the glossary and invariant rules that need it were not run. |
| `R002` | warning | **Malformed domain reference.** `kalka_domain:` in a blueprint is not `<team>/<domain>@v<N>`. |
| `R003` | info | **Newer domain version.** A newer version of the referenced domain is available. |
| `R010` | warning | **Reference does not resolve.** With --with-plugins: a trace `ref` or an invariant's `verified_by` reference has no plugin or does not resolve. |
| `R011` | warning | **Referenced lines changed.** With --with-plugins: a plugin warns about a reference, for example that its lines changed since the plan was written. |

#### Hints (H)

| Rule | Severity | What it means |
|---|---|---|
| `H001` | info | **No domain reference.** An opted-in blueprint names no domain version: no `kalka_domain:` and no "Domain reference" section. |
| `H002` | info | **No scope.** An opted-in blueprint has no "Scope" section. |
| `H003` | info | **No verification plan.** An opted-in blueprint has neither an invariants block nor a "Verification plan" section. |
| `H004` | info | **No critical sections.** An opted-in blueprint has no "Critical sections" section. |
| `H005` | info | **No operations.** An opted-in blueprint has no "Operations" section. |
| `H010` | info | **Unknown blueprint reference version.** The document follows a version of the blueprint reference this kalka does not know; no hints are given. |

<!-- /rules -->

## A skeleton

````markdown
---
kalka_owner: dana@example.org
kalka_reference: kalka-blueprint@v1
kalka_domain: parcels/delivery@v7
---

# Redelivery scheduling

## Context
## Scope
## Plan
## Verification plan

```invariants v1
invariants:
  - id: N1
    statement: "…"
    verified_by: ["…"]
```

## Critical sections
## Operations
````

The review log is added by kalka when the first comment arrives.
