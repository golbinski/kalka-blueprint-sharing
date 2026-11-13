# Plan: teams, roles, replication, domains, seeds and the blueprint reference

Triage and implementation plan for the feature request "extend kalka with teams, roles, replication, domains, seeds, and a blueprint reference". Written 2026-11-13 against `specification.md` v0.3 and the code on `main` (275614d). Nothing here is implemented.

This plan **replaces** `docs/plan-domains-seeds-reference.md` (the "previous plan"). Section 7 says, item by item, what is kept from it. References to it are written "old T2", "old D4", "old N3".

`§n` points into `specification.md`. `IP §n` points into `implementation-plan.md`. Code is cited as `path:line`.

Contents: 1 summary, 2 triage, 3 decisions, 4 milestones, 5 order and size, 6 risks and how to reduce them, 7 relation to the previous plan, 8 questions for the product owner.

---

## 1. Summary

The earlier request added three document features to a tool that syncs one file between two people on a click. This request changes what kalka is: a team gets a signed membership log, and every member holds a replica that syncs in the background. That is roughly twice the work of the previous plan (about 19 weeks against 9.5).

| Feature | Weight | State today |
|---|---|---|
| 1 Identity | Medium | Key generation and pinning exist. Peer IDs come from the email. Only handoff entries are signed. |
| 2 Teams and roles | Large, risky | Nothing exists. Needs a convergent signed log and an invite flow. |
| 3 Domain docs | Medium | Designed in the previous plan. The roster moves from the document to the team log. |
| 4 Seeds | Medium | Designed in the previous plan. The cross-team listing protocol is no longer needed. |
| 5 Replication | Large, risky | Nothing exists. Overturns "manual sync only" and the accept prompt for team documents. |
| 6 Reference | Small | Designed in the previous plan. Unchanged. |

Four findings shape the plan.

1. **Four statements in the request cannot work as written.** A key-derived peer ID does not stop anyone from claiming it on a PeerJS server (T1). "Lower hash wins" lets a removed owner undo their own removal (T10). "Body edits only by the owner" is checked against the *sender* today, so a relayed copy is always refused (T8). A signature over today's 48-bit entry id does not bind the entry's text (T6). Each has a fix in section 3.
2. **One mechanism covers three requirements.** A signed *release* entry (old D1) carries "highest valid signed version wins" for domain docs, "body edits only by the owner" for relayed blueprints and seeds, and tombstones. A tombstone is a final release.
3. **Entry order does not change.** "Union ordered by timestamp with a hash tiebreak" is exactly what the merge does now (T7).
4. **The convergence rules can be built and tested with no network.** The team log is a pure function from a set of operations to a state. Milestone R2 builds it as a library with a randomized simulation before any transport work.

There are four blockers, all in the design as literally requested, none in the feasibility of the work. Eight points need a product-owner answer (section 8).

---

## 2. Triage

Severity: **blocker** = the request as written cannot work or is unsafe; **decision** = workable, but someone must choose; **note** = worth knowing, no choice needed.

### 2.1 Overview

| # | Item | Severity |
|---|---|---|
| T1 | A key-derived peer ID can still be claimed by anyone who has the public key; the server checks nothing | blocker |
| T2 | With key-only IDs nobody can reach a person whose key they do not have (first one-to-one send) | decision |
| T3 | Changing the ID scheme cuts off installed peers, not through `proto` but through the ID check | decision |
| T4 | Feature 1 is half done: keys and pinning exist, key-derived IDs and signed entries do not | note |
| T5 | Signed comments: format, merge, CLI, agents, legacy unsigned files | decision |
| T6 | A signature over the 12-hex entry id can be moved to another text by brute force; this affects today's handoff signature too | blocker |
| T7 | Entry order and layout are unchanged by the request | note |
| T8 | "Body edits only by the owner" is sender-based today; any relay is refused | blocker |
| T9 | Team log: file, format, team identity | decision |
| T10 | "Lower hash wins" can be ground, and "concurrent" is declared by the signer, so a removed owner can win | blocker |
| T11 | "Concurrent conflicting" needs a definition; last owner, re-adding, concurrent ops of a removed owner | decision |
| T12 | `reassign-document` versus `owner:` and the handoff chain: which is the authority | decision |
| T13 | An old owner can keep releasing after reassignment unless the log fixes a cut-off | decision |
| T14 | Reassignment covers only documents the removing owner's replica holds | note |
| T15 | Invite: where the proof sits; the code prompt is in the way; only the joiner can dial | decision |
| T16 | Invite: the joiner's email is a bare claim unless the invite names it | decision |
| T17 | Invite single use: local list plus a rule in the log; several devices are out of scope | note |
| T18 | Automatic replication against "manual only", the accept prompt, and three non-goals | decision |
| T19 | Connection and probing cost grows with the square of the team size | decision |
| T20 | New wire messages, gated by `caps`; protocol stays 1 | note |
| T21 | "Published to the team" is a new concept: where the flag lives and who sets it | decision |
| T22 | Unsigned or outsider entries on a published document; entries by removed members | decision |
| T23 | Tombstones: a deleted file is "new" to the store today and comes back | decision |
| T24 | Blueprint revisions: replicate on every save or on an explicit action | decision |
| T25 | Store: where the replica lives; `.log` files; snapshots; duplicate ids | note |
| T26 | Size limits: 2 MiB per file holds; the team log needs incremental transfer | note |
| T27 | Config `team` salt, several teams per user, teams and rendezvous servers | note |
| T28 | Local state grows beyond "config, key, peers" | note |
| T29 | Financial examples: the same four lines as before, no new hits | note |
| T30 | Smaller conflicts with the spec and the previous plan | note |

### 2.2 Identity and peer IDs

**T1. A key-derived ID is still claimable (blocker).**
The request says the ID is derived from the public key "so nobody can pose as someone else on the rendezvous server". A PeerJS server accepts any well-formed ID from the first client that asks. `kalka rendezvous` checks only the ID's shape and length (`internal/rendezvous/signal.go:128`) and gives `ID-TAKEN` to a second client with another token (`signal.go:135`). The public PeerJS cloud and stock `peerjs-server` behave the same. A public key is public, and every team member has every other member's key from the team log. So any member, or ex-member, can register a teammate's key-derived ID first.

What the change does and does not give:

| Threat | Today (ID from email, §9.2) | ID from key, stock server | ID from key, `kalka rendezvous` with proof (D1) |
|---|---|---|---|
| Read or alter a sync as a man in the middle | Stopped: channel binding (§8.4, `docs/m1-transport.md` MITM test) plus pinning | Same | Same |
| Impersonate on first contact | Stopped only by the verification code (§8.3) | Same for strangers; team members get keys from the log, so no first-contact gap | Same |
| Claim someone's ID so they cannot come online | Anyone who knows the email and the `team` salt | Anyone who has the public key | Stopped |
| Lock a victim out of their pinned peers | Possible: a squatter answering with another key makes the dialer record `key_changed` (`internal/identity/identity.go:207`), after which syncs are refused before dialing (`internal/daemon/daemon.go:487-489`) until a manual `peers forget` | Stopped: the dialer checks ID against the presented key before pinning | Stopped |

