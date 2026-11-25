---
name: kalka-blueprint
description: How to write a blueprint (a Markdown implementation plan reviewed with kalka) that agents can act on safely, in the kalka language v2, with plan nodes, goals, acceptance criteria and domain trees. Use when writing, extending or implementing a blueprint or a plan node, or when recording where an implementation departed from one.
kalka_reference: kalka-blueprint@v2
---

# Writing blueprints agents can use

A *blueprint* is one Markdown file describing an implementation plan. People and agents write it with their own tools; kalka passes it between teammates for review and collects their comments in a `## Review log` at the end of the file.

This reference describes what a blueprint useful to agents usually contains. It is guidance, not a template you must fill: a short plan for a small change may need two of these sections, and kalka accepts any Markdown. kalka takes meaning only from the frontmatter and from typed blocks (`goals`, `invariants`, `glossary`, `trace`), all written in one line-oriented language, the kalka language v2. The examples come from an invented lending library.

## Opting in

Add one line to the blueprint's frontmatter:

```yaml
kalka_reference: kalka-blueprint@v2
```

kalka then shows soft hints, such as "no verification plan" (rules `H001` to `H005`), which anyone can dismiss. Documents without this line get no hints. Hints are information: they never block a sync and never fail `kalka check`.

## Rules for agents working on a blueprint

- **The body belongs to the owner.** The frontmatter `kalka_owner:` names them. If you are not acting for the owner, do not edit the body: add a comment instead (`kalka comment`).
- **Never edit the review log by hand.** Entries are signed and hashed; an edited entry becomes invalid. Add entries with `kalka comment <file> --anchor <slug> -m "<text>"`.
- **Leave unknown frontmatter alone.** Keys you do not understand belong to someone's pipeline.
- **Stay inside the scope.** Work that does not fit becomes a seed, not a larger change (see "When to split off a seed").
- **Do not "clean up" critical sections.** They look wrong on purpose. Read their reasoning first and ask a human.
- **Ids are permanent.** Comments attach to the ids of steps, invariants, goals and criteria; keep them when you change a block.
- **Start from the context.** `kalka context <file>` prints what the document's work needs: the parent goals it realizes and the goals above them, every invariant that binds it, the domain terms it uses, how current each document is, and what is missing ("not here", who else claims a goal, which goals nothing claims). It leaves out the document's own text: read the document itself. Terms and other goals it left out are named on one line.
- **Ask, do not guess.** For a term the context names but does not define, run `kalka context <file> --term <name>`; `--full` prints everything on the paths. A gap the context names ("not here", "not looked for") is a gap: do not fill it with an assumption.
- **When the code and the context disagree,** do not quietly follow either. Domain terms and invariants are binding at the pinned versions; an approved blueprint describes the intended change as of its approval, and the code may have moved since. Record a deviation (`kalka comment <file> --kind deviation --anchor <slug> -m "<what differs>"`) or ask a human.

## The sections

Use these headings (any level) so that people, agents and kalka's hints find them. Each one says why it helps an agent.

### Domain reference

Which domain document, and which version of it, the plan was written against. Put it in the frontmatter, and repeat it in prose if the plan relies on particular terms:

```yaml
kalka_domain: team-lending/library.reservations@v1
```

One pin is enough: the domain node names its parent's version, so the plan rests on the whole path from the root domain (`library@v2`) to the node. Write the plan in the glossary's terms. Where the domain lists a word as an alias ("book" for "copy"), use the term: `kalka check` warns about each alias it finds, with the term to write instead (`G020`). Name the domain invariants the plan touches with `affects` (see "Verification plan").

*Why agents benefit:* the domain documents define the team's words (what a "hold" is, when a copy counts as "returned"). An agent that reads the right versions uses the same names and respects the same invariants as the rest of the code. kalka shows when a newer version changes something the plan uses, so the plan's author can decide whether it matters.

### Scope

The bounded contexts, modules and interfaces the change is expected to touch, and anything it must not touch.

> Touches the `loans` module and a new `holds` module. Does not change the catalogue or the members' web page.

*Why agents benefit:* an explicit boundary lets an agent tell "part of this plan" from "a new idea". When the work turns out to need more, the agent stops and proposes a seed instead of quietly growing the change, which keeps the review the reviewers agreed to.

### Verification plan

