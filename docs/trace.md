# kalka trace blocks: language specification, version 1

This is the full definition of the trace language that `specification.md` section 6.10 summarizes. Every block fenced as `trace v1` in this document is a valid instance, and the divergences listed under each example are the ones kalka computes; a test (`TestTraceSpecExamples`) keeps both true.

## 1. Purpose and scope

A trace block describes one process at two or more levels of abstraction (for example business steps and the code that carries them out) and links the levels. kalka computes from it where the levels stop lining up, so that a reviewer can check each claim quickly.

- Traces are written by agents and read by people. kalka renders them and never edits them.
- A trace block is part of a blueprint's Markdown body. It is owned by the document owner and replicated with the file.
- The more abstract lane is not a list of requirements. It describes the same pipeline as the lanes below it, with less detail.
- A trace shows that a mapping was claimed and that referenced code exists. It does not prove the mapping is right.

This document defines the syntax, the meaning of each construct, validation, the divergence classes, versioning and limits. It does not define the picture kalka draws, the plugin protocol that resolves `ref`, or review-log entries, except where they depend on the language.

Words used throughout:

| Word | Meaning |
|---|---|
| lane | One level of abstraction. Lanes are ordered, most abstract first. |
| node | One step in one lane. |
| flow | The `next` and `back` edges between nodes of one lane. |
| realization link | A `realizes` entry: a node implements a node in a more abstract lane. |
| live node | A node whose `change` is not `removed`. |
| above, below | Lane A is above lane B when A comes earlier in `lanes` (A is more abstract). |

## 2. Lexical structure and grammar

### 2.1 The fence

A trace block is a Markdown fenced code block whose info string starts with the word `trace`:

```ebnf
info     = "trace" , [ ws , version ] , [ ws , reserved ] ;
version  = "v" , nonzero , { digit } ;
reserved = ? any text; ignored in v1 ? ;
ws       = ( " " | "\t" ) , { " " | "\t" } ;
nonzero  = "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9" ;
digit    = "0" | nonzero ;
```

- `trace v1` is the form this document defines.
- `trace` with no second word is read as v1 and reported with warning T101.
- `trace vN` with N greater than 1, or a second word that is not a version, is not interpreted (T005, section 6.3).
- The word is case-sensitive. A block fenced as `yaml` is never a trace (but see T122).

### 2.2 The content

The content is one YAML 1.2 document, restricted as follows.

1. No anchors, aliases, explicit tags, merge keys (`<<`) or second document (T002).
2. No mapping has the same key twice (T003).
3. Every scalar is taken as **text**: its content after YAML's unquoting and unescaping, with no type resolution. `404`, `yes`, `1e3` and `no` are the texts "404", "yes", "1e3" and "no". An unquoted `~`, `null`, `Null`, `NULL` or an empty value counts as absent.
4. In every text, leading and trailing white space is dropped and each run of white space, including line breaks, becomes one space.
5. Each entry of `nodes` is a block mapping, one key per line. A flow mapping (`{id: B1, …}`) is not allowed there (T021). Lists of ids may be written inline (`[B1, B2]`) or as block lists.
6. Comments on their own line are allowed. A comment after a value on the same line draws warning T103, because an unquoted text containing ` #` is cut there by YAML.

Free text (`text`, `why`, edge labels) and `ref` should be written in double quotes. This is advice, not a rule: an unquoted text that YAML reads back unchanged is valid.

### 2.3 Schema

```ebnf
block     = "lanes" , ":" , lane-list ,
            "nodes" , ":" , node-list ;                 (* a mapping; key order is free *)
lane-list = sequence of 2..5 lane-name ;
lane-name = text , non-empty , at most 40 characters ;
node-list = sequence of 1..40 node ;
node      = block mapping with the keys below ;
id        = letter , { letter | digit | "-" | "_" } ;   (* at most 32 characters *)
letter    = "A".."Z" | "a".."z" ;
id-list   = id | sequence of id ;                       (* one id may stand without brackets *)
edges     = id-list | mapping of id to label ;          (* label: text, may be empty *)
change    = "new" | "modified" | "removed" ;
guards    = inv-ref | sequence of inv-ref ;
inv-ref   = id | domain , "#" , id ;                    (* domain: [a-z0-9][a-z0-9-]* *)
```

