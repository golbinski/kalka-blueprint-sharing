# kalka trace blocks: language specification, version 2

This is the full definition of the trace block that `specification.md` section 6.10 summarizes. It is written in the kalka language v2, whose lexis is shared by every typed block and defined in [`language.md`](language.md); this document defines what is particular to traces. Every block fenced as `trace v2` in this document is a valid instance, and the divergences listed under each example are the ones kalka computes; a test (`TestTraceSpecExamples`) keeps both true.

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
| step, node | One step in one lane. "Node" is used where the graph is meant. |
| flow | The `next` and `back` edges between nodes of one lane. |
| realization link | A `realizes` entry: a node implements a node in a more abstract lane. |
| live node | A node whose `change` is not `removed`. |
| above, below | Lane A is above lane B when A is opened earlier in the block (A is more abstract). |

## 2. Syntax

### 2.1 The fence

A trace block is a Markdown fenced code block whose info string is `trace v2`. The fence and its version follow the rules every typed block shares (language.md, section 2): `trace` with no version is read as v2 with warning S013; `trace v1`, the retired YAML form, and any newer or malformed version are shown as code (S012).

### 2.2 The content

```ebnf
block      = lane-group , { lane-group } ;
lane-group = { blank } , "lane " , lane-name , nl , { blank | step } ;
step       = "step " , id , ": " , text , nl , { attribute } ;
attribute  = "  " , keyword , " " , value , nl ;
lane-name  = text ;                                       (* at most 40 characters *)
id         = letter , { letter | digit | "-" | "_" } ;    (* at most 32 characters *)
edge       = id , [ ": " , label ] ;                       (* next and back *)
inv-ref    = id | "parent#" , id | domain , "#" , id ;   (* guards *)
change     = "new" | "modified" | "removed" ;
```

A `lane` line in column 0 opens a lane; the steps under it, until the next `lane` line, belong to it. A `step` line in column 0 is one step; its attributes are indented by exactly two spaces below it. One line holds one value: a list is the keyword repeated.

| Keyword | Level | How many | Value | Meaning in brief |
|---|---|---|---|---|
| `lane` | section | 2 to 5, distinct | a name | Opens a level; lanes come most abstract first. |
| `step` | item | 1 to 40 | `<id>: <text>` | One step of the current lane. |
| `next` | attribute | any | `edge` | A step of the same lane that follows this one. |
| `back` | attribute | any | `edge` | A step of the same lane the flow returns to (a loop or retry). |
| `realizes` | attribute | any | `id` | A step in a lane above that this step implements. |
| `covers` | attribute | any | `id` | An acceptance criterion of the document this step demonstrates (specification 6.18). |
| `ref` | attribute | one | `scheme:payload` | Link to the code or other source, resolved by a plugin. |
| `why` | attribute | one | text | Why the step exists or differs from the lanes above. |
| `change` | attribute | one | `change` | What the plan does to this step. Absent means unchanged. |
| `guards` | attribute | any | `inv-ref` | An invariant the step keeps true. |

A branch is several `next` lines, each with a label after `: ` saying when the edge is taken:

```text
step C2: Check the file size
  next C3: file is small
  next C4: file is large
```

The order of lanes, steps and repeated attributes is significant and must be kept by a parser: it decides ties in layout and the order of reports.

## 3. Semantics

**Lanes.** The `lane` lines list the levels from most to least abstract. kalka attaches no meaning to the names. The last lane is the one whose nodes are expected to carry a `ref`.

**Steps.** A step is one node in one lane. Its text says what happens, not how well or why. Steps are listed under their lane, in flow order where there is one.

**`next`.** `next` on node A naming node B says B follows A in the same lane. The `next` edges of a lane must not form a cycle, and a node may not name itself. A node with several `next` edges is a point where the flow divides; the labels say when each edge is taken. v2 does not distinguish "one of these" from "all of these, in parallel": the label must say it (section 6.2 reserves keywords for a later distinction). A node with no incoming `next` edge is a start of its lane's flow and a node with no outgoing `next` is an end; a lane may have several of each.

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