So the real gains are the last row and a smaller set of possible squatters. Posing was already impossible. Full protection needs the server to check possession of the key. That check can be stateless (D1), but it works only on `kalka rendezvous`.

**T2. Reaching someone without their key (decision).**
Today you type an email and kalka derives the ID (`internal/transport/binding.go:21-24`, used for dialing at `daemon.go:537` and probing at `daemon.go:648`). With key-only IDs:

- *Team members:* keys are in the team log. No problem.
- *Invite flow:* the token carries the owner's key. No problem.
- *First one-to-one send to a colleague outside any team:* impossible. There is nothing to derive the ID from.

One-to-one sends are existing behaviour that the request says to keep. Either the email-derived ID stays as a way to find people, or first contact needs an out-of-band code. See D1.

**T3. What breaks for installed peers (decision).**
The strict `proto` check (`binding.go:87-90`) is not the problem, as long as the number stays 1. The problem is the ID check: each side requires that the peer's ID equals the ID derived from the claimed email (`binding.go:118-120`). An old peer dialed from a key-derived ID refuses the handshake. An old peer is also never registered under a key-derived ID, so a new peer cannot find it there.

A new peer must therefore keep its email-derived ID for as long as old peers exist, and use it for every connection with them. That is the same hybrid as in T2. Note that the product has not shipped publicly: the repository has no remote and CI has never run (IP §6h). The installed base is test machines. See Q4.

**T4. Is Feature 1 already done? (note)**

| Requirement | State | Evidence |
|---|---|---|
| ed25519 key generated on first run, no interaction, stored locally | Done | `identity.go:36-49`, written with owner-only permissions via `fsutil.WritePrivate` |
| Identity is email plus key; email alone is not trusted | Done for one-to-one: first-seen pinning, loud key change | `identity.go:192-212`, §8.2 |
| Peer ID from the public key | Not done | `binding.go:21-24` |
| Team operations and domain releases signed | Nothing to sign yet | — |
| Comments signed | Not done; only `handoff` | `internal/blueprint/entry.go:202-216`, §7.6 |

### 2.3 Signed entries and merge

**T5. Signing every entry (decision).**

- *Format.* The header already allows a seventh `sig:` field on any kind, and old peers keep it (`entry.go:99-103`, `:126-128`). No format break. Existing unsigned files stay valid.
- *Merge.* The signature is not part of the id (`entry.go:134-138`). The union keeps the first copy it sees, local first (`internal/merge/merge.go:157-163`, `:243-248`). So a peer holding an unsigned copy never takes the signed one, and a relay can strip a signature without changing the id. The union must prefer the copy whose signature verifies.
- *CLI.* `kalka comment` loads only the config (`cmd/kalka/main.go:220-296`). It can load `identity.key` from the same directory and sign without the daemon. The cost is that the CLI now needs read access to the key file.
- *Agents.* An agent that calls `kalka comment` gets signed entries for free. An agent that writes an entry by hand cannot sign. Such entries stay legal in the file and are marked "unsigned".
- *Which key.* For a team document, the key the team log binds to the author's email. For a one-to-one document, the pinned key from `peers.yaml`.
- *Laundering.* kalka must not sign entries it did not write. If the daemon signed every unsigned entry carrying the local email, a teammate could forge an entry in my name and my own kalka would sign it. Signing earlier entries is an explicit user action.

**T6. The 12-hex id is too short to sign (blocker).**
The handoff signature covers `kalka_id` and the entry id (`entry.go:202-204`). The id is 48 bits of a SHA-256 (`entry.go:137`). Finding another header and body with the same id takes about 2^48 hash computations, which is days on one graphics card. The attacker is free to vary the timestamp, the anchor, the `rev` and the body. A signed comment could be given a different text. Worse, an existing signed handoff by Alice could be given a different target, which is the plan takeover that handoff signatures were added to stop (§17.15). The previous plan noticed this for releases (old T1) but not for handoffs.

Fix in D3: a second signature context that covers the full 256-bit digest of the id input.

**T7. "Union ordered by timestamp with a hash tiebreak" (note).**
This is the current rule, word for word: de-duplicate by id, sort by `(timestamp, id)` (§7.2 step 6, `entry.go:143-149`, `merge.go:169`). The id is a content hash. There is no change in merge semantics or file layout. "Uniquely identified" is the existing id.

**T8. "Body edits only by the owner" fails on a relay (blocker).**
The merge accepts a changed plan only when the *sender* is the local copy's owner, or the new owner at the end of a handoff chain (`merge.go:91-107`). In replication the sender is usually a third member. Their copy of the owner's new body is refused with `not-owner` (`merge.go:106-107`). The request needs the opposite: "any member can relay any document, so no author or owner needs to be online".

The previous plan solved this for domain docs with a signed release entry (old D1). It must now apply to every team-shared document. See D4.

### 2.4 Team log

**T9. File and format (decision).**
A Markdown rendering is attractive for reading but poor for hashing: editors change line endings and whitespace, and a hash must cover exact bytes. The store indexes only `*.md` (`internal/store/store.go:112-122`), so a `.log` file is invisible to the blueprint list with no code change, which is what we want.

A team name is not an identity. Two teams can both be called `parcels`. The identity must be the hash of the `create-team` operation. See D5.

**T10. "Lower hash wins" is unsafe (blocker).**
Two problems, and the second is the serious one.

- *Grinding.* A signer chooses the content of their operation, including its timestamp. Trying 2^20 variants gives a hash lower than the opponent's with near certainty. In a mutual removal, whoever spends more computing wins.
- *Self-declared concurrency.* Whether two operations are "concurrent" follows from the hashes each one names as its predecessors. The signer writes those. An owner who has been removed can, a week later, sign a new operation that names the state *before* their removal as its predecessor. It then counts as concurrent with the removal. With a ground hash it wins, the removal is discarded, and the remover is removed instead.

So under the literal rule a hostile owner can never be removed. No rule fixes this completely without coordination: the request's own threat model accepts that an owner's key compromises the team. But the rule can make the outcome independent of computing effort and limit who can win. See D6.

**T11. What counts as "concurrent conflicting" (decision).**
The request gives one example. A full list is needed, and the edge cases must have defined results:

- *Two owners remove each other.* Exactly one removal may stand.
- *Three owners remove each other in a ring.* The result must not depend on arrival order and must leave at least one owner.
- *Last owner.* "Removing the last owner is invalid" is easy to check for one operation. Two concurrent removals that are each valid alone can together empty the owner set.
- *An owner removed while they sign other things.* Alice removes Bob; at the same time Bob, not yet knowing, adds Carol. Is Carol a member? If yes, a removed owner can keep adding people by back-dating (T10).
- *Re-adding.* A removed member who is added again must be able to act again, with operations that come after the new `add-member`.
- *Same subject, different values.* Two `assign-domain` for one domain; two `add-member` for one email with different keys.

See D6.

**T12. Reassignment versus `owner:` and the handoff chain (decision).**
Today ownership has one source: `owner:` in the frontmatter, changed by the owner, with signed handoff entries as proof for peers who missed it (§7.2 rule 4, `merge.Chain` at `merge.go:176-200`). Frontmatter is part of the plan (`internal/blueprint/document.go:499-501`), so only the owner can change the `owner:` line.

