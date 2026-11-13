# Plan: team domains, seeds and the blueprint reference

Triage and implementation plan for the feature request "extend kalka with team domains, seeds, and a blueprint reference". Written 2026-11-13 against `specification.md` v0.3 and the code on `main` (b8e932f). Nothing here is implemented.

`§n` points into `specification.md`. `IP §n` points into `implementation-plan.md`. Code is cited as `path:line`.

Contents: 1 summary, 2 triage, 3 decisions, 4 milestones, 5 order and size, 6 questions for the product owner.

---

## 1. Summary

The request has three features of very different weight.

| Feature | Weight | Why |
|---|---|---|
| Blueprint reference | Small | One embedded file, one CLI command, a handful of opt-in hints. No protocol or merge change. |
| Seeds | Medium | The format part fits the existing entry grammar. Listing other members' seeds needs the first new wire messages since protocol 1 shipped. |
| Team domains | Large | Needs a signed release, a new merge rule, background syncs and local snapshots. It touches four written decisions of the spec. |

Three findings shape the plan:

1. **Domain docs do not break append-only.** In kalka, append-only is a rule about the entry set, and the owner may already rewrite the body (§7.2). What is new is that a *non-owner* must be able to pass a newer version on. That needs a release signed by the owner.
2. **Domain transfer needs no new wire message.** A sync is a symmetric merge. Once the merge knows "the newer verified release wins", a plain `sync_offer` moves the newer version in either direction. Protocol 1 stays as shipped.
3. **The pre-backlog view of other members' seeds depends on the domain roster.** "Team members" has no definition in kalka until a `members` list exists. So the seed work splits in two, with domains in between.

There is no blocker that stops the work. There are two blockers in the design *as literally requested* (snapshot files in the folder, and unsigned "newer version wins"), both with a fix below. Six points need a product-owner answer (section 6).

---

## 2. Triage

Severity: **blocker** = the request as written cannot work or is unsafe; **decision** = workable, but someone must choose; **note** = worth knowing, no choice needed.

### 2.1 Overview

| # | Item | Severity |
|---|---|---|
| T1 | "Newer version wins" with no signature lets any member replace the domain doc | blocker |
| T2 | Snapshots `<team>-domain@v7.md` in the folder share a `kalka_id` with the live doc; syncing is then refused | blocker |
| T3 | Domain docs and append-only: not an exception, but a new merge rule | decision |
| T4 | Comments across a release: kept, shown outdated; the log grows without bound | decision |
| T5 | "Push to online members" and "when peers connect" are automatic syncs; the spec says manual only | decision |
| T6 | Snapshots are local version history, a stated non-goal | decision |
| T7 | Config `team` and the domain doc's `team` are different things with one name | decision |
| T8 | Listing other members' seeds: new wire messages, a privacy rule, a cache | decision |
| T9 | Core becomes aware of about ten frontmatter keys; "two keys only" ends | decision |
| T10 | kalka creates the seed file: first time it creates a document | decision |
| T11 | "Deviation log" as a body section clashes with deviation entries in the review log | decision |
| T12 | Seed staleness has no source: a blueprint carries no date | decision |
| T13 | Protocol version check is strict equality; new messages must not bump it | note |
| T14 | `budded` entry and `origin` fit the existing grammar | note |
| T15 | Hints map onto `Problems`; dismissal goes to browser storage | note |
| T16 | "Never block on content" versus today's refusal to send a file with format errors | note |
| T17 | Domain docs must be found by frontmatter, not by file name | note |
| T18 | Financial examples: one test fixture to replace | note |
| T19 | The request asks for spec and implementation in one pass | note |

### 2.2 Details

**T1. Unsigned "newer wins" is a takeover (blocker).**
The request says "they compare versions and the newer one is transferred". `version` is a frontmatter integer. If the merge trusted it, any member could write `version: 99` over a body of their choice and every peer would take it. This is the same hole as the forged handoff found earlier (IP §5.1, §17.15), and the answer is the same: sign it.

Today a plan change is accepted only from the owner, or from the new owner after a signed handoff chain (`internal/merge/merge.go:91-107`). A member who relays the owner's v7 to another member is neither, so the relay is rejected with `not-owner` (`merge.go:106-107`). The request needs exactly that relay ("every peer caches the latest version, so an offline owner never blocks reading").

Fix: a signed **release entry** (D1). The handoff signature covers only `kalka_id` and the 12-hex entry id (`internal/blueprint/entry.go:202-204`); a release signature must also cover the full SHA-256 of the plan, because a 48-bit id is too short to bind a body.

