# kalka — a pull request for implementation plans

The text and speaker notes of the team presentation about kalka, one section per slide. The rendered slides are in [kalka-presentation.pdf](kalka-presentation.pdf). Rule IDs on slide 13 are real; see [rules.md](../rules.md).

---

## 1. kalka

*Team-local · serverless · built for agents*

A pull request for implementation plans.

**Speaker notes:** Kalka is Polish for tracing paper: the sheet you lay over a drawing to trace, annotate and pass around. That's the idea: a light layer for working on plans together before they become code.

## 2. Why

**AI made writing code cheap. The plan is now the cheapest place to review.**

A wrong turn caught in a plan costs a comment. Caught in a pull request, it costs a rewrite.

**Speaker notes:** One point only: when agents write most of the code, the expensive part is checking that it does the right thing. The plan is where that check is cheapest, so kalka makes plans as easy to review as code.

## 3. Team-local, serverless, for short-lived plans

**kalka is a working layer for plans:**

- It runs on each member's machine; there is no central server.
- There is nothing to host or maintain: the optional rendezvous server stores nothing.
- It's made for plans that live while they're discussed.

**kalka is not a place to keep things:**

- Not a wiki: Confluence stays.
- Not a tracker: Jira stays.
- Not code review: pull requests stay.

Once a plan is accepted, it moves on: into the repository, the ticket or the wiki.

**Speaker notes:** Blueprints are volatile by design. kalka doesn't compete with Confluence or Jira; it covers the short phase where a plan is drafted, argued about and agreed. That's also why it can be serverless: losing a finished plan from kalka loses nothing, because it already moved on.

## 4. A blueprint: one Markdown file, three parts

~~~markdown
---
kalka_id: 5b0e7c1d-…
kalka_owner: ewa@example.org
kalka_team: parcel-ops
kalka_domain: parcel-ops/delivery@v3
---
# Redelivery after a failed attempt
## Context
## Plan
```trace … ```   ```invariants … ```
## Review log
### comment · marek · §plan
### deviation · marek · §plan
### release · ewa · v2 · sig:…
~~~

- **Frontmatter:** identity, owner, team, and the domain version the plan was written against.
- **The plan:** free Markdown by the owner, plus optional typed blocks kalka understands.
- **Review log:** signed entries that are only ever added: comments, deviations, replies, releases.

**Speaker notes:** It stays an ordinary Markdown file: any editor or agent can write it, and it's readable without kalka. kalka writes only a few frontmatter keys and the review log. The typed blocks (trace, invariants) are where structure helps review; everything else is free text.

## 5. Ownership: only the owner changes the plan

Everyone else adds entries. A release is what makes a version official.

1. **Write:** the owner writes the plan in any editor, often with an agent.
2. **Publish:** it becomes v1 and reaches every team member.
3. **Review:** teammates add comments and deviations to sections.
4. **Release:** the owner releases v2, listing the comments it includes.

- Unreleased edits stay on the owner's machine.
- Ownership moves by handoff; team owners can reassign.
- Every entry and release is signed.

**Speaker notes:** This is the rule that makes a serverless design possible: one writer for the plan, many writers for the log. Two reviewers never conflict, because comments are only ever added. If the owner goes on holiday, they hand the plan off, or a team owner reassigns it.

## 6. Teams: every plan belongs to a team

**Getting in:**

1. A team owner creates the team and shares an invite link in your team's chat.
2. You click it; kalka joins once the inviter is online.
3. Join as many teams as you need and switch at the top of the window.

Drafts stay in **Personal**, on your machine, until you publish.

**Two roles:**

| Action | Owner | Member |
|---|---|---|
| Publish, comment, split seeds | Yes | Yes |
| Invite and remove members | Yes | No |
| Add co-owners | Yes | No |
| Reassign any plan | Yes | No |
| Accept plans from other teams | Yes | No |

**Speaker notes:** Teams are the only way to share; there is no one-to-one mode. A plan can be copied or moved to another team, and the receiving team's owner accepts it. Invites are one-time links that also carry the inviter's key, which is how trust starts.

## 7. Seeds: scope creep becomes a seed, not a bigger plan

1. **Deviation:** review finds work beyond the plan's scope.
2. **Split off:** one click creates a small seed that links back to its plan.
3. **Seeds list:** a team-wide pre-backlog with owner, parent and age.
4. **Promote:** when someone picks it up, the seed becomes a draft.

Plans stay small enough to review in one sitting, and good ideas found along the way aren't lost.

**Speaker notes:** A seed is just a blueprint with `kalka_status: seed` and a link to its parent. It's a lightweight place for "we should also…" that would otherwise either bloat the plan or disappear in a thread.

## 8. Domains: the team's shared language, versioned

Domain documents hold the ubiquitous language from DDD. Each domain has its own versioned document, and every plan names the version it was written against.