A removed member will not sign a handoff. A `reassign-document` in the team log is therefore a second way to move ownership, signed by a team owner and not by the document's owner. Until the new owner's kalka rewrites the `owner:` line, the file and the log disagree. The spec must say which one rules. See D7.

**T13. Releases by the old owner after reassignment (decision).**
The request says the domain owner "must match the team log's *current* `assign-domain`". If "current" is judged when a peer happens to verify, then version 7, released properly by the old owner, turns invalid on every peer the moment a new owner is assigned, and peers that learn of the change in a different order disagree. If instead a release stays valid forever once its signer was the owner, the old owner can go on releasing higher versions and win "highest version wins". The log has to fix a cut-off version. See D7.

**T14. Reassignment covers only what the remover has seen (note).**
The removing owner's kalka can list only the documents in its own replica. A seed the removed member published an hour ago may not have arrived. It stays owned by a non-member. Any team owner must be able to reassign such an orphan later. Until then the removed member's releases of that document are still accepted. The UI should show "owner is no longer a member".

### 2.5 Invite flow

**T15. Where the proof sits (decision).**
After `hello` and `auth`, every connection runs code verification (`daemon.go:416`, `internal/syncproto/syncproto.go:286-327`). For an unknown joiner the owner's side would raise "compare this code" with nobody at the keyboard, before any join message could arrive. The proof must therefore be known before that step.

"Both sides retry in the background" cannot be symmetric. The owner does not know the joiner's key, so cannot derive the joiner's ID. Only the joiner can dial. The owner's part is to be online. The joiner adds one address to its probe set (`daemon.go:682-700`) until the invite expires. A probe is one signaling message, not a full offer (`internal/transport/presence.go:45-67`), so the cost is small.

**T16. The joiner's email (decision).**
The joiner "presents email, public key, and proof of the secret". The secret proves they hold the token. Nothing proves the email. With auto-approval, whoever holds a leaked token joins under any email they like, including a colleague's. The request itself says "email alone is never trusted". The cheap fix is that the owner names the invitee when making the invite. See D8.

**T17. Single use (note).**
The token names the inviting owner's key, and the joiner dials that key's ID. So only that one installation can redeem it. Other owners never see the secret, and there is no double spending between owners. Several devices per person remain a non-goal (§1, §17.10); with key-derived IDs a second device is a second identity, and an invite belongs to the device that made it. A local list of open invites is enough, with one extra rule in the log as a backstop (D8).

### 2.6 Replication

**T18. Replication against decisions already taken (decision).**

| Decision today | Where | What the request needs |
|---|---|---|
| Sync is manual only | §18 Q1; old D2 allowed automatic sync for domain docs only | Automatic, continuous sync of all team-shared documents |
| A new document asks "accept?" | §7.2 step 2, `syncproto.go:550-566` | Files appear in the folder with no click |
| First contact asks both users to compare a code | §8.3, `syncproto.go:286-327` | Members trust each other's keys through the team log, with no code |
| "Asynchronous delivery" is a non-goal | §1 | Relaying through members is asynchronous delivery, without a server queue |
| "Version history, or any local VCS-like state" is a non-goal | §1 | A signed log, snapshots and tombstones |
| "Deleting a file simply removes it from the list" | §14 | A deleted replica file is fetched again |

None of these is wrong to change, but each must be changed in the spec on purpose. One-to-one sync keeps all its current rules.

**T19. Cost (decision).**
If every member syncs with every other member whenever both are online, a team of 20 holds 190 pairs, each a full WebRTC connection with handshake. IP §5 risk 5 already warns about probe volume, and the public cloud's limits are unknown. Since any member can relay, a full mesh is not needed. Each session should start with one digest of the whole inventory, so that two peers already in step finish in one round trip. See D9.

**T20. Wire messages and gating (note).**
The responder ignores unknown message types and waits 30 s (`syncproto.go:229-243`). So an old peer must never be sent a new message. `hello` is decoded with unknown fields ignored (`internal/transport/conn.go:133`), so an optional `caps` list can be added under protocol 1, as the previous plan proposed (old T13). New messages go only to peers that announce `team/1`. The responder today expects `sync_offer` as the first message (`syncproto.go:472`); it must dispatch on the first message type. A frame may be up to 16 MiB (`conn.go:27-29`), which is ample.

**T21. "Explicitly published to the team" (decision).**
There is no such flag today. It must be something only the owner can set, that travels with the file, and that a relay cannot forge. Frontmatter fits the first two, because it is part of the plan. A signed release covers the third. A document can belong to one team. A blueprint that is both published and shared one-to-one with an outsider must keep working for the outsider, who has no team log. See D10.

**T22. Entries that cannot be verified (decision).**
Three cases appear as soon as entries must be signed to replicate:

- *Legacy entries.* A blueprint reviewed before publishing holds unsigned comments from several people. The owner cannot sign for them.
- *Outsiders.* A one-to-one reviewer outside the team comments on a published blueprint. Their key is not in the team log.
- *Removed members.* A comment has a timestamp but no place in the log's causal order. If a peer refuses entries from a removed member only after it learns of the removal, two peers that learn things in a different order end with different files, forever.

See D11.

**T23. Tombstones (decision).**
When a responder finds no file for an offered `kalka_id`, the document is new (`syncproto.go:519-566`, §14). So today any stale copy brings a deleted file back. A tombstone must stay visible to replication after the file is gone. The store ignores dot-folders (`store.go:113`), so a tombstone kept there is invisible to it. Open points: who may delete, what the tombstone looks like, what happens to the entries, and what a plain `rm` by the user means. See D12.

**T24. When a blueprint revision replicates (decision).**
Owners edit blueprints with their own tools at any time (§1). If every save produced a signed release, an active blueprint would collect dozens of release entries that can never be removed. If nothing is released automatically, the team sees a body older than the owner's. See D4 and Q6.

**T25. Store (note).**

- The team log as `*.log` is not indexed. Good.
- Snapshots `…@v<N>.md` share the live document's `kalka_id` and would trip the duplicate check (`store.go:288-298`). Old T2 and old D4 still apply: ignore that name pattern.
- Received replica files need a home. Putting them in the folder root mixes them with the user's own work. See D9.
- Lookup of team documents must go by frontmatter, never by file name (old T17).

**T26. Size (note).**
The 2 MiB file limit (§6.9, `document.go:28`) holds for every replicated document. A signature adds about 93 bytes per entry. The team log has no natural bound; it must be exchanged as missing operations by hash, never as one file. Old T4 still applies: a long-lived domain doc cannot prune comments (old D3, old Q4, accepted).

**T27. Teams, the `team` salt and rendezvous (note).**
Old T7 and old D5 hold: the config `team` is a namespace for email-derived IDs and has nothing to do with teams. A user's teams are the team logs in their folder, so several teams per user need no config. One limit is new: a team lives on one rendezvous server, and an installation has one. A person in two teams on two servers cannot serve both. The invite token should carry the server's address, so that a mismatch is shown at join time.