**T2. Snapshot files collide with the live document (blocker).**
The store indexes every `*.md` in the folder except dotfiles and `*.conflict-*.md` (`internal/store/store.go:112-122`). A snapshot beside the live doc has the same `kalka_id`. `Store.Find` then returns `ErrDuplicate` (`store.go:288-298`), the UI flags both files (`store.go:227`), sending is refused (`internal/daemon/daemon.go:506-509`) and incoming syncs are declined (`internal/syncproto/syncproto.go:519-522`). This is §14 working as specified.

Also, `Store.SafeName` turns `@` into `-` (`store.go:388-396`), so the name could never arrive from a peer unchanged. That is harmless here, because each peer writes its own snapshots.

Fix (D4): keep snapshots beside the live doc, as requested, and make the store ignore `*@v<N>.md` exactly as it ignores conflict copies.

**T3. Append-only and the merge (decision).**
§7.2 states it directly: "Append-only is a rule about the *set of entries*, not about bytes." The owner may change frontmatter and body at any time, and peers take the change (rule 4, `merge.go:91-93`). So "domain docs are the one exception to append-only" is not needed, and the spec should not say it.

What the merge lacks:

- No notion of version order. Rule 4 takes whatever the owner sends, including an older body.
- No way to take a plan from a non-owner (T1).
- Frontmatter is part of the plan (`internal/blueprint/document.go:499-501`), so `version` and `members` are owner-only already. That is what we want.

`syncproto` assumes nothing more than the merge does. One place needs care: when a reply is `not-owner` with content, the initiator asks "Replace your plan with theirs?" and saves a conflict copy (`syncproto.go:387-420`). For a verified newer release that prompt should not appear.

**T4. Comments across a release (decision).**
Nothing is lost: the entry union never drops a valid entry (`merge.go:156-171`). Each entry records the `rev` it was written against, so after a release every earlier comment shows as *outdated*, and as *orphaned* if its heading is gone (§6.7, `document.go:397-400`). That is correct but noisy on a document meant to live for years.

Two consequences:

- The release entry lists the ids of incorporated comments, so the UI can show them as "incorporated in v7" and fold them away.
- The log can never be pruned. An entry deleted on one machine comes back from any other copy on the next merge. A long-lived domain doc will approach the 2 MiB file limit (§6.9, `document.go:28`). See D3 and question Q4.

**T5. Automatic syncs (decision).**
Every sync today starts with a user action; §18 Q1 records "manual only in v1". Three parts of the request are automatic.

| Requested | What exists | Gap |
|---|---|---|
| "On release, push to online members" | `Daemon.Sync` to one address (`daemon.go:468`) | A fan-out. The owner clicked Release, so this is still user-initiated. |
| "When peers connect, they compare versions" | No connect event. A connection exists per operation (§9.5). Presence is a probe once a minute (`daemon.go:662-680`). | A trigger, and an unattended initiator. |
| No prompt for the transfer | First contact asks both users to compare a code (`syncproto.go:286-327`, run for every incoming connection at `daemon.go:416`). A new document asks to accept (`syncproto.go:550-566`). | An unattended sync with an unverified peer would raise a code prompt with nobody on a call. |

An update to a document the receiver already holds is applied with no prompt today (`syncproto.go:574-597`), so silent application of a verified release is in line with current behaviour. The members list is written by the owner and is part of the signed plan, so it cannot be changed by others; but it says where to *send*, not whom to *trust*. Trust still comes from pinned, verified keys.

Recommendation in D2.

**T6. Snapshots are version history (decision).**
§1 lists "Version history, branching, or any local VCS-like state" as a non-goal, and §2.1 limits local state to config, key and peers. Snapshots are plain files in the user's folder, not a database, so the spirit holds, but both sentences need an amendment. A second effect: a member who jumps from v5 to v8 never held v6 and v7, so a blueprint pinned to `@v6` cannot resolve there. See D4 and Q2.

**T7. Two meanings of `team` (decision).**
The config `team` is a salt mixed into peer IDs (§5, §9.2, `internal/transport/binding.go:21-24`, `daemon.go:164`). It has one value per installation, and two peers with different values cannot reach each other at all. The domain doc's `team` is a name with a roster, and one person may belong to several.

They must stay separate. Deriving the salt from a domain doc would cut a person off from their other teams. No new config key is needed: the teams a user belongs to are read from the domain docs in their folder. The "brainless" rule holds. See D5.

**T8. Seeds from online peers (decision).**
The wire protocol can only sync a file the initiator already has (§9.5). Listing someone else's seeds needs a query. Four points:

