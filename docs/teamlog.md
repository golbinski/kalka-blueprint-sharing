# The team log: encoding and replay rules (rule version 2)

This note fixes what the specification (§19.1, §19.4, §20.4) leaves to the implementation: the exact encoding of an operation, and the replay rules as `internal/teamlog` implements them. It also records what the convergence harness found while the rules were being settled. The specification and this note agree; where the specification leaves a detail open, this note and `internal/teamlog` settle it.

## 1. Encoding

One operation per line of `<team>-team.log`, as JSON:

```json
{"args":{"email":"bob@example.org","pubkey":"…"},"at":"2026-11-20T09:00:00Z","op":"add-member","prev":["…"],"sig":"…","signer":"…","team":"…","v":2}
```

- **Canonical form.** Keys in lexicographic byte order at every level, no whitespace, strings escaped as Go's `encoding/json` does with HTML escaping switched off, integers only (`v`, `after`). `prev` sorted and without duplicates.
- **One line per operation.** A line must be exactly its canonical form, byte for byte; any other spelling of the same JSON (whitespace, key order, escapes, a key twice) is refused. So a line is an operation, and the hash can be taken from the line.
- **Hash.** Lowercase hex SHA-256 of the line with its `sig` member removed (the canonical form without `sig`). It is the operation's identity, and computing it needs no knowledge of the operation's kind. The team's identity is the hash of its `create-team`.
- **Signature.** Ed25519 over `"kalka-teamlog-sig\n" + hash`. The context is the same for every rule version, so a peer can check an operation it cannot replay. `signer` and `sig` are base64url without padding, as are the keys in `add-member`.
- **Decoding** is strict: unknown fields, unknown argument keys, missing arguments, non-integer numbers, bad hashes or keys, and an unsorted `prev` are refused. A line over 8 KiB is refused.
- **Newer rule versions.** An operation whose `v` is above the version a `kalka` implements is checked as far as the envelope goes (`team`, `prev`, `signer`, `sig`, the canonical form and the signature) and then kept as its exact line: stored, passed on to peers and counted among the heads, but not replayed. It is *pending* ("made by a newer kalka"), and so is every operation built on it, until an update can read it. Nothing else of the state changes. An operation of an older version than this `kalka` reads is refused. A signed newer operation also counts as binding its signer's descendants when a peer decides whether to accept operations from a session (a newer version may bind keys in ways an older one cannot see).
- **Arguments by kind.**

| `op` | `args` |
|---|---|
| `create-team` | `name`, `email` |
| `add-member` | `email`, `pubkey`, optional `invite` (32 hex) |
| `remove-member` | `email` |
| `add-owner` | `email` |
| `remove-owner` | `email` |
| `reassign-document` | `doc`, `from`, `owner`, `after`, `plan` (64 hex, empty when `after` is 0) |
| `accept-transfer` | `doc`, `owner`, `transfer` (64 hex) |
| `decline-transfer` | `doc`, `transfer` |

`reassign-document` names `from`, the owner the signer saw. It is kept for the history and for notices; replay does not judge it (rule 9). `after` and `plan` name the document state the signer held: that is how a reassignment establishes which releases came before it (spec §20.4). `transfer` is the digest of the offer's `transfer` entry (spec §20.7).

- **Unsupported versions.** An operation with `v` other than 2 decodes, is kept and passed on, and is *pending* (with everything that builds on it). It never makes a peer diverge from a newer peer by being discarded.

## 2. Terms and views

- A **member term** starts with an effective `add-member` (or the `create-team`, for the creator) and binds one email to one key. It ends with an effective `remove-member` of it.
- An **owner term** starts with an effective `add-owner` (or the `create-team`) of a member, and ends with an effective `remove-owner`, or when its member term ends. A grant made when the target was already an owner *in the signer's view* joins that term. Two concurrent grants make two terms, and the owner counts as long as one stands.
- An operation's **view** is the set of its ancestors (through `prev`). "In its view" means "granted by an ancestor and not ended by an ancestor".

## 3. Replay rules

The state is a pure function of the set of known operations (operations with a valid signature). Operations whose predecessors are not all known are *pending*. The others are visited in a fixed topological order: by causal depth, then by hash.

1. **Authority.** An operation counts only if its signer holds an owner term in its view. Every operation in the log is an owner's: there is no member-signed operation.
2. **Removal wins.** When a removal of X is effective, every non-removal operation X signed under the ended term and that is not an ancestor of the removal is discarded. That includes operations concurrent with it. Back-dating gains nothing.
3. **Seniority decides between removals.** Removals are decided in order of their signers' seniority (rule 5). A removal is blocked if an already accepted removal ended its signer's term without having seen it. Otherwise it is accepted, and it stands even if a *less* senior owner removes its signer concurrently. So:
   - In a mutual removal, the senior owner stays.
   - A junior owner can still remove a senior owner who did not counter-remove them. In that case a concurrent removal by the senior owner, of someone else, still holds.