**T28. Local state (note).**
§2.1 limits local state to config, key and peers. This request adds, in the config directory: open invites, pending joins, and what is learned about each peer's capabilities. In the folder it adds the team log, tombstones and snapshots. No per-document database is needed. §2.1 must be reworded.

### 2.7 Other

**T29. Financial examples (note).**
Rechecked every tracked file with a wider term list. The hits are the same four lines as old T18; there are no new ones.

| Path:line | Text | Action |
|---|---|---|
| `internal/blueprint/trace_test.go:142` | `text: Take orders` | Replace |
| `internal/blueprint/trace_test.go:144` | `text: Order API` | Replace |
| `internal/blueprint/trace_test.go:145` | `text: Fraud check` | Replace; the clearest hit |
| `docs/example-trace.md:48` | `why: storage costs` | Reword to "storage quota" |

`web/src/Views.tsx:48` says "not an account" about a login, not money; leave it. The trace mockups on the external design canvas were not checked. All new fixtures use parcel logistics and library lending.

**T30. Smaller conflicts (note).**

- *"Non-blocking" and rejection.* The request itself separates content validation from signature and role checks. The plan follows that: content gives hints at most; an invalid signed operation or release is not applied, and the UI says why. Today's refusal to send a file with structural errors (`daemon.go:498-500`) stays (old T16).
- *"Factory-agnostic" and seeds.* kalka assembles the seed file from typed text (old T10, old D7). Still the one case of kalka creating a document.
- *"Append-only except domain docs."* In kalka, append-only is a rule about the entry set (§7.2). An owner may already rewrite any body. So the exception is not needed (old T3). Tombstones are the real exception: they drop entries (D12).
- *Pinned key against team-log key.* If `peers.yaml` pins one key for an email and a team log binds another, kalka must not pick silently. Team traffic uses the log's key; the Peers view shows the mismatch loudly.
- *Handoff of a published document.* A handoff to a non-member would leave a team document owned by an outsider. The UI should refuse it, or offer to unpublish first.
- *Open question in the request:* manual accept for joins. Recorded as Q5.
- *Key rotation.* Not cheap: every signature check would need "which key was valid when". Future work, as the request allows. Until then a lost key means remove and re-invite.

---

## 3. Decisions and recommendations

**D1. Two IDs: a key-derived ID for everything known, the email-derived ID for first contact.** (T1, T2, T3)

- `key_id = "kalka-k" + hex(SHA-256("kalka-key-v1|" + pubkey))[0:32]`. No `team` salt.
- Each daemon registers both IDs, on two signaling connections. On the key ID, the handshake checks that the remote ID matches the presented key, before pinning. On the email ID, the handshake is as shipped.
- Dialing: if a key is known for the peer (team log or pin) and the peer is known to speak the new scheme, dial the key ID. Otherwise dial the email ID, as today. All team traffic uses key IDs only.
- `kalka rendezvous` accepts a key ID only with a proof: an extra query parameter holding the public key, a timestamp and a signature over the ID and timestamp. It checks the signature and that the ID matches the key. No state is kept. Stock servers ignore the parameter (to confirm in R5), and there the gain is limited to the "lockout" row in T1. The spec says so.

*Reason:* keeps one-to-one sends and old peers working, and gives the full gain where kalka runs its own server. *Alternative A:* key IDs only, first contact through a contact code passed by hand. Cleaner, and one connection instead of two, but typing an email no longer works and every old peer is cut off. *Alternative B:* keep email IDs only. No work, and it fails the request. This is Q1.

**D2. Feature 1 is delivered by D1, D3 and the team log.** (T4) No separate work on key generation or storage.

**D3. Entry signatures: one `sig` field, a new message, written on every entry.** (T5, T6)

- New context: `"kalka-entry-v2\n" + kalka_id + "\n" + hex(full SHA-256 of the id input)`. kalka writes it on every entry it creates, in the UI and in `kalka comment`.
- Verification tries v2, then the old handoff message. A v1 handoff signature is still accepted on documents that are *not* team-shared, as the limit it is today. Team rules accept v2 only.
- The merge prefers, among copies with one id, the copy whose signature verifies.
- Unsigned entries stay legal and are shown as "unsigned". kalka never signs an entry it did not write, except through an explicit "sign my earlier comments" action.

*Compatibility:* an old peer keeps the field and ignores it on comments. An old peer cannot verify a v2 handoff, so a handoff from a new peer to an old one shows as "unverified handoff" there. That is a reason to update peers together (Q4). *Alternative:* a second field `sig2:`. Rejected: the old parser fails on an unknown seventh field (`entry.go:100`) and would turn the entry into unparsed text.

**D4. Signed releases for every team-shared document.** (T8, T24; extends old D1)

```
### release · alice@corp.com · 2026-11-20T09:00:00Z · § · rev:<8 hex> · id:<12 hex> · sig:<base64url>
v7 · plan:<64 hex SHA-256 of the normalized frontmatter and body>
incorporates: 7c2e91ab04f3, a41b77c09e12
Renamed "consignment" to "parcel"; added the invariant on redelivery attempts.
```

- The signature covers the context, `kalka_id`, the version, the plan hash and the entry digest.
- Merge rule for a document with a `team:` key, when the local peer holds that team's log: take the incoming plan if its log holds a release that names the incoming plan's hash, has a higher version than the local one, and is signed by the document's *effective owner for that version* (D7). The sender does not matter. Unreleased edits do not travel.
- Domain docs carry `version:` in frontmatter, as requested. Blueprints and seeds carry the version in the release entry only.
- **Releases are made on an action, not on save.** For a domain doc that is "Release". For a blueprint or seed it is "Publish" and then "Publish update". kalka's own actions (promote a seed, hand off) release automatically. An owner with unreleased edits sees a banner.

*Alternative:* release automatically whenever the owner's saved body differs from the last release. Always current, but each save burst leaves a permanent entry. This is Q6.

**D5. The team log is `<team>-team.log`, one JSON object per line.** (T9)

- One operation: `{v, team, op, args, prev: [hashes], signer: <pubkey>, at, sig}` in a canonical encoding. Its hash is SHA-256 over the encoding without `sig`.
- `team` is the hash of the `create-team` operation. That hash is the team's identity. The name is a label, limited to `[a-z0-9][a-z0-9-]*` so it works in file names and references. Two teams with one name get a suffix in the file name, and a `domain:` reference to that name is shown as ambiguous.
- The UI renders the log as a readable history. `kalka team log` prints it. The file is not meant to be edited.

*Alternative:* a Markdown file with one entry per operation, reusing the review-log grammar. Readable in any editor, but hashing would depend on normalization, and the entry grammar has no place for predecessor hashes.

**D6. Replay rules: causal validity, removal wins, seniority breaks ties.** (T10, T11)

The state is a pure function of the *set* of operations with valid signatures. Proposed rules, to be fixed in R2:

1. *Causal validity.* An operation is considered only if all its predecessors are known and considered, and its signer held the needed role in the state built from its ancestors.
2. *Removal wins.* When a removal of X is effective, every operation signed by X that is not an ancestor of that removal is discarded. This is stricter than the request's "causally later": it also covers operations concurrent with the removal. Back-dating then gains nothing.
3. *Mutual removal goes to the senior owner.* Removals are evaluated in order of their signers' seniority. A removal is effective if its signer has not already been removed by an effective removal that is concurrent with it or precedes it, and if it leaves at least one owner. Seniority is the position of the operation that made the signer an owner: the creator first, then by causal depth, then by hash. The signer of a removal cannot influence it.
4. *Dependants fall with their parent.* An operation whose predecessor is discarded is discarded. `reassign-document` operations name their `remove-member` as a predecessor, so they are dropped with it, as the request requires.
5. *Same subject, different values* (two `assign-domain` for one domain, two keys for one email): the operation later in the replay order wins. Replay order extends causal order, with seniority and then hash among concurrent operations. Only here does the hash decide, and nothing of value is gained by grinding it.
6. *Re-adding.* A new `add-member` or `add-owner` after a removal starts a new term. Seniority counts from the new operation.

Results for the edge cases: mutual removal leaves the senior owner; a ring of three leaves at least one; two concurrent removals of the last two owners keep the senior one; Carol added by Bob while Bob is being removed is not a member.

What remains, and goes into the threat model: a removed owner who is *senior* to the remover can still fork and counter-remove. The most senior owner's key is the root of the team.

*Alternative:* the literal "lower hash wins". Simple, and it meets the letter of the request, but removal of a hostile owner never holds (T10). This changes a written requirement, so it is Q2.

**D7. The team log is a link in the ownership chain, with a cut-off version.** (T12, T13, T14)

- `reassign-document {doc, new owner, after: <version>}` and `assign-domain {domain, owner, after: <version>}` name the last release version the signing owner had seen.
- *Effective owner for version n:* start from the document's first owner. Signed handoff entries move ownership as today. An effective reassignment or assignment moves it for all versions above its `after`. Releases by the previous owner above the cut-off are void.
- For a team-shared document the chain, including the log, is the authority. The `owner:` line is what people read. When the new owner's kalka sees the reassignment, it writes the `owner:` line and makes a release. Until then the UI shows "owner: X, reassigned to Y, pending".
- If a reassignment is later discarded (rule 4), releases made under it become void. A peer holding one marks the document "ownership disputed" and takes the highest valid release it is offered. It does not rewrite its file without a valid replacement.
- `reassign-document` is valid only with the removal of the current owner as predecessor, or when the current owner is not a member (the orphan case of T14). A team owner cannot take a present member's blueprint.

*Alternative:* let any team owner reassign any document. Simpler rule, larger power; not asked for.

**D8. Invite: proof in `hello`, bound to an email, single use in two places.** (T15, T16, T17)

- Token (one line, base32): team name, team hash, inviting owner's key, invitee email, 32-byte secret, expiry (default 72 h, `--expires`), rendezvous address.
- `kalka team invite <email>` stores the open invite in `invites.yaml` in the config directory.
- `kalka join <token>` pins the owner's key as verified, stores the pending join, and probes the owner's key ID once a minute until the expiry.
- The joiner's `hello` carries `invite: {id, proof}`, where the proof is an HMAC of both public keys and the team hash under the secret. The owner checks it after `auth`. A good proof counts as verification, so no code prompt appears on either side. A bad, expired or used invite ends with a `bye` that says which.
- The owner appends `add-member {email, pubkey, invite: <id>}`, marks the invite used, and sends the log on the same connection. The joiner checks that the log starts at the team hash from the token and contains its own `add-member`.
- Backstop in the log: a second `add-member` with the same invite id is discarded at replay.
- The email in `add-member` is the one the owner wrote, not the one the joiner claims. A mismatch is refused.

*Alternative:* an invite with no email, as the request reads literally. One step less for the owner; a leaked token then joins under any name.

**D9. Replication runs as gossip between members, into a team subfolder, with no prompts.** (T18, T19, T25)

- *Who:* members of a team, by the keys in its log. No verification code between members: the invite and the log carry the trust.
- *When:* at daemon start, when a member comes online, after a local change to a team document, and every ten minutes with jitter. Each round picks at most three online members. A local change is pushed to at most five.
- *What a session does:* exchange log heads and one digest over the inventory. If equal, stop. Otherwise exchange missing log operations first, then inventory lines, then documents. Every operation, release and entry is verified before it is written.
- *Where files go:* received documents are created in `<folder>/<team>/`. A document the user published stays where it is; it is found by `kalka_id`.
- *Prompts:* none. A team document that fails verification is not written, and the Team view lists it with the reason.
- *Probing:* team members are probed only while a round needs them, not every minute.

*Alternative:* sync with every online member. Simplest to reason about, and fine up to about ten people.

**D10. Publishing is a frontmatter key plus a first release.** (T21)

- The owner publishes with `kalka publish <file> --team <team>` or the UI. kalka writes `team: <team>` and a release. Only a member can publish, and only to a team they belong to. One team per document.
- A document replicates to a peer only if it has `team:`, a valid release, and the peer is a member of that team. Everything else never enters an inventory. A request for an unknown or private `kalka_id` gets the same answer as a request for one that does not exist.
- Unpublishing is a tombstone (D12). Replicas cannot be recalled.
- One-to-one sync of a published document keeps working. A peer without the team's log applies the classic sender-based rule.
- A seed budded from a published parent is published to the same team by default.

**D11. Which entries replicate.** (T22)

- An entry replicates within a team if its v2 signature verifies against a key the log has *ever* bound to the author's email. This is a function of the entry and the log, so all peers agree. Entries by a removed member are shown as such.
- Unsigned entries, and entries signed by a key outside the log, stay in the files that have them, marked "local only". They do not enter the replica.
- On publishing, the UI lists entries that will stay local and offers each author "sign my earlier comments".

*Alternative:* refuse entries dated after the author's removal. Timestamps are written by the author, so this stops nobody. This is Q7.

**D12. A tombstone is a final release.** (T23)

- Deleting is done by the document's effective owner: `kalka delete <file>` or the UI. It makes a release with the next version, marked `deleted`, whose plan is a stub: the frontmatter and nothing else.
- It spreads like any release. No release above a tombstone is valid.
- On each peer the file leaves the folder. The stub goes to `<folder>/<team>/.kalka/tombstones/<kalka_id>.md`, and the last full copy to `<folder>/<team>/.kalka/trash/`. The replication layer reads the tombstone directory, so an offer of an older copy is answered with the tombstone.
- The stub keeps only the tombstone entry. This is the one place where valid entries are dropped, and the spec must say so.
- A plain `rm` of a replica file is not a tombstone. The file is fetched again, and the UI says how to delete for the team.
- Tombstones are kept forever. They are a few hundred bytes each.

*Alternative:* a member-signed `delete-document` operation in the team log. One place for all authority, but it lets every member write to the log, which the request reserves for owners.

**D13. Carried over unchanged from the previous plan.** Comments are kept forever and folded in the UI (old D3). Snapshots sit beside the live doc and are ignored by the index (old D4). The `team` salt and teams stay apart (old D5). The spec states which frontmatter keys kalka reads and writes (old D7), now adding `team`, `domain` and `deleted`. The deviation log is the set of `deviation` entries (old D8). kalka writes `created:` when it buds a seed (old D9). Budding details (old D10), with the `budded` entry now signed.