- **Compatibility.** A responder skips unknown message types and keeps waiting (`syncproto.go:229-243`). An old peer asked for a seed list would stay silent for 30 s and then time out. The asker must know beforehand whether the peer understands the query (T13).
- **Privacy.** Today a peer learns about a file only when its holder sends it. A list query reveals titles to whoever asks. A rule is needed for who may ask and what is listed.
- **Cost.** Each query is a full WebRTC connection with handshake, far heavier than a probe. IP §5 risk 5 already warns about probe volume. Queries must run on demand, not in a loop.
- **Cache.** "Plus the local cache" needs a home. §2.1 forbids a sidecar database.

Recommendation in D6.

**T9. Core becomes aware of more frontmatter (decision).**
§6.2: "`kalka` reads and writes exactly two keys." §2.4: "no meaning attached to frontmatter beyond its own two keys." The code enforces it (`internal/blueprint/frontmatter.go:84`). The request adds `kind`, `team`, `version`, `members`, `status`, `parent`, `origin`, `domain`, `reference`. These are common names; an existing pipeline may already use `status` or `version` for its own purposes. See D7 and Q5.

**T10. kalka creates a document (decision).**
The existing pattern: kalka writes two frontmatter keys (§17.3 calls it "the one exception") and review-log entries. Entry text comes from the user; the handoff entry is generated.

| New write | Within the pattern? |
|---|---|
| `budded` entry on the parent | Yes. Generated, like a handoff entry. |
| Release entry with changelog | Yes. Structure by kalka, text by the owner. |
| `version:` bump on release | Small extension: a third managed key, on domain docs only. |
| `status:` change on promotion | Small extension, and only if promotion gets a button. |
| Creating the seed file | New. kalka assembles a whole file: frontmatter plus the three texts the user typed. |

The last row is a change of the letter of §1 ("Authoring or editing blueprints" is a non-goal), though not of its intent: kalka still writes no content of its own. The spec should say so plainly (D7).

**T11. Where the deviation log lives (decision).**
The reference lists "Deviation log: append-only record" as a recommended section. kalka already has one: `deviation` entries in the review log (§6.5, `entry.go:18-22`). A body section would be owner-only, so the implementer (often not the owner) could not write to it. And budding "from a deviation-log entry" only works if a deviation is an entry with an id. See D8.

**T12. Staleness needs a date (decision).**
The view must show staleness. A blueprint has no creation date; file modification time is local and changes on every sync (`store.go:172-174`). The only dates in a file are entry timestamps, and a fresh seed has no entries. See D9.

**T13. Protocol versioning (note).**
The handshake fails unless both sides send the same `proto` (`binding.go:87-90`). Bumping it to 2 would cut off every shipped peer for ordinary syncs too. `hello` has no capability field, but JSON decoding ignores unknown fields (`internal/transport/conn.go:133`), so an optional `caps` list can be added under protocol 1. An unknown `intent` is treated as a plain sync by an old responder (`syncproto.go:554`, `576`).

**T14. `budded` and `origin` in the grammar (note).**
Unknown kinds matching `[a-z][a-z0-9-]*` are preserved, merged and shown generically (§6.5, `entry.go:34`). A `budded` kind is therefore valid on old peers today. `re: <id>` already links an entry to another (`entry.go:164-169`), so a `budded` entry can sit under the deviation that caused it with no new syntax. Anchors address headings and trace nodes, not entries; an entry is addressed by its 12-hex id. So `origin:` is an entry id, read relative to `parent:`.

`blueprint.Append` refuses only `handoff` (`internal/blueprint/sections.go:72-74`), so it can write `budded` entries as it is.

**T15. Hints (note).**
`Problem` has severities info, warning and error (`document.go:39-46`). Only errors count: they set the `check` exit code (`cmd/kalka/main.go:159-161`), show in the list (`store.go:187-191`) and stop a send (`daemon.go:498-500`). `check` hides info lines unless `--verbose` (`main.go:167-169`). So hints are info-level problems with a `hint-` code prefix, produced only when the file declares `reference: kalka-blueprint@v1`. No new mechanism.

Dismissal: the UI already keeps one dismissal in `localStorage` (`web/src/App.tsx:27`, `:249`). Hints do the same, keyed by `kalka_id` and hint code. No config, no sidecar, nothing in the file. The cost is that dismissals are per browser.

The "newer domain version available" indicator is not a hint of this kind. It needs the folder, not the file, so the daemon computes it for the document view (`internal/daemon/model.go:26`).