| Key | Where | Required | Value | Meaning in brief |
|---|---|---|---|---|
| `lanes` | block | yes | list of 2 to 5 distinct names | The levels, most abstract first. |
| `nodes` | block | yes | list of 1 to 40 nodes | The steps of all lanes. |
| `id` | node | yes | `id` | Name of the node, unique in the block. |
| `lane` | node | yes | one of `lanes` | The lane the node belongs to. |
| `text` | node | yes | text | What the step does, in one short line. |
| `next` | node | no | `edges` | Nodes of the same lane that follow this one. |
| `back` | node | no | `edges` | Nodes of the same lane the flow returns to (a loop or retry). |
| `realizes` | node | no | `id-list` | Nodes in lanes above that this node implements. |
| `ref` | node | no | text of the form `scheme:payload` | Link to the code or other source, resolved by a plugin. |
| `why` | node | no | text | Why the step exists or differs from the lanes above. |
| `change` | node | no | `change` | What the plan does to this step. Absent means unchanged. |
| `guards` | node | no | list of invariant references | Invariants the step keeps true. |

Keys are case-sensitive. Any other key is an unknown key (section 6.1).

The two forms of `edges`:

```yaml
next: [C2, C3]          # no labels
next:                   # a label per edge, in double quotes
  C2: "file is small"
  C3: "file is large"
```

The order of items in `lanes`, `nodes`, `next`, `back` and `realizes`, and of the keys of an `edges` mapping, is significant and must be kept by a parser: it decides ties in layout and the order of reports.

## 3. Semantics

**Lanes.** `lanes` lists the levels from most to least abstract. kalka attaches no meaning to the names. The last lane is the one whose nodes are expected to carry a `ref`.

**Nodes.** A node is one step in one lane. `text` says what happens, not how well or why. Nodes should be listed lane by lane, each lane in flow order; the language does not require it.

**`next`.** `next` on node A naming node B says B follows A in the same lane. The `next` edges of a lane must not form a cycle, and a node may not name itself. A node with several `next` edges is a point where the flow divides; the labels say when each edge is taken. v1 does not distinguish "one of these" from "all of these, in parallel": the label must say it (section 6.2 reserves keys for a later distinction). A node with no incoming `next` edge is a start of its lane's flow and a node with no outgoing `next` is an end; a lane may have several of each.

**Order.** In one lane, A *precedes* B (written A ≺ B) when a path of one or more `next` edges leads from A to B. Because `next` has no cycles, ≺ is a strict partial order. Two nodes of a lane that neither precede nor follow each other are *unordered* (for example the two arms of a branch).

**`back`.** `back` on node A naming node B says that after A the flow may return to B and repeat from there. B must be A itself (A repeats) or a node that precedes A. `back` edges are drawn and may carry a label ("up to 3 times", "while pages remain"), and they take no part in ≺. Every edge that would close a cycle is therefore written under `back`, and every other edge can be written under `next`.

**`realizes`.** `realizes` on node N naming node M says N implements M, or part of M. M must be in a lane above N's, at any distance. A node may realize several nodes, and several nodes may realize one. N *realizes M through others* when a chain of realization links leads from N to M over live nodes; R⁺(N) is the set of all nodes N reaches that way, direct links included.

**`ref`.** A reference target of the form `scheme:payload` with no white space, where `scheme` matches `[a-z][a-z0-9+.-]*`. It is the bare target, not a Markdown link. Plugins resolve it as described for references in the main specification; each reader resolves with their own plugins and credentials, and nothing resolved is stored in the file. A `ref` is allowed in any lane.

**`why`.** The reason the step exists, or differs from the lanes above. It is shown with the node and in the divergence list. It is expected on every node that realizes nothing (T110).

**`change`.** Says what the plan does to the step, relative to the code the blueprint was written against:

| Value | Meaning | `ref` points at |
|---|---|---|
| absent | The step exists and the plan leaves it alone. | the existing code |
| `new` | The step does not exist yet; the plan adds it. | nothing, or the place it will go |
| `modified` | The step exists and the plan changes it. `why` or the prose says how. | the existing code |
| `removed` | The step exists and the plan deletes it. | the existing code |

`change` may be used in any lane (a new business step is a new requirement). A trace with no `change` keys describes one state, usually the code as built. A trace with `change` keys describes the state **after** the plan, with removed steps kept visible:

- A removed node stays in the flow where it was. Order passes through it: if A → R → B and R is removed, A still precedes B.
- Removed nodes take no part in divergences (section 5). They are neither reported nor counted as realizing or being realized.
- A new node is not expected to have a `ref` (section 5.3).

Edges cannot be marked. An edge is taken to exist after the plan when both its ends are live.

**`guards`.** Names the invariants (main specification 6.15) the step keeps true: an id of an invariant of the same document, or `<domain>#<id>` for an invariant of the domain the document references. A reference that does not resolve is reported by the invariant rules (`I020`), not by the trace: it never makes the block invalid. `guards` takes no part in divergences; it decides which invariants count as guarded (`I030`).

**Identity.** A node id names the node for comments: a review-log entry is anchored to `§<slug>#<id>`, where `<slug>` is the slug of the nearest heading above the block. Ids are scoped to the block, and should be unique among all blocks under the same heading (T120). An id is a permanent name: section 9 says how to keep it stable.

## 4. Validation

A validator returns **errors** and **warnings**, each with an identifier, a line in the file, and the node id where one applies.

- A block with one or more errors is not a trace: kalka shows its source as code with the errors beside it, and computes no divergences. The rule for making something an error is that the graph cannot be built without guessing, or that the divergences could not be computed as defined.
- A block with only warnings is a trace: it is drawn, its divergences are computed, and the warnings are shown with it.
- Neither errors nor warnings ever block a sync, a release, or any other operation. `kalka check` reports both as diagnostics (main specification 6.17): an error makes it exit 1, a warning 2.

Validation runs in the stages below. A stage runs only if the stages before it found no error; within a stage, all errors are collected.

### 4.1 Errors

| Id | Stage | Condition |
|---|---|---|
| T001 | 1 | The content is not well-formed YAML. |
| T002 | 1 | Anchor, alias, explicit tag, merge key, or more than one document. |
| T003 | 2 | A key appears twice in one mapping. |
| T004 | 2 | The document is not a mapping. |
| T005 | 0 | The fence names a version this reader does not know (`trace v2`), or its second word is not a version. Not an authoring mistake when the version is newer: shown as "needs a newer kalka". |
| T010 | 2 | `lanes` is missing or is not a list of texts. |
| T011 | 2 | Fewer than 2 or more than 5 lanes. |
| T012 | 2 | A lane name is empty, longer than 40 characters, or listed twice. |
| T020 | 2 | `nodes` is missing, is not a list, or is empty. |
| T021 | 2 | An entry of `nodes` is not a block mapping. |
| T022 | 2 | More than 40 nodes. |
| T030 | 3 | `id` is missing or does not match the `id` grammar. |
| T031 | 3 | Two nodes have the same `id`. |
| T032 | 3 | `lane` is missing or is not one of `lanes`. |
| T033 | 3 | `text` is missing or empty. |
| T034 | 2 | A value has the wrong shape: `text`, `why`, `ref`, `id`, `lane` or `change` is not a single text; `next` or `back` is not an `edges` value; `realizes` is not an `id-list`; `guards` is not a text or a list of texts; a label is not a text. This includes the retired form `{to: B2, label: …}`. |
| T040 | 4 | `next`, `back` or `realizes` names an id that is not a node of the block. |
| T041 | 4 | `next` or `back` names a node in another lane. |
| T042 | 4 | `next` names the node itself. |
| T043 | 5 | The `next` edges form a cycle. The message names the nodes on it. |
| T044 | 5 | `back` names a node that is neither the node itself nor one that precedes it. |
| T045 | 4 | `realizes` names a node that is not in a lane above. |
| T046 | 3 | `change` is not `new`, `modified` or `removed`. |