Invariants that must hold, how each is verified, and what a human reviewer must specifically check, as an `invariants v2` block. Write it while planning, not while reviewing.

````markdown
```invariants v2
affects library.reservations#R1
affects parent#E1

invariant N1: A copy on hold is never put back on the shelf
  verified-by gh:test/holds_test.py#L62-L90
  status new

invariant N2: A member who cancels a reservation while a hold is created gets no email
  verified-by Reviewer: run the migration on a copy of last week's reservations; it rewrites 2 million rows
  status new
```
````

- `invariant <id>: <statement>`: the statement is one sentence saying what holds, not what the code does.
- `verified-by`, one line each, says how it is checked: a reference to the test (`gh:…`), or a check a person makes, starting `Reviewer:`. An invariant without one is a gap `kalka check` reports (`I031`).
- `why` says why it must hold; `status` what the plan does to it: `new`, `changed` or `removed`. Leave `status` out for an invariant the plan keeps as it is.
- `affects` names another document's invariant the plan touches: a domain's as `<domain>#<id>`, the parent plan node's as `parent#<id>`. A block may hold only `affects` lines.
- Inherited invariants (from the plan nodes above) may not be loosened or removed: never reuse an inherited id for an invariant of your own (`P023`).
- In a trace, the steps that keep an invariant true name it with `guards` (see "Trace blocks"). An invariant no step guards is a gap (`I030`).

*Why agents benefit:* an agent cannot know which properties matter most to the team. A verification plan written up front turns them into tests the agent writes first, and tells the reviewer where to spend their attention instead of rereading everything.

### Critical sections

Places where the design deliberately departs from the clean model, for example for performance, compatibility or an external constraint, with the reasoning.