**T16. "Never block on content" (note).**
Today a file with format *errors* cannot be sent (`daemon.go:498-500`), and a received one is `malformed` (`merge.go:145-150`). These are structural: the file cannot be parsed or appended to safely. I read the new constraint as "the new fields never produce an error", and the plan holds to that: a wrong `version`, an unknown team or a dangling `origin` gives at most a hint. If the product owner means that the existing refusals should go too, that is separate work.

**T17. File names (note).**
A received file is stored under the sender's name, sanitized, with a numeric suffix if taken (`store.go:339-358`). A member may end up with `parcels-domain-1.md`. Lookup must go by `kind: domain` and `team`, never by name. The store index has none of the new fields yet (`store.go:48-66`, `:178`).

**T18. Financial examples (note).**
Searched every tracked file (spec, plans, `docs/`, `testdata/`, fuzz corpus, Go and web sources and tests) for payments, invoices, trading, stocks, banking, prices, billing, accounts, currency and similar terms.

| Path:line | Text | Action |
|---|---|---|
| `internal/blueprint/trace_test.go:142` | `text: Take orders` | Replace. With lines 143-147 it forms an order-taking example. |
| `internal/blueprint/trace_test.go:144` | `text: Order API` | Replace. |
| `internal/blueprint/trace_test.go:145` | `text: Fraud check` | Replace. This is the clearest hit. |
| `docs/example-trace.md:48` | `why: storage costs` | Borderline. A reason, not a financial example. Reword to "storage quota" to be safe. |

No hits in `specification.md`, `testdata/golden/`, `web/src/` or the other docs. The existing examples use a document import service. The trace mockups live on an external design canvas ("bp — trace view mockups", IP header) and are not in the repository, so they were not checked.

**T19. One pass or several (note).**
The request says: update the spec, then implement everything. Given T1 to T12, the spec update should be reviewed by the product owner before code starts. It is milestone N0.

**Other conflicts checked.**

- IP Q5 (Review-log tab) stays open and is unaffected.
- §1 "one device per email" is unaffected, but a member with two machines still cannot have both online.
- Members are not added to background presence probing (`daemon.go:682-700`); see D2.
- Two domain docs may claim the same team name under different `kalka_id`s. Both are shown, and a `domain: <team>@v<N>` reference is then marked ambiguous. Nothing is blocked.

---

## 3. Decisions and recommendations

**D1. Releases are signed entries; a new merge rule accepts them from anyone.** (T1, T3)

A release is a review-log entry of kind `release`, authored by the owner, with the existing optional `sig` field (§6.5 already says a `sig` on other kinds is preserved, and the parser accepts it on any kind: `entry.go:99-103`).

```
### release · alice@corp.com · 2026-11-20T09:00:00Z · § · rev:<8 hex> · id:<12 hex> · sig:<base64url>
v7 · plan:<64 hex SHA-256 of the normalized frontmatter and body>
incorporates: 7c2e91ab04f3, a41b77c09e12
Renamed "consignment" to "parcel"; added the invariant on redelivery attempts.
```

The signature covers `"kalka-release-v1\n" + kalka_id + "\n" + version + "\n" + plan hash + "\n" + id`.

New merge rule for documents with `kind: domain`, replacing rule 4: take the incoming plan if its log holds a release entry that (a) is signed by the owner of the local copy, or by someone a verified handoff chain leads to, (b) names a plan hash equal to the incoming plan, and (c) has a higher version than the local copy's latest verified release. The sender does not matter. An owner's unreleased edits do not propagate; the version is the unit of change.

If the receiver has no key pinned for the owner, the release cannot be checked. The merge then answers as it does for handoffs (`merge.go:186-190`): not applied, "sync with alice@ once to verify".

Handing over stewardship is a release whose only change is `owner:`, plus the usual handoff entry.

*Alternative:* trust `version` from any authenticated member. Simpler, and unsafe (T1). *Alternative:* only the owner distributes. Safe with no new rule, but an offline owner blocks updates, which the request rules out.

*Compatibility:* no wire change. An old peer treats a domain doc as an ordinary blueprint: it takes the owner's direct sends (rule 4) and rejects relays with `not-owner`. `release` entries pass through it as an unknown kind.

**D2. Automatic sync for domain docs only, between verified peers only.** (T5)