`change` may be used in any lane (a new business step is a new requirement). A trace with no `change` describes one state, usually the code as built. A trace with `change` lines describes the state **after** the plan, with removed steps kept visible:

- A removed node stays in the flow where it was. Order passes through it: if A → R → B and R is removed, A still precedes B.
- Removed nodes take no part in divergences (section 5). They are neither reported nor counted as realizing or being realized.
- A new node is not expected to have a `ref` (section 5.3).

Edges cannot be marked. An edge is taken to exist after the plan when both its ends are live.

**`guards`.** Names an invariant (main specification 6.15) the step keeps true: an id of an invariant of the same document, `parent#<id>` for one the document inherits from its parent plan node, or `<domain>#<id>` for an invariant of a domain node on the document's domain path. A reference that does not resolve is reported by the invariant rules (`I020`), not by the trace: it never makes the block invalid. `guards` takes no part in divergences; it decides which invariants count as guarded (`I030`).

**`covers`.** Names an acceptance criterion (main specification 6.18) of the same document that the step demonstrates, usually on a business step. A name that is not a criterion of the document is P022. `covers` takes no part in divergences; it closes the chain from a goal through a criterion to the code.

**Identity.** A node id names the node for comments: a review-log entry is anchored to `§<slug>#<id>`, where `<slug>` is the slug of the nearest heading above the block. Ids are scoped to the block, and should be unique among all blocks under the same heading (T120). An id is a permanent name: section 9 says how to keep it stable.

## 4. Validation

A validator returns **errors** and **warnings**, each with an identifier, a line in the file, and the node id where one applies.

- A block with one or more errors is not a trace: kalka shows its source as code with the errors beside it, and computes no divergences. The rule for making something an error is that the graph cannot be built without guessing, or that the divergences could not be computed as defined.
- A block with only warnings is a trace: it is drawn, its divergences are computed, and the warnings are shown with it.
- Neither errors nor warnings ever block a sync, a release, or any other operation. `kalka check` reports both as diagnostics (main specification 6.17): an error makes it exit 1, a warning 2.

Validation runs in the stages below. A stage runs only if the stages before it found no error; within a stage, all errors are collected.

### 4.1 Errors

The lexical errors every typed block shares (S001 to S031: tabs, capitals, a colon after a keyword, indentation, an incomplete header line, an empty value, a keyword on the wrong level, a reference with spaces, drift into Gherkin or YAML, comments, the version, a bad id) are defined in language.md, section 5. A block with one of them is not a trace. The errors particular to traces:

| Id | Stage | Condition |
|---|---|---|
| T011 | 2 | Fewer than 2 or more than 5 lanes. |
| T012 | 2 | A lane name is longer than 40 characters, or a lane is opened twice. |
| T020 | 2 | The block has no steps. |
| T022 | 2 | More than 40 steps. |
| T032 | 2 | A step comes before any `lane` line. |
| T031 | 3 | Two steps have the same id. |
| T040 | 4 | `next`, `back` or `realizes` names an id that is not a step of the block. |
| T041 | 4 | `next` or `back` names a step in another lane. |
| T042 | 4 | `next` names the step itself. |
| T045 | 4 | `realizes` names a step that is not in a lane above. |
| T043 | 5 | The `next` edges form a cycle. The message names the steps on it. |
| T044 | 5 | `back` names a step that is neither the step itself nor one that precedes it. |

Stage 1 is the lexis of language.md.

### 4.2 Warnings

The shared lexical warnings (S013 no version, S020 an unknown keyword, with the nearest known one suggested) are in language.md. The warnings particular to traces:

| Id | Condition |
|---|---|
| T104 | An id is named twice by one keyword of a step, or the same id under both `next` and `back`. Duplicates count once. |
| T105 | A step realizes M directly and also through another step it realizes. The direct link is redundant. |
| T106 | A lane has no steps. |
| T107 | The steps of a lane fall into two or more groups that no `next` or `back` edge joins. Usually a forgotten `next`. |
| T108 | More than 20 steps. |
| T109 | A step's text longer than 100 characters, `why` longer than 300, or a label longer than 40. |
| T110 | A live step outside the first lane realizes no live step and has no `why`. |
| T111 | Two ids differ only in letter case. |
| T112 | `ref` is not of the form `scheme:payload`. |
| T113 | A step has several `next` edges and only some have a label. |