---

## 4. Milestones

Numbered R0 to R8. The mapping to old N0 to N4 is in section 7.

### R0. Spec update and review

1. New sections: Teams and roles (log, roles, validity, removal, invite); Team replication (scope, sync, merge rules, deletion); Domain documents; Seeds; Blueprint reference and hints; **Threat model**, with the three points of the request plus T1, D6 and D11; **Open questions / decisions**, holding section 8 and decisions D1 to D13.
2. Amend: §1 non-goals (asynchronous delivery, version history, authoring), §2.1 and §2.4, §3 glossary, §5, §6.2, §6.5 (`release`, `budded`, signatures on all kinds), §7.2 and §7.6, §8 (key IDs, team trust), §9.2, §9.3, §9.5 (`caps`, `invite`, new messages), §9.6 (ID proof), §9.7, §11.1, §12, §14, §16, §17, §18 Q1.
3. Replace the four lines of T29.
4. Bump the spec to v0.4.

Exit: the product owner has answered Q1 to Q8 or accepted the defaults; the spec is merged.

Size: 1 week. Files: `specification.md`, `implementation-plan.md`, `internal/blueprint/trace_test.go`, `docs/example-trace.md`.

### R1. Blueprint reference and hints

Unchanged from old N1: `BLUEPRINT-REFERENCE.md` with skill frontmatter and the six sections; `internal/reference` embedding it; `kalka reference export [-o <file>] [--force]` and `kalka reference version`; opt-in hints as info-level problems with a `hint-` prefix; dismissal in browser storage.

| | |
|---|---|
| Spec | Reference section, §11.1, §12 |
| Packages | new `internal/reference`; `internal/blueprint` (`hints.go`); `cmd/kalka`; `web/src/Review.tsx` |
| Wire and format | None on the wire. One optional frontmatter key read. |
| Tests | **`kalka reference export` (required):** exported bytes equal the embedded file; `-o` writes it; no overwrite without `--force`; the file declares its version. No hints without `reference:`; each hint alone; hints never set an exit code or stop a send. A test that the reference holds no term from the T29 list. |
| Size | 4 days |

### R2. Team log library and convergence harness (no network)

1. New package `internal/teamlog`: operation type, canonical encoding, hash, sign, verify.
2. `Replay(ops) -> State`: members, owners with seniority, domain assignments, document reassignments, used invites, and the list of discarded operations with reasons. Rules of D6.
3. Builders for the seven operations. `Remove` returns the `remove-member` together with its `reassign-document` operations.
4. Invite tokens: encode, decode, proof, expiry check. Pure functions; the clock is a parameter.
5. **Simulation harness.** Several replicas, each an operation set. Random generation of valid and hostile operations, including back-dated ones and ground hashes. Random delivery order, duplication and partitions. After each run: all replicas with the same set have the same state and the same discarded list; at least one owner exists; no operation by a removed signer is effective unless it precedes the removal.
6. A short design note, `docs/teamlog.md`, with the final rules and the cases the harness found.

| | |
|---|---|
| Spec | Teams and roles, threat model |
| Packages | new `internal/teamlog`; `internal/name` (signature contexts) |
| Wire and format | Defines the log format. Nothing reaches a peer yet. |
| Tests | **Required:** operations from non-owners rejected; operations from removed signers rejected; concurrent owner operations converge on all replicas; mutual owner removal; reassignment to the removing owner; reassignment discarded when the removal loses. **Added:** ring of three; last owner; re-adding; two keys for one email; duplicate invite id; back-dating and grinding gain nothing; replay is independent of input order (property test over permutations); fuzzing of the decoder. Token: expired, tampered, wrong team. |
| Size | 2.5 weeks |

Exit: the harness runs 100 000 random histories with no divergence, and the rules in the spec match the code.

### R3. Signed entries, releases and tombstones (format and merge, no network)

1. Entries: v2 signature (D3); `Entry.Verify(kalkaID, key)` trying v2 then v1; `NewEntry` takes a signer; `blueprint.Append` signs.
2. `kalka comment` loads the identity key and signs. `kalka check` reports signed, unsigned and bad-signature counts as info lines.
3. Merge: prefer the verified copy of an entry (`merge.go:157-163`, `:243-248`).
4. `release` entries: `ReleaseInfo()`, `NewRelease`, `Document.PlanHash()`, `LatestRelease`.
5. The release merge rule of D4 in `merge.Docs`, taking a team state from R2 through a small interface. `merge.Chain` gains team-log links and cut-off versions (D7).
6. Tombstones as final releases (D12), in the merge only.
7. Entry filter for team documents (D11).
8. UI and document model: badges "unsigned", "signature invalid", "by a removed member".

| | |
|---|---|
| Spec | §6.5, §7.2, §7.6, Replication merge rules |
| Packages | `internal/blueprint`, `internal/merge`, `cmd/kalka`, `internal/daemon/model.go`, `web/src/Review.tsx` |
| Wire and format | A `sig` on all new entries; a new kind `release`. Both are legal for old peers. An old peer cannot verify a v2 handoff (D3). |
| Tests | **Required, at merge level:** a domain release signed by a non-assigned owner is rejected; a tombstone beats a stale copy in both merge directions; reassignment moves ownership and its discard voids later releases. **Added:** a signature stripped by a relay is restored from a signed copy; a moved signature fails v2; relay of an owner's release is accepted, relay of an unreleased edit is not; a release above a cut-off is void; old unsigned files parse and merge as before; merge properties (idempotent, commutative on entries, version never goes down); golden files; the existing ten scenarios still pass. |
| Size | 2 weeks |

### R4. Seeds: format, budding, pre-backlog

Old N2, with three changes: the `budded` entry is signed; a seed budded from a published parent is published by default (once R7 exists); the pre-backlog view reads the local folder, which after R7 includes the whole team's seeds. There is no listing protocol.

1. Library: `status`, `parent`, `origin`, `created`; `IsSeed()`; `NewSeed(...)`; the `budded` kind.
2. Daemon: `Bud`, `Promote`, `Seeds()`. API: `POST /api/seeds`, `GET /api/seeds`, `POST /api/promote`.
3. CLI: `kalka seed <parent> [--from <entry id>] --title <t> [-m <text>]`, `kalka seeds [--json]`.
4. UI: "Create seed" on deviation entries and in the document menu; the `budded` entry as a link; a Pre-backlog view with owner, parent and age; "Promote to draft" for the owner.

| | |
|---|---|
| Spec | Seeds, §6.5, §11.1, §12 |
| Packages | `internal/blueprint` (`seed.go`), `internal/store`, `internal/daemon` (`seeds.go`), `cmd/kalka`, `web/src` |
| Wire and format | None on the wire. A seed is an ordinary blueprint to old peers. |
| Tests | **Seed budding with the parent backlink (required):** the seed has `status: seed`, one owner, `parent`, `origin`, `created` and a new id; the parent gains exactly one signed `budded` entry and its plan bytes are unchanged; the backlink survives a sync both ways. Also: budding from a reviewer's copy; retry after a failed second write; promotion by the owner only; handoff of a seed. |
| Size | 1.5 weeks |