### 4.2 Warnings

| Id | Condition |
|---|---|
| T101 | The fence has no version. Read as v1. |
| T102 | An unknown key on the block or on a node, not starting with `x-`. The key is ignored. The message should suggest the nearest known key (`realises` → `realizes`). |
| T103 | A comment follows a value on the same line. |
| T104 | An id is named twice in one `next`, `back` or `realizes`, or the same id is under both `next` and `back`. Duplicates count once. |
| T105 | A node realizes M directly and also through another node it realizes. The direct link is redundant. |
| T106 | A lane has no nodes. |
| T107 | The nodes of a lane fall into two or more groups that no `next` or `back` edge joins. Usually a forgotten `next`. |
| T108 | More than 20 nodes. |
| T109 | `text` longer than 100 characters, `why` longer than 300, or a label longer than 40. |
| T110 | A live node outside the first lane realizes no live node and has no `why`. |
| T111 | Two ids differ only in letter case. |
| T112 | `ref` is not of the form `scheme:payload`, or contains white space. |
| T113 | A node has several `next` edges and only some have a label. |

Warnings about the block's place in the document, raised by the document validator, not the block validator:

| Id | Condition |
|---|---|
| T120 | Two trace blocks under the same heading both have a node with this id. Comments on it are ambiguous. |
| T121 | The block has no heading above it, so its nodes cannot be commented on. |
| T122 | A block fenced as `yaml` or `yml` has a root mapping with both `lanes` and `nodes`. It was probably meant as a trace. |

### 4.3 Messages

A message names the node and the offending value and says what to write instead, because the usual reader of a message is the agent that wrote the block. Examples:

```text
T043 line 31, node C4: next edges form a cycle through C1, C2, C3, C4; write the edge that returns under back
T044 line 12, node C2: back names C5, which does not come before C2; a forward edge belongs under next
T034 line 9, node B1: next is a list of ids, or a mapping of id to label
```

## 5. Divergences

Divergences are computed from a valid block. An author cannot declare or suppress one. Each divergence is a record:

| Field | Meaning |
|---|---|
| `class` | One of the names below. |
| `node` | The node the divergence is reported on. |
| `nodes` | The other nodes involved, in the order the class defines. |
| `lane` | For classes that compare two lanes, the other lane. |

### 5.1 Definitions used

- Only live nodes are considered. A removed node appears in no divergence, in no set below, and satisfies no "there is a node" condition. It still carries order (section 3).
- up(N, U): the live nodes of lane U that N names in `realizes`, in the order written, duplicates dropped.
- real(M, L): the live nodes of lane L that name M in `realizes`, in block order.
- R⁺(N): the live nodes reachable from N over realization links through live nodes.
- ≺: the order of section 3, built from all `next` edges including those through removed nodes.

### 5.2 Graph classes

These depend only on the block, so every reader computes the same list.

**implementation-only.** A live node N outside the first lane with no live node in `realizes`. Record: `node` = N.

**unrealized.** A live node M outside the last lane with real(M, L) empty for every lane L below. Record: `node` = M.

**merged.** For a live node N and a lane U above it: up(N, U) has two or more nodes. One record per (N, U): `node` = N, `nodes` = up(N, U), `lane` = U. Realizing one node in each of two lanes is not merged.

**expanded.** For a live node M and a lane L below it: real(M, L) has two or more nodes. One record per (M, L): `node` = M, `nodes` = real(M, L), `lane` = L.

**scattered.** For a live node M and a lane L below it with R = real(M, L) of two or more nodes: there is a live node X in lane L that is not in R, realizes at least one live node, and lies between two nodes of R, that is R1 ≺ X and X ≺ R2 for some R1, R2 in R. One record per (M, L): `node` = M, `nodes` = R followed by the first such X in block order, `lane` = L.

It follows that realizers on two arms of a branch are not scattered (no path joins them), and that a node that realizes nothing does not scatter the nodes around it (it is already reported as implementation-only).

**reordered.** Take a lane L and two live nodes A, B of one lane above L with A ≺ B. The pair is *inverted in L* when