Warnings about the block's place in the document, raised by the document validator, not the block validator:

| Id | Condition |
|---|---|
| T120 | Two trace blocks under the same heading both have a step with this id. Comments on it are ambiguous. |
| T121 | The block has no heading above it, so its steps cannot be commented on. |

### 4.3 Messages

A message names the node and the offending value and says what to write instead, because the usual reader of a message is the agent that wrote the block. Examples:

```text
T043 line 31, step C4: next edges form a cycle through C1, C2, C3, C4; write the edge that returns as back
T044 line 12, step C2: back names C5, which does not come before C2; a forward edge is a next
S003 line 9, step B1: no colon after the keyword next: write "  next B2"
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

### 6.1 Unknown keywords

- A keyword starting with `x-` belongs to the author or their tools. A reader ignores it without a warning. The language will never define a keyword starting with `x-`.
- Any other unknown keyword is ignored with warning S020. It never makes a block invalid.
- kalka never rewrites a block, so unknown keywords stay in the file.

### 6.2 Reserved names

These keywords are reserved for later versions and must not be given another meaning by tools. In v2 they are unknown keywords (S020).

| Name | Level | Kept for |
|---|---|---|
| `title`, `link` | section | A block title; links between blocks. |
| `kind` | attribute | Kinds of step, such as decision, event, end. |
| `split`, `join` | attribute | Telling parallel flow from a choice. |
| `detail` | attribute | A link to a block that expands this step. |
| `data` | attribute | What a step passes to the next. |
| `partial` | attribute | Saying a realization link covers part of its target. |
| `group` | attribute | Grouping steps inside a lane. |

### 6.3 Versions

- The version is the number in the fence, the same for every typed block: the version of the kalka language (language.md, section 2). It changes only when a block that was valid before would be read differently, or when the syntax changes in a way an old reader cannot ignore.
- Any version but v2 is shown as code with S012 and not read.
- A reader that meets a newer version does not interpret the content. It shows the source as code with the note "needs a newer kalka" (S012). It does not guess.
- Within v2, later revisions may add optional keywords, and only where a reader that ignores the keyword still reads a correct, less detailed trace and computes the same graph divergences for it. Such a keyword shows as S020 on an older reader.
- Later revisions of v2 may add warnings. They may not add errors that reject blocks valid under this document, and may not change the definitions of section 5.2.
- Tools that read divergence lists must accept class names they do not know.
- Validity and divergences are computed on each reader's machine and stored nowhere, so two peers on different kalka versions may see different warnings for the same block. This is the same situation as unverified and stale.

## 7. Limits

| Limit | Value | Past the limit |
|---|---|---|
| Lanes per block | 2 to 5 | T011 |
| Steps per block | 1 to 40 | T020, T022 |
| Steps per block, advised | 20 | T108 |
| Id length | 32 characters | S030 |
| Lane name length | 40 characters | T012 |
| Step text length, advised | 100 characters | T109 |
| `why` length, advised | 300 characters | T109 |
| Label length, advised | 40 characters | T109 |

A block is self-contained: no construct in v2 names a step of another block. A process too large for one block is split under several headings, one block each (section 9).

## 8. Examples

Each example lists the graph divergences a validator must report, in the notation `class:node[nodes]@lane`. Unverified and stale are left out because they depend on the reader; on a machine with no plugin, every live step with a `ref`, and every live last-lane step without one, is also unverified unless it is `new`.

### 8.1 Minimal: two lanes

```trace v2
lane business

step B1: A member returns a book
  next B2
step B2: The next member waiting for it is told

lane code

step C1: Close the loan and free the copy
  next C2
  realizes B1
  ref gh:src/loans/return.py#L18-L52
step C2: Email the first open reservation
  realizes B2
  ref gh:src/reservations/notify.py#L9-L40
```

Divergences: none.

### 8.2 Three lanes

`C5` realizes the business step `P3` directly; the service lane says nothing about returns.

```trace v2
lane business

