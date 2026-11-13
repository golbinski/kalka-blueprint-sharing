# The team log: encoding and replay rules (rule version 1)

This note fixes what the specification (§19.1, §19.4) leaves to the implementation: the exact encoding of an operation, and the replay rules as `internal/teamlog` implements them. It also records what the convergence harness found while the rules were being settled. Where this note and the specification disagree, the code and this note describe the intended behaviour, and the specification has been updated to match.

## 1. Encoding

One operation per line of `<team>-team.log`, as JSON:

```json
{"args":{"email":"bob@example.org","pubkey":"…"},"at":"2026-11-20T09:00:00Z","op":"add-member","prev":["…"],"sig":"…","signer":"…","team":"…","v":1}
```

- **Canonical form.** Keys in lexicographic byte order at every level, no whitespace, strings escaped as Go's `encoding/json` does with HTML escaping switched off, integers only (`v`, `after`). `prev` sorted and without duplicates.
- **Hash.** Lowercase hex SHA-256 of the canonical form *without* `sig`. It is the operation's identity. The team's identity is the hash of its `create-team`.
- **Signature.** Ed25519 over `"kalka-teamlog-v1\n" + hash`. `signer` and `sig` are base64url without padding, as are the keys in `add-member`.
- **Decoding** is strict: unknown fields, unknown argument keys, missing arguments, non-integer numbers, bad hashes or keys, and an unsorted `prev` are refused. A line need not be byte-identical to the canonical form (the hash is taken over the re-encoding), but a reordered `prev` is refused, so two different lines never stand for one operation. A line over 8 KiB is refused.
- **Arguments by kind.**

| `op` | `args` |
|---|---|
| `create-team` | `name`, `email` |
| `add-member` | `email`, `pubkey`, optional `invite` (32 hex) |
| `remove-member` | `email` |
| `add-owner` | `email` |
| `remove-owner` | `email` |
| `assign-domain` | `domain`, `owner`, `after` |
| `reassign-document` | `doc`, `from`, `owner`, `after` |

`reassign-document` names `from`, the owner the document is taken from, so replay can check that this person was removed (§3, rule 9) without knowing the document.

- **Unsupported versions.** An operation with `v` other than 1 decodes, is kept and passed on, and is *pending* (with everything that builds on it). It never makes a peer diverge from a newer peer by being discarded.

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
4. **Support, not ancestry.** Spec v0.4 said "an operation with a discarded predecessor is discarded". That rule is gone (see §5, finding 1). An operation stands or falls on its own support: the signer's owner term, the target's member term, the removal a reassignment relies on. All of these must be effective and in its view.
5. **Seniority** is a chain. The creator's is empty. An owner appointed by grant `g` has the appointer's chain followed by `(depth(g), hash(g))`. Chains compare lexicographically, and a prefix comes first. An owner therefore always ranks above everyone they appointed, and no owner can create someone senior to themselves. With several grants or terms, the best chain counts. A new term after a removal starts a new chain (re-adding).
6. **Never without an owner.** A removal that would leave no owner is discarded. If the effective removals together empty the owner set, the least senior of them is discarded, and replay runs again. The harness shows that this case occurs (§5, finding 3).
7. **No self-removal.** An owner cannot remove themselves. Stepping down means asking another owner, which also keeps the last owner from leaving.
8. **Same subject.**
   - `add-member` for an email that is a member *in the signer's view* is discarded. Two concurrent `add-member` for one email, one key or one invite id: the first in visiting order wins and the other is discarded ("added concurrently"). That covers the "two keys for one email" and "duplicate invite" cases (spec rule 7).
   - `assign-domain` and `reassign-document` for one subject: the last effective one in visiting order wins.
   - These are the only places where the hash decides, and it decides nothing about who holds power.