> The queue length is kept on the title row. Counting reservations on every return was too slow at the busiest branch (p99 above the desk's 200 ms budget). Keep the two in sync in the same transaction; do not replace the counter with a count.

*Why agents benefit:* agents are trained to remove duplication and restore the "proper" design. Here that would reintroduce the problem the design avoids. Naming these places, and requiring human review there, prevents a well-meant cleanup from undoing a decision.

### Operations

How the change fails in production, what to watch, and how to roll it back.

> - Failure: returns stop creating holds. Watch `holds_created` per hour; alert below 50 % of the same hour last week.
> - Rollback: switch off the `holds-on-return` flag. The migration is additive, so no schema rollback is needed.

*Why agents benefit:* an agent implementing the plan adds the metrics, logs and flags operations will need, instead of leaving them for later. A reviewer can check that the rollback really works without the author present.

### Deviation log

Where the implementation departed from the plan as written. This is not a section in the body: it is the set of `deviation` entries in the review log, which kalka keeps in order and never loses.

Record a deviation as soon as it happens:

```sh
kalka comment plan.md --anchor scope --kind deviation \
  -m "Also changed the renewal endpoint: it ignored open reservations."
```

*Why agents benefit:* the plan stays the agreed reference, and the log says what changed and why. The next agent, or the reviewer, sees both, instead of a plan silently rewritten to match the code.

## Plan nodes, goals and acceptance criteria

Plans can form a tree. A *plan node* (an epic, say) holds goals and invariants; the blueprints below it hold acceptance criteria and are what gets implemented. kalka has no level names: one edge, `kalka_parent`, any depth. All of it is optional, and a single blueprint may hold goals, criteria, invariants and a trace together.

A child names its parent by the parent's `kalka_id`:

```yaml
kalka_parent: 8d1c0f2a-4b7e-4c39-9a51-2e6f0b3d7c11
```

In the text the parent is always `parent`: `parent#G1` is goal G1 of the parent plan node, `parent#E1` its invariant E1.

A plan node declares its goals in the domain's business language:

````markdown
```goals v2
goal G1: The queue head learns that a copy is waiting for them
goal G2: A held copy is not lent to anyone else
```
````

A leaf holds acceptance criteria in the same block. Each realizes a goal of its direct parent, and says given, when and then, one line each, in that order; repeat a keyword instead of writing `and`:

````markdown
```goals v2
criterion AC1: The queue head is notified exactly once
  realizes parent#G1
  given a copy whose title has two open reservations
  when the copy is returned
  then the queue head gets one email
  then the second member gets nothing
```
````

- A goal at a middle level realizes a goal of its own parent: `goal G4: …` with `realizes parent#G1`. It may name several goals of the same parent.
- A goal's state is computed, never written: *unclaimed* (nothing realizes it) or *claimed*. Criteria carry no state: kalka does not record whether anything is implemented; the tracker, git and CI do.
- A team document also has an approval state, computed from its review log: *draft*, *approved vN* or *changed since approved vN*. The owner approves with `kalka approve <file>`; never write it by hand. `kalka context` names each level's state: it says where the review stands, not whether the work is done.
- An unclaimed goal is a warning (`P030`), on a draft or an approved document alike. With `kalka_breakdown: adopted` (the stories came from outside kalka), coverage is a finding to decide on (`P032`); `decomposed` says kalka was used to make the split.
- In a trace, a business step names the criterion it demonstrates with `covers AC1`. One chain then runs from the epic's goal through the criterion and the business steps to code with a `ref`.

*Why agents benefit:* the goals above a blueprint say why it exists and what it must not break, and the criteria say when it is done. An agent that reads `kalka context` before starting works toward the epic's goals instead of only the words of one story, sees which sibling story already claims a goal, and a reviewer sees at once which goal nothing serves yet.

## The kalka language v2

Every typed block is written in one line-oriented language, readable as plain text:

- A header line starts in column 0: an item `<keyword> <name>: <text>` (`step B1: A member returns a copy`), or a section `<keyword> <text>` (`lane business`).
- An attribute line is indented by exactly two spaces: `<keyword> <value>`. There is no third level.
- One line is one value. A list is a repeated keyword (`alias patron`, then `alias reader`). There is no line continuation and no `and`.
- Keywords are lower case, singular and whole words (`criterion`, `verified-by`), and come only at the start of a line. Never put a colon after a keyword: `why because`, not `why: because`.
- Free text may hold commas, colons and quotes; nothing needs escaping or quoting.
- Ids (`invariant`, `goal`, `criterion`, `step`) start with a letter and use letters, digits, `-` and `_`, at most 32 characters. A `term` name may hold spaces and ends at the first `: `.
- References: `<id>` within the document, `parent#<id>` for the parent plan node, `<domain>#<id>` for a domain node (`library.reservations#R1`).
- No tabs and no comments: say it in the prose around the block. A keyword of your own starts with `x-` and is ignored.
- Do not drift into familiar formats: no `Scenario:` or `Given` (Gherkin), no `- id:` or `lanes:` (YAML). `kalka check` reports them with the v2 form to write (`S010`).

## The issue it addresses

When the plan is written for an issue in the team's tracker and you know its key, put it in the frontmatter while planning:

```yaml
kalka_issue: jira:DEV-123
```

The part before the colon picks the tracker's plugin (`jira:` for Jira); the rest is the issue's key. One issue per blueprint. kalka shows each reader the issue's title and status through their own plugin and credentials, and never writes them into the file. Once a version is released, the owner can export it to that issue (`kalka export`), which attaches the file and records an `exported` entry. Do not write `exported` entries yourself. `kalka check` warns about a malformed key (`F020`).

Why it helps: the issue holds the request and the discussion the plan answers. Linking it lets people find the plan from the work item, and lets kalka group plans by epic or status.

## Trace blocks

A trace block shows one process at two or more levels (lanes), most abstract first, and links the levels. kalka draws it and lists where the levels differ. Write one when a plan describes a pipeline or algorithm in words and the reader needs to find it in the code.

*Why agents benefit:* the next agent finds the code for each step of the plan without searching, and the reviewer sees at once which code steps the plan never mentioned.

````markdown
```trace v2
lane business

step B1: A member returns a copy at the desk
  next B2
step B2: The queue head is told the copy waits for them
  covers AC1

lane code

step C1: Record the return
  next C2
  realizes B1
  ref gh:src/loans/returns.py#L30-L77
step C2: Send the hold email
  realizes B2
  ref gh:src/holds/notify.py#L9-L40
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

Renaming the heading above a block detaches the comments on its steps, as it does for comments on a section.

## Domain documents

A domain document (`kalka_kind: domain`) holds a team's shared language and what must always hold. Blueprints reference one released version of it with `kalka_domain: <team>/<domain>@v<N>`, and kalka links their text against that version.

**The domain tree.** Domains form a tree: the team's root domain, its subdomains, theirs and so on. A name is a path, `library.reservations`, and starts with its parent's name. A subdomain pins its parent's version:

```yaml
kalka_kind: domain
kalka_domain: library.reservations
kalka_parent: team-lending/library@v2
```

A new parent version bumps nothing below it: the subdomain shows "parent newer" (`D030`) until its owner moves the pin. A subdomain only adds words and invariants; redefining an inherited term is an error (`D010`). To narrow an inherited term, write an invariant that says so with `refines`:

````markdown
```invariants v2
invariant R2: A loan of a copy whose title has open reservations cannot be renewed
  refines loan
  why renewing would keep the queue waiting indefinitely
```
````

An invariant that speaks only of inherited terms and does not say which it refines is an error (`D011`). When two branches need different meanings of one word, use two terms, or move the term down into each branch. A plan node may name several domain nodes, as a list `kalka_domain: [team-lending/library.reservations@v1, team-lending/library.fines@v1]`; where two of them define a word differently, write it qualified, `library.reservations#hold` (`D020`).

**Glossary.** Define the terms in one or more `glossary v2` blocks:

````markdown
```glossary v2
term copy: One physical item of a title that can be lent
  form copies
  alias item
  alias book

term queue head: The member whose reservation is first in the queue
  see queue
  see member
```
````

- `term <word>: <definition>`: the word the team uses, and one sentence a reader sees on hover. `form` lines are other forms of the same word (plurals); `alias` lines are words people also use that the plan should not (each use is a warning). `context` names the bounded context; `see` names related terms of the same glossary or of a domain above it.
- A word names one entry: the same term, form or alias in two entries is an error.
- Matching ignores case and works on whole words; a term interrupted by markup does not match. Code, link targets and the frontmatter are never matched.

**A plan with no domain.** A blueprint without `kalka_domain:` may define its own words in a `glossary v2` block, and its own invariants in an `invariants v2` block. kalka links and checks the plan against them as it would against a domain's. Once the plan names a domain, move its terms into the domain document: its own block is then shown as code (`G008`).

**Invariants.** List them in an `invariants v2` block, as in a plan but without `status` or `affects`. Ids are referenced from blueprints as `<domain>#<id>`; never reuse the id of a removed invariant for a different one. When the owner releases a version that removes an invariant or adds an alias, kalka lists the held blueprints that rely on it.

## When to split off a seed

A *seed* is a small blueprint (`kalka_status: seed`) holding an idea split off from a plan that is growing. Split one off when:

- the work needs a module outside the plan's scope;
- a deviation turns out to be a feature of its own;
- a reviewer's comment raises something worth doing, but not now.

In kalka, "Create seed" on a deviation entry, or `kalka seed <file> --from <entry id> --title "<title>"`, writes the seed with its problem and why it was split off, and with `kalka_origin: <kalka_id>#<entry id>` naming where it came from, and records a `budded` entry in the document it came from so the two stay linked. The seed waits in the team's pre-backlog until its owner promotes it.

## Check before handing over

Run `kalka check --json <file>` before you hand a blueprint over, and again after every change:

```sh
kalka check --json plan.md
```

Each diagnostic has a rule id, a severity, a line and a message saying what to write instead; some carry a `fix` with the replacement text. Fix every error. Read every warning: fix it, or make sure the plan says why it stays. Information needs nothing. `kalka check --explain <id>` says what a rule means and why it exists. kalka never rewrites the file for you.

The exit code is 0 when there is nothing to fix, 1 for errors, 2 for warnings only. kalka check reads the domain versions and plan nodes it needs from the files it is given and from the local kalka folder; when they are elsewhere, point `--domain` at a document or a folder. A domain version that is not there is reported once (`R001`, `D002`), and the rules that need it are not run.

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
| `F010` | warning | **Domain name missing or malformed.** A domain document (`kalka_kind: domain`) has no valid `kalka_domain:` name: segments of lower-case letters, digits and dashes, a subdomain's joined by dots. |
| `F011` | warning | **Domain version is not a number.** `kalka_version:` of a domain document is not a positive number. |
| `F012` | error | **Domain named parent.** A domain document is named `parent`, the reserved name of the parent plan node in references. |
| `F013` | warning | **Domain parent malformed.** A domain document's `kalka_parent:` is not `<team>/<domain>@v<N>`; it is ignored. |
| `F014` | warning | **Parent plan node malformed.** `kalka_parent:` of a document that is not a domain document is not a `kalka_id`; it is ignored. |
| `F015` | warning | **Seed origin malformed.** `kalka_origin:` is not `<kalka_id>` or `<kalka_id>#<entry id>`; it is ignored. |
| `F016` | warning | **Breakdown not known.** `kalka_breakdown:` is not `decomposed` or `adopted`; it is ignored. |
| `F020` | warning | **Issue link malformed.** `kalka_issue:` is not `<scheme>:<issue>`: a plugin scheme, a colon and the issue's key, with no spaces. |
| `F021` | info | **Issue scheme without a shipped plugin.** `kalka_issue:` names a scheme no plugin shipped with kalka handles; a plugin installed for it may. |
| `F022` | warning | **Archive time malformed.** `kalka_archived:` is not a UTC time; the blueprint is not treated as archived. |

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

#### The kalka language v2 (S)

| Rule | Severity | What it means |
|---|---|---|
| `S001` | error | **Tab in a block.** A line of a typed block holds a tab. The fix replaces each tab with two spaces. |
| `S002` | error | **Keyword not in lower case.** A keyword is written with capitals. The fix writes it in lower case. |
| `S003` | error | **Colon after a keyword.** A keyword is followed by a colon, as in YAML. The fix removes the colon. |
| `S004` | error | **Wrong indentation.** A line is indented by something other than nothing (a header) or exactly two spaces (an attribute). There is no third level. |
| `S005` | error | **Header line incomplete.** A header is not `<keyword> <name>: <text>` (an item) or `<keyword> <text>` (a section): the name, the colon or the text is missing. |
| `S006` | error | **Attribute with no item.** An indented attribute line comes before any item it could belong to. |
| `S007` | error | **Empty value.** A keyword has no value after it, or a label after `: ` is empty. |
| `S008` | error | **Keyword on the wrong level.** An attribute keyword starts a line in column 0, or a header keyword is indented. |
| `S009` | error | **Reference with spaces.** A keyword of type reference has a value with spaces, or several references on one line. |
| `S010` | error | **Familiar format instead of v2.** The block drifts into a familiar format: Gherkin (`Given`, `Scenario:`, `And`) or YAML (`- id:`, `lanes:`). The message, and where it can a fix, shows the v2 form. |
| `S011` | error | **Comment in a block.** A line of a typed block starts with `#` or `//`. |
| `S012` | error | **Block version not read.** The fence names a version this kalka does not read: anything but v2. A newer one says it needs a newer kalka. The block is shown as code. |
| `S013` | warning | **Fence without a version.** The fence names the block (`goals`) with no version; it is read as v2. |
| `S020` | warning | **Unknown keyword.** A line starts with a word that is not a keyword of the block and does not start with `x-`; the line, and for a header its attributes, is ignored. The message names the nearest keyword. |
| `S021` | error | **Keyword written twice.** A keyword that takes one value (`why`, `ref`, `status`, `change`, `context`) appears twice under one item; the second is ignored. |
| `S022` | error | **Value not one of the allowed words.** `status` is not `new`, `changed` or `removed`, or `change` is not `new`, `modified` or `removed`. |
| `S030` | error | **Not an id.** The name of an invariant, goal, criterion or step is not an id: a letter, then letters, digits, `-` and `_`, at most 32 characters. |
| `S031` | error | **Empty block.** A typed block has no items. |

#### Trace blocks (T)

| Rule | Severity | What it means |
|---|---|---|
| `T011` | error | **Wrong number of lanes.** Fewer than 2 or more than 5 lanes. |
| `T012` | error | **Bad lane name.** A lane name is longer than 40 characters, or a lane is opened twice. |
| `T020` | error | **No steps.** The block has lanes and no steps. |
| `T022` | error | **Too many steps.** More than 40 steps. |
| `T031` | error | **Step id used twice.** Two steps have the same id. |
| `T032` | error | **Step before any lane.** A step comes before the first `lane` line. |
| `T040` | error | **Unknown step named.** `next`, `back` or `realizes` names an id that is not a step of the block. |
| `T041` | error | **Edge across lanes.** `next` or `back` names a step in another lane. |
| `T042` | error | **Step follows itself.** `next` names the step itself. |
| `T043` | error | **Cycle in next.** The `next` edges form a cycle. |
| `T044` | error | **back goes forward.** `back` names a step that is neither the step itself nor one before it. |
| `T045` | error | **realizes names a lower lane.** `realizes` names a step that is not in a lane above. |
| `T104` | warning | **Id named twice.** An id is named twice by one keyword of a step, or as both `next` and `back`. |
| `T105` | warning | **Redundant realizes.** A step realizes a step directly and also through another step it realizes. |
| `T106` | warning | **Empty lane.** A lane has no steps. |
| `T107` | warning | **Lane in pieces.** The steps of a lane fall into groups that no `next` or `back` edge joins. |
| `T108` | warning | **Many steps.** More than 20 steps. |
| `T109` | warning | **Text too long.** A step's text longer than 100 characters, `why` longer than 300, or a label longer than 40. |
| `T110` | warning | **Step without why.** A live step outside the first lane realizes nothing and has no `why`. |
| `T111` | warning | **Ids differ only in case.** Two ids differ only in letter case. |
| `T112` | warning | **ref is not scheme:payload.** `ref` is not of the form `scheme:payload`. |
| `T113` | warning | **Some branches unlabelled.** A step has several `next` edges and only some have a label. |
| `T120` | warning | **Step id shared under one heading.** Two trace blocks under the same heading both have a step with this id. |
| `T121` | warning | **Trace without a heading.** The block has no heading above it, so its steps cannot be commented on. |

#### Glossary (G)

| Rule | Severity | What it means |
|---|---|---|
| `G008` | warning | **Glossary in a blueprint that names a domain.** A blueprint with `kalka_domain:` has a glossary block of its own; it is shown as code and links nothing. A blueprint that names no domain may keep its own glossary. |
| `G012` | error | **A word names two glossary entries.** A term, form or alias is the same (ignoring case) as a term, form or alias of another entry of the document. |
| `G013` | error | **Term too long.** A term is longer than 60 characters. |
| `G014` | warning | **Too many terms.** The glossary has more than 500 terms; the rest are not read. |
| `G015` | warning | **Definition too long.** A definition is longer than 400 characters. |
| `G016` | warning | **see names an unknown term.** `see` names a word that is not a term of the same glossary, or of a domain above it. |
| `G020` | warning | **Alias used instead of the term.** The text uses an alias of a glossary term; the diagnostic offers the term as a replacement. |
| `G030` | info | **Release would make held blueprints use an alias.** Shown to a domain owner releasing a new version: a held blueprint uses, as a word, an alias the new version introduces. |

#### Invariants (I)

| Rule | Severity | What it means |
|---|---|---|
| `I008` | warning | **Blueprint-only keyword in a domain document.** A domain document's invariants block uses `affects` or `status`, which are for blueprints; they are ignored. |
| `I011` | error | **Invariant id used twice.** Two invariants of the document have the same id. |
| `I014` | warning | **Too many invariants.** The document has more than 100 invariants; the rest are not read. |
| `I015` | warning | **Statement too long.** An invariant's statement is longer than 300 characters. |
| `I016` | warning | **Invariant id is also a trace step id.** An invariant and a trace step under the same heading share an id, so the anchor `§slug#id` names both. |
| `I017` | error | **affects names an own invariant.** `affects` names an id without a document: the document's own invariants are written as invariant lines. |
| `I020` | error | **Invariant reference does not resolve.** A `guards` or `affects` line names an invariant that does not exist: an unknown id, `parent#<id>` with no parent or not among its invariants, or a domain that is not on the path from the pinned domain node to the root. |
| `I030` | warning | **No trace step guards the invariant.** The document has a trace, and no step lists the invariant (one it introduces or affects) under `guards`. |
| `I031` | warning | **Invariant not verified.** An invariant has no `verified-by`. |
| `I040` | info | **Referenced invariant removed in a newer domain version.** A blueprint references a domain invariant that a newer version of the domain no longer has. Also shown to the domain owner releasing that version, listing the held blueprints concerned. |

#### Plan graph and goals (P)

| Rule | Severity | What it means |
|---|---|---|
| `P001` | error | **Goal id used twice.** Two goals or criteria of the document have the same id, or a goal or criterion has the id of an invariant of the document. |
| `P002` | error | **Criterion steps out of order.** A criterion's `given`, `when` and `then` lines are not in that order. |
| `P003` | warning | **Criterion without then.** A criterion has no `then` line. |
| `P004` | error | **realizes is not parent#<id>.** A goal's `realizes` does not name a goal of the parent plan node as `parent#<id>`, or a criterion's names neither that nor a goal of its own document. |
| `P010` | error | **Parent plan node not found.** `kalka_parent:` names a document that is not here, or a plan node further up names one that is not. |
| `P011` | info | **Plan graph not checked.** Only the files checked were read, with no folder to look plan nodes up in, and a parent, an ancestor or the children the rules need are not among them, so those rules were not run. |
| `P012` | error | **Parent cycle.** Following `kalka_parent:` from the document leads back to it. |
| `P020` | error | **realizes names no such goal.** `realizes parent#<id>` names no goal of the direct parent (a goal of a node further up is named through the levels between), or a criterion's `realizes <id>` names no goal of its own document. The parent must be here. |
| `P021` | error | **parent# with no parent.** The document writes `parent#<id>` and has no `kalka_parent:`. |
| `P022` | error | **covers names no such criterion.** A trace step's `covers` names an id that is not a criterion of the document. |
| `P023` | error | **Inherited invariant redefined.** The document defines an invariant with the id of one it inherits from a plan node above it. |
| `P030` | warning | **Goal unclaimed.** No child goal and no criterion names the goal with `realizes`. It is reported the same whether the document is a draft or approved. |
| `P032` | info | **Goal coverage to decide.** With `kalka_breakdown: adopted`, a goal that no child goal or criterion claims: a finding for a person to decide on, not a planning error. |

#### Domain tree (D)

| Rule | Severity | What it means |
|---|---|---|
| `D001` | error | **Subdomain name not under its parent.** A domain node's name does not start with its parent's name and a dot. |
| `D002` | info | **Domain path not here, can't check.** A version on the path from a domain node to the root is not available, so the rules that need it were not run. |
| `D003` | error | **Domain parent cycle.** Following the domain nodes' `kalka_parent:` pins leads back to the node. |
| `D010` | error | **Subdomain redefines a term.** A subdomain defines a term, form or alias that a domain above it already defines. |
| `D011` | error | **Narrowing without refines.** A subdomain's invariant speaks only of terms its ancestors define, and none of its own, without saying which it narrows with `refines`. |
| `D012` | error | **refines names no ancestor term.** `refines` names a word that is not a term on the path from this domain to the root. |
| `D020` | error | **Word needs qualifying.** The document names several domain nodes, two of them define the same word differently, and the text uses it without saying which: `library.reservations#hold`. |
| `D021` | warning | **Inconsistent ancestor versions.** Two domain nodes the document names pin different versions of a common ancestor; the message shows both paths. |
| `D030` | info | **Parent domain newer.** A domain node pins an older version of its parent than the newest held. |
| `D031` | info | **Siblings share a term.** Two subdomains with the same parent define the same term the same way. |

#### References to other documents (R)

| Rule | Severity | What it means |
|---|---|---|
| `R001` | info | **Domain version not here, can't check.** A domain version `kalka_domain:` names is not available, so the glossary, invariant and domain-tree rules that need it were not run. |
| `R002` | warning | **Malformed domain reference.** `kalka_domain:` in a blueprint is not `<team>/<domain>@v<N>`, or a list `[<team>/<domain>@v<N>, …]` of them. |
| `R003` | info | **Newer domain version.** A newer version of the referenced domain node is available, and it changes a term or an invariant the document uses. A newer version that changes nothing the document uses is not reported. |
| `R010` | warning | **Reference does not resolve.** With --with-plugins: a trace `ref` or an invariant's `verified-by` reference has no plugin or does not resolve. |
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
kalka_reference: kalka-blueprint@v2
kalka_domain: team-lending/library.reservations@v1
kalka_parent: 8d1c0f2a-4b7e-4c39-9a51-2e6f0b3d7c11
kalka_issue: jira:DEV-123
---

# Return creates a hold

## Context
## Scope
## Acceptance criteria

```goals v2
criterion AC1: …
  realizes parent#G1
  given …
  when …
  then …
```

## Plan
## Verification plan

```invariants v2
affects parent#E1

invariant N1: …
  verified-by …
```

## Critical sections
## Operations
````

The review log is added by kalka when the first comment arrives.