step P1: The courier attempts delivery
  next P2
step P2: If nobody is home, try again the next working day
  next P3
step P3: After the third failed attempt, return the parcel to the depot

lane service

step S1: Delivery attempt recorded
  next S2
  realizes P1
step S2: Redelivery scheduled
  realizes P2

lane code

step C1: Scan handler stores the attempt
  next C2
  realizes S1
  ref gh:src/scan/handler.py#L30-L77
step C2: Drop duplicate scans
  next C3
  ref gh:src/scan/dedupe.py#L8-L29
  why the depot's scanner sends each scan twice
step C3: Count attempts on the parcel row
  next C4: first or second failure
  next C5: third failure
  realizes S2
  ref gh:src/redelivery/counter.py#L14-L52
  why joining the attempts table on every scan was too slow
step C4: Schedule the next attempt
  realizes S2
  ref gh:src/redelivery/schedule.py#L20-L64
step C5: Create the return order
  realizes P3
  ref gh:src/returns/create.py#L11-L40
```

Divergences:

- `implementation-only:C2`
- `expanded:S2[C3,C4]@code`
- `skipped:P3[C5]@service`

### 8.3 A branch with an error path

```trace v2
lane business

step D1: Receive uploaded files
  next D2
step D2: Extract text
  next D3
step D3: Classify the document
  next D4
step D4: Notify the user

lane code

step I1: Upload endpoint handlers
  next I2
  realizes D1
  ref gh:src/upload/handlers.py#L40-L120
step I2: Is the file larger than 20 MB?
  next I3: yes
  next I4: no
  ref gh:src/import/size_guard.py#L12-L48
  why the parser runs out of memory on larger files
step I3: Reject: tell the user the file is too large
  ref gh:src/import/size_guard.py#L50-L61
  why otherwise the user waits for an import that never finishes
step I4: Extract and classify in one pass
  next I5
  realizes D2
  realizes D3
  ref gh:src/import/parse.py#L200-L310
  why avoids parsing each file twice
step I5: Batch notifications, at most one per user per minute
  realizes D4
  ref gh:src/notify/batch.py#L15-L90
```

Divergences:

- `implementation-only:I2`
- `implementation-only:I3`
- `merged:I4[D2,D3]@business`

### 8.4 A loop

The archive returns documents a page at a time. `F3` returns to `F1` under `back`; the forward flow stays `F1 → F2 → F3 → F4`.

```trace v2
lane business

step A1: Fetch the list of documents from the archive
  next A2
step A2: Import each document
  next A3
step A3: Report how many were imported

lane code

step F1: Request one page of 100 document ids
  next F2
  realizes A1
  ref gh:src/archive/client.py#L22-L60
step F2: Import the documents on the page
  next F3
  realizes A2
  ref gh:src/archive/import_page.py#L10-L88
step F3: Read the cursor for the next page
  next F4
  back F1: while a cursor is returned
  realizes A1
  ref gh:src/archive/client.py#L62-L79
  why the archive refuses requests for more than 100 ids
step F4: Write the totals row
  realizes A3
  ref gh:src/archive/report.py#L5-L31
```

Divergences:

- `expanded:A1[F1,F3]@code`
- `scattered:A1[F1,F3,F2]@code` (fetching the list is interleaved with importing: the business view says "fetch, then import", the code pages)

### 8.5 Reordered

```trace v2
lane business

step B1: Check the address
  next B2
step B2: Store the parcel

lane code

step C1: Insert the parcel row
  next C2
  realizes B2
  ref gh:src/intake/store.py#L12-L40
step C2: Validate the address in a background job
  realizes B1
  ref gh:src/intake/address_job.py#L8-L55
  why the address service takes up to 3 s
```

Divergences:

- `reordered:B1[B2,C1,C2]@code`

### 8.6 A planned change

The plan adds a reservation check on the server, changes the eligibility check, and deletes two things: the desk app's own warning and the nightly reminder job.

```trace v2
lane business

step B1: A member asks to borrow a book
  next B2
step B2: Check the member may borrow
  next B3