- *Release push:* the owner's Release action syncs the doc with every member who is online and already verified. Unverified members are listed with "sync once by hand first". No code prompt is ever raised by a background sync.
- *Catch-up:* when the daemon starts, and when a verified member's presence turns online (`daemon.go:630-637`), the daemon syncs the domain docs they share, at most once per member per hour. A plain `sync_offer` is enough: the responder's merge keeps the newer plan and replies with it.
- *Receiving:* a verified newer release of a doc already held is applied without a prompt. The first copy of a team's domain doc still asks "accept new", as any new document does.
- *Probing:* members are probed only if they are already in `peers.yaml`. Rosters do not enlarge the probe set (IP §5 risk 5).

*Alternative:* manual only, with a "check for updates" button. Keeps §18 Q1 intact but fails "an offline owner never blocks reading" in practice. This overrides a recorded default, so it is Q1.

**D3. Comments are kept forever and folded in the UI.** (T4)

No pruning in this version. Entries named in a release's `incorporates:` line are shown as "incorporated in v7" and collapsed. The snapshot holds the log as it was at release. Pruning would need tombstones in the merge; raise it again if a real domain doc gets near the size limit (Q4).

**D4. Snapshots beside the live doc, ignored by the index.** (T2, T6)

`<live file name without .md>@v<N>.md`, written by each peer when it applies or makes release N. The store ignores `*@v<digits>.md` like conflict copies. Beside the live doc, agents and people find them without knowing about kalka.

A late joiner has gaps. A pin to a missing version shows "v6 is not on this machine" and resolves to nothing; it never blocks.

*Alternative:* a hidden `.kalka/` folder (already ignored, `store.go:113`). Tidier, but agents commonly skip dot-folders. *Alternative:* fetch missing versions from peers on demand; a new wire message, deferred.

**D5. Keep the two `team`s apart; add no config.** (T7)

The spec renames the config key in prose to "rendezvous namespace (`team`)" and states that it is unrelated to domain teams. The key itself keeps its name for compatibility. A user's teams are derived from the domain docs in the folder whose `members` include them.

Team names are used in file names and references, so only names matching `[a-z0-9][a-z0-9-]*` are interpreted; others get a hint.

**D6. Seed listing: two messages behind a capability flag, roster-scoped, nothing persisted.** (T8)

- `hello` gains an optional `caps` list. A peer is asked for seeds only if it announces `seeds/1`. Others show as "kalka too old to list seeds".
- `seed_list` returns summaries: `kalka_id`, title, owner, `parent`, `created`, latest entry time, and the first 4 KiB of the body for preview. `seed_fetch` returns one whole file, which the asker stores as an ordinary reviewer copy.
- A responder answers only a verified peer. It lists a seed only if the seed's owner and the asker are both members of one domain roster the responder holds. So members can serve each other's seeds while the owner is offline, and nobody outside a roster sees anything.
- Queries run when the view opens and on Refresh, at most once per member per five minutes.
- "Local cache" means the seeds present in the folder. Summaries from peers live in memory with a "seen at" time and are gone after a restart.

*Alternative:* persist summaries under the config directory. Better offline view, but it is per-document local state, against §2.1. *Alternative:* no listing; seeds are sent by hand like any blueprint. No protocol work, and it does not meet the request. Serving files unattended is new, so it is Q3.

**D7. State the new principle in the spec.** (T9, T10)

Proposed wording for §2.4 and §6.2: kalka *reads* a fixed list of optional keys and attaches the meaning given in the spec only when the value has the expected shape; anything else is pass-through as before. kalka *writes* `kalka_id`, `owner`, `version` (on release), `status` (on promotion from the UI), and the frontmatter of a seed file it creates. It never writes or rewrites a body, except to assemble a new seed from text the user typed.

Key names stay as requested (Q5 offers the alternative).

**D8. The deviation log is the set of `deviation` entries.** (T11)

The reference says so, and tells agents to record deviations with `kalka comment --kind deviation`. There is no hint for a missing deviation log, since a plan not yet implemented has none. The UI gets a Deviations filter on the review log.

**D9. kalka writes `created:` when it buds a seed.** (T12)

Staleness is the time since the later of `created` and the newest entry. A hand-made seed without `created` shows "age unknown". The view also shows, per remote seed, when its summary was last seen.

**D10. Budding details.** (T14)

- Anyone holding the parent may bud; the seed's owner is whoever buds. The backlink reaches the parent's owner on the next sync, like any entry.
- The seed gets its `kalka_id` at creation, since the backlink needs it. A parent without a `kalka_id` gets one first, as on a first send (`daemon.go:572-585`).
- The seed file is written first, then the parent entry. If the second write fails, the UI offers "add the backlink" and does not create a second seed.
- The parent entry: kind `budded`, the anchor of the deviation it came from, `re: <deviation id>` when there is one, then the line `budded: <seed kalka_id>` and the seed's title.

