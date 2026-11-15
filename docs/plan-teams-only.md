# Plan: kalka around teams only

Triage and implementation plan for the request "simplify kalka around teams". Written 2026-11-15 against `specification.md` v0.5 and the code on `main` (9dd0ec9). Nothing here is implemented. `specification.md` and the code are untouched.

`§n` points into `specification.md`. `IP §n` points into `implementation-plan.md`. Code is cited as `path:line`.

No compatibility is kept anywhere (IP §6q, and the request): formats, the team log, the folder layout and local state change freely, and removed code is deleted. The one exception is the PeerJS signaling protocol, which must keep working against the public cloud and stock `peerjs-server`.

Examples use a parcel depot and a library. Names: the teams `parcel-ops` and `library-desk`; the people Ewa, Marek, Olga and Jan.

Contents: 1 summary, 2 triage, 3 decisions, 4 CLI and UI parity audit, 5 switcher, waiting counts and tray, 6 spec change outline, 7 milestones, 8 order and size, 9 risks, 10 questions for the product owner.

---

## 1. Summary

The request removes more than it adds. One-to-one sync, key pinning, the verification code, the accept prompt, the convergence dialog, the email-derived peer ID and the domain-owner role all go. What is added: a strict folder layout with Personal, reassignment by a team owner at any time, copy and move between teams with acceptance, join links, a team switcher, and CLI and UI parity as a tested rule.

| Request point | Weight | State today |
|---|---|---|
| 1 Remove one-to-one | Medium, mostly deletion | About 2 500 lines of code and tests exist only for it |
| 2 Several teams, Personal | Medium | Several teams already work in the core (`internal/core/teams.go:150`). Personal and "location decides" do not exist |
| 3 Roles | Small to medium | Owner and member exist. `assign-domain` and the domain rule are removed |
| 4 Ownership | Medium, risky | `reassign-document` exists but needs a removal behind it. Handoff needs both peers online and a prompt |
| 5 Copy and move | Large, risky | Nothing exists |
| 6 Join links | Medium | Tokens exist. No URL scheme anywhere |
| 7 App first | Large | Views exist for one team at a time. No switcher, no per-team counts. Five workflows are UI only, two are CLI only |
| 8 Rendezvous binary | Small | `kalka rendezvous` is a command of the main binary |

Six findings shape the plan.

1. **"A team owner can do anything a document owner can" cannot mean two people may sign the same version.** The release chain is free of forks only because exactly one person may sign version n+1 (§20.4, `internal/merge/team.go:112`). A team owner therefore acts on a document by taking it over first, with `reassign-document`, in the same click (D4).
2. **The race rule needs no new causal graph.** A reassignment already names the last version its signer had seen (`after`). "Causally before the reassignment" is "version at most `after`". What must change: a reassignment becomes possible without a removal, it becomes an anchor that restarts the chain, and several reassignments of one document must all count (today only the last is kept, `internal/teamlog/replay.go:924`, which breaks the chain at the first one) (D3).
3. **"Ownership disputed" shrinks to two cases** that no reassignment causes: one key signing two different releases of one version, and two members publishing the same `kalka_id` (D3).
4. **Countersigned history is attested, not verified.** The destination team cannot check source signatures, and it cannot check that the actor was allowed to take the document out. The scheme binds two named people to an exact set of entries; the badge must say so (D6).
5. **A move must use a new `kalka_id`.** A tombstone is final for its id (§20.6). With the same id, a document could never move back to a team it once left (D6).
6. **Without one-to-one, the email-derived peer ID has no user.** Nobody is reached by a typed address any more. One registration per daemon, on the key ID, is enough (D8).

There is no blocker. Nine points need a product-owner answer (section 10); each has a default.

About 43 days of work for one engineer, in nine milestones T0 to T8, then one Windows pass by the product owner (T9), which should absorb the open D8 pass.

---

## 2. Triage

Severity: **blocker** = cannot work as written; **decision** = workable, someone must choose; **note** = worth knowing.

### 2.1 Overview

| # | Item | Severity |
|---|---|---|
| X1 | One-to-one sync: what goes, where it lives | note |
| X2 | The convergence flow is not needed for replication; one small part of it is | decision |
| X3 | Prompts and popups have nothing left to ask | note |
| X4 | Trust without pinning; what a peer is; presence | decision |
| X5 | The email-derived peer ID and the config `team` salt | decision |
| X6 | Several teams already work; replication selects by team *name*, not by team | note |
| X7 | Personal, the folder layout, and `team:` against location | decision |
| X8 | Publish moves the file | decision |
| X9 | Removing the domain-owner role | note |
| X10 | Tiebreak for owners added concurrently already exists | note |
| X11 | Immediate handoff: a release, with no connection | decision |
| X12 | Reassignment without a removal; several reassignments per document | decision |
| X13 | "Anything a document owner can" against one signer per version | decision |
| X14 | The race rule and what is left of "ownership disputed" | decision |
| X15 | A reassignment can displace the owner's unreleased edits | decision |
| X16 | Transfers: how a pending item reaches destination owners | decision |
| X17 | Transfers: the attestation scheme | decision |
| X18 | Transfers: same id or new id; the source tombstone; late entries | decision |
| X19 | Transfers: the source-side role check is not a security boundary | note |
| X20 | Join links: token contents, parsing, registration, hand-over to the running app | decision |
| X21 | Invites name one email, so a link posted in a channel works for one person | decision |
| X22 | Team switcher, scoped views and API | note |
| X23 | "Waiting for you" needs a definition | decision |
| X24 | CLI and UI gaps in both directions; error kinds are mostly empty today | note |
| X25 | Rendezvous as its own binary | decision |
| X26 | One rendezvous per installation limits which teams a person can join | note |
| X27 | The open D8 Windows pass would test views this work deletes | note |

### 2.2 Removing one-to-one (request 1)

**X1. What goes.**

| Concept | Spec | Code |
|---|---|---|
| `send`, "Sync with…" | §7.1, §12 | `core.Node.Sync` (`internal/core/daemon.go:661-781`), `SyncRequest`, `ErrOffline`; `POST /api/sync` (`internal/daemon/api.go:63`); `internal/cli/net.go` (send, handoff, the terminal prompts); `SyncDialog` (`web/src/Dialogs.tsx:77`) |
| Sync session | §7.1, §9.5 | `syncproto.Session`: `Initiate`, `Respond`, `respondOne`, `apply`, `Confirm` (`internal/syncproto/syncproto.go:354-685`). The message types that replication uses stay |
| One-to-one merge | §7.2 rules 2, 4, 5 | `merge.Docs` and the handoff chain in `internal/merge/merge.go`; the union and assembly helpers stay for `merge.DocsTeam` |
| Verification code | §8.3 | `identity.Code` (`internal/identity/identity.go:86`), `syncproto.Verify`, `ErrNotVerified`, `GET/POST /api/verify/{email}`, `kalka verify`, the buttons in `web/src/Views.tsx:181-209` |
| Pinning, "different key blocks all syncs" | §8.2 | `identity.Peers`, `Pin`, `PinVerified`, `SetVerified`, `Forget`, `KeyChangedError`, `ErrPinClash` (`identity.go:100-296`); `peers.yaml`; `pinFromTeam` (`internal/core/join.go:380`); `refuse` (`daemon.go:630`) |
| Peers view | §11.1 | `Peers` (`web/src/Views.tsx:220`), the nav link (`web/src/App.tsx:221`), `GET/POST/DELETE /api/peers`, `kalka peers`, `PeerList`, `AddAddress`, the probe loop over `peers.yaml` (`daemon.go:894-940`) |
| Accept prompt | §7.2 rule 2 | `syncproto.Prompt`, `core.Hub` (`internal/core/prompts.go`), `GET/POST /api/prompts`, `PromptCard` (`Dialogs.tsx:13`), `internal/daemon/popups.go` |
| Handoff as a prompted sync | §7.4 | `candidate` and `pending` (`daemon.go:803-857`), `IntentHandoff` |
| `handoff` entries moving ownership | §6.5, §7.2 | `KindHandoff`, `NewHandoff`, `handoffStates` (`internal/core/model.go:263`), "unverified handoff" |
| Conflict files | §7.5 | `Store.SaveConflict` (`internal/store/store.go:374`), the `.conflict-` ignore rule (`store.go:123`), `syncproto.Diverged` |

Tests that go or are rewritten: `internal/syncproto/syncproto_test.go`, the one-to-one half of `internal/merge/merge_test.go` and its golden files, `internal/identity/identity_test.go` (pins), `internal/daemon/popups_test.go`, scenarios 1 to 10 in `internal/core/scenario_test.go`. Effort: 4 days, most of it re-seating tests that used a one-to-one sync to set a scene. Risk: low.

**X2. Is the convergence flow still needed? (decision)**
No. Replication never asks: the release chain decides which plan a peer holds, and the sender does not matter (§20.4). `not-owner`, `diverged`, the "replace with the owner's plan" question and `.conflict-…md` are deleted.