### M6. Canvas review

As in IP §3 M6, with the acceptance criteria proposed there. Size 1.5 weeks. Placement is discussed in section 5.

### R5. Key-derived peer IDs and capabilities

1. `transport.KeyPeerID(pub)`. The daemon registers both IDs; the endpoint accepts on both.
2. Handshake: on a key ID, check the ID against the presented key before pinning. `hello` gains optional `caps` and `invite`. The daemon records per peer whether it speaks the new scheme.
3. Dialing and probing choose the ID per D1.
4. `kalka rendezvous`: check the proof for key IDs. Confirm that stock `peerjs-server` and the public cloud ignore the extra query parameter.
5. Status and Peers views show the key fingerprint. The §9.7 notice is reworded.

| | |
|---|---|
| Spec | §8.4, §9.2, §9.3, §9.5, §9.6, §9.7 |
| Packages | `internal/transport`, `internal/peerjs`, `internal/rendezvous`, `internal/daemon`, `internal/identity` |
| Wire and format | Protocol stays 1. Two optional `hello` fields. A second ID per daemon. Old peers are reached on the email ID exactly as today. |
| Tests | New to new over key IDs; new to old and old to new over email IDs, with an old-handshake fixture; a squatter on a key ID with another key fails with no `key_changed` record; `kalka rendezvous` refuses a key ID without a valid proof and accepts an email ID as before; the M1 transport suite passes unchanged. |
| Size | 1.5 weeks |

### R6. Teams and the invite flow

1. `kalka team create <name>`, `invite <email> [--expires]`, `members`, `remove <email>`, `add-owner`, `remove-owner`, `assign-domain <domain> <email>`, `log`. `kalka join <token>`.
2. The log file in `<folder>/<team>/<team>-team.log`, written atomically, re-read before write like any other file (§14).
3. `invites.yaml` and `joins.yaml` in the config directory, owner-only permissions.
4. Join handshake per D8, on both sides. The joiner's background retry until expiry, surviving a restart.
5. Trust: keys from a team log count as verified for that team's traffic. A clash with a pin is shown loudly (T30).
6. UI: a Team view with members, owners, domain owners, open invites and the rendered log; a Join dialog that takes a token; removal with a preview of the documents to be reassigned.

| | |
|---|---|
| Spec | Teams and roles, §8, §11.1, §12 |
| Packages | `internal/teamlog`, `internal/daemon` (new `teams.go`), `internal/syncproto`, `internal/transport`, `internal/identity`, `cmd/kalka`, `web/src` |
| Wire and format | `hello.invite`; a `team_log` message carrying operations, sent only to peers with `team/1`. |
| Tests | **Invite flow end to end (required)**, over real WebRTC on loopback as in `internal/daemon/scenario_test.go`: join succeeds with no prompt on either side and both logs hold the `add-member`; **expired token** refused with a clear reason; **reused token** refused, and a second `add-member` with the same invite id is discarded at replay. Also: owner offline, then online, and the joiner's retry completes; wrong email; token for another team; the rendezvous mismatch hint; a restart between `join` and meeting. |
| Size | 2 weeks |

### R7. Team replication

1. `syncproto`: the responder dispatches on the first message. A replication session: `team_sync {team, heads, digest}`, `team_log {ops}`, `team_inv {items}`, `team_want {ids}`, then documents as offers with intent `replicate`, with no accept prompt.
2. The replication set per D10, computed in one place and used for both inventory and requests.
3. Scheduling per D9: triggers, peer choice, limits, jitter.
4. Writing received documents to `<folder>/<team>/`; ownership updates after a reassignment (D7).
5. `kalka publish`, `kalka delete`; the tombstone directory and trash (D12).
6. Daemon reaction when the log changes: recheck held documents, mark "ownership disputed" or "owner is no longer a member".
7. UI: Publish and Publish update; a replication status line per team (last round, peers reached, documents refused and why); Deleted filter; the Pre-backlog view now shows the team's seeds.

| | |
|---|---|
| Spec | Team replication, §7, §9.3, §9.5, §14, §18 Q1 |
| Packages | `internal/syncproto`, `internal/daemon` (new `replicate.go`), `internal/store`, `internal/merge`, `cmd/kalka`, `web/src` |
| Wire and format | Four new messages and one new intent, all behind `team/1`. Old peers are never sent them and take no part in replication. |
| Tests | **Required, as scenarios with three or more daemons:** **private documents never leave** (a recording channel asserts that no private id, title or byte appears, and that a direct request for a private id is answered as unknown); **tombstones survive a stale peer** (Carol offline with v3, Alice deletes, Carol returns and syncs with Bob only; the file does not come back anywhere); **concurrent owner operations converge** over the network, including mutual removal across a partition; **reassignment to the removing owner** arrives on all peers, and is undone on all when the removal loses. Also: relay with author and owner offline; a forged release and a forged operation are not written and are reported; equal peers finish in one round trip; a one-to-one document and a published one side by side; an outsider's one-to-one sync of a published blueprint; a deleted replica file is restored; rate limits; the ten §16.1 scenarios unchanged. |
| Size | 3.5 weeks |

### R8. Team domain documents

Old N3 on the new base. The signed release (R3) and the transfer (R7) exist already, so this milestone is format, local actions and UI.

1. Library: `kind: domain`, `team`, `domain`, `version`; the `domain: <team>/<domain>@v<N>` reference.
2. Store: ignore `*@v<N>.md`; index kind, team, domain, version; `SaveSnapshot`, `FindDomain`, `Snapshot`.
3. Daemon: `Release(path, text, incorporates)` for the assigned owner; a snapshot on every release made or applied; the "newer domain version available" fields in the document view.
4. CLI: `kalka release <file> [-m <text>] [--incorporates <id>,...]`, `kalka domains [--json]`.
5. UI: Domains view; draft banner; Release dialog with a checklist of open comments; comments folded as "incorporated in v7"; version switcher over local snapshots; the dismissible indicator in blueprints.

| | |
|---|---|
| Spec | Domain documents, §6.5, §11.1, §12, §14 |
| Packages | `internal/blueprint` (`domain.go`), `internal/store`, `internal/daemon` (`domains.go`), `cmd/kalka`, `web/src` |
| Wire and format | None beyond R7. |
| Tests | **Domain releases rejected when signed by a non-assigned owner (required)**, as a scenario: Bob raises `version` and signs; no peer applies it and each says why. Also: release reaches an offline member through a relay; a new `assign-domain` with a cut-off voids the old owner's later release; snapshots never show as duplicates; a pinned version missing locally; the indicator appears and can be dismissed; comments marked incorporated. |
| Size | 2.5 weeks |

---

## 5. Order, size and M6