1. some RB in real(B, L) and RA in real(A, L), RA ≠ RB, have RB ≺ RA, and
2. no RA in real(A, L) and RB in real(B, L), RA ≠ RB, have RA ≺ RB.

An inverted pair (A, B) is reported unless a live node C of the same lane with A ≺ C ≺ B makes (A, C) or (C, B) inverted in L too. One record per reported pair and lane: `node` = A, `nodes` = [B, RB, RA] with the first witnessing pair in block order of RB, then RA, `lane` = L.

Condition 2 means realizers that interleave (A1 ≺ B1 ≺ A2) are not reordered; they are scattered. The exception for C keeps the list short: a chain of four steps realized in reverse gives three records, not six, and one step moved to the front gives one.

**skipped.** For a live node M in lane U and a lane K strictly between U and the last lane: let J be the live nodes N below K that name M in `realizes` and for which no live node S of lane K has S in R⁺(N) and M in R⁺(S). If J is not empty, one record per (M, K): `node` = M, `nodes` = J in block order, `lane` = K. In words: something below realizes M directly, and lane K has no step that stands between them.

**Order of the list.** By class in the order above; then by the block position of `node`; then by the position of `lane` in `lanes`.

### 5.3 Reader-dependent classes

These depend on the reader's plugins and may differ between two people looking at the same file. They are listed after the graph classes.

**unverified.** A live node whose `change` is not `new`, and either (a) it is in the last lane and has no `ref`, or (b) it has a `ref`, in any lane, that no plugin resolves or that fails to resolve. Record: `node`, with the reason as detail.

**stale.** A live node whose `ref` resolved with a warning from the plugin (for example: the lines changed since the commit the blueprint names). Record: `node`, with the warning as detail.

### 5.4 Reference algorithm

```text
live  = nodes with change != removed
P[a]  = { b : a path of >= 1 next edges leads from a to b }      (all nodes)
RT[n] = live nodes reachable from live n over realizes, through live nodes only

for n in live, lane(n) > first, no live node in n.realizes:      implementation-only(n)
for m in live, lane(m) < last, real(m, L) empty for all L:       unrealized(m)
for n in live, U above lane(n), |up(n, U)| >= 2:                 merged(n, up(n, U), U)
for m in live, L below lane(m), |real(m, L)| >= 2:               expanded(m, real(m, L), L)
for m in live, L below lane(m), R = real(m, L), |R| >= 2:
    x = first live x in L, x not in R, x realizes a live node,
        with x in P[r1] and r2 in P[x] for some r1, r2 in R
    if x exists:                                                 scattered(m, R + [x], L)
for L in lanes after the first:
    inv = {}
    for a, b in live, same lane above L, b in P[a]:
        w    = first (rb, ra), rb in real(b, L), ra in real(a, L), ra != rb, ra in P[rb]
        fwd  = any  (ra, rb), ra in real(a, L), rb in real(b, L), ra != rb, rb in P[ra]
        if w exists and not fwd: inv[a, b] = w
    for (a, b), (rb, ra) in inv:
        if no live c in lane(a) with c in P[a], b in P[c], and ((a, c) in inv or (c, b) in inv):
                                                                 reordered(a, [b, rb, ra], L)
for m in live, K strictly between lane(m) and the last lane:
    J = [ n in live below K : m in n.realizes
          and no live s in K with s in RT[n] and m in RT[s] ]
    if J not empty:                                              skipped(m, J, K)
```

With at most 40 nodes every step is small; no optimisation is needed.

## 6. Extension and versioning

### 6.1 Unknown keys

- A key starting with `x-` belongs to the author or their tools. A reader ignores it without a warning. The language will never define a key starting with `x-`.
- Any other unknown key is ignored with warning T102. It never makes a block invalid.
- kalka never rewrites a block, so unknown keys stay in the file.

### 6.2 Reserved names

These names are reserved for later versions and must not be given another meaning by tools. In v1 they are unknown keys (T102).