---

## 4. Milestones

Numbered N0 to N4 so they do not collide with M0 to M7.

### N0. Spec update and review

1. Add §6.11 Domain documents, §6.12 Seeds, §6.13 Blueprint reference and hints.
2. Amend: §1 non-goals (version history; authoring), §2.1 and §2.4 (D7), §3 glossary (domain, steward, release, snapshot, seed, budding, hint), §5 (D5), §6.2 (key list), §6.5 (`release`, `budded`, `sig` on releases), §7.2 (the domain rule), §7.6 (releases are signed), §9.3 (D2 probing), §9.5 (`caps`, `seed_list`, `seed_fetch`), §11.1 (views), §12 (commands), §14 (snapshot names ignored), §16 (milestones, scenarios 11 to 16), §17 (findings), §18 (open questions from section 6).
3. Replace the examples named in T18. Use parcel logistics and library lending in all new examples.
4. Bump the spec to v0.4.

Exit: the product owner has answered Q1 to Q6 or accepted the defaults; the spec is merged.

Size: 3 days. Files: `specification.md`, `implementation-plan.md`, `internal/blueprint/trace_test.go`, `docs/example-trace.md`.

### N1. Blueprint reference and hints

1. Write `BLUEPRINT-REFERENCE.md`: frontmatter with `name` and `description` so it works as a skill file unchanged; its version `kalka-blueprint@v1`; the six sections, each with a rationale and a short parcel-logistics example; how to record deviations (D8) and when to split off a seed.
2. New package `internal/reference`: embeds the file (same technique as `internal/ui/ui.go:11`), exposes `Version` and `Bytes()`.
3. CLI: `kalka reference export [-o <file>] [--force]`, to stdout by default, refusing to overwrite without `--force`. `kalka reference version`. `kalka version --json` reports the reference version.
4. Hints in `internal/blueprint` (new `hints.go`): only when `reference:` equals `kalka-blueprint@v1`. Codes `hint-no-domain-reference`, `hint-no-scope`, `hint-no-verification-plan`, `hint-no-critical-sections`, `hint-no-operations`. A section counts as present when a heading at any level has the matching slug. Severity info. An unknown reference version gives one info line and no hints.
5. UI: a hint strip in the review view, each hint dismissible, state in `localStorage` under `kalka.hint-dismissed.<kalka_id>.<code>`; a "show dismissed hints" link. Hints never appear in the list's Problems column.

| | |
|---|---|
| Spec | §6.13, §11.1, §12 |
| Packages | new `internal/reference`; `internal/blueprint`; `cmd/kalka/main.go`; `web/src/Review.tsx`, `web/src/api.ts` |
| Wire and format | None on the wire. One optional frontmatter key read. Old peers pass it through. |
| Tests | **Reference export (required):** exported bytes equal the embedded file; `-o` writes it; no overwrite without `--force`; the file parses with its own library and declares its version. Hints: none without `reference:`; each code alone; unknown version; hints never set `check` exit code 1 and never stop a send; golden `check.json` for a file with all sections and one with none. A test that the reference contains no term from the T18 list. |
| Size | 4 days |

### N2. Seeds: format, budding, local pre-backlog

1. Library: read `status`, `parent`, `origin`, `created`. `Document.IsSeed()`. `budded` as a known kind with `BuddedSeed()` reading the `budded:` line. `blueprint.NewSeed(title, problem, why, owner, parent, origin, now)` builds the file: frontmatter, H1 title, `## Problem`, `## Why it was split off`.
2. Store: index `Status`, `Parent`, `Created` (`store.go:48`). `Store.Create` already picks a free name.
3. Daemon: `Bud(parentPath, entryID, title, problem, why)` per D10, under the parent's lock. `Promote(path, status)` for the owner: a one-line frontmatter edit. `Seeds()` lists local seeds with owner, parent title when held, and staleness (D9).
4. API: `POST /api/seeds` (bud), `GET /api/seeds`, `POST /api/promote`.
5. CLI: `kalka seed <parent> [--from <entry id>] --title <t> [-m <text>]`, `kalka seeds [--json]`.
6. UI: "Create seed" on every deviation entry and in the document menu, opening a three-field dialog; a `budded` entry rendered as a link to the seed; a seed badge and parent link in the review header; a Pre-backlog view (title, owner, parent, age) and its nav item; "Promote to draft" for the owner. Handoff of a seed uses the existing dialog unchanged.
7. Hints for seeds, shown only under an opted-in reference: none planned. A dangling `parent` or `origin` shows as plain text.