9. **Reassignment** counts only if an effective `remove-member` of `from` is in its view, and the new owner is a member in its view. If the removal is discarded, the reassignment falls with it, by rule 4 rather than by ancestry. A team owner cannot take a present member's document: without a removal of `from`, there is nothing to support it.
10. **Domain assignment** requires the new domain owner to be a member in the signer's view.

### How the replay computes this

Rules 2, 3 and 6 refer to each other: whether a removal holds depends on the removals of its signer, which depend on seniority, which depends on grants that removals may discard. The code resolves this by a monotone iteration:

1. Decide the removals greedily in seniority order (rule 3), using a *structural* seniority computed from the graph alone.
2. Evaluate every operation in visiting order with those decisions. Then apply rule 2's discards, and evaluate again until nothing more is discarded.
3. If a removal accepted in step 1 turns out invalid, it is marked lost, and the iteration starts again from step 1. The same happens if one ranks below a removal of its signer it did not see, or if rule 6 is violated. The lost set only grows, so the iteration ends.

## 4. Invites

- The token is base32 (no padding; case and whitespace ignored on input) over `"kalka-invite-v1\n"`, the JSON record and the first 4 bytes of SHA-256 of both. The record holds name, team, owner key, email, a 32-byte secret, the expiry and the rendezvous address.
- The **invite id** is the first 16 bytes of `SHA-256("kalka-invite-v1 id\n" + secret)`, in hex. It goes into `add-member`.
- The **proof** is HMAC-SHA-256 under the secret over `"kalka-invite-v1 proof\n" + team + "\n" + owner key + "\n" + joiner key`. It is bound to both parties, so a proof relayed to another key fails.
- `Invite.Check` refuses a wrong team, an expired invite, another owner key, or a bad proof. Single use is enforced by `invites.yaml` and by rule 8.

## 5. The harness, and what settling the rules found

`sim_test.go` plays random histories with six actors. Each starts from a small team that everyone holds. Actors sign operations from their own replica's view: honest ones, back-dated ones (`prev` from an earlier snapshot), ground ones (the lowest hash of 16 variants), and pointless ones such as non-owners signing. Operations spread by random partial gossip. Every replica, and the union, must satisfy:

- there is at least one owner;
- no counting operation was signed under a term that a removal ended without seeing it, except a removal ranked above that removal;
- every counting reassignment has an effective removal of `from` in its view;
- the state does not depend on input order;
- outside the rule 8 conflicts, the roster and the owner order do not depend on the visiting order. This was checked by replaying with salted tie-breaks, so neither the hash nor timing gains anything.

Findings while the rules were settled. Findings 2 and 3 came from the harness. Findings 1, 4 and 5 came from working through attacks on the draft rules, and the harness now guards them:

1. **"Dependants fall with their parent" lets a removed owner poison the log.** A removed owner signs a back-dated operation. Honest peers merge it, and their next operations name it in `prev`. Under spec v0.4 rule 4, every later honest operation would be discarded, removals included. The rule was replaced by support checks (rule 4). The scenario test `TestBackdatedOperationLoses` keeps it fixed.
2. **Concurrent grants to one person made the owner order depend on visiting order.** Two owners promoted the same member at the same time, and the member's rank came from whichever grant was visited first. Seniority now takes the best over all of an owner's terms (rule 5).
3. **The greedy decision alone can empty the owner set.** In 44 of 100 000 histories, the removals accepted in seniority order left no owner. Rule 6 is therefore a necessary part of the replay, not only a check on single operations.
4. **Seniority by depth alone could be gamed.** An owner could back-date a grant to a sock puppet, giving it a small depth and so a high rank. The chain form of rule 5 makes an appointee always rank below their appointer.
5. **Self-removal made rule 6 depend on visiting order.** Two last owners stepping down concurrently were resolved by visiting order. Self-removal is now refused (rule 7).

Run the harness at scale with:

```sh
KALKA_SIM_N=100000 go test ./internal/teamlog -run Harness -v -timeout 2h
```
