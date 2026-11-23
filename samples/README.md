# Sample blueprints

Blueprints for trying kalka by hand: a demo, a first look, or a manual
test of the app. Most describe kalka itself, as the plans a team working on
it would review; the `lending` and `library` sets describe a lending library. Except in
`library`, where the plan tree needs them, none holds a `kalka_id`, owner
or review log: kalka adds those as you publish them and comment on them,
under your own identity. Every typed block is in the kalka language v2
(`docs/language.md`).

`TestSamples` (`internal/cli/samples_test.go`) keeps each one passing
`kalka check` with the hints and warnings listed below. Each folder is
checked against its domain document (`--domain`).

Four folders:

- the top folder, for the team `kalka-core`: plans about kalka itself, on the domain `sync`;
- `packaging/`, for the team `packaging`: plans about building and publishing kalka, on the domain `release`;
- `lending/`, for the team `lending`: a lending library, on the domain `loans`, with no code to link;
- `library/`, for the team `team-lending`: a plan tree (an epic and a story) on a domain tree (`library` and its subdomain `library.reservations`).

Every domain document says `kalka_version: 1`, as a released copy kept in
a repository does, so `kalka check --domain` can read it from the folder.
Publishing one to its team makes version 1; the plans next to it refer to
that (`kalka-core/sync@v1`, `packaging/release@v1`, `lending/loans@v1`).

## kalka-core