| Name | Where | Kept for |
|---|---|---|
| `title`, `version`, `links` | block | A block title; a version inside the content; links between blocks. |
| `kind` | node | Kinds of step, such as decision, event, end. |
| `split`, `join` | node | Telling parallel flow from a choice. |
| `detail` | node | A link to a block that expands this node. |
| `data` | node | What a step passes to the next. |
| `partial` | node | Saying a realization link covers part of its target. |
| `group` | node | Grouping nodes inside a lane. |

The mapping form of `realizes` (id to text) is reserved too; in v1 it is error T034.

### 6.3 Versions

- The version is the number in the fence. It changes only when a block that was valid before would be read differently, or when the syntax changes in a way an old reader cannot ignore. v2 is free to change everything inside the fence, including leaving YAML.
- A reader that meets a version it does not know does not interpret the content. It shows the source as code with the note "trace vN: needs a newer kalka" (T005). It does not guess.
- Within v1, later revisions of this document may add optional keys, and only where a reader that ignores the key still reads a correct, less detailed trace and computes the same graph divergences for it. Such a key shows as T102 on an older reader. A key that changes which divergences are computed needs a new version; this is why `change` is in v1 and not left for later.
- Later revisions of v1 may add warnings. They may not add errors that reject blocks valid under this document, and may not change the definitions of section 5.2.
- Tools that read divergence lists must accept class names they do not know.
- Validity and divergences are computed on each reader's machine and stored nowhere, so two peers on different kalka versions may see different warnings for the same block. This is the same situation as unverified and stale.

## 7. Limits

| Limit | Value | Past the limit |
|---|---|---|
| Lanes per block | 2 to 5 | T011 |
| Nodes per block | 1 to 40 | T020, T022 |
| Nodes per block, advised | 20 | T108 |
| Id length | 32 characters | T030 |
| Lane name length | 40 characters | T012 |
| `text` length, advised | 100 characters | T109 |
| `why` length, advised | 300 characters | T109 |
| Label length, advised | 40 characters | T109 |

A block is self-contained: no construct in v1 names a node of another block. A process too large for one block is split under several headings, one block each (section 9).

## 8. Examples

Each example lists the graph divergences a validator must report, in the notation `class:node[nodes]@lane`. Unverified and stale are left out because they depend on the reader; on a machine with no plugin, every live node with a `ref`, and every live last-lane node without one, is also unverified unless it is `new`.

### 8.1 Minimal: two lanes

```trace v1
lanes: [business, code]
nodes:
  - id: B1
    lane: business
    text: "A member returns a book"
    next: [B2]
  - id: B2
    lane: business
    text: "The next member waiting for it is told"

  - id: C1
    lane: code
    text: "Close the loan and free the copy"
    realizes: [B1]
    ref: "gh:src/loans/return.py#L18-L52"
    next: [C2]
  - id: C2
    lane: code
    text: "Email the first open reservation"
    realizes: [B2]
    ref: "gh:src/reservations/notify.py#L9-L40"
```

Divergences: none.

### 8.2 Three lanes

`C5` realizes the business step `P3` directly; the service lane says nothing about returns.

```trace v1
lanes: [business, service, code]
nodes:
  - id: P1
    lane: business
    text: "The courier attempts delivery"
    next: [P2]
  - id: P2
    lane: business
    text: "If nobody is home, try again the next working day"
    next: [P3]
  - id: P3
    lane: business
    text: "After the third failed attempt, return the parcel to the depot"

  - id: S1
    lane: service
    text: "Delivery attempt recorded"
    realizes: [P1]
    next: [S2]
  - id: S2
    lane: service
    text: "Redelivery scheduled"
    realizes: [P2]

  - id: C1
    lane: code
    text: "Scan handler stores the attempt"
    realizes: [S1]
    ref: "gh:src/scan/handler.py#L30-L77"
    next: [C2]
  - id: C2
    lane: code
    text: "Drop duplicate scans"
    ref: "gh:src/scan/dedupe.py#L8-L29"
    why: "the depot's scanner sends each scan twice"
    next: [C3]
  - id: C3
    lane: code
    text: "Count attempts on the parcel row"
    realizes: [S2]
    ref: "gh:src/redelivery/counter.py#L14-L52"
    why: "joining the attempts table on every scan was too slow"
    next:
      C4: "first or second failure"
      C5: "third failure"
  - id: C4
    lane: code
    text: "Schedule the next attempt"
    realizes: [S2]
    ref: "gh:src/redelivery/schedule.py#L20-L64"
  - id: C5
    lane: code
    text: "Create the return order"
    realizes: [P3]
    ref: "gh:src/returns/create.py#L11-L40"
```

