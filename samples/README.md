# Sample blueprints

Blueprints for trying kalka by hand: a demo, a first look, or a manual
test of the app. They describe kalka itself: real changes and ideas,
written as the plans a team working on kalka would review. None holds a `kalka_id`, owner or review log: kalka
adds those as you publish them and comment on them, under your own
identity.

Most of them link to kalka's own code with `gh:` references, pinned by
`repo: golbinski/kalka-blueprint-sharing` and `base_sha` in their
frontmatter. With the GitHub CLI installed and signed in (`gh auth
login`), the bundled `kalka-plugin-gh` shows the referenced lines beside
the text, as they were at `base_sha`, and warns when a file has changed on
`main` since. `windows-app-icon.md` also references a second repository,
the Wails runtime, named under `repos:` and linked as `gh:wails:<path>`.
Both repositories are public, so any GitHub login can read them.
Without `gh`, the references show as unresolved and everything else works.

`TestSamples` (`internal/cli/samples_test.go`) keeps each one passing
`kalka check` with the hints and warnings listed below.

| File | What it exercises |
|---|---|
| `kalka-core-sync-domain.md` | A domain document (`kalka_kind: domain`, `kalka_domain: sync`): a glossary block, an invariants block whose `verified_by` link to the tests, the team log's operations. Publishing it to `kalka-core` makes version 1, which the two blueprints below refer to (`kalka-core/sync@v1`). |
| `push-during-round.md` | A full blueprint opted into the reference: every section, so no hints; a Mermaid diagram; `gh:` links in the text; a trace block whose code nodes link to their lines, with one divergence to review (C4, implementation-only); a critical section. |
| `retry-first-release.md` | A short opted-in plan with `gh:` links: hints for no verification plan, no critical sections and no operations. |
| `relay-indicator.md` | No frontmatter at all: listed in Personal as a local blueprint until published. |
| `join-flow-trace.md` | Trace blocks over kalka's join flow, every code node linked to its lines: two and three lanes, a labelled branch, and the divergence classes implementation-only, merged and expanded. |
| `packaging/windows-installer.md` | For a second team, `packaging`, to copy or move between teams; `gh:` links to the CI workflow and the release tool; hints for no domain reference and no critical sections. |
| `packaging/windows-app-icon.md` | A blueprint spanning two repositories: kalka by default, and Wails under `repos:` as `wails`, in the text and in a trace; the divergences implementation-only and merged; a hint for no domain reference. |

The `lending` folder is a small set about a lending library, for the review aids. Check it against its domain document:

```sh
kalka check --domain samples/blueprints/lending/loans-domain.md samples/blueprints/lending
kalka invariants --domain samples/blueprints/lending/loans-domain.md samples/blueprints/lending/online-renewals.md
```

| File | What it exercises |
|---|---|
| `lending/loans-domain.md` | A domain document with a glossary (terms, forms, aliases, a context, `see`) and four invariants. It says `kalka_version: 1`, as a released copy kept in a repository does. |
| `lending/online-renewals.md` | An opted-in plan on `lending/loans@v1`: a trace whose steps declare `guards`, an invariants block that `affects` two domain invariants, and two gaps on purpose: N2 is guarded by no step (`I030`), N3 has no `verified_by` (`I031`). |
| `lending/hold-notices.md` | A short plan that uses two aliases, "reservation" and "patron": a `G020` warning for each, with the term to write instead. |

In the app, publish `loans-domain.md` to a team named `lending` to make version 1; the two plans then link their terms against it and show its invariants.

## Using them

Copy the folder into Personal. Never point kalka at this folder itself:
it writes to the files it holds.

```sh
cp -R samples/blueprints "<your kalka folder>/personal/samples"
```

```powershell
Copy-Item samples\blueprints "<your kalka folder>\personal\samples" -Recurse
```

All seven appear in *Blueprints · Personal* within a second. Then:

1. Create the team `kalka-core`. Publish the domain document to it first
   (*Publish to team…*, or `kalka publish <file> --team kalka-core`):
   that is version 1. Then publish `push-during-round.md` and
   `retry-first-release.md`; their domain reference resolves to v1.
2. Open `push-during-round.md` and `join-flow-trace.md`: with `gh` signed
   in, the referenced code shows beside the text and in the trace.
3. Have a teammate comment on sections; record a deviation on
   `push-during-round.md` and split a seed from it.
4. Edit the domain document and release v2 (*Release v2…*, or `kalka
   release <file>`): the two blueprints show "newer domain version
   available (v2)".
5. Create `packaging`, publish `windows-installer.md` and
   `windows-app-icon.md` to it, and copy or move documents between the
   two teams.

To start over, delete what you copied; nothing else holds the samples.