One part of its purpose survives, in another place. Replication today replaces a local plan that is not a released version, and keeps nothing (`internal/core/replicate.go:598-670` calls `Store.Update` and no more). That was tolerable while an owner could lose unreleased edits only by being removed. With reassignment at any time (X12) it is ordinary. See D5: the displaced file is kept in the team's trash and a notice says where.

**X3. Prompts and popups (note).**
All three prompt kinds (accept a blueprint, compare a code, accept a handoff) belong to one-to-one. Nothing in the new model holds a connection open waiting for a person. The hub, the prompt API, the popup driver and the terminal prompts of `kalka daemon` are deleted. What needs a person becomes a *waiting item*, computed from files and logs (D10). Notifications stay.

**X4. Trust without pinning (decision).**
Today trust has two sources, pins and team logs, and §8.2 has a rule for when they disagree. With pins gone there is one source. See D7.

**X5. Email-derived IDs (decision).**
The email ID exists so that a first send to a typed address works (§17.18, §18.2 decision 1). No such send remains. Members are dialed by the key in a team log; a joiner dials the key in the token. See D8.

### 2.3 Teams and Personal (request 2)

**X6. Several teams already work; one isolation gap (note).**
`teamDirs` finds every team log under the folder (`teams.go:150`), each team replicates on its own (`replicate.go:843`), and `--team` picks one (`teams.go:194`). What is missing is UI.

One gap: the replica set of a team is "every document in the whole folder whose `team:` equals the team's *name*" (`replicate.go:220`). A name is a label, and two teams may share one (§19.2). A member of both would offer a document of one to the other. "Location decides" (D9) closes this: the replica set is read from the team's own folder only.

**X7. Personal and the layout (decision).**
Today a published document "stays where it is" anywhere in the folder (§14), and a private document is anything without a valid release. The request wants location and `team:` to agree. See D9.

**X8. Publish moves the file (decision).**
"A member moves a document from Personal into a team." The file path changes at publish. Agents that hold the old path must read the new one from the command's JSON. See D9.

### 2.4 Roles (request 3)

**X9. Removing the domain owner (note).**
Goes: the `assign-domain` operation (`internal/teamlog/op.go:36`, `replay.go:760`, `build.go:46`), `State.Domains`, `merge.DomainRule` (`internal/merge/team.go:26`), `logTeam.DomainOwner` and `isDomain` (`replicate.go:49-91`), `Node.AssignDomain` and `domainAfter` (`teams.go:524`, `:844`), the domain branch of `afterLogChange` (`publish.go:262-281`), `POST /api/teams/{team}/domains`, `kalka team assign-domain`, the form in `web/src/Team.tsx:358`, §19.6, and the sentences of §6.12 that forbid handing a domain document off.

What stays of a domain document: `kind: domain`, `domain:`, `version:`, snapshots, the incorporated-comments list, the newer-version indicator. Its owner releases it like any owner.

One thing the assignment did quietly: it made one document per domain name official. Without it, two members can each publish a document claiming `domain: parcels`. See D4, last point.

**X10. Owners added concurrently (note).**
The tiebreak exists and the harness guards it (`docs/teamlog.md` rule 5, finding 2): seniority is a chain; an appointee ranks directly below their appointer; two appointees of one appointer are ordered by the causal depth of their grant, then by its hash; a person granted by two owners concurrently takes the better chain. The plan keeps it and writes it into the spec as the stated tiebreak (D1).

### 2.5 Ownership (request 4)

**X11. Immediate handoff (decision).**
Today a handoff is a sync that needs the recipient online and their click (§7.4). For a published document it already ends in a release that names the new owner (`daemon.go:823-845`). The new handoff is only that release, written locally and replicated like any other. See D2.

**X12. Reassignment at any time (decision).**
Today replay accepts `reassign-document` only with an effective removal of `from` in its view (`replay.go:767-778`, `docs/teamlog.md` rule 9), and the state keeps one reassignment per document (`replay.go:924`). Two changes are needed, and the second is a defect today as well: a document reassigned twice in its life loses the first cut-off, and the releases signed under it turn void. See D3.

**X13. One signer per version (decision).**
See finding 1 and D4.

**X14. The race and "ownership disputed" (decision).**
See D3.

**X15. Displaced edits (decision).**
See X2 and D5.

### 2.6 Moving and copying (request 5)

**X16. Reaching the destination owners (decision).**
The actor may go offline right after the click. The destination owners may not be in the source team. Replication never crosses teams. So the pending item has to live in the destination team's own replica and travel by its replication. See D6.

**X17. The attestation scheme (decision).**
Entry signatures cover the `kalka_id` and are checked against keys the team log bound (§6.5, §7.6). In the destination both fail by design. See D6.

**X18. Identity, tombstone, late entries (decision).**
See D6.

**X19. The source-side role check (note).**
"Available to the document owner or a team owner of the source team" can be enforced only on the actor's own machine. The destination holds no source log. And any member of the source team already holds the text and could copy it by hand. The check is a product rule, not a protection; the threat model says so. For a move the real control is in the source: only the document's owner can sign its tombstone.

### 2.7 Joining (request 6)

**X20. Join links (decision).**
The token is one base32 line (`internal/teamlog/invite.go:44`). It holds the team name but not the inviter's address, only their key. The join screen is asked to show the inviter, so the record gains a field. Registration differs per system, and a click must reach an app that is already running. See D11.

**X21. One link, one person (decision).**
An invite names the invitee's email and is single use (§18.2 decision 5, §19.7). "Clicking a link (e.g. in Slack)" will work in a direct message. A link posted in a channel works for the one named person and fails for everyone else with "this invite is for olga@…". The request says invites stay as they are, so the plan keeps this (Q5).

### 2.8 App and CLI (request 7)

**X22. Switcher and scope (note).**
Routes, list endpoints and views take a team. See section 5.

**X23. Waiting items (decision).**
`Summary.Waiting()` is prompts plus pending joins (`internal/core/summary.go:34`). Prompts are gone, and a pending join waits for the inviter, not for the user. See D10.

**X24. Parity (note).**
Section 4. Also: `errKind` knows seven kinds, five of which this work deletes (`internal/daemon/api.go:131-156`). Every other failure has an empty `kind`. "Stable error kinds" therefore means introducing them (D12).

### 2.9 Rendezvous and the rest

**X25. Rendezvous binary (decision).** See D13.

**X26. One rendezvous per installation (note).**
§18.1 question 7 accepted that a person cannot serve teams on two servers. With "any number of teams" this limit will be met sooner. The plan keeps it: the join screen says when a token names another server, and refuses. Lifting it means one signaling connection per server and is not in this work.

**X27. D8 (note).**
The D8 script (`docs/desktop-testing.md`) tests prompts, the Peers view and a first send. All three are deleted here. The plan rewrites the script in T8 and proposes one Windows pass at the end (Q9).

---

## 3. Decisions

### D1. Roles and the owner tiebreak

- Two roles. A team owner changes the roster and reassigns documents. A member publishes, owns, comments, buds seeds, hands off what they own, and starts transfers.
- Replay rules 1 to 7 of `docs/teamlog.md` stay as they are: authority in view, removal wins, seniority between removals, support, the seniority chain, never without an owner, no self-removal.
- **Tiebreak for owners added concurrently:** the existing chain order. An appointee ranks directly below the appointer. Appointees of one appointer are ordered by causal depth of the grant, then by its hash. Someone granted twice takes the better chain. The hash orders only siblings under one appointer, who could have appointed them in either order anyway, so searching for a hash gains nothing.
- The log's rule version becomes 2. Rule 9 changes, rule 10 goes, and two operations are added (D6). Logs of rule version 1 need no handling: there are none.

### D2. Handoff is a release

- `kalka handoff <file> <email>`, or *Hand off…*: the owner's kalka writes `owner: <recipient>` and makes release n+1 naming the recipient. It is a daemon command that needs no network; it works paused and offline, and replicates when it can.
- The recipient must be a current member in the local replay. A document in Personal cannot be handed off.
- No accept step. The recipient gets a notice and a waiting item (D10), and hands it back the same way.
- The `handoff` entry kind loses its behaviour and is no longer written. The release that changes `owner:` is the record; the UI renders it as "handed off to marek@…". Old `handoff` entries need no handling: there are none. This removes the second ownership mechanism (the handoff chain of §7.2) entirely.
- Reason: a release naming the next signer is already what moves ownership of a published document (§20.4). The prompt and the write-after-acknowledgement only served the one-to-one case.

### D3. Reassignment, the race rule, and what is left of "disputed"

**The operation.** `reassign-document {doc, from, owner, after, plan}`, signed by a team owner.

- `owner` must be a member in the signer's view. No removal is needed any more.
- `after` is the highest valid release of the document the signer holds. `plan` is that release's plan hash (empty when `after` is 0).
- `from` is the owner the signer saw. It is information for the history and for notices; replay does not judge it.
- An operation whose `after` is lower than that of an effective reassignment of the same document in its view is discarded ("stale cut-off").