- **Glossary** (terms, definitions, retired names): terms in a plan show their definition on hover. Using a retired name is flagged: "consignment" is now "parcel".
- **Invariants** (rules that must always hold): for example, "I1: a parcel is delivered at most once". Flagged: an invariant no plan step guards, or one with no test.

If the domain moves on to a newer version, plans written against an older one say so.

**Speaker notes:** The domain owner releases new versions like any plan. Members propose changes as comments. When a newer domain version exists, plans written against an older one show it. Glossary and invariants are typed blocks, so kalka can check them deterministically.

## 9. Trace blocks: the same process at two levels, checked

| Business | Code |
|---|---|
| B1 · Parcel arrives at the depot | C1 · Scan handler (implements B1) |
| B2 · Attempt delivery | C2 · Retry scheduler (no business step) |
| B3 · Notify the recipient | C3 · Deliver, then notify in one call (implements B2 and B3) |

kalka computes the mismatches:

- **Code only:** C2 has no business step. Intended?
- **Merged:** C3 does B2 and B3 together.

**Speaker notes:** Business explanations lose exactly the details that matter for implementation. A trace keeps both views side by side and links them; kalka computes where they diverge instead of trusting the author to say so. Other checks: steps nothing implements, one step split across many, reordering, and code references that don't resolve. Agents write traces; people read them.

## 10. Two layers: meet, then talk directly

```
                 ┌────────────────────────────┐
                 │ Rendezvous                 │
                 │ signaling + relay          │
                 │ stores nothing             │
                 └─────┬────────┬────────┬────┘
      1 · signaling    ┆        ┆        ┆
                 ┌─────┴──┐ ┌───┴────┐ ┌─┴──────┐
                 │ Ewa    │═│ Marek  │═│ Ola    │
                 └────────┘ └────────┘ └────────┘
                 2 · documents flow peer to peer
```

- **Layer 1 · Rendezvous:** introduces peers and relays traffic when UDP is blocked. It keeps nothing. Self-host it, or try the public PeerJS cloud.
- **Layer 2 · Peer to peer:** an encrypted WebRTC channel between two members. Plans, comments and team changes travel only here.

**Speaker notes:** Layer 1 is a PeerJS-compatible signaling server with a built-in TURN relay for networks that block UDP. It holds state in memory only. Layer 2 is kalka's own protocol over a WebRTC data channel. The rendezvous never sees documents; even when it relays traffic, the channel is end-to-end encrypted and bound to both members' keys.

## 11. Trust: no accounts, no server to trust

- **Keys, one per install:** each installation creates an ed25519 key on first run. Nobody ever handles it.
- **Team log, membership is signed:** members and roles live in a signed log that only grows. Every peer replays it to the same result, with no coordinator.
- **Channel binding, no one in the middle:** both sides sign the connection with their keys, so even a compromised rendezvous can't sit between them.

Only people who share a team can connect at all. The invite link carries the inviter's key; that's where trust starts.

**Speaker notes:** Email is a name, never an identity: identity is email plus key, bound in the team log. Removing someone is a signed operation too. Limits worth knowing: a removed member keeps what they already synced, and one key means one device for now.

## 12. Replication: whoever is online passes it on

Every member holds a full copy of the team's plans. The author doesn't need to be online.

1. **09:00, Ewa releases v2,** then closes her laptop.
2. **09:01, Marek receives it:** his kalka was running in the tray.
3. **14:00, Ola comes online** and gets v2 from Marek, not Ewa.

- Syncs at start, when someone comes online, after a change, and every 10 minutes.
- Releases are signed, so it doesn't matter who relays them.
- Starts at login and keeps running in the tray.

**Speaker notes:** This is why there is no server: the team itself is the storage. As long as members overlap online now and then, everyone converges. Each document and the team log merge deterministically, so peers end up identical whatever order things arrive in.

## 13. Agents: built for plans that agents write

**`BLUEPRINT-REFERENCE.md`** is shipped with kalka as an agent skill: what a useful plan contains. Domain reference, scope, verification plan, critical sections, operations.

```
$ kalka check plan.md --json
warning G020  line 42
“consignment” is a retired name for “parcel”
warning I030  line 57
no trace step guards invariant I2
exit 2 · same result on every machine
```

Agent writes → `kalka check` → agent fixes → publish → people review what's flagged.

**Speaker notes:** kalka reads meaning only from frontmatter and typed blocks: trace, glossary, invariants. Never from prose. That keeps the check deterministic: no network, no daemon, same output everywhere. The point: by the time a person opens a plan, the mechanical problems are already fixed.

## 14. In short

- A pull request for **plans**, before the code exists.
- Team-local and serverless: **nothing to host** or maintain.
- Structure where it helps review: **traces, domains, invariants**.

Questions?

**Speaker notes:** Leave time for questions. If people ask about rollout: it installs per user without admin rights, and a team can start on the public PeerJS cloud before hosting its own rendezvous.