| File | What it exercises |
|---|---|
| `kalka-core-sync-domain.md` | A domain document (`kalka_kind: domain`, `kalka_domain: sync`): a glossary block (terms, forms, aliases, `context`, `see`) and an invariants block (`why`, `verified-by` linking to tests with `gh:`), plus the team log's operations. Pinned by `repo` and `base_sha`. |
| `push-during-round.md` | A full blueprint opted into the reference (`kalka_reference: kalka-blueprint@v2`) on `kalka-core/sync@v1`: every section, so no hints; a Mermaid flowchart; `gh:` links in the text; a two-lane trace with `ref`, `why` and `guards` (a domain invariant, `sync#I5`, and the plan's own); an invariants block that `affects` a domain invariant; one divergence to review (C4, implementation-only); a critical section. |
| `retry-first-release.md` | A short opted-in plan with `gh:` links and a trace of a planned change: `change removed`, `new` and `modified`, a labelled branch and a `back` loop (retry). Hints for no verification plan, no critical sections and no operations. |
| `join-flow-trace.md` | Trace blocks over kalka's join flow, every code node linked to its lines: two and three lanes, a labelled branch, a node that loops back to itself, an author's `x-` keyword, and the divergence classes implementation-only, merged and expanded. A Mermaid sequence diagram. No domain, so a glossary of its own under `## Words`. |
| `seed-push-latency.md` | A seed: `kalka_status: seed` with `kalka_origin` (the document and the entry it was split off from) and `kalka_created`, and the body kalka writes (a title, `## Problem`, `## Why it was split off`). The origin ids are placeholders: publishing the seed shows it in *Seeds* split off from a document that is not held. For a real one, use *Split off a seed…* on `push-during-round.md`. |
| `rendering-checks.md` | A manual test of the review view: raw HTML and a `javascript:` link that must not run, a plugin reference with no plugin, glossary linking edge cases (a term across a line break, in code, in a link, interrupted by markup), a table, a task list, Mermaid, repeated and non-ASCII headings for the anchors. On `kalka-core/sync@v1`. |
| `relay-indicator.md` | No frontmatter at all: listed in Personal as a local blueprint until published. |

`push-during-round.md`, `retry-first-release.md` and `join-flow-trace.md`
name the repository `golbinski/kalka-blueprint-sharing` and a `base_sha` in
their frontmatter; every `gh:` path exists there at that commit. With the
GitHub CLI installed and signed in (`gh auth login`), the bundled
`kalka-plugin-gh` shows the referenced lines beside the text, as they were
at `base_sha`, and warns when a file has changed on `main` since. Without
`gh`, the references show as unresolved and everything else works.

## packaging

A second team, to copy or move documents between teams (*Copy to another
team…*, *Move to another team…*).

| File | What it exercises |
|---|---|
| `packaging/packaging-release-domain.md` | A second team's domain document: glossary with aliases ("release candidate", "build output", "setup program") and four invariants verified by tests, a workflow and the release tool, all linked with `gh:`. |
| `packaging/windows-installer.md` | An opted-in plan on `packaging/release@v1`, with `kalka_status: draft` (a label) and the pass-through keys `ticket` and `labels`; a trace with `change new` and `guards`; an invariants block that `affects` two domain invariants; `gh:` links; a hint for no critical sections. |
| `packaging/windows-app-icon.md` | A plan spanning two repositories: kalka by default, and Wails under `repos:` as `wails`, in the text and in a trace (`gh:wails:<path>`, pinned by its own `base_sha`; both repositories are public); the divergences implementation-only and merged; an invariants block that `affects` `release#I4`; a critical section. |

## lending

A small set about a lending library, for the review aids, with no code to
link. Check it against its domain document:

```sh
kalka check --domain samples/blueprints/lending/loans-domain.md samples/blueprints/lending
kalka invariants --domain samples/blueprints/lending/loans-domain.md samples/blueprints/lending/online-renewals.md
```

| File | What it exercises |
|---|---|
| `lending/loans-domain.md` | A domain document with a glossary (terms, forms, aliases, contexts, `see`) and six invariants. |
| `lending/online-renewals.md` | An opted-in plan on `lending/loans@v1`: a trace whose steps declare `guards`, an invariants block that `affects` two domain invariants, and two gaps on purpose: N2 is guarded by no step (`I030`), N3 has no `verified-by` (`I031`). One divergence: B1 is unrealized. |
| `lending/overdue-reminders.md` | A clean opted-in plan: a three-lane trace (business, service, store) of a planned change with `change new` and `removed`, a `back` loop and the divergences merged, expanded and skipped; an invariants block with `affects`, and own invariants with `status changed`, `new` and `removed`; a critical section and operations. |
| `lending/hold-notices.md` | A short plan that uses two aliases, "reservation" and "patron": a `G020` warning for each, with the term to write instead. |

## library

A plan tree on a domain tree: an epic with goals and invariants, a story
that realizes them with acceptance criteria and a trace, and a root domain
with a subdomain that narrows one of its terms. The files carry their
`kalka_id`s, since `kalka_parent` names one. Check, read the context, and see the graph:

```sh
kalka check --domain samples/blueprints/library samples/blueprints/library
kalka context --domain samples/blueprints/library samples/blueprints/library/return-creates-hold.md
kalka graph --domain samples/blueprints/library samples/blueprints/library/return-creates-hold.md
```

| File | What it exercises |
|---|---|
| `library/team-lending-library-domain.md` | The root domain node `library` (version 2): terms with aliases, and invariants every subdomain inherits. |
| `library/team-lending-library.reservations-domain.md` | A subdomain, `library.reservations`, pinning `kalka_parent: team-lending/library@v2`: terms of its own with `see` to inherited ones, and an invariant that narrows the inherited `loan` with `refines`. |
| `library/holds-on-return.md` | An epic (`kalka_issue: jira:DEV-100`, `kalka_breakdown: decomposed`): a goals block with three goals, and an invariants block whose invariants hold in every child. G3 is claimed by nothing on purpose: a `P030` warning. |
| `library/return-creates-hold.md` | A leaf (`jira:DEV-123`) whose `kalka_parent` names the epic: acceptance criteria that realize the epic's goals with given, when and then; `affects parent#E1`; a trace whose business steps `covers` the criteria and whose code steps `guards` inherited and domain invariants. |

The warnings the samples show on purpose are exactly these: `I030` and
`I031` in `online-renewals.md`, two `G020` in `hold-notices.md`, and
`P030` in `holds-on-return.md`. The
hints (`H003`, `H004`, `H005` in `retry-first-release.md`, `H004` in
`windows-installer.md`) are information, never errors.

## Using them

kalka has these samples built in: `kalka samples`, *Add sample
blueprints* in Settings (or in an empty Personal), or the checkbox on the
Windows installer's last page copies them into `personal/samples`,
keeping any file already there.

To copy this folder by hand instead, copy it into Personal. Never point
kalka at this folder itself: it writes to the files it holds.

```sh
cp -R samples/blueprints "<your kalka folder>/personal/samples"
```

```powershell
Copy-Item samples\blueprints "<your kalka folder>\personal\samples" -Recurse
```

Everything appears in *Blueprints · Personal* within a second. Then:

1. Create the team `kalka-core`. Publish the domain document to it first
   (*Publish to team…*, or `kalka publish <file> --team kalka-core`):
   that is version 1. Then publish `push-during-round.md`,
   `retry-first-release.md` and `rendering-checks.md`; their domain
   reference resolves to v1, and their terms and invariants link.
2. Open `push-during-round.md` and `join-flow-trace.md`: with `gh` signed
   in, the referenced code shows beside the text and in the trace. Open
   `rendering-checks.md` and go through it section by section.
3. Have a teammate comment on sections, trace steps and invariants; record
   a deviation on `push-during-round.md` and split a seed from it, then
   look at the *Seeds* view, where `seed-push-latency.md` also appears.
4. Edit the domain document and release v2 (*Release v2…*, or `kalka
   release <file>`): the blueprints show "newer domain version
   available (v2)". Remove an invariant first to see the information about
   the plans that rely on it.
5. Create `packaging`, publish `packaging-release-domain.md` first, then
   `windows-installer.md` and `windows-app-icon.md`, and copy or move
   documents between the two teams.
6. Create `lending`, publish `loans-domain.md` and then the three plans;
   open `online-renewals.md` and `hold-notices.md` to see the warnings and
   the invariants panel.
7. Create `team-lending`, publish the two domain documents of `library/`
   (the root first), then `holds-on-return.md` and `return-creates-hold.md`.
   Open the story: the breadcrumb leads to the epic, the goals block
   shows the criteria with their states, and *More*, *Context for an
   agent* shows what an agent would read. The *Graph* tab draws the epic, the
   story and the domain tree as one graph, with what needs attention.
   Close the story (*Close as delivered*) and watch the epic's goals
   become ready.

To start over, delete what you copied; nothing else holds the samples.