| Order | Milestone | Depends on | Size |
|---|---|---|---|
| 1 | R0 Spec update and review | — | 1 week |
| 2 | R1 Reference and hints | R0 | 4 days |
| 3 | R2 Team log library and harness | R0, answer to Q2 | 2.5 weeks |
| 4 | R3 Signed entries, releases, tombstones | R2 | 2 weeks |
| 5 | R4 Seeds | R3 | 1.5 weeks |
| 6 | M6 Canvas review | M3 (done) | 1.5 weeks |
| 7 | R5 Key-derived IDs and capabilities | R0, answers to Q1, Q4 | 1.5 weeks |
| 8 | R6 Teams and invite flow | R2, R5, answer to Q5 | 2 weeks |
| 9 | R7 Team replication | R3, R6, answers to Q3, Q6, Q7 | 3.5 weeks |
| 10 | R8 Team domain documents | R7 | 2.5 weeks |

About 19 weeks for one engineer, M6 included.

Why this order:

- **Spec first.** Six written decisions change (T18), and one requirement is replaced (Q2). A week of review is cheap against R7.
- **Reference second.** Smallest piece, no dependencies, and it defines words the later UI uses.
- **The two pure libraries before any network work.** R2 and R3 hold every rule that can make peers disagree. They are tested in milliseconds, with no WebRTC. If the rules turn out wrong, only library code changes.
- **Seeds before the network milestones.** Budding and the local view deliver value at once and need only signed entries. The team-wide view arrives with R7 at no extra cost.
- **Transport before teams.** The invite flow dials a key ID.
- **Domains last.** They need `assign-domain` and replication. Their own logic is small once both exist.

**Where M6 fits.** Canvas is UI only, depends on nothing here and still has no acceptance criteria (IP §5.8). It keeps the slot it had in the previous plan: after seeds, before the protocol work, which closes the original v1 scope. It can move to the end, or go to a second person at any time, with no effect on the rest.

R5 depends only on R0, so with two people it can run beside R2 to R4.

---

## 6. What is large or risky, and how to reduce the risk

| Risk | Why | Reduction |
|---|---|---|
| Team log convergence (R2) | A subtle rule set; a mistake splits teams for good, and the log cannot be patched afterwards | A pure library. The randomized harness with hostile signers runs before any other team code. Rules are written down in `docs/teamlog.md` and reviewed against the spec. The format carries a version field. |
| Rules change after logs exist | Old logs would replay differently | The `v` field in each operation selects the rule set. No real team is created before R2's exit. |
| Replication (R7) | Background writes to the user's folder; many interleavings; the first unattended network activity in kalka | The rules live in R2 and R3. R7 is transport and scheduling on top. Scenarios use the existing multi-daemon harness with the stepping clock and the fault hook (IP §6c). Everything is verified before it is written. |
| A private document leaks | The worst failure for trust in the tool | One function decides the replication set. A recording-channel test fails on any private byte. Requests for private ids look like unknown ids. |
| Rendezvous load | Unknown limits on the public cloud | Gossip with small fan-out (D9), a digest that ends idle sessions in one round trip, and a measurement of sessions per hour in the R7 scenarios. |
| Mixed versions | Old and new peers side by side | No protocol bump. New messages only behind `caps`. An old-handshake fixture in R5. Or one coordinated update (Q4). |
| Two signaling connections per daemon (D1) | More moving parts in the endpoint | R5 is its own milestone with the M1 suite as the regression test. |

---

## 7. Relation to the previous plan

| Previous | Now | Change |
|---|---|---|
| N0 Spec update | **Replaced** by R0 | Larger: teams, replication, threat model |
| N1 Reference and hints | **Kept** as R1 | None |
| N2 Seeds, local | **Kept** as R4 | The `budded` entry is signed; moved after R3; default publishing |
| N3a Domain format, release, snapshots | **Split:** release and merge rule into R3, the rest into R8 | The release rule now covers all team documents; the owner comes from `assign-domain`, not from `owner:` alone |
| N3b Domain distribution | **Replaced** by R7 | Roster-based push and catch-up give way to team replication |
| N3c Domain UI | **Kept** in R8 | Members and presence move to the Team view |
| N4 Pre-backlog across the team | **Dropped** | `seed_list` and `seed_fetch` are not needed: seeds are in the replica |
| — | **New:** R2, R5, R6, R7 | Team log, key IDs, invite flow, replication |

Previous decisions:

| Previous | Status |
|---|---|
| D1 signed release | Kept and extended (D4, D7): all team documents, signer from the team log, cut-off versions, full digest |
| D2 automatic sync for domain docs, verified peers only | Replaced by D9: all team documents, trust from the team log |
| D3 comments kept and folded | Kept |
| D4 snapshots beside the live doc | Kept; file name becomes `<team>-<domain>-domain@v<N>.md` |
| D5 two meanings of `team` | Kept. The `members:` frontmatter list is dropped; the roster is the team log |
| D6 seed listing messages | Dropped. `caps` in `hello` is kept and reused (T20) |
| D7 frontmatter principle | Kept, with `team`, `domain`, `deleted` added |
| D8, D9, D10 | Kept |

Previous answers: Q2, Q4, Q5 and Q6 stand. Q1 was accepted for domain docs only and is asked again as Q3 for all team documents. Q3 (seed visibility) is replaced by explicit publishing (D10). The reference form changes from `domain: <team>@v<N>` to `domain: <team>/<domain>@v<N>`.

---

## 8. Questions for the product owner

Each has a default; work proceeds on the default unless answered.

**Answered 2026-11-13: all eight defaults accepted.** This plan replaces `docs/plan-domains-seeds-reference.md`.

1. **Peer IDs.** Keep the email-derived ID beside the new key-derived one, so that a first send to a typed email still works and old peers stay reachable? Full protection against ID squatting then exists only on `kalka rendezvous`. *Default: yes, both IDs (D1).* The alternative is key IDs only, with first contact through a code passed by hand.
2. **Conflict rule.** The request says "the operation with the lower hash wins". That lets a removed owner undo the removal by computing (T10). May it be replaced by "removal wins; between two owners removing each other, the one who has been an owner longer wins", with the hash only as a last tiebreak? *Default: yes (D6).*
3. **Automatic replication.** May kalka sync all team-shared documents in the background, with no click, no accept prompt and no verification code between team members, writing received files into `<folder>/<team>/`? This overrides §18 Q1 for team documents. *Default: yes (D9).*
4. **Installed peers.** Are there installations that must keep working without an update? *Default: yes, stay compatible: protocol 1, new features behind `caps`, with the one known gap that an old peer cannot verify a handoff signed by a new one (D3).* If no, one coordinated update is simpler and removes the email-derived ID and the old signature.
5. **Joining.** (a) Should an optional manual "accept" step exist for joins? (b) Should an invite name the invitee's email? *Default: (a) no setting in this version, recorded in the spec as open; (b) yes, required (D8).*
6. **Blueprint revisions.** Do an owner's edits to a published blueprint reach the team only on "Publish update", or automatically on save? *Default: on the action (D4).*
7. **Comments by removed members.** Accept a signed comment if its key was ever in the roster, and mark it "by a removed member"? Strict refusal cannot be made consistent across peers. *Default: yes (D11).*
8. **Reassignment power.** May a team owner reassign only documents of removed or absent members, and never take a present member's document? *Default: yes, limited (D7).*