| | |
|---|---|
| Spec | §6.12, §6.5, §11.1, §12 |
| Packages | `internal/blueprint` (`seed.go`, `entry.go`), `internal/store`, `internal/daemon` (`model.go`, `api.go`, new `seeds.go`), `cmd/kalka`, `web/src` (`Views.tsx`, `Review.tsx`, `Dialogs.tsx`, `App.tsx`, `api.ts`) |
| Wire and format | None on the wire. `budded` is a valid unknown kind for old peers (T14). A seed is an ordinary blueprint to them. |
| Tests | **Budding (required):** the seed has `status: seed`, the budder as `owner`, `parent`, `origin`, `created` and a fresh `kalka_id`; the parent gains exactly one `budded` entry naming the seed, replying to the deviation; the parent's frontmatter and body bytes are unchanged; the backlink survives a sync to the parent's owner and back (scenario 11); budding from a reviewer's copy; a parent with no `kalka_id`; retry after a failed second write makes no second seed. Promotion: owner only; a non-owner's `status` edit is rejected at sync as today. Handoff of a seed, then promotion by the new owner (scenario 12). Golden file: a seed round trip. Fuzz seeds with `budded` entries. |
| Size | 1.5 weeks |

### N3. Team domains

**N3a. Format, release, snapshots (local only)**

1. Library: read `kind`, `team`, `version`, `members`, `domain`. `release` kind with `ReleaseInfo()` (version, plan hash, incorporated ids). `NewRelease` signs per D1. `Document.PlanHash()`. `LatestRelease(keys)` returns the highest verified release matching the current plan.
2. Merge: the domain rule of D1 in `merge.Docs`, with property tests alongside the existing ones.
3. Store: ignore `*@v<N>.md`; index `Kind`, `Team`, `Version`; `SaveSnapshot(f, n)`; `FindDomain(team)`; `Snapshot(team, n)`.
4. Daemon: `Release(path, text, incorporates)` for the owner: bump `version`, append the signed entry, write the snapshot. A draft state when the body differs from the latest release.
5. CLI: `kalka release <file> [-m <text>] [--incorporates <id>,...]`, `kalka domains [--json]`.
6. Document view: for a blueprint with `domain: <team>@v<N>`, the fields `domain_pinned`, `domain_latest`, `domain_missing`.

**N3b. Distribution**

1. Release push to verified online members (D2), with a per-member result list.
2. Catch-up trigger on daemon start and on a verified member coming online, rate-limited, never prompting for a code.
3. `syncproto`: no convergence prompt and no conflict copy when the reply holds a verified newer release (`syncproto.go:387-420`); a snapshot is written on every applied release, on both sides.
4. A responder serves a domain doc only to a peer listed in its `members`, or to its owner.

**N3c. UI**

1. Domains view: team, version, steward, members with presence and verification state, "v8 seen at bob@ but not verified" notices.
2. Domain document view: draft banner for the owner, Release dialog with a checklist of open comments to mark as incorporated, comments folded as "incorporated in v7", version switcher over local snapshots (read-only).
3. In a blueprint: a "newer domain version available (v9, pinned v7)" chip, dismissible like a hint, linking to both versions.

| | |
|---|---|
| Spec | §6.11, §6.5, §7.2, §7.6, §9.3, §14, §11.1, §12, §1, §2 |
| Packages | `internal/blueprint` (`domain.go`, `entry.go`), `internal/merge`, `internal/store`, `internal/syncproto`, `internal/daemon` (new `domains.go`, `daemon.go`, `model.go`, `api.go`), `internal/name` (signature context), `cmd/kalka`, `web/src` |
| Wire and format | No wire change; protocol stays 1. New entry kind and a `sig` on it, both legal for old peers. Mixed versions: an old peer takes releases sent by the owner and rejects relays; a new peer never takes an unreleased owner edit of a domain doc, even from an old owner client. |
| Tests | **Domain version sync (required), as scenarios over real WebRTC on loopback like `internal/daemon/scenario_test.go`:** (13) Alice releases v2, Bob online gets it with no prompt, both hold a `@v2` snapshot; (14) relay: Alice releases v3 while Carol is offline, Alice goes offline, Carol comes online and gets v3 from Bob; (15) forged release: Bob raises `version` and edits the body, Carol's copy is unchanged and says why; (16) older meets newer in both directions, the newer wins and comments from both sides are kept. Also: unknown owner key gives "sync with the owner"; a release push skips unverified members without a code prompt; steward handoff; comments before a release are outdated and marked incorporated; snapshot names never show as duplicates; a pinned version missing locally; two docs for one team. Merge properties: idempotent, commutative on entries, version never goes down. A test with an old-rule merge on one side for the compatibility claims. |
| Size | 3.5 weeks (a: 1.5, b: 1, c: 1) |

