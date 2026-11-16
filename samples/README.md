# Sample blueprints

Blueprints for trying kalka by hand: the desktop pass in
`docs/desktop-testing.md`, a demo, or a first look. Most describe an
invented library's lending desk; one is a trace example from a document
service. None holds a `kalka_id`, owner or review log: kalka adds those
as you publish them and comment on them, under your own identity.

`TestSamples` (`internal/cli/samples_test.go`) keeps each one passing
`kalka check` with the hints listed below.

| File | What it exercises |
|---|---|
| `lending-desk-circulation-domain.md` | A domain document (`kind: domain`, `domain: circulation`): glossary table, invariants, events. Publishing it to `lending-desk` makes version 1, which the two blueprints below refer to (`lending-desk/circulation@v1`). |
| `hold-shelf-expiry.md` | A full blueprint opted into the reference: every section, so no hints; a Mermaid diagram; a trace block with one divergence to review (C3, implementation-only); a critical section. |
| `renewal-reminders.md` | A short opted-in plan: hints for no verification plan, no critical sections and no operations. |
| `interlibrary-requests.md` | No frontmatter at all: listed in Personal as a local blueprint until published. |
| `import-pipeline-trace.md` | Trace blocks with code references (`gh:` refs, with `repo` and `base_sha` for the GitHub plugin), two and three lanes, a labelled branch, and the divergence classes implementation-only, merged and expanded. The refs point at an invented repository, so without access they show as unverified. |
| `returns-desk/book-drop-sorting.md` | For a second team, `returns-desk`, to copy or move between teams; hints for no domain reference and no critical sections. |

## Using them

Copy the folder into Personal. Never point kalka at this folder itself:
it writes to the files it holds.

```sh
cp -R samples/blueprints "<your kalka folder>/personal/samples"
```

```powershell
Copy-Item samples\blueprints "<your kalka folder>\personal\samples" -Recurse
```

All six appear in *Blueprints · Personal* within a second. Then:

1. Create the team `lending-desk`. Publish the domain document to it
   first (*Publish to team…*, or `kalka publish <file> --team
   lending-desk`): that is version 1. Then publish `hold-shelf-expiry.md`
   and `renewal-reminders.md`; their domain reference resolves to v1.
2. Have a teammate comment on sections; record a deviation on
   `hold-shelf-expiry.md` and split a seed from it.
3. Edit the domain document and release v2 (*Release v2…*, or `kalka
   release <file>`): the two blueprints show "newer domain version
   available (v2)".
4. Create `returns-desk`, publish `book-drop-sorting.md` to it, and copy
   or move documents between the two teams.

To start over, delete what you copied; nothing else holds the samples.