4. **Support, not ancestry.** A tempting rule, "an operation with a discarded predecessor is discarded", is not used (see §5, finding 1). An operation stands or falls on its own support: the signer's owner term and the target's member term. Both must be effective and in its view.
5. **Seniority** is a chain. The creator's is empty. An owner appointed by grant `g` has the appointer's chain followed by `(depth(g), hash(g))`. Chains compare lexicographically, and a prefix comes first. An owner therefore always ranks above everyone they appointed, and no owner can create someone senior to themselves. With several grants or terms, the best chain counts. A new term after a removal starts a new chain (re-adding).
6. **Never without an owner.** A removal that would leave no owner is discarded. If the effective removals together empty the owner set, the least senior of them is discarded, and replay runs again. The harness shows that this case occurs (§5, finding 3).
7. **No self-removal.** An owner cannot remove themselves. Stepping down means asking another owner, which also keeps the last owner from leaving.
8. **Same subject.**
   - `add-member` for an email that is a member *in the signer's view* is discarded. Two concurrent `add-member` for one email, one key or one invite id: the first in visiting order wins and the other is discarded ("added concurrently"). That covers the "two keys for one email" and "duplicate invite" cases (spec rule 7).
   - `reassign-document` for one document: an operation whose `after` is lower than that of an effective reassignment of the same document in its view is discarded ("stale cut-off"). Of effective ones with the same `after`, the last in visiting order counts. Those with different `after` all count; each is an anchor of the release chain (spec §20.4). `State.Reassigned` therefore holds every effective reassignment of a document, ordered by `after`.
   - `accept-transfer` for one `transfer`, or for one `doc`: the first in visiting order counts, the others are discarded ("already decided"). Per document too, so a second offer forged with the same `kalka_id` cannot bring a second version 1. A `decline-transfer` is discarded when an `accept-transfer` for the same `transfer` counts.
   - These are the only places where the hash decides, and it decides nothing about who holds power: any team owner can sign a later operation.
9. **Reassignment** counts when its signer holds an owner term in its view and the new owner is a member in its view. It needs no removal: a team owner may reassign a present member's document. `from` is not checked. Removing a member emits reassignments of their documents to the remover; they do not cite the removal, and if the remover loses a mutual removal, rule 2 discards them with the remover's term.
10. **Transfer decisions** count when the signer holds an owner term in its view; for `accept-transfer` the named `owner` must be a member in its view. Replay does not look at the document: whether the offer itself is valid is checked by the document layer (spec §20.7).

### How the replay computes this

Rules 2, 3 and 6 refer to each other: whether a removal holds depends on the removals of its signer, which depend on seniority, which depends on grants that removals may discard. The code resolves this by a monotone iteration:

1. Decide the removals greedily in seniority order (rule 3), using a *structural* seniority computed from the graph alone.
2. Evaluate every operation in visiting order with those decisions. Then apply rule 2's discards, and evaluate again until nothing more is discarded.
3. If a removal accepted in step 1 turns out invalid, it is marked lost, and the iteration starts again from step 1. The same happens if one ranks below a removal of its signer it did not see, or if rule 6 is violated. The lost set only grows, so the iteration ends.

## 4. Invites

- The token is base32 (no padding; case and whitespace ignored on input) over `"kalka-invite-v1\n"`, the JSON record and the first 4 bytes of SHA-256 of both. The record holds name, team, owner key, the inviter's email, the invitee's email, a 32-byte secret, the expiry and the rendezvous address.
- **Links.** `kalka://join/<token>`. `ParseJoin` takes a link or a bare token, in any case, with surrounding whitespace or angle brackets, at most 4 KiB, and refuses another scheme, another path, extra path parts, a query or a bad checksum (spec §19.7).
- The **invite id** is the first 16 bytes of `SHA-256("kalka-invite-v1 id\n" + secret)`, in hex. It goes into `add-member`.
- The **proof** is HMAC-SHA-256 under the secret over `"kalka-invite-v1 proof\n" + team + "\n" + owner key + "\n" + joiner key`. It is bound to both parties, so a proof relayed to another key fails.
- `Invite.Check` refuses a wrong team, an expired invite, another owner key, or a bad proof. Single use is enforced by `invites.yaml` and by rule 8.

## 5. The harness, and what settling the rules found

`sim_test.go` plays random histories with six actors. Each starts from a small team that everyone holds. Actors sign operations from their own replica's view, reassignments and transfer decisions among them: honest ones, back-dated ones (`prev` from an earlier snapshot), ground ones (the lowest hash of 16 variants), and pointless ones such as non-owners signing. Operations spread by random partial gossip. Every replica, and the union, must satisfy:

- there is at least one owner;
- no counting operation was signed under a term that a removal ended without seeing it, except a removal ranked above that removal;
- every counting reassignment and transfer decision has a signer with an owner term in its view, and a new owner who is a member in its view;
- of the reassignments of one document, none counts with an `after` lower than a counting one in its view;
- at most one `accept-transfer` counts per transfer, and no `decline-transfer` counts beside it;
- the state does not depend on input order;
- outside the rule 8 conflicts, the roster and the owner order do not depend on the visiting order. This was checked by replaying with salted tie-breaks, so neither the hash nor timing gains anything.

Findings while the rules were settled. Findings 2 and 3 came from the harness. Findings 1, 4 and 5 came from working through attacks on the draft rules, and the harness now guards them:

1. **"Dependants fall with their parent" lets a removed owner poison the log.** A removed owner signs a back-dated operation. Honest peers merge it, and their next operations name it in `prev`. Under that rule every later honest operation would be discarded, removals included; replay uses support checks instead (rule 4). The scenario test `TestBackdatedOperationLoses` keeps it fixed.
2. **Concurrent grants to one person made the owner order depend on visiting order.** Two owners promoted the same member at the same time, and the member's rank came from whichever grant was visited first. Seniority now takes the best over all of an owner's terms (rule 5).
3. **The greedy decision alone can empty the owner set.** In 44 of 100 000 histories, the removals accepted in seniority order left no owner. Rule 6 is therefore a necessary part of the replay, not only a check on single operations.
4. **Seniority by depth alone could be gamed.** An owner could back-date a grant to a sock puppet, giving it a small depth and so a high rank. The chain form of rule 5 makes an appointee always rank below their appointer.
5. **Self-removal made rule 6 depend on visiting order.** Two last owners stepping down concurrently were resolved by visiting order. Self-removal is now refused (rule 7).

Run the harness at scale with:

```sh
KALKA_SIM_N=100000 go test ./internal/teamlog -run Harness -v -timeout 2h
```