### N4. Pre-backlog across the team

1. `hello` gains `caps` (`binding.go:42-48`); the daemon remembers each peer's caps per session.
2. `syncproto`: `Respond` dispatches on the first message, `sync_offer` as today or `seed_list` / `seed_fetch` (`syncproto.go:472`); the initiator side `ListSeeds` and `FetchSeed`.
3. The roster rule of D6 on the responder, using the domain index from N3.
4. Daemon: on-demand fan-out to online, verified co-members; in-memory summary table with "seen at"; rate limit.
5. UI: the Pre-backlog view gains remote rows, a per-member status line (online, offline, too old, not verified), Refresh, and "Keep a copy".

| | |
|---|---|
| Spec | §9.5, §6.12, §11.1 |
| Packages | `internal/transport`, `internal/syncproto`, `internal/daemon` (`seeds.go`), `web/src` |
| Wire and format | Additive under protocol 1: an optional `caps` field in `hello`, and two message pairs sent only to peers that announce `seeds/1`. Old peers are never asked. |
| Tests | Listing between two co-members; a non-member and an unverified peer get nothing; a seed whose owner is offline is served by another co-member; an old peer (no `caps`) is skipped without a 30 s wait; fetch stores a reviewer copy that then syncs normally; the rate limit; summaries are not written to disk. |
| Size | 1.5 weeks |

---

## 5. Order, size and M6

| Order | Milestone | Depends on | Size |
|---|---|---|---|
| 1 | N0 Spec update and review | — | 3 days |
| 2 | N1 Reference and hints | N0 | 4 days |
| 3 | N2 Seeds, local | N0 | 1.5 weeks |
| 4 | M6 Canvas review | M3 (done) | 1.5 weeks |
| 5 | N3 Team domains | N0, answers to Q1, Q2, Q4 | 3.5 weeks |
| 6 | N4 Pre-backlog across the team | N2, N3, answer to Q3 | 1.5 weeks |

About 9.5 weeks for one engineer, M6 included.

Why this order:

- **Spec first, as its own milestone.** Four written decisions change (T5, T6, T9, T10). Reviewing them on paper costs days; discovering a disagreement after N3 costs weeks.
- **Reference second.** It is the smallest piece, touches no protocol, and its text defines terms (scope, seed, deviation log) that the seed and domain UI then use.
- **Seeds split in two.** Budding and the local view need nothing from domains and deliver the main benefit, keeping blueprints from growing. The cross-team view needs the roster, so it comes after domains.
- **Domains late.** It is the largest and riskiest piece and the one waiting on the most answers.

**Where M6 fits.** Canvas is UI only, depends on nothing here, and still has no acceptance criteria (IP §5.8). Placing it after N2 uses the time while the domain questions are being answered, and closes the original v1 scope before the protocol work starts. It can move to the end with no effect on the other milestones if domains are the priority; it should not go first, because N1 and N2 are smaller and were asked for explicitly.

---

## 6. Questions for the product owner

Each has a default; work proceeds on the default unless answered.

**Answered 2026-11-13: all six defaults accepted.** Implementation is on hold until a further request has been triaged.

1. **Automatic sync for domain docs.** May kalka sync domain docs in the background between members who have verified each other, with no click and no prompt, when a signed release is newer? This overrides §18 Q1 for this one document kind. *Default: yes (D2).*
2. **Local history.** Are per-version snapshot files beside the live doc acceptable, given that "version history" is a non-goal today? And is it acceptable that a late joiner lacks old versions? *Default: yes to both (D4).*
3. **Seed visibility.** May a co-member's kalka list and fetch your seeds while you are away from the keyboard, limited to people on a shared roster? *Default: yes (D6).* If no, the cross-team pre-backlog view is dropped and N4 with it.
4. **Comments on domain docs.** Keep every comment forever and fold the incorporated ones, or design pruning now? *Default: keep and fold (D3).*
5. **Key names.** Keep the bare names from the request (`kind`, `status`, `version`, `team`, …), which may collide with keys a team's pipeline already uses, or prefix them (`kalka_kind`, …) like `kalka_id`? *Default: bare names, interpreted only when the value has the expected shape (D7).*
6. **Deviation log.** Is it right that the "deviation log" of the reference is the set of `deviation` entries in the review log, not a body section? *Default: yes (D8).*