step B3: Refuse if another member reserved the copy
  next B4
  change new
step B4: Hand over the book and set the due date
  next B5
step B5: Remind the member before the due date

lane code

step C1: POST /loans handler
  next C2
  realizes B1
  ref gh:src/loans/api.py#L20-L58
step C2: Check overdue items and the loan limit
  next C3
  realizes B2
  ref gh:src/loans/rules.py#L10-L47
  why returns a reason code instead of true or false, so the desk can show it
  change modified
step C3: Look up open reservations for the copy
  next C4
  realizes B3
  change new
step C4: Desk app warns about a reservation
  next C5
  ref gh:desk/src/loan_dialog.ts#L88-L120
  why the warning could be dismissed; the server decides now
  change removed
step C5: Create the loan row with the due date
  next C6
  realizes B4
  ref gh:src/loans/store.py#L80-L112
step C6: Nightly job sends reminders 2 days before the due date
  realizes B5
  ref gh:src/jobs/reminders.py#L5-L40
  change removed
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
```trace v2
lane business

step B1: A member returns a book
  next B2
step B2: The next member waiting for it is told

lane code

step C1: Close the loan and free the copy
  next C2
  realizes B1
  ref gh:src/loans/return.py#L18-L52
step C2: Email the first open reservation
  realizes B2
  ref gh:src/reservations/notify.py#L9-L40
```
````

**Format**

- Fence the block as `trace v2`.
- A `lane <name>` line opens each lane, most abstract first; the steps of a lane follow it, in the order things happen.
- `step <id>: <text>` is one step; its attributes are indented below it: `next`, `back`, `realizes`, `covers`, `ref`, `guards`, `why`, `change`. Do not invent other keywords; one of your own must start with `x-`.

**Content**

- Each lane describes the *same* process. The top lane is how a user or the business would tell it; the bottom lane is what the code does. Add a middle lane only if the reader needs it.
- The step's text is one short line saying what happens. Keep it under 100 characters.
- Describe the code as it is, including the steps nobody asked for: guards, retries, caches, batching, clean-up. Do not leave a code step out to make the lanes match, and do not add `realizes` to a step that implements nothing above it. The differences are what the reviewer needs.
- A step that realizes nothing gets a `why`.
- Every step in the bottom lane gets a `ref` to the lines it describes, unless it is `change new`.
- `next` names what follows in the same lane, one per line. For a branch, add a label after `: ` saying when each edge is taken:

  ```text
    next C3: queue not empty
    next C5: queue empty
  ```

- A loop or retry is a `back` on the step where the flow returns, with a label saying when: `back C1: while pages remain`. `next` must never lead back to an earlier step.
- `realizes` names steps in lanes above. In three lanes, link each step to the lane directly above where you can; do not also link it to the top lane.
- `covers` names an acceptance criterion of the document that a business step demonstrates.
- A step that keeps an invariant true names it with `guards`: the document's own by id, the parent plan node's as `parent#<id>`, a domain's as `<domain>#<id>`. Put it on the step where the property is enforced, in the lowest lane that shows it.
- When the trace describes a plan, mark what the plan does: `change new`, `change modified` or `change removed`. Leave unchanged steps unmarked. Keep removed steps in the flow where they are today.
- Aim for 20 steps or fewer in a block; 40 is the limit. For a larger process, put an overview block under one heading and one block per phase under its own heading, and repeat the last step of a phase as the first step of the next.

**Ids are permanent**

Reviewers' comments are attached to step ids. When you change an existing trace:

- read the existing block first and keep the id of every step that is still the same step, even if its text changes or it moves;
- give a new step an id that was never used in that block; do not renumber the others to make room;
- never give a deleted step's id to a different step;
- use a different prefix per lane and per block under the same heading (`B1…`, `C1…`), so that two blocks never share an id.

**Check**

Run `kalka check <file> --json` after writing. Fix every error (the block is shown as plain code until you do) and read every warning. Then read the divergence list: each item should be something true about the code. If one is an artefact of how you wrote the trace, fix the trace; if it is real, leave it, and make sure the step's `why` explains it.