**Anchors.** Every effective reassignment counts, not only the last. Each is an *anchor*: its `owner` is the only valid signer of version `after`+1, and the chain restarts there. A gap or a dispute below an anchor does not void what stands above it. Of two effective reassignments with the same `after`, the later in replay order counts (causal depth, then hash). Any team owner can always sign a later one, so the hash gives nobody anything they did not have.

**How causality is established.** Document releases are not in the team log's graph. The link is made from the log's side: the reassignment cites the document state its signer saw. A release is *causally before* a reassignment exactly when its version is at most `after`. Everything the former owner signs above `after`, whenever they signed it and whatever timestamp it carries, is void. No release needs to cite log heads; that was considered and adds nothing, because the only question the rule asks is whether the reassignment had seen the release, never the reverse.

**The race, step by step.** Ewa owns the depot routing plan at version 5. Marek, a team owner, reassigns it to Olga with `after: 5`. Ewa, not yet knowing, releases version 6.

- A peer that sees only Ewa's release takes version 6.
- When Marek's operation arrives, Ewa's version 6 is void on that peer. Olga's kalka releases version 6 in her name when it holds the operation and version 5.
- Every peer holding the same operations and entries computes the same chain. The outcome is Olga's version 6 everywhere.
- Olga's kalka releases the plan it holds. If her copy had already taken Ewa's void version 6, that text becomes the starting point of Olga's version 6. That is harmless: she is the owner now and may publish any text. Peers keep no history, so no peer can be asked to go back to version 5's text.
- Until Olga's release arrives, peers show "reassigned to olga@…, pending" as today (§19.5). This is not a dispute.

**Notices.** A peer that replays a new effective reassignment naming the local user as `from` or `owner` raises one notice.

**Removing a member** still emits one reassignment per document the remover holds, to the remover (`teams.go:447-478`). They no longer depend on the removal. If the remover loses a mutual removal, their term ends and rule 2 discards the reassignments with it. If the removal is discarded for another reason (rule 6), the reassignments stand; a team owner could have made them anyway.

**What "ownership disputed" still covers.** Only two valid releases of one version with different content, which can happen in two ways:

1. One key signs two different releases of the same version: an identity key copied to a second machine, a restore from a backup, or bad intent. Picking one by hash would let that signer take a handed-off document back by searching for a lower id.
2. Two members publish the same `kalka_id`, each naming themselves in version 1: a Personal file passed around by hand and published twice.

Both are settled by any team owner with one reassignment, which is an anchor and restarts the chain. The state is a waiting item for the team's owners (D10).

One more state exists and gets its own wording, "last release withdrawn": a release made under a reassignment that replay later discards (its signer lost an owner term concurrently). Peers that took its plan keep the file, mark it, and take the next valid release they are offered, as today (§19.5 last point).

### D4. A team owner acts by taking the document over

- For a document they do not own, a team owner has one direct power: `reassign-document`, to anyone, themselves included.
- Release, delete for the team, promote and move are owner actions. When a team owner chooses one on someone else's document, the UI says "you will become the owner first", and kalka signs the reassignment to the actor and then the action. On the CLI these are two commands, `kalka reassign <file> <me>` and the action; no hidden step.
- Copy to another team needs no take-over: it reads and changes nothing in the source.
- Reason: one signer per version. If both the owner and every team owner could sign version n+1, ordinary concurrent work would fork the chain, and the dispute state would become common instead of rare. With a take-over the race is the one D3 already settles.
- **Who may sign release n+1:** the owner version n names; or, when an effective reassignment has cut-off n, its `owner`. Nobody else. Version 1: the publisher, or the owner an effective `accept-transfer` names (D6).
- **Domain documents.** Owner releases, handoff and reassignment as for any document. Publishing a domain document when the team folder already holds one with the same `domain:` name is refused (`domain-exists`). Two published concurrently are both kept and both flagged "two documents claim this domain"; a reference to that name is shown as ambiguous; a team owner deletes one.
- `kalka publish` now means only "move from Personal into a team". `kalka release` releases the next version of any team document and replaces *Publish update*. `--incorporates` works for every kind.

### D5. Nothing is silently overwritten

When replication replaces a local plan that is not the plan of any release (the owner's unreleased edits after a reassignment, or a non-owner's stray edit), the displaced file is first written to `<folder>/<team>/.kalka/trash/<name>.displaced-<timestamp>.md`, and a notice names it. No question is asked, and the file never returns to the index. This is all that remains of §7.5.

### D6. Copy and move between teams

**Shape.** One mechanism, *transfer*, with a mode. A transfer has three parts in three places:

| Part | Where | Signed by |
|---|---|---|
| The **offer**: the new document, carrying a `transfer` entry | Destination team's replica, as a pending document | The actor (a member of both teams) |
| The **decision**: `accept-transfer` or `decline-transfer` | Destination team's log | A destination team owner |
| The **source tombstone** (move only) | Source team, a final release | The document's owner in the source, who is the actor |

**Is a transfer a team-log operation?** The offer is not. The actor may be a plain member of the destination, and every log operation is an owner's (`docs/teamlog.md` rule 1); the content has to travel as a document in any case. The decision is. It needs owner authority, "removal wins" for a decider who is being removed, and one winner among concurrent decisions. Replay already gives all three.

**Starting a transfer.** `kalka copy <file> --to <team>` or `kalka move <file> --to <team>`:

1. Checks, all local: the actor is a member of the source and of the destination; the actor is the document's owner or a source team owner; the document has no dispute. For a move by a team owner who is not the owner, the take-over of D4 comes first.
2. A new document is built: a fresh `kalka_id`; `team: <destination>`; `copied-from: <source team name>/<source kalka_id>`, or `moved-from:` for a move; `owner:` the actor for now; the plan of the source's head release (unreleased edits do not travel); every valid entry of the source, byte for byte, old signatures included.
3. The actor's kalka appends a signed `transfer` entry and writes the file to `<folder>/<destination>/.kalka/incoming/<new id>.md`.
4. For a move, a signed `transfer` entry is also appended to the source document, so the source team sees "being moved to library-desk; comments added now will not travel". A copy leaves the source byte for byte unchanged.