Divergences:

- `implementation-only:C2`
- `expanded:S2[C3,C4]@code`
- `skipped:P3[C5]@service`

### 8.3 A branch with an error path

```trace v1
lanes: [business, code]
nodes:
  - id: D1
    lane: business
    text: "Receive uploaded files"
    next: [D2]
  - id: D2
    lane: business
    text: "Extract text"
    next: [D3]
  - id: D3
    lane: business
    text: "Classify the document"
    next: [D4]
  - id: D4
    lane: business
    text: "Notify the user"

  - id: I1
    lane: code
    text: "Upload endpoint handlers"
    realizes: [D1]
    ref: "gh:src/upload/handlers.py#L40-L120"
    next: [I2]
  - id: I2
    lane: code
    text: "Is the file larger than 20 MB?"
    ref: "gh:src/import/size_guard.py#L12-L48"
    why: "the parser runs out of memory on larger files"
    next:
      I3: "yes"
      I4: "no"
  - id: I3
    lane: code
    text: "Reject: tell the user the file is too large"
    ref: "gh:src/import/size_guard.py#L50-L61"
    why: "otherwise the user waits for an import that never finishes"
  - id: I4
    lane: code
    text: "Extract and classify in one pass"
    realizes: [D2, D3]
    ref: "gh:src/import/parse.py#L200-L310"
    why: "avoids parsing each file twice"
    next: [I5]
  - id: I5
    lane: code
    text: "Batch notifications, at most one per user per minute"
    realizes: [D4]
    ref: "gh:src/notify/batch.py#L15-L90"
```

Divergences:

- `implementation-only:I2`
- `implementation-only:I3`
- `merged:I4[D2,D3]@business`

### 8.4 A loop

The archive returns documents a page at a time. `F3` returns to `F1` under `back`; the forward flow stays `F1 → F2 → F3 → F4`.

```trace v1
lanes: [business, code]
nodes:
  - id: A1
    lane: business
    text: "Fetch the list of documents from the archive"
    next: [A2]
  - id: A2
    lane: business
    text: "Import each document"
    next: [A3]
  - id: A3
    lane: business
    text: "Report how many were imported"

  - id: F1
    lane: code
    text: "Request one page of 100 document ids"
    realizes: [A1]
    ref: "gh:src/archive/client.py#L22-L60"
    next: [F2]
  - id: F2
    lane: code
    text: "Import the documents on the page"
    realizes: [A2]
    ref: "gh:src/archive/import_page.py#L10-L88"
    next: [F3]
  - id: F3
    lane: code
    text: "Read the cursor for the next page"
    realizes: [A1]
    ref: "gh:src/archive/client.py#L62-L79"
    why: "the archive refuses requests for more than 100 ids"
    next: [F4]
    back:
      F1: "while a cursor is returned"
  - id: F4
    lane: code
    text: "Write the totals row"
    realizes: [A3]
    ref: "gh:src/archive/report.py#L5-L31"
```

Divergences:

- `expanded:A1[F1,F3]@code`
- `scattered:A1[F1,F3,F2]@code` (fetching the list is interleaved with importing: the business view says "fetch, then import", the code pages)

### 8.5 Reordered

```trace v1
lanes: [business, code]
nodes:
  - id: B1
    lane: business
    text: "Check the address"
    next: [B2]
  - id: B2
    lane: business
    text: "Store the parcel"

  - id: C1
    lane: code
    text: "Insert the parcel row"
    realizes: [B2]
    ref: "gh:src/intake/store.py#L12-L40"
    next: [C2]
  - id: C2
    lane: code
    text: "Validate the address in a background job"
    realizes: [B1]
    ref: "gh:src/intake/address_job.py#L8-L55"
    why: "the address service takes up to 3 s"
```

Divergences:

- `reordered:B1[B2,C1,C2]@code`

### 8.6 A planned change

The plan adds a reservation check on the server, changes the eligibility check, and deletes two things: the desk app's own warning and the nightly reminder job.

```trace v1
lanes: [business, code]
nodes:
  - id: B1
    lane: business
    text: "A member asks to borrow a book"
    next: [B2]
  - id: B2
    lane: business
    text: "Check the member may borrow"
    next: [B3]
  - id: B3
    lane: business
    text: "Refuse if another member reserved the copy"
    change: new
    next: [B4]
  - id: B4
    lane: business
    text: "Hand over the book and set the due date"
    next: [B5]
  - id: B5
    lane: business
    text: "Remind the member before the due date"

  - id: C1
    lane: code
    text: "POST /loans handler"
    realizes: [B1]
    ref: "gh:src/loans/api.py#L20-L58"
    next: [C2]
  - id: C2
    lane: code
    text: "Check overdue items and the loan limit"
    realizes: [B2]
    ref: "gh:src/loans/rules.py#L10-L47"
    change: modified
    why: "returns a reason code instead of true or false, so the desk can show it"
    next: [C3]
  - id: C3
    lane: code
    text: "Look up open reservations for the copy"
    realizes: [B3]
    change: new
    next: [C4]
  - id: C4
    lane: code
    text: "Desk app warns about a reservation"
    ref: "gh:desk/src/loan_dialog.ts#L88-L120"
    change: removed
    why: "the warning could be dismissed; the server decides now"
    next: [C5]
  - id: C5
    lane: code
    text: "Create the loan row with the due date"
    realizes: [B4]
    ref: "gh:src/loans/store.py#L80-L112"
    next: [C6]
  - id: C6
    lane: code
    text: "Nightly job sends reminders 2 days before the due date"
    realizes: [B5]
    ref: "gh:src/jobs/reminders.py#L5-L40"
    change: removed
```

Divergences:

- `unrealized:B5` (the plan deletes the only step that realizes B5 and adds nothing in its place; the reviewer should ask where reminders go)

`C4` and `C6` are removed, so they are not reported, and `C3` precedes `C5` through `C4`. `C3` is new, so it is not unverified although it has no `ref`.

## 9. How to write a good trace (for agents)

The same guidance is in `BLUEPRINT-REFERENCE.md`, the guide agents are given.

### Trace blocks

A trace block shows one process at two or more levels (lanes), most abstract first, and links the levels. kalka draws it and lists where the levels differ. Write one when a plan describes a pipeline or algorithm in words and the reader needs to find it in the code.

*Why agents benefit:* the next agent finds the code for each step of the plan without searching, and the reviewer sees at once which code steps the plan never mentioned.

````markdown
```trace v1
lanes: [business, code]
nodes:
  - id: B1
    lane: business
    text: "A member returns a book"
    next: [B2]
  - id: B2
    lane: business
    text: "The next member waiting for it is told"

  - id: C1
    lane: code
    text: "Close the loan and free the copy"
    realizes: [B1]
    ref: "gh:src/loans/return.py#L18-L52"
    next: [C2]
  - id: C2
    lane: code
    text: "Email the first open reservation"
    realizes: [B2]
    ref: "gh:src/reservations/notify.py#L9-L40"
```
````

**Format**

- Fence the block as `trace v1`, never as `yaml`.
- One key per line for every node. Never write a node as `{id: …, text: …}`.
- Put `text`, `why`, `ref` and labels in double quotes. Inside them write `\"` for a quote and `\\` for a backslash.
- Keys: `id`, `lane`, `text` always; `next`, `back`, `realizes`, `ref`, `why`, `change` when they apply. Do not invent other keys; a key of your own must start with `x-`.
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
    I3: "file larger than 20 MB"
    I4: "otherwise"
  ```

- A loop or retry goes under `back`, on the node where the flow returns, with a label saying when: `back: {F1: "while pages remain"}` or the block form. `next` must never lead back to an earlier node.
- `realizes` names nodes in lanes above. In three lanes, link each node to the lane directly above where you can; do not also link it to the top lane.
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