**The `transfer` entry** (the actor's countersignature):

```
### transfer · ewa@example.org · 2026-12-01T10:00:00Z · § · rev:<8 hex> · id:<12 hex> · sig:<base64url>
move · from:parcel-ops/<source kalka_id> · source:<64 hex team hash> · to:<64 hex team hash>
plan:<64 hex>
history:<64 hex> · entries: 7c2e91ab04f3, a41b77c09e12
```

- `history` is SHA-256 over the lines `<64 hex digest of the entry's id input> <sig or "-">`, one per listed entry, in `(timestamp, id)` order. It covers the full digests, not the 48-bit ids, and the original signature text.
- `entries` lists the ids of the attested set, so every peer knows which entries the digest speaks for, also after members add their own.
- The entry carries the ordinary v2 signature (§6.5), which covers its body and the new `kalka_id`. So the actor signs: the mode, both teams by hash, the plan, and the exact history.

**How a pending transfer reaches the owners.** The pending document is part of the destination's replica: it has an inventory line with a "pending" mark and is offered and fetched like a published document. Any destination member relays it. Once one other member holds it, the actor can go offline. It is kept under `.kalka/incoming/`, outside the index, and shown only to the team's owners (to decide) and to the actor (to watch or withdraw). A peer accepts a pending document only if all of this holds:

- the `transfer` entry's signature verifies against a key the destination log has bound to its author;
- `to` is this team's hash and `plan` is the document's plan hash;
- the listed entries are present and hash to `history`;
- the document has no release.

Anything else is refused and listed in the Team view with the reason. A transfer signed by a non-member is refused here on every peer, as well as on the actor's own machine in step 1.

**Deciding.** Any destination team owner, in the app or with `kalka transfer accept|decline <id>`:

- **Accept:** `accept-transfer {doc, owner, transfer}` in the destination log. `transfer` is the 64-hex digest of the `transfer` entry, so the owner's signature on the operation is the second countersignature: it covers, through that digest, the plan and the history. `owner` is the accepting owner unless `--owner <member>` names another member. The operation works as an anchor with cut-off 0: `owner` is the only valid signer of version 1.
- The named owner's kalka then rewrites `owner:`, makes release 1, and moves the file from `.kalka/incoming/` into the team folder. From there it is an ordinary team document; the owner hands it off as usual.
- **Decline:** `decline-transfer {doc, transfer}`. Every peer replaces the pending file by a stub in `.kalka/tombstones/` (the `transfer` entry only), so a lagging replica cannot bring the offer back. A copy is thereby discarded. For a move, the actor's kalka appends "move declined" to the source document and nothing else changes there.
- **Withdraw:** the actor signs a `transfer-withdrawn` entry on the pending document. Same effect as a decline.
- **Concurrent decisions on one transfer:** an accept beats a decline or a withdrawal; of two accepts, the first in replay order counts and the other is discarded ("already decided"). So there is never a second version 1.

**How the destination treats history.** Entries listed by an effective transfer are *attested*. They are not checked against destination keys, take no part in the destination's release chain (source releases are shown as history, "v3 in parcel-ops"), and are shown with the badge "from parcel-ops, attested by Ewa and Marek": the actor, and the signer of the accept. Before the accept the badge names the actor only. An attested entry never changes: one that no longer matches `history` is dropped with a warning, as an entry with a wrong id is today. Entries written after the transfer are ordinary destination entries.

**The source tombstone (move).** Written by the actor's kalka when its destination replica shows the effective accept: a final release in the source, as in §20.6, whose first line ends `· deleted · moved:<destination team name>/<new id>`, and whose stub carries `moved-to:`. The source team sees "moved to library-desk" in place of the document. Until then the source is live and unchanged. The trigger is derived from files (a document in a team folder with `moved-from`, a `transfer` entry by the local user, and a live source the local user owns), so it needs no state file and survives restarts.

- If the actor is offline, the tombstone waits for them. Both documents exist meanwhile, and the source shows the "being moved" banner.
- If the actor can no longer sign it (the source document was reassigned, or the actor removed, in the meantime), the move ends as a copy. The actor gets a notice; the new owner in the source may delete it.
- **Entries added in the source after the transfer started** are not carried. They stay in the source document and in its trash copy after the tombstone. Carrying them would need a second attestation by one person only; see Q4.

**Why a new `kalka_id` for a move.** A tombstone is final for its id, forever, on every peer of the team. With the id kept, a document could never move back. And a member of both teams would hold two live files with one id between the accept and the tombstone, which the store flags as duplicates (§14). The store keeps an index from `moved-from` and `copied-from` ids, so a seed's `parent:` and a `budded:` link still resolve to a moved document that is held.

**Domain documents and seeds.** `version:` restarts with release 1 in the destination; snapshots are not carried. An accept is refused (`domain-exists`) when the destination holds a document with that domain name. A seed keeps `parent` and `origin`.

**Personal.** Personal to a team is publishing (D9): no offer, no acceptance, same `kalka_id`. A team to Personal is not in this work (Q8).

### D7. Trust comes from team logs only

- **A peer** is a pair of email and key that a team log you hold binds, for a team you are both members of. Nothing else is a peer.
- **Sharing no team:** the connection is refused. After the handshake, the first message must be a `team_sync` for a team in which, after the log operations it brings are merged, both sides are members (as today, `replicate.go:1000`), or the `hello` must carry an invite proof. Anything else ends with `bye`, reason `not-member`. No file is read and nothing is recorded about the caller.
- **Channel binding (§8.4) is unchanged** and still does the same job: the rendezvous cannot sit in the middle, and the other side proves it holds the key it presents. What follows it changes: the key is checked against team logs, not against a pin.
- **The key-ID proof (§9.6) is unchanged.** On `kalka-rendezvous` nobody can register a member's ID without the key.
- **First contact** rests on the invite token alone: it carries the inviter's key, and the joiner dials that key's ID. Whoever passes the token on is trusted with that, as today.
- **One email with different keys in two teams** (someone reinstalled and was re-invited to one of them) no longer blocks anything. Each team uses its own binding. The Team view shows one information line. This replaces the loud mismatch of §8.2.
- **Presence** is per team: the members of the selected team, with online or last-reached from replication rounds and probes (probing only while a round needs them, as in §9.3). The Peers view, `kalka peers` and the minute-by-minute probe of `peers.yaml` go. `kalka status` and the tray count distinct members across all teams.
- `internal/identity` keeps the key functions only. `peers.yaml` is no longer read or written.

### D8. Key IDs only

- A daemon registers one ID, the key ID, on one signaling connection. `email_id`, `peerid.Email`, the second connection (`signalLoop(ctx, key bool)`, `daemon.go:425`), the email fallback in `routes` (`daemon.go:511-529`), the email branch of the handshake's ID check (`internal/transport/binding.go:139-146`), and the config `team` salt with `kalka setup --team` are deleted.
- `hello` still carries the email: it is the name the log binds to the key.
- `ID-TAKEN` now means "this identity is already online on another machine".
- §18.1 question 3 loses "shared `team` string" as an access control at the rendezvous; the default becomes network policy alone.
- Compatibility with the public cloud and stock servers is unaffected: key IDs are already used there, and `kproof` is already ignored by them (§9.6).
- Reason: half the signaling code paths, one fewer way to be held offline, and no ID that anyone who knows an address can guess.

### D9. Personal, the folder layout and `team:`

```
<folder>/
  personal/                      unshared drafts, never replicated
  parcel-ops/                    one folder per team
    parcel-ops-team.log
    depot-routing.md
    .kalka/incoming/             pending transfers (D6)
    .kalka/tombstones/  .kalka/trash/
  library-desk/
```

- **Location decides.** A document's scope is the top-level folder it lies in. `team:` must agree for it to replicate. One function decides what replicates: the file lies in the team's folder, `team:` is the team's name, and it has a valid release or is a valid pending transfer.
- **Personal** is `<folder>/personal/`, created at setup. Files directly in `<folder>/` count as Personal too, so "drop a file in the folder" keeps working. No file there appears in any inventory, and a request for its id is answered like an unknown id.
- **`personal` is a reserved name.** `kalka team create personal` is refused; a joined team of that name gets the folder `personal-2`.
- **Publish** (`kalka publish <file> --team <team>`, or *Publish to team…*): kalka adds `kalka_id` and `owner` if missing, writes `team:`, makes release 1, and moves the file into the team's folder. The JSON result carries the new path. The member stays the owner.
- **Mismatches are shown, never replicated:** a file in a team folder without that `team:` or without a release shows "not shared: publish it or move it to Personal". A file in Personal with `team:` shows "has `team:` but lies in Personal".
- A seed budded from a team document is written beside it and released to that team, as today. A seed budded in Personal stays in Personal.
- §14's "a document the user published from elsewhere stays where it is" is deleted. Within its team folder a user may still move a file into subfolders; documents are found by `kalka_id` within their scope.
- Two teams with one name: the second folder gets a suffix, as today (`teams.go:222`). The switcher shows the name with a short hash when names collide.

### D10. Waiting for you

A waiting item is something only the local user can move forward. Each has a team, a time and a target in the UI. All are derived from files and logs, except the one "seen" mark.

| Item | For whom | Since | Cleared by |
|---|---|---|---|
| Pending transfer into the team | The team's owners | The `transfer` entry's timestamp | Accept, decline or withdrawal |
| Document handed off or reassigned to you, or accepted in your name | The new owner | The release's or operation's timestamp | Opening the document, `kalka waiting --seen <file>`, or any release of yours |
| Ownership disputed | The team's owners | The later of the two releases | A reassignment |
| Owner is no longer a member | The team's owners | The removal | A reassignment |

- Not waiting items: your own pending join (it waits for the inviter; the switcher shows "joining library-desk…"), a queued push, new comments (a notice, as today).
- The "seen" marks are a small list in `state.yaml`: document id and release id. A mark is dropped when the release is no longer the head.
- `GET /api/waiting` and `kalka waiting [--json]` list the items. `GET /api/summary` gains `waiting: {total, oldest: {team, at, target}, teams: [{team, name, count}]}` and loses `prompts` and `joins` from the count.
- Timestamps come from other people's clocks. "Oldest" is good enough for choosing which team to open; nothing else depends on it.

### D11. Join links

**The link.** `kalka://join/<token>`. The token is the existing one with one more field, `inviter` (the inviting owner's email, shown on the join screen; a claim until the join completes, like the team name). `teamlog.ParseJoin(s)` accepts a link, a bare token, either with surrounding whitespace or angle brackets as chat tools add them, any letter case, and nothing over 4 KiB. `kalka join` and the paste box both go through it.

**The join screen.** Route `#/join/<token>`. The page calls `POST /api/joins/preview` and shows the team name, the inviter, the invited email and the expiry, with one *Join* button. It shows instead of the button: "this invite is for olga@…, you are jan@…"; "expired"; "you are already a member"; "this invite uses another rendezvous server". The CLI twin is `kalka join --show <token>`. Opening a link never joins; only the button or `kalka join` does.

**First run from a link.** With no configuration, the Setup view opens with the email and the rendezvous filled in from the token, and goes on to the join screen. A new teammate types only the folder.

**Hand-over to a running app.** One path for all systems, using what exists: the process that receives the link calls `POST /api/ui/show` with the join route, and the shell that already runs shows its window there (`internal/desktop/shell.go:83`). Wails' single-instance feature stays unused (§18.3 decision 2).

| System | Registration | How the link arrives |
|---|---|---|
| Windows | Per user, no elevation: `HKCU\Software\Classes\kalka` with `URL Protocol` and `shell\open\command` = `"…\kalka-desktop.exe" "%1"`. Written by the installer (`tools/release/kalka.nsi.tmpl`, removed by the uninstaller) and re-checked by the shell at start | A new `kalka-desktop.exe` with the link as its only argument. `desktop.Main` (`internal/desktop/wails.go:34`) treats a single `kalka://` argument as a shell start with a route, not as a CLI call. If a shell runs, it hands over and exits 0 |
| macOS | `CFBundleURLTypes` with the scheme `kalka` in `Kalka.app`'s `Info.plist` (`tools/release/desktop.go`). The system registers it when the bundle is first opened; nothing to do at run time | An Apple Event to the running app, or to a newly started one. Wails v3.1.0 turns it into `events.Common.ApplicationLaunchedWithUrl` in both cases (`application_darwin.go:557-569`). The shell's handler shows the window on the join route |
| Linux | `MimeType=x-scheme-handler/kalka;` and `Exec=kalka-desktop %u` in `kalka.desktop`. The shell writes the file to `$XDG_DATA_HOME/applications` and runs `xdg-mime default kalka.desktop x-scheme-handler/kalka` at start, since there is no installer | As on Windows: a new process with one argument |

- Code: a new package `internal/urlscheme` beside `internal/autostart`, with `Register`, `Unregister` and `Status` per system. The registry root, the data directory and the `xdg-mime` runner are parameters.
- **Tests never register anything real.** They run against a fake registry interface, a temp `XDG_DATA_HOME` and a recording command runner, and they check the generated `Info.plist` and `.desktop` text. No test opens a built `Kalka.app`, because opening a bundle is what makes macOS register its scheme.
- Without a desktop shell (headless install): `kalka open <link>` opens the browser on the join route, and `kalka join <link>` joins directly.
- The link is untrusted input from any web page or chat. It is parsed strictly before anything else and can only open the join screen. The team name and inviter are rendered as text.

### D12. Parity as a tested rule

- The CLI's dispatch switch (`internal/cli/main.go:64-116`) becomes a table of commands. A second table, `internal/cli/parity.json`, maps each command to a class and either a UI action id or a written exemption.
- **Go test** (`internal/cli/parity_test.go`): every command in the dispatch table has a row; every row with class "workflow" has a UI action id; every workflow command accepts `--json`; every error kind a command can return is in the list of kinds in the spec.
- **Web test** (`web/src/parity.test.ts`): reads the same JSON. Every UI action id in it is declared in `web/src/actions.ts`, and every declared action is used by a component (an `action="…"` on its control). Every action declared in `actions.ts` appears in the JSON, so a UI workflow without a command fails too.
- **Error kinds.** A typed `core.Error{Kind, Message}` replaces bare `fmt.Errorf` where a caller can act on the difference. The list is in section 4.3. `offline`, `key-changed`, `diverged`, `rejected-*` and `not-verified` are deleted.

### D13. Rendezvous becomes `kalka-rendezvous`

**Decision: split.** A new `cmd/kalka-rendezvous` holds what is now `internal/cli/rendezvous.go`; `kalka rendezvous` is deleted.

Evaluated against `tools/release`:

| | Today | After the split |
|---|---|---|
| What a server admin downloads | The headless archive: `kalka` (client, daemon, embedded web UI, WebRTC) and `kalka-plugin-gh`, to run one subcommand | `kalka-rendezvous_<version>_<os>_<arch>`: one static binary that imports `internal/rendezvous`, `internal/peerid`, `internal/peerjs`, `coder/websocket` and `pion/turn`, and nothing else |
| What a desktop user installs | A client that can also open a public signaling and TURN listener | A client with no server mode |
| Release build | Four kinds of artefact | One more build line and archive per target (`linux/amd64`, `linux/arm64`, and the desktop targets for demos); no new tooling, no cgo |
| Parity audit | `rendezvous` is the one command with no UI and needs an exemption | Every `kalka` command is a user's command |

It simplifies what each side packages and reviews, at the cost of one more archive. The server needs no config directory, identity or folder, so nothing is shared at run time. An import test (`internal/core/imports_test.go` has the pattern) keeps `cmd/kalka-rendezvous` free of `internal/core`, `internal/daemon`, `internal/ui` and `pion/webrtc`. The in-process rendezvous the tests use stays as it is.

### 3.14 Smaller decisions

- **Wire.** `verify` and the `sync` and `handoff` intents go. A document travels only inside a team session. T1 decides, from what `exchange` actually reads (`replicate.go:369`), whether `sync_offer`, `sync_reply` and `sync_ack` shrink to one `team_doc` message. The protocol number stays 1; there are no installed peers.
- **`kalka daemon --no-popups`** becomes `--no-notifications`. There are no popups left.
- **Releases by removed members** keep verifying against any key the log ever bound (§20.4). Unchanged.
- **Pause** is unchanged. Handoff, reassign, release, publish and transfers all write locally and are pushed after resume. Only `join` still fails with `paused`.

---

## 4. CLI and UI parity audit

### 4.1 Today

Commands from `usage` in `internal/cli/main.go:122-160`.

| Command | UI entry point today | After |
|---|---|---|
| `kalka` | — (starts the UI) | kept |
| `daemon` | — | kept; `--no-popups` renamed |
| `stop` | Tray *Quit* | kept |
| `status` | Header status line, tray | kept; per-team waiting added |
| `pause`, `resume` | Settings, tray | kept |
| `autostart on\|off\|status` | Settings *Start at login* | kept |
| `open [--print]` | — | kept; takes a link |
| `setup` | Setup view | kept; `--team` deleted |
| `send` | *Sync with…* | **deleted** |
| `handoff` | *Hand off to…* (prompted sync) | changed (D2) |
| `peers`, `peers add\|forget` | Peers view | **deleted** |
| `verify` | Peers view | **deleted** |
| `check` | Problems in the Blueprints list and the review view | kept |
| `comment` | Review: comment, deviation, reply | kept |
| `seed` | *Create seed* | kept |
| `seeds` | Pre-backlog | kept; `--team` |
| `team create` | Team page form | UI moves to the switcher |
| `team invite` | Team page | kept; prints the link too |
| `team members\|log\|invites` | Team page | kept |
| `team remove\|add-owner\|remove-owner` | Team page | kept |
| `team assign-domain` | Team page form | **deleted** |
| `join` | Team page paste box | kept; takes a link; `--show` |
| `publish` | Publish dialog, review banner | changed (D9) |
| `release` | Domains page, review view | widened to every team document (D4) |
| `delete` | Review *Delete for the team* | kept |
| `domains` | Domains page | kept; `--team` |
| `rendezvous` | **none** | **moved** to `kalka-rendezvous` |
| `reference export\|version` | **none** | kept; UI added |
| `version` | **none** | kept; UI added |

UI workflows with no command today:

| UI action | Where | Command to add |
|---|---|---|
| Promote a seed | `web/src/Views.tsx:346`, `Review.tsx:107` | `kalka promote <file> [--status draft]` |
| Add the missing backlink | `Review.tsx:115` | `kalka seed backlink <seed file>` |
| Cancel an invite | `Team.tsx:444` | `kalka team uninvite <id>` |
| Sync a team now | `Team.tsx:482` | `kalka team sync [--team]` |
| Notifications, keep running when closed | `web/src/Settings.tsx` | `kalka settings [--notifications on\|off] [--keep-running on\|off]` |

So today: three commands without a UI entry, five UI workflows without a command.

### 4.2 Target

Classes: **W** workflow (must have a UI action), **V** view (must have a UI place), **P** process control (exempt, with the reason in the table).

| Command | Class | UI entry point (action id) |
|---|---|---|
| `kalka setup --email --rendezvous --folder` | W | Setup view (`setup.save`) |
| `kalka teams` | V | Team switcher (`team.switch`) |
| `kalka team create <name>` | W | Switcher *Create team…* (`team.create`) |
| `kalka team invite <email> [--expires]` | W | Team *Invite*, *Copy link* (`team.invite`) |
| `kalka team uninvite <id>` | W | Team, open invites, *Cancel* (`team.uninvite`) |
| `kalka join <link\|token> [--wait]`, `--show` | W | Switcher *Join with link or token…*, join screen (`team.join`) |
| `kalka team members\|log\|invites` | V | Team view |
| `kalka team remove <email> [--dry-run]` | W | Team *Remove*, with the preview (`team.remove`) |
| `kalka team add-owner\|remove-owner <email>` | W | Team *Make owner*, *Remove as owner* (`team.owner`) |
| `kalka team sync` | W | Team *Sync now* (`team.sync`) |
| `kalka publish <file> --team <team>` | W | Personal: *Publish to team…* (`doc.publish`) |
| `kalka release <file> [-m] [--incorporates]` | W | Review banner *Release*; Domains *Release* (`doc.release`) |
| `kalka handoff <file> <email>` | W | Review *Hand off…* (`doc.handoff`) |
| `kalka reassign <file> <email>` | W | Review *Reassign…*; Team, a member's documents (`doc.reassign`) |
| `kalka copy <file> --to <team>` | W | Review *Copy to team…* (`doc.copy`) |
| `kalka move <file> --to <team>` | W | Review *Move to team…* (`doc.move`) |
| `kalka transfers` | V | Team *Transfers*; Waiting list |
| `kalka transfer accept <id> [--owner]` | W | Waiting list, Team *Transfers* (`transfer.accept`) |
| `kalka transfer decline <id>` | W | same (`transfer.decline`) |
| `kalka transfer withdraw <id>` | W | Team *Transfers*, your own (`transfer.withdraw`) |
| `kalka delete <file>` | W | Review *Delete for the team* (`doc.delete`) |
| `kalka comment <file> …` | W | Review comment, deviation, reply (`entry.add`) |
| `kalka seed <parent> …` | W | *Create seed* (`seed.create`) |
| `kalka seed backlink <seed>` | W | Review *Add the backlink* (`seed.backlink`) |
| `kalka promote <file>` | W | Pre-backlog and review *Promote* (`seed.promote`) |
| `kalka seeds`, `kalka domains` | V | Pre-backlog, Domains |
| `kalka check <file>` | V | Problems in the Blueprints list and the review view |
| `kalka waiting`, `--seen <file>` | V | Switcher badges, Waiting list, tray |
| `kalka status` | V | Header status line, tray |
| `kalka pause`, `kalka resume` | W | Settings, tray (`sync.pause`) |
| `kalka settings …` | W | Settings (`settings.save`) |
| `kalka autostart on\|off\|status` | W | Settings *Start at login* (`settings.autostart`) |
| `kalka reference export\|version` | W | Settings *Blueprint reference: save a copy* (`reference.export`) |
| `kalka version` | V | Settings, *About* line |
| `kalka`, `kalka open [link]`, `kalka daemon`, `kalka stop` | P | Exempt: they start, show or stop the app itself. *Quit* in the tray is the UI of `stop` |

Team commands take `--team <name or hash>`; with exactly one team it may be omitted. Commands that take a file read the team from where the file lies.

### 4.3 Error kinds

`not-set-up`, `not-running`, `paused`, `no-shell`, `format`, `duplicate`, `no-team`, `unknown-team`, `ambiguous-team`, `not-member`, `not-team-owner`, `not-owner`, `last-owner`, `in-personal`, `not-shared`, `disputed`, `domain-exists`, `transfer-pending`, `transfer-decided`, `unknown-transfer`, `invite-token`, `invite-expired`, `invite-used`, `invite-email`, `rendezvous-mismatch`. The spec lists them with one line each; the parity test checks the list against the code.

---

## 5. Team switcher, waiting counts and the tray

**Switcher.** At the top left of the window, in place of the product name's dead space (`web/src/App.tsx:208`): the current team's name; a dropdown listing Personal first, then every team by name with a badge showing its waiting count; a line "joining library-desk…" for each pending join; then *Create team…* and *Join with link or token…*. Two teams with one name show a short hash.

**Scope.** The route carries the scope: `#/t/<team>/…`, with `personal` for Personal. The choice is remembered in the page's storage. `GET /api/blueprints`, `/api/seeds`, `/api/domains` and `/api/waiting` take `?team=`. Blueprints, Review, Canvas, Trace, Pre-backlog, Team and Domains show the selected scope. In Personal, the Team and Domains links are hidden and Blueprints offers *Publish to team…* per file. Opening a document by link switches the scope to the document's.

**Waiting list.** The badge opens the selected team's list: each item with its kind, who and when, and its buttons (*Accept*, *Decline*, *Open*, *Reassign…*).

**Setup** keeps email, rendezvous and folder. The create and join forms leave the Team page (`web/src/Team.tsx:90-140`) for the switcher. The Peers link and view go.

**Tray.** "N waiting for you" shows `summary.waiting.total`. A click calls `ShowWindow` with `summary.waiting.oldest.target`: the waiting list of the team holding the oldest item (`internal/desktop/model.go:77-80`, `shell.go:403`). The status line counts members reachable across all teams.

**Notices** (§22.4) change to: handed off or reassigned to you; your document was reassigned to someone else; a transfer waits for your team; your transfer was accepted, declined; a move finished or ended as a copy; your edits were displaced (D5); plus the existing comment, join and domain-version notices. "Handoff waiting" goes.

---

## 6. Spec change outline (v0.6)

Removed concepts are deleted, not marked. Section numbers that the code cites are kept where the section survives.

| Section | Change |
|---|---|
| Preamble | v0.6 paragraph; triage is this file |
| §1 | Purpose in terms of teams. Non-goals: drop the one-to-one sentences; add "sharing with someone outside every team" and "a team to Personal" |
| §2 | Principle 1: local state loses `peers.yaml`. Principle 3 unchanged |
| §3 | Delete *Sync*, *Domain owner*, *Private document*, *Email ID*. Add *Personal*, *Handoff*, *Reassignment*, *Transfer*, *Attested entry*, *Waiting item*. *Peer* becomes "a member of a team you share" |
| §4 | New diagram with a team of three. Rendezvous is `kalka-rendezvous` |
| §5 | Delete the `team` salt and `peers.yaml`. `state.yaml` gains the seen marks |
| §6.2 | `team` written on publish; add `copied-from`, `moved-from`, `moved-to`; "first send" becomes "first publish". §6.5: delete `handoff` behaviour; add `transfer`, `transfer-withdrawn`; signature text says "the key the team log holds". §6.8: rewritten for Personal. §6.11: seeds in Personal and in teams. §6.12: owner releases; no assignment; duplicate domain names |
| §7 | **Rewritten** as "Ownership": owner, immediate handoff (D2), reassignment and the race (D3), team-owner take-over (D4), displaced edits (D5). §7.1, §7.2 rules 2, 4, 5, §7.4 as written, §7.5 and the private half of §7.6 are deleted; the entry union moves to §20.4 |
| §8 | **Rewritten** as "Identity and trust": keys; trust from team logs (D7); channel binding. §8.2 and §8.3 deleted |
| §9 | §9.2 key ID only. §9.3 presence per team. §9.5 messages without `verify` and the sync intents; pending inventory lines. §9.6 retitled `kalka-rendezvous`. §9.7 notice text |
| §11 | Switcher, scoped views, waiting list, join screen, attested badge, transfer dialogs; Peers and the prompt cards deleted |
| §12 | The table of section 4.2; the parity rule; the list of error kinds |
| §13 | Scoped endpoints, `/api/waiting`, `/api/joins/preview`; prompts and peers endpoints deleted |
| §14 | The layout of D9; `.kalka/incoming/`; `.conflict-` rule deleted; `.displaced-` files in the trash |
| §15 | `cmd/kalka-rendezvous`; `internal/urlscheme` |
| §16 | T milestones; §16.1 scenarios 1 to 10 replaced by team scenarios (isolation, Personal, handoff, reassignment race, copy, move, decline, join link) |
| §17 | Item 20: what v0.6 removed and why (findings 1 to 6 of this plan). Items 2, 4, 15, 18 and 19 marked as overtaken by it |
| §19 | §19.1 table: `assign-domain` out; `reassign-document` new arguments; `accept-transfer`, `decline-transfer` in; rule version 2. §19.3 two roles. §19.4 rule 8 and the reassignment rule. §19.5 rewritten per D3. **§19.6 deleted.** §19.7: `inviter` in the token; links |
| §20 | §20.1 scope by location; publish from Personal. §20.3 pending documents. §20.4 one signer per version; anchors; what is disputed; attested entries. §20.6 the `moved:` tombstone. **New §20.7 Transfers between teams**: the exact scheme of D6 |
| §21 | Trust without pins; a leaked link; attested history is two people's word; the source role check (X19); pending transfers are readable by destination members before the decision; a link can only open the join screen |
| §22 | Tray per section 5; notices per section 5; `--no-notifications` |
| §23 | URL scheme registration per system (D11); the rendezvous archive; `docs/desktop-testing.md` rewritten |

**§18 draft, "Open questions and decisions".**

Open questions, with defaults: Q1 to Q9 of section 10, plus the surviving ones: multi-repo blueprints, access control at the rendezvous (default now network policy alone), planned change in a trace, manual accept for joins, key rotation, teams across rendezvous servers, several devices.

Decisions (v0.6):

| # | Question | Decision |
|---|---|---|
| 1 | Sharing | Teams only. One-to-one sync, pins, codes, accept prompts and conflict files are deleted |
| 2 | Peer IDs | Key ID only; reverses §18.2 decision 1 |
| 3 | Roles | Team owner and member; no domain owner |
| 4 | Owner tiebreak | The seniority chain; siblings by grant depth, then hash |
| 5 | Handoff | A release naming the new owner; immediate; no connection |
| 6 | Reassignment | Any document, any time, by a team owner; an anchor with a cut-off; reverses §18.2 decision 8 |
| 7 | Race rule | A former owner's release above the cut-off is void |
| 8 | Team owner as document owner | By take-over; one signer per version |
| 9 | Disputes | Only two different valid releases of one version; settled by a reassignment |
| 10 | Displaced edits | Kept in the team's trash, with a notice |
| 11 | Layout | Location decides; Personal; publish moves the file |
| 12 | Transfers | Offer as a pending document; decision in the destination log; new id for copy and move; tombstone after acceptance |
| 13 | Attestation | `transfer` entry by the actor, `accept-transfer` by the owner; badge names both |
| 14 | Waiting items | Transfers, handed-to-you, disputes, ownerless documents |
| 15 | Join links | `kalka://join/<token>`; per-user registration; the link only opens the join screen |
| 16 | Rendezvous | Its own binary |
| 17 | Parity | A tested table; stable error kinds |

---

## 7. Milestones

T0 to T8, then T9. Each ends with `go build ./...`, `go vet ./...`, `go test ./...` and the web tests green, and a commit on `main`. No milestone keeps code for the state before it.

### T0. Spec v0.6

Write the changes of section 6, with the product owner's answers to section 10 or the defaults. Rewrite `docs/teamlog.md` for rule version 2. Add IP §6u.

| | |
|---|---|
| Files | `specification.md`, `docs/teamlog.md`, `implementation-plan.md` |
| Tests | None. A grep check that the spec no longer names `send`, `verify`, `peers.yaml`, `assign-domain`, `email_id` |
| Size | 4 days |

### T1. Remove one-to-one, pins, prompts and email IDs

Everything in X1, X3, D7 and D8. Handoff is cut here and comes back in T3; `kalka handoff` is absent in between.

| | |
|---|---|
| Spec | §7, §8, §9.2, §9.3, §9.5, §11, §12, §13 |
| Packages | `internal/core` (`daemon.go`, `prompts.go` deleted, `summary.go`, `join.go`, `model.go`), `internal/syncproto` (session deleted), `internal/merge` (`merge.go` reduced to helpers), `internal/identity`, `internal/transport` (`binding.go`, `presence.go`), `internal/peerid`, `internal/config`, `internal/store` (conflict files), `internal/daemon` (`api.go`, `popups.go` deleted, `client.go`), `internal/cli` (`net.go` mostly deleted, `main.go`), `internal/desktop/model.go`, `web/src` (`Views.tsx`, `Dialogs.tsx`, `App.tsx`, `api.ts`, `notify.ts`) |
| Tests | A connection from a peer that shares no team is closed with `not-member`, and no file is read (recording store). A member with another key than the log binds is refused. One signaling registration per daemon; the M1 suite on key IDs; the live public-cloud check (`internal/peerjs/cloud_test.go`) still passes. Team scenarios that used a one-to-one sync as setup are re-seated on replication. Import test: nothing imports the deleted symbols |
| Size | 4 days |

### T2. Personal and team folders

D9, and the isolation gap of X6.

| | |
|---|---|
| Spec | §6.2, §14, §20.1, §20.5 |
| Packages | `internal/store` (scope of a path, per-scope lookup, alias index stub), `internal/core` (`publish.go`: publish moves the file; `replicate.go:213`: replica set from the team folder; `teams.go`: reserved name; `seeds.go`), `internal/daemon/web.go` (setup creates `personal/`), `internal/cli` |
| Tests | **Replication isolation (required):** Ewa and Marek share `parcel-ops`; Ewa and Olga share `library-desk`; a recording channel shows that Marek receives no byte of a `library-desk` document or log, and Olga none of `parcel-ops`, across full rounds in both directions; a request for a foreign id is answered like an unknown one. Two teams with the same name on one machine exchange nothing across. **Personal never leaves (required):** with Personal files present, including one that carries `team:` and one with a forged release, no inventory line, no content and no id of them appears on any channel. Publish moves the file and reports the new path. Mismatch states are listed and not replicated. `personal` is refused as a team name |
| Size | 4 days |

### T3. Roles and ownership

D1 to D5.

1. `internal/teamlog`, rule version 2: delete `assign-domain`; `reassign-document` without the removal rule, with `plan`, with the stale cut-off rule; `State.Reassigned` holds every effective reassignment per document.
2. The harness (`sim_test.go`) generates reassignments at random, concurrent with removals of their signers and with each other.
3. `internal/merge/team.go`: `Team.Override` becomes `Team.Anchors(id)`; the chain restarts at an anchor; `DomainRule` deleted.
4. `internal/core`: `Handoff` (a release), `Reassign`, take-over for release and delete, auto-release by the new owner, notices, displaced copies.
5. CLI: `handoff`, `reassign`, `release` for every kind; local API routes.

| | |
|---|---|
| Spec | §6.12, §7, §19.1 to §19.5, §20.4 |
| Packages | `internal/teamlog`, `internal/merge`, `internal/blueprint` (`release.go`, `entry.go`), `internal/core` (`publish.go`, `teams.go`, `replicate.go`, `domains.go`, `notices.go`), `internal/daemon/teams_api.go`, `internal/cli` |
| Tests | **Immediate handoff (required):** with the recipient offline, the owner hands off; the release names the recipient; the recipient's first round delivers it with a notice and no question; the recipient hands it back. Handoff to a non-member and of a Personal file is refused. **Reassignment without consent (required):** a team owner reassigns a present member's document while that member is offline; previous and new owner each get one notice; a plain member's attempt fails with `not-team-owner`. **The race (required):** the former owner releases version n+1 concurrently with a reassignment at cut-off n; over every delivery order of the operation and the two releases to three peers (enumerated, not sampled), all peers end with the same chain, plan and owner. Two team owners reassigning one document concurrently converge. A document reassigned twice keeps a valid chain through both cut-offs. A dispute (one key, two releases of one version) is ended by a reassignment. The harness invariants hold for 100 000 histories with rule version 2. Displaced edits land in the trash with a notice. A domain document is released by its owner, handed off, and reassigned; a second document with the same domain name is refused locally and flagged when concurrent |
| Size | 6 days |

### T4. Transfers between teams

D6.

| | |
|---|---|
| Spec | §6.2, §6.5, §19.1, §20.3, §20.4, §20.6, new §20.7, §21 |
| Packages | `internal/blueprint` (new `transfer.go`: the entry, the history digest), `internal/teamlog` (`accept-transfer`, `decline-transfer`, the same-subject rule), `internal/merge` (attested entries outside the chain and outside the key check), `internal/core` (new `transfer.go`; `replicate.go`: pending inventory lines and `.kalka/incoming/`; `publish.go`: the `moved:` tombstone), `internal/store` (alias index), `internal/daemon`, `internal/cli` (`copy`, `move`, `transfers`, `transfer accept\|decline\|withdraw`) |
| Tests | **Required:** copy accepted: new id, `copied-from`, source bytes unchanged, the accepting owner is the owner, and they can hand it off. Move accepted: the source has no tombstone before the accept and has the `moved:` tombstone after it; the source team's members see it gone. Decline of a copy: every destination peer discards it and a lagging peer cannot bring it back. Decline of a move: the source keeps the document, no tombstone. **Countersigned history (required):** a destination member who is not in the source team accepts the pending document, shows its entries as attested by the actor and then by both, and a document with one attested entry altered, removed or added is refused. **Non-member (required):** refused on the actor's machine with `not-member`; and a pending document whose `transfer` entry is signed by a key the destination log does not bind is refused by every receiving peer, with the reason listed. Added: the actor goes offline right after the click and an owner reached only through a third member still gets the item. Two owners accept concurrently: one version 1 everywhere. Accept against decline, and accept against withdrawal: accept wins everywhere. A move whose actor lost the source document ends as a copy with a notice. A move to a team and back works. A seed's `parent` resolves to a moved document. The harness covers the two new operations |
| Size | 7 days |

### T5. Join links

D11.

| | |
|---|---|
| Spec | §19.7, §13, §23 |
| Packages | `internal/teamlog/invite.go` (`inviter`, `ParseJoin`), `internal/core/join.go` (`JoinPreview`), `internal/daemon/teams_api.go` (`POST /api/joins/preview`), `internal/cli` (`join --show`, `open <link>`), new `internal/urlscheme`, `internal/desktop` (`wails.go`: the single-argument start and the `ApplicationLaunchedWithUrl` handler; `shell.go`: start with a route), `tools/release` (`kalka.nsi.tmpl`, the `Info.plist` writer, `desktopEntry`), `web/src` (join screen, setup prefill) |
| Tests | **Parsing (required):** a link, a bare token, upper and lower case, whitespace, angle brackets; refused: another scheme, another path, extra path parts, a query, over 4 KiB, a damaged checksum. **Hand-over (required):** with a daemon and a fake shell attached, a start with a link calls `ui/show` with the join route and exits 0; with no shell, the start plan carries the route; `kalka open <link>` prints the join URL with `--print`. Preview reports a wrong email, an expired invite, another rendezvous, an existing membership, and never joins. Setup prefill from a token. `internal/urlscheme` against a fake registry, a temp data directory and a recording runner; the installer script, the plist and the `.desktop` text contain the registration (`tools/release/release_test.go`). No test registers a real handler |
| Size | 4 days |

### T6. Waiting items, the scoped API and the missing commands

D10, D12 (error kinds and the command table), the commands of 4.1's second table.

| | |
|---|---|
| Spec | §12, §13, §22.1, §22.4 |
| Packages | `internal/core` (new `waiting.go`; `summary.go`; a typed `Error`), `internal/daemon` (`api.go`: `?team=`, `/api/waiting`; `state.go`: seen marks; `--no-notifications`), `internal/cli` (command table; `teams`, `waiting`, `promote`, `seed backlink`, `team uninvite`, `team sync`, `settings`; `--team` everywhere), `internal/desktop/model.go` |
| Tests | Each waiting item appears for the right people only and clears as D10 says. The summary's total, per-team counts and oldest target over three teams. The tray model shows the total and opens the oldest team. Every new command with `--json`, and with no daemon where its class allows. A table test: each error kind is produced by at least one command and is in the spec's list |
| Size | 4 days |

### T7. The app

Section 5, and a UI entry for every workflow of 4.2.

| | |
|---|---|
| Spec | §11, §22 |
| Packages | `web/src`: new `Switcher.tsx`, `Waiting.tsx`, `Join.tsx`, `Transfer.tsx`, `actions.ts`; `App.tsx` (scoped routes), `Review.tsx` (hand off, reassign, copy, move, release for every kind, attested badge, "being moved" banner, take-over wording), `Team.tsx` (transfers, presence, no domain form, no create or join form), `Views.tsx` (Personal list with publish; Peers deleted), `Settings.tsx` (reference, version), `api.ts`; `internal/ui` (rebuilt bundle); `internal/cli/parity.json` |
| Tests | **Parity (required):** the Go test and the web test of D12. Web unit tests: route parsing with a scope, the switcher's badge and ordering model, the waiting list model, the join screen's states, the attested badge text. The daemon web test (`internal/daemon/web_test.go`) walks one workflow of each family through the API the page uses. A grep test that fixtures and mockups hold no term from the excluded domains |
| Size | 7 days |

### T8. Rendezvous binary, packaging and documents

D13, and everything users read.

| | |
|---|---|
| Spec | §4, §9.6, §15, §23 |
| Packages | new `cmd/kalka-rendezvous` (from `internal/cli/rendezvous.go` and its test); `tools/release` (`main.go`: the new archive per target; `desktop.go`); `Makefile`; `docs/rendezvous.md`, `docs/install.md`, `docs/headless.md`, `docs/desktop-testing.md` (rewritten as the T9 script), `docs/m1-transport.md` (the `kalka send` line), `internal/reference/BLUEPRINT-REFERENCE.md` if it names a removed command |
| Tests | `kalka-rendezvous` passes the tests `kalka rendezvous` had. The import test of D13. The release test lists the new archives and `SHA256SUMS` covers them. `kalka rendezvous` answers with a line naming the new binary and exit code 2 |
| Size | 3 days |

### T9. Windows pass (product owner)

One pass with `docs/desktop-testing.md` as rewritten in T8. Untested on real Windows until then: the `kalka://` registration and the hand-over, the installer's registry writes and their removal, and everything D8 already listed. Fixes follow as found.

---

## 8. Order and size

| Order | Milestone | Depends on | Size |
|---|---|---|---|
| 1 | T0 Spec v0.6 | answers or defaults for section 10 | 4 days |
| 2 | T1 Remove one-to-one | T0 | 4 days |
| 3 | T2 Personal and team folders | T1 | 4 days |
| 4 | T3 Roles and ownership | T2 | 6 days |
| 5 | T4 Transfers | T3 | 7 days |
| 6 | T5 Join links | T1 | 4 days |
| 7 | T6 Waiting, scoped API, commands | T4, T5 | 4 days |
| 8 | T7 The app | T6 | 7 days |
| 9 | T8 Rendezvous, packaging, documents | T7 | 3 days |
| 10 | T9 Windows pass | T8 | product owner |

About 43 days for one engineer before T9.

Why this order:

- **Deletion before construction.** T1 removes a second trust source and a second merge rule. Everything after it is written against one model, and the remaining tests say what the team path really needs.
- **Layout before ownership.** "Which team does this file belong to" must have one answer before reassignment and transfers ask it.
- **Rules as libraries first.** T3 and T4 start in `internal/teamlog` and `internal/merge`, with the harness and enumerated orders, before any command or screen.
- **Every command before the app.** T2 to T6 each ship their CLI with `--json`. T7 then has a complete, tested API to draw, and the parity test has both sides to compare.
- **T5 depends only on T1** and can move earlier or go to a second person.
- **Packaging last**, so the installer, the documents and the Windows script describe the final command set.

---

## 9. Risks and how to reduce them

| # | Risk | Why | Reduction |
|---|---|---|---|
| R1 | Reassignment rules split a team | Rule version 2 changes replay; anchors change the chain | Pure libraries first (T3). The harness with hostile signers; the race test enumerates every delivery order. `docs/teamlog.md` rewritten before code |
| R2 | A document leaks across teams or out of Personal | The worst failure for trust in the tool, and "location decides" is new | One function decides the replica set. Recording-channel tests fail on any foreign byte or id (T2). The same-name case is tested |
| R3 | Attested history is read as verified | A destination member cannot check it; a careless or hostile actor can attribute any text to anyone "from the source team" | The badge names who vouches. The threat model says what it is worth. The accepting owner sees the full history before accepting |
| R4 | A move hangs half done | The tombstone needs the actor online after the accept | Stateless trigger that runs at every start and log change. A visible "being moved" banner. A defined ending as a copy. A notice either way |
| R5 | Pending transfers pile up or are spam | Any destination member can offer | Only owners see them; withdraw and decline leave a small stub; a member who floods is removed, as for any flooding (§21) |
| R6 | Losing edits to a reassignment | No consent, no prompt | D5: displaced copy and a notice. The UI warns the team owner when the owner has been reached recently |
| R7 | Join links do not open | Chat tools may not make `kalka://` clickable; a system may lack a handler | The paste box takes a link or a token. `kalka join <link>` works. The invite dialog offers *Copy link* and *Copy token* |
| R8 | The URL handler is an entry from the web | Any page can raise the link | Strict parse, 4 KiB cap, join screen only, a click to join, text rendering |
| R9 | macOS registers a development build's scheme | Opening any bundle with `CFBundleURLTypes` registers it | Tests never open a bundle; the smoke test keeps starting the bare executable (`cmd/kalka-desktop/smoke_test.go`) |
| R10 | Windows registration and hand-over untested until T9 | No Windows machine | A fake registry in unit tests; `GOOS=windows` vet and build; the T9 script lists each step |
| R11 | Publish changes a file's path under an agent | Pipelines hold the old path | The result JSON carries the new path; the reference document for agents says so |
| R12 | Large deletion breaks test scaffolding | Many scenarios start with a one-to-one sync | T1 re-seats them first and is its own milestone |
| R13 | Parity test becomes a list nobody trusts | A table can drift from the real UI | The web test ties each id to a control in a component, in both directions |
| R14 | One rendezvous per installation | More teams per person meets the limit sooner | Refused at the join screen with a clear line; recorded as open (X26) |

---

## 10. Questions for the product owner

Each has the default the plan uses if unanswered.

1. **Team owners and other people's documents.** The request says a team owner can do anything a document owner can. The plan gives them one direct power, reassignment, and makes release, delete and move work by taking the document over first, in the same click (D4). Otherwise two people could sign the same version and disputes would be ordinary. *Default: take-over.*
2. **Handoffs in the waiting count.** Should "handed off or reassigned to you" count as waiting until you open the document, or be a notice only? *Default: it counts, and is cleared by opening the document or `kalka waiting --seen`.*
3. **Who sees a pending transfer.** It replicates to every destination member's machine so that it reaches the owners while the actor is offline, but only owners and the actor see it in the app. *Default: as described. The alternative, owners' machines only, loses relaying through members.*
4. **Entries added to the source during a pending move.** They are not carried to the destination; the source shows a banner while the move is pending. *Default: not carried.*
5. **Invite links name one person.** A link posted in a channel works only for the invited address. Is that intended, or should a team-wide link exist? *Default: named and single use, as today. A team-wide link reopens §18.2 decision 5.*
6. **Key IDs only.** Drop the email-derived ID and the `team` salt (D8)? *Default: yes.*
7. **Publish moves the file** from `personal/` into the team folder (D9). *Default: yes; the new path is in the command's result.*
8. **From a team back to Personal.** Not in the request. *Default: not in this work; "delete for the team" keeps the last copy in the team's trash.*
9. **The open D8 pass.** Run it now on a UI that is about to lose three views, or once after T8? *Default: once, as T9.*
