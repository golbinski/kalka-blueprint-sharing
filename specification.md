# kalka — peer-to-peer blueprint sharing

*Kalka* (Polish: tracing paper, carbon copy) is the working name. It appears in the binary, the plugin prefix (`kalka-plugin-<scheme>`), the frontmatter key (`kalka_id`) and protocol strings. Keep the name in one constant per component so a later rename is cheap; availability of the name has not been checked.

Specification v0.3 · 2026-11-12 · status: ready for implementation

This document is the hand-over to the implementation agent. Sections 1–15 are normative. Section 16 gives milestones and acceptance criteria. Section 17 records what changed during the final review and why. Section 18 lists questions only the product owner can answer; each has a default so work is not blocked.

Wording: **must** is a requirement, **should** is a strong default the implementer may deviate from with a written reason, **may** is optional.

---

## 1. Purpose and scope

A *blueprint* is a single Markdown file describing an implementation plan. `kalka` lets teammates pass a blueprint to each other for review or handoff, directly between their machines, and get comments back inside the same file. It is the equivalent of a pull request for an implementation plan.

`kalka` is agnostic about how blueprints are produced and consumed. Each teammate may use a different workflow or agent pipeline to write a blueprint and to act on it afterwards. `kalka` assumes nothing about section names, frontmatter fields or pipeline steps. The shared folder is an exchange area: a blueprint is dropped in for grooming and moved back out to its owner's own pipeline once accepted.

### Goals

- Use without thinking: one binary, three settings, drop a file in a folder.
- No server-side storage. The only server is a stateless rendezvous point.
- One artifact: the blueprint `.md` file is the whole state of a review.
- Peers other than the owner can only add entries, never change the plan.
- References to code, tickets and docs are resolved by plugins; the core knows nothing about GitHub, Jira or Confluence.

### Non-goals (v1)

- Authoring or editing blueprints. Users and agents write them with their own tools.
- Asynchronous delivery. Both peers must be online; handoff is agreed verbally first.
- Version history, branching, or any local VCS-like state.
- More than one device per email address.
- A Jira or Confluence plugin. Only a GitHub reference plugin ships.
- Real-time co-editing or live cursors.

---

## 2. Design principles

1. **The file is the state.** Everything needed to continue a review is in the `.md` file. `kalka` keeps no per-blueprint database, sidecar or history. Local application state is limited to configuration, the identity key and the list of known peers.
2. **Grow-only log.** The plan body is changed only by the owner. Everyone else adds entries to a review log. Entries are never edited or removed by `kalka`.
3. **The server is not trusted.** The rendezvous server only introduces peers. Peer identity is verified end to end.
4. **Core is concept-free.** The core handles files, peers and entries. Anything domain-specific lives in a plugin. The core knows nothing about the workflow that produced a blueprint: no assumed sections, no pipeline steps, no meaning attached to frontmatter beyond its own two keys.
5. **Windows is a first-class platform.** Primary users work on Windows with PowerShell. Paths, line endings, file locking and `.exe` lookup must work there first.

---

## 3. Glossary

| Term | Meaning |
|---|---|
| Blueprint | One Markdown file with frontmatter, a body and an optional review log. |
| Body | Everything after the frontmatter, excluding the review log section. |
| Review log | The section headed `## Review log`, containing entries. Managed by `kalka`. |
| Entry | One comment, deviation or handoff record in the review log. |
| Owner | The person named in frontmatter `owner:`. The only one allowed to change frontmatter or body. |
| Peer | A teammate running `kalka`, addressed by email. |
| Sync | The single network operation: two peers exchange a blueprint and both end with the same merged file. |
| Rendezvous | The signaling server (PeerJS protocol) that lets two peers find each other. |
| Rev | Short hash of the normalized body. Tells which body an entry was written against. |

---

## 4. Architecture

```
 Owner's machine                     Rendezvous                 Reviewer's machine
┌──────────────────────────┐   ┌──────────────────┐   ┌──────────────────────────┐
│ blueprints/  (folder)    │   │ PeerJS signaling │   │ blueprints/  (folder)    │
│      │                   │   │ (+ TURN, opt.)   │   │      ▲                   │
│      ▼                   │   │ stateless        │   │      │                   │
│ kalka daemon ── plugins     │┄┄▶│                  │◀┄┄│ kalka daemon ── plugins     │
│      │                   │   └──────────────────┘   │      │                   │
│ Web UI (localhost)       │                          │ Web UI (localhost)       │
└──────────┬───────────────┘                          └───────────┬──────────────┘
           └────────── WebRTC data channel (DTLS, P2P) ───────────┘
```

Components:

- **`kalka` daemon** — one Go process. Watches the folder, serves the web UI on localhost, maintains the rendezvous connection, runs syncs, invokes plugins.
- **Web UI** — single-page app embedded in the binary. Reads and comments; triggers sync and handoff.
- **CLI** — the same binary. File-only commands work without the daemon; network commands talk to the running daemon.
- **Plugins** — separate executables named `kalka-plugin-<scheme>`, JSON over stdio.
- **Rendezvous** — any PeerJS-protocol server: the public PeerJS cloud (demo), stock `peerjs-server`, or `kalka rendezvous` (same binary, adds TURN).

---

## 5. Configuration

File: `%APPDATA%\kalka\config.yaml` on Windows, `$XDG_CONFIG_HOME/kalka/config.yaml` (default `~/.config/kalka/`) elsewhere. If it is missing, the UI opens on a setup form with exactly three required fields.

```yaml
email: gracjan@corp.com                 # required: who you are
rendezvous: wss://kalka.corp.example/      # required: or the literal "peerjs-cloud"
folder: C:\Users\gracjan\blueprints     # required: where blueprints live

# optional, never asked for during setup
team: ""                # shared string mixed into peer IDs (section 9.2)
ui_port: 7717           # localhost port
ice_servers: []         # manual override of ICE configuration
```

Also stored in the config directory, never in the blueprint folder:

- `identity.key` — ed25519 private key, generated on first run, file mode 0600 (on Windows: ACL restricted to the current user).
- `peers.yaml` — known peers: email, public key, `verified` flag, first-seen date.
- `plugins/` — optional directory searched before `PATH`.

---

## 6. Blueprint file format

### 6.1 Structure

```markdown
---
kalka_id: 6f1c2a0e-8d1b-4c57-9a43-0e2b7c9d51aa
owner: alice@corp.com
repo: org/doc-service        # opaque to the core; read by plugins
base_sha: 3f9c2a1
---

# Import pipeline refactor

## Context
…

## Plan
See [the parser](gh:src/import/parse.py#L120-L180) and [PROJ-123](jira:PROJ-123).

## Review log

### comment · bob@corp.com · 2026-11-11T17:40:03Z · §plan · rev:3fa9c1d2 · id:7c2e91ab04f3
Doesn't handle the retry path when an upload is resumed halfway.

### deviation · bob@corp.com · 2026-11-12T09:12:40Z · §plan · rev:3fa9c1d2 · id:a41b77c09e12
Moved the size check into the upload handler because …

### handoff · alice@corp.com · 2026-11-12T10:02:11Z · § · rev:3fa9c1d2 · id:19d0c3e2b7aa
→ bob@corp.com
```

### 6.2 Frontmatter

- YAML between `---` lines at the top of the file.
- `kalka` reads and writes exactly two keys: `kalka_id` (UUID v4) and `owner` (email, compared case-insensitively).
- All other keys are **pass-through**: never interpreted, validated or required by the core, whatever their names, types or nesting. They are preserved byte for byte on disk, travel with the file to peers, and are handed to plugins as an opaque map. A blueprint with any frontmatter, or none, is valid.
- The `repo` and `base_sha` keys in the example above are an instance of this: they mean something to the `gh` plugin only.
- A file without `kalka_id` is a *local* blueprint: listed in the UI, not yet shareable.
- **First send:** `kalka` adds `kalka_id` and, if absent, `owner: <config email>`, creating the frontmatter block if the file has none. When editing frontmatter `kalka` must change only those lines and must not re-serialize the rest (key order and comments stay intact).

### 6.3 Body

Everything after the frontmatter except the review log section. Free-form Markdown; the core gives it no meaning beyond headings (for anchors) and links (for references).

### 6.4 Review log

- Exactly one H2 with the text `Review log` (case-insensitive). Its extent runs to the next H1/H2 or to end of file. It should be the last section, but `kalka` must cope if another tool has appended sections after it.
- If absent, `kalka` creates it at end of file when the first entry is added.
- Content between entries that does not parse as an entry is preserved and shown as "unparsed" in the UI. It never blocks a sync. It belongs to the entry before it (or to the log heading, if it comes before the first entry) and moves with that entry when the log is rewritten in canonical order (7.2). It is not part of that entry's body or `id`.

### 6.5 Entries

Header line, strict:

```
### <kind> · <author-email> · <timestamp> · §<anchor> · rev:<8 hex> · id:<12 hex>
```

- Separator is ` · ` (space, U+00B7, space).
- `kind`: `comment` (a remark), `deviation` (a proposed or actual departure from the plan as written, recorded instead of editing the body) or `handoff`. Only `handoff` has behaviour in the core; the other two differ in label and styling alone. Unknown kinds matching `[a-z][a-z0-9-]*` must be preserved, merged and rendered generically, so a team's own workflow can introduce kinds without changes to `kalka`.
- `timestamp`: UTC, RFC 3339, second precision, `Z` suffix.
- `anchor`: slug of the body heading the entry refers to, or empty (`§`) for a general entry. Slug rule: lower-case; drop everything except letters, digits, spaces and hyphens; spaces to hyphens; duplicates get `-1`, `-2`… in document order (GitHub's algorithm). A node inside a trace block (6.10) is addressed as `§<slug>#<node-id>`, where the slug is the section containing the block.
- `rev`: first 8 hex characters of SHA-256 over the normalized body (6.6) at the time of writing.
- `id`: first 12 hex characters of SHA-256 over `kind \n author \n timestamp \n anchor \n rev \n normalized entry body`. An entry whose `id` does not match its content is invalid and is dropped on merge with a visible warning.
- `sig` (handoff entries only): an optional seventh field ` · sig:<base64url>`, with no padding, holding an ed25519 signature by the author's identity key over `"kalka-handoff-v1\n" + kalka_id + "\n" + id`. `kalka` always writes it on handoff entries. It is not part of the `id` input. On any other kind it is preserved but ignored.

Entry body: Markdown up to the next entry header or the end of the log.

- Headings inside an entry body must be level 4 or deeper. The UI and `kalka comment` demote shallower headings on input.
- Optional first line `re: <id>` marks a reply to another entry.
- Optional leading blockquote (`> …`) quotes the body text the entry targets; the UI uses it to highlight the passage if it still exists.
- `handoff` body is the single line `→ <new-owner-email>`.

Entries are immutable. A correction is a new entry with `re:`.

### 6.6 Normalization and hashing

Used for comparison and hashing only; never changes what is on disk.

1. Decode as UTF-8; strip a leading BOM.
2. Convert CRLF and lone CR to LF.
3. Strip trailing spaces and tabs from every line.
4. Strip leading and trailing blank lines of the unit being hashed (body, frontmatter, or entry body).

On write, `kalka` preserves the file's existing line-ending style (CRLF if the file predominantly uses it) and writes UTF-8 without BOM.

### 6.7 Derived display states

Computed on the fly, stored nowhere:

- **Outdated** — entry `rev` differs from the current body rev.
- **Orphaned** — entry anchor matches no heading in the current body, or names a trace node that no longer exists.

### 6.8 Leaving the exchange folder

A blueprint is expected to be moved or copied out of the folder once it has been groomed and accepted. Nothing in `kalka` depends on it staying. The file remains ordinary Markdown: the two frontmatter keys and the review log are harmless to other tools and can be kept or deleted by whatever consumes the file next. `kalka` offers no export or clean-up step in v1.

### 6.9 Limits

File at most 2 MiB; entry body at most 64 KiB. Larger inputs are rejected with a clear message.

### 6.10 Trace blocks

A trace block describes the same process at two or more levels of abstraction and links the levels. Its purpose is to make visible where a high-level description (the idea) and its formalization (typically code) stop lining up, because those are the details a reviewer needs and prose loses.

It is a fenced code block with the info string `trace`, containing YAML. It is part of the body, so only the owner can change it. It is intended to be written by an agent and read by people; `kalka` renders it and never edits it.

````markdown
```trace
lanes: [business, code]
nodes:
  - {id: B1, lane: business, text: Receive uploaded files, next: [B2]}
  - {id: B2, lane: business, text: Extract text, next: [B3]}
  - {id: B3, lane: business, text: Classify the document, next: [B4]}
  - {id: B4, lane: business, text: Notify the user}

  - {id: C1, lane: code, text: Upload endpoint handlers, realizes: [B1],
     ref: "gh:src/upload/handlers.py#L40-L120", next: [C2]}
  - {id: C2, lane: code, text: Skip files larger than 20 MB,
     ref: "gh:src/import/size_guard.py#L12-L48", next: [C3],
     why: the parser runs out of memory on larger files}
  - {id: C3, lane: code, text: Extract and classify in one pass,
     realizes: [B2, B3], ref: "gh:src/import/parse.py#L200-L310", next: [C4],
     why: avoids parsing each file twice}
  - {id: C4, lane: code, text: Batch and rate-limit per user,
     realizes: [B4], ref: "gh:src/notify/batch.py#L15-L90"}
```
````

**Schema**

| Key | Where | Meaning |
|---|---|---|
| `lanes` | block | Ordered list of two or more free-form lane names, most abstract first. The core attaches no meaning to the names. |
| `nodes` | block | Flat list of nodes. |
| `id` | node | Required. `[A-Za-z][A-Za-z0-9_-]*`, unique within the block; should be unique within the section so anchors are unambiguous. |
| `lane` | node | Required. One of `lanes`. |
| `text` | node | Required. One short line. |
| `next` | node | Optional. Ids of nodes in the *same* lane that follow this one. An item may be `{to: <id>, label: <text>}` to name a branch. |
| `realizes` | node | Optional. Ids of nodes in any *more abstract* lane that this node implements. |
| `ref` | node | Optional. A reference as in 10.1, resolved by plugins. |
| `why` | node | Optional. Reason this node exists or differs from the more abstract view. |

Unknown keys on the block or on a node are ignored by the core and left in place.

**Validation** (`kalka check`, and on render): YAML parses; ids unique; every `lane`, `next` and `realizes` target exists; `next` stays within one lane; `realizes` points to a more abstract lane; at most 40 nodes per block (authors should aim for 20 or fewer and use several blocks). An invalid block is shown as a plain code block with the error beside it. It never blocks a sync.

**Divergences** are computed by `kalka` from the graph, never declared by the author:

| Class | Definition |
|---|---|
| Implementation-only | A node outside the most abstract lane with no `realizes`. |
| Unrealized | A node outside the least abstract lane that nothing realizes. |
| Merged | One node realizes two or more nodes. |
| Expanded | One node is realized by two or more nodes. |
| Scattered | The nodes realizing one node do not form a connected run in their own lane's flow. |
| Reordered | A precedes B in the abstract flow, but a realizer of B precedes a realizer of A in the lower flow and not the other way round. |
| Unverified | A node in the least abstract lane has no `ref`, or its `ref` has no plugin or fails to resolve. |
| Stale | A plugin returned a warning for the node's `ref`. |

The last two depend on the reader's plugins and may differ between peers.

A trace shows that referenced code exists, not that the mapping is correct. The divergence list exists to make each claim quick for a reviewer to check.

Fenced blocks with the info string `mermaid` are rendered as diagrams by a bundled renderer (no network access) and carry no semantics for `kalka`. They remain the place for free-form sketches.

---

## 7. Sync, ownership and handoff

### 7.1 One operation

Every exchange is a **sync**: the initiator sends its file, the responder merges and replies with the merged file, the initiator merges the reply and acknowledges. Sending for review, returning comments, pushing a revised plan and handing off are all the same operation. After a successful sync both files are equal under normalization.

### 7.2 Merge algorithm

`merge(local L, incoming I, sender S)` → new local file or a rejection. `L` may be absent.

1. **Parse** `I`. Not parseable, over limits, or missing `kalka_id`/`owner` → reject `malformed`.
2. **New blueprint** (no local file with this `kalka_id`): ask the user to accept (shows sender, title, size). Declined → reject `declined`. Accepted → result is `I`; file name is the sender's suggested name, sanitized, with a numeric suffix if taken.
3. **Same plan** (frontmatter and body of `I` equal those of `L` after normalization): result is `L` with the entry union (step 6).
4. **Owner change of plan** — accept `I`'s frontmatter and body if either holds:
   - `S` equals `L.owner`; or
   - `S` equals `I.owner` and `I`'s log contains a chain of `handoff` entries leading from `L.owner` to `S`.
   Result is `I`'s frontmatter and body with the entry union.

   **Handoff chain:** walk the handoff entries of the entry union in canonical order, starting with `current = L.owner`. An entry is a link only if its author equals `current` and its `sig` verifies against the key pinned for that author (8.2). A link sets `current` to the entry's target. The chain leads to `S` if `current` equals `S` at any point of the walk. A handoff entry that is not a link stays in the log as an ordinary entry; the UI marks it "unverified handoff". If a signature cannot be checked because no key is pinned for its author, the rejection says so and names the author to sync with.
5. **Otherwise** reject `not-owner`. The rejection carries the responder's current file so the sender can converge (7.5).
6. **Entry union**: all valid entries from both sides, de-duplicated by `id`, sorted by `(timestamp, id)`. Unparsed log content from the side whose body was kept is preserved.

Properties the implementation must test: merge is idempotent, commutative on the entry set, and never drops a valid entry.

"Append-only" is therefore a rule about the *set of entries* (it only grows), not about bytes. The log section is rewritten in canonical order on each merge.

### 7.3 Ownership

- The owner is whoever `owner:` names in a peer's local copy.
- Only the owner's edits to frontmatter or body propagate. The UI shows non-owners a read-only body.
- If the watcher sees a non-owner's file whose body no longer matches what the owner last sent, nothing is blocked locally; the next sync is rejected with `not-owner` and the peer is offered convergence (7.5).

### 7.4 Handoff

"Hand off to…" in the UI (owner only), or `kalka handoff`:

1. Build a candidate in memory: `owner:` set to the recipient, plus a `handoff` entry.
2. Sync with `intent: handoff`. The recipient is prompted: "alice@ wants to hand *Import pipeline refactor* over to you."
3. Only after the recipient's acknowledgement is the candidate written to the sender's disk. Declined or failed → the sender's file is untouched.
4. A retry after a lost acknowledgement is harmless: the recipient already has the identical file (rule 3) and acknowledges again.

Editing `owner:` by hand and syncing also works (rule 4, first branch), without the write-after-ack protection or the log entry. The UI should warn when it detects this.

### 7.5 Convergence after rejection

When a sync is rejected with `not-owner` and the reply carries a file that satisfies rule 4 from the sender's point of view, the sender's client offers: "Your copy differs from the owner's plan. Replace your plan with theirs? Your entries are kept." On confirmation it applies the merge, first saving the displaced file as `<name>.conflict-<timestamp>.md` beside it. This covers stale reviewers after a handoff, accidental local body edits, and the lost-ack fork.

If neither side can accept the other's plan, both see: "Plans diverged; owner is `<x>` — sync with the owner."

### 7.6 Trust model for entries

Entries are not signed. A peer accepts any well-formed entry from any authenticated peer, including entries authored by third parties (that is how reviewers see each other's comments). The `id` hash catches accidental corruption, not forgery. This protects against outsiders, not against a teammate deliberately impersonating another. That is an accepted limit for v1 for comments, deviations and unknown kinds; per-entry signatures can be added for them later in the same way as `sig`. **Handoff entries are the exception:** they move ownership, so they are signed (6.5) and verified against pinned keys (7.2). A forged handoff cannot take over a plan.

---

## 8. Identity and trust

### 8.1 Keys

Each installation generates an ed25519 key pair on first run. The user never handles it.

### 8.2 Pinning

- First contact with an email: the peer's public key is stored in `peers.yaml` as unverified.
- Later contacts must present the same key. A different key blocks all syncs with that email until the user removes the old pin and re-verifies. The UI must make this state loud.

### 8.3 Verification code

For each peer pair: `code = base32(SHA-256("kalka-sas-v1" ‖ min(pkA, pkB) ‖ max(pkA, pkB)))`, first 8 characters shown as `XXXX-XXXX`. Both sides see the same code.

- First sync with an unverified peer requires both users to confirm the code once (they are on a call anyway, per the verbal-handoff assumption). Confirmation sets `verified: true`.
- `kalka verify <email>` shows the code on the CLI.

### 8.4 Channel binding

After the data channel opens and before any blueprint data flows:

1. Each side sends `hello {proto, email, pubkey, nonce}`.
2. Each side sends `auth {sig}` where `sig` signs `"kalka-auth-v1" ‖ own DTLS certificate fingerprint ‖ peer DTLS certificate fingerprint ‖ peer nonce`.
3. Each side verifies the signature against the pinned (or first-seen) key, using fingerprints read from the DTLS transport itself, not from the SDP relayed by the server.
4. Each side checks that the peer ID it dialed or was dialed from equals the ID derived from the claimed email (9.2).

Any failure closes the connection. This makes a malicious or compromised rendezvous server unable to sit in the middle.

---

## 9. Rendezvous and transport

### 9.1 Signaling protocol

The client must speak the PeerJS server WebSocket protocol so that one client works against the public PeerJS cloud, stock `peerjs-server` and `kalka rendezvous`. Message types include `OPEN`, `OFFER`, `ANSWER`, `CANDIDATE`, `HEARTBEAT`, `LEAVE`, `EXPIRE`, `ERROR`, `ID-TAKEN`.

- Only the signaling protocol is PeerJS. What flows over the data channel is `kalka`'s own protocol (9.5); there is no interoperability with browser PeerJS clients and none is required.
- Treat the `peers/peerjs-server` source as the authority for URL shape, query parameters, heartbeat interval and ID character rules. Do not rely on this document for those details.
- `github.com/muka/peerjs-go` is an existing Go port of client and server on Pion (Apache-2.0). Evaluate it in M1; adopt, vendor or reimplement the signaling part. It is small.

### 9.2 Peer IDs

```
peer_id = "kalka-" + hex(SHA-256("kalka-peer-v1|" + team + "|" + lowercase(email)))[0:32]
```

No directory is needed: to reach a teammate, derive their ID and connect. The server refuses a second connection with an ID already in use (`ID-TAKEN`); the UI reports this as "this email is already online elsewhere".

### 9.3 Presence

PeerJS servers do not list peers by default. Presence is learned by probing: a connection attempt to an offline ID fails quickly. The daemon probes the peers in `peers.yaml` plus addresses typed this session, at most once a minute each, and on demand before a sync.

### 9.4 ICE

Order of precedence:

1. `ice_servers` from config, if set.
2. `GET <rendezvous-http-origin>/kalka/ice` — served by `kalka rendezvous`, returns ICE servers with short-lived TURN credentials. A 404 is normal for other servers.
3. Built-in default: a public STUN server; for `peerjs-cloud`, also whatever ICE defaults the current PeerJS client library ships.

TURN over TCP/TLS on 443 must be supported; corporate networks commonly block UDP.

### 9.5 Wire protocol (data channel)

One reliable, ordered data channel labelled `kalka/1` per operation. Messages are JSON objects, each sent as a 4-byte big-endian length followed by the payload; writes to the channel are chunked to at most 16 KiB so no single SCTP message exceeds common limits.

| Message | Fields | Notes |
|---|---|---|
| `hello` | `proto`, `email`, `pubkey`, `nonce` | Both directions, first. |
| `auth` | `sig` | Both directions, second. |
| `sync_offer` | `kalka_id`, `filename`, `intent` (`sync` \| `handoff`), `content` | Initiator. |
| `sync_reply` | `kalka_id`, `status` (`merged` \| `rejected`), `reason`, `content` | `reason`: `malformed`, `declined`, `not-owner`, `diverged`. `content` present when merged or when offering convergence. |
| `sync_ack` | `kalka_id`, `rev` | Initiator confirms it holds the merged file. |
| `bye` | — | Either side. |

Unknown message types are ignored. A `proto` mismatch ends the session with a human-readable error. Connections close after `sync_ack` or 30 s of silence; a user prompt (accept, verify) extends the wait to 5 min with keep-alives.

### 9.6 `kalka rendezvous`

Server mode of the same binary for corporate hosting:

- PeerJS-compatible signaling, state in memory only.
- Embedded TURN relay (`pion/turn`) with time-limited credentials exposed at `/kalka/ice`.
- `--allow-origin` / reverse-proxy friendly, so corporate SSO or network policy can sit in front. No built-in user database.
- TLS via flags or behind a proxy.

### 9.7 Public cloud mode

`rendezvous: peerjs-cloud` is for demos. The UI must show a permanent notice in this mode: the service is community-run with no availability guarantee, IDs can collide or be claimed by anyone who knows an email, and only the pinned keys protect the exchange. Not for sensitive blueprints.

---

## 10. Plugins

### 10.1 References

A reference is a Markdown link whose target has a non-web scheme: `[label](scheme:payload)`. Scheme matches `[a-z][a-z0-9+.-]*`. The schemes `http`, `https`, `mailto` and `file` are never dispatched to plugins. A reference with no installed plugin renders as plain text with its label.

Blueprints carry references only. Resolved content is never written into the file and never sent to a peer; each peer resolves with their own plugins and credentials.

### 10.2 Discovery

Executable named `kalka-plugin-<scheme>` (plus `.exe`, `.cmd` on Windows), searched in `<config>/plugins` and then `PATH`.

### 10.3 Protocol

One process per request. Request as one JSON object on stdin, response as one JSON object on stdout, exit code 0. Stderr is logged. Timeout 10 s. Arguments from blueprints are never placed on the command line.

```json
{ "protocol": 1, "op": "describe" }
→ { "protocol": 1, "name": "github", "version": "0.1.0", "schemes": ["gh"] }

{ "protocol": 1, "op": "resolve",
  "context": { "frontmatter": { "repo": "org/doc-service", "base_sha": "3f9c2a1" } },
  "refs": [ { "ref": "gh:src/import/parse.py#L120-L180", "payload": "src/import/parse.py#L120-L180" } ] }
→ { "protocol": 1, "results": [
    { "ref": "gh:src/import/parse.py#L120-L180", "ok": true,
      "title": "src/import/parse.py · L120–180",
      "url": "https://github.com/org/doc-service/blob/3f9c2a1/src/import/parse.py#L120-L180",
      "status": "",
      "snippet": { "lang": "python", "start_line": 120, "text": "…" },
      "warnings": [ { "text": "File changed in 7 commits since base_sha" } ] } ] }
```

Failed items return `{ "ref": …, "ok": false, "error": "…" }`. All refs of one scheme in a blueprint are batched into one call. Results are cached in memory for 5 minutes, keyed by ref and frontmatter hash.

The core renders `title`, `url` (http/https only), `status`, `snippet` and `warnings` generically. It must not branch on plugin name or scheme.

### 10.4 Reference plugin: `kalka-plugin-gh`

Separate binary in the same repository, shipped alongside `kalka`. Uses the user's `gh` CLI authentication (`gh api`); no token handling of its own.

- Frontmatter keys it reads: `repo`, `base_sha`, optional `host` (GitHub Enterprise).
- `gh:<path>[#L<a>[-L<b>]]` → permalink at `base_sha` and snippet via the contents API.
- Staleness: one compare call `base_sha...<default branch>` per batch; any referenced path among the changed files gets a warning.
- If `base_sha` is unknown to the remote (unpushed spike), every result carries a warning saying so.
- Paths and line numbers from the payload are validated and passed as API parameters, never through a shell.

---

## 11. Web UI

Served from the binary (`embed.FS`). The frontend is prebuilt in CI; end users need no Node toolchain.

### 11.1 Views

- **Setup** — the three settings.
- **Blueprints** — files in the folder: title (first H1), role (owner / reviewer / local), owner, entry count, validity problems.
- **Review (linear)** — rendered body; entries shown next to the section they anchor to; general entries at the end; badges for outdated and orphaned; resolved references shown inline with snippet and warnings. Actions: add comment, add deviation, reply, *Sync with…*, *Hand off to…* (owner only).
- **Review (canvas)** — the same data as an annotated graph: each body section is a fixed read-only card laid out in reading order; each entry is a note connected to its section's card; references are small cards beside the section. Layout is computed, not saved, so there is no canvas state to persist. A node-graph library is sufficient (for example `@xyflow/react`; confirm its licence before adopting). Freehand drawing is out of scope.
- **Trace** — each trace block rendered as lanes side by side, most abstract on the left, flow running top to bottom within a lane. A lower-lane node is placed level with the first node it realizes; realization links are drawn across lanes in a style distinct from flow arrows. Nodes with a divergence are visibly marked, and a gap is left (and labelled) in the abstract lane opposite an implementation-only node. A side panel lists the computed divergences; selecting a node replaces it with that node's detail: `why`, what it realizes, the resolved reference with snippet and warnings, entries anchored to the node, and a comment box that anchors to `§<slug>#<node-id>`. A Diagram / Source toggle shows the raw YAML. Layout is computed, nothing is saved. The same block appears inline in the linear review view at reduced size. Reference mockups: the "bp — trace view mockups" design canvas.
- **Peers** — add by email, presence, verification code, pin status.

The UI updates live from the daemon (SSE or WebSocket) when files change or syncs arrive.

### 11.2 Rendering untrusted content

Blueprints from peers are untrusted input rendered in a page that can trigger file writes and syncs.

- Raw HTML in Markdown must be disabled or sanitized.
- Link targets limited to http, https and plugin schemes.
- Plugin output is treated as text; `snippet.text` is never interpreted as HTML.
- Strict Content-Security-Policy: no inline script, no remote origins.

---

## 12. CLI

| Command | Needs daemon | Purpose |
|---|---|---|
| `kalka` | starts it | Run the daemon and open the UI. |
| `kalka check <file>` | no | Validate format; exit code for use in scripts and agents. |
| `kalka comment <file> --anchor <slug> [--kind deviation] [--re <id>]` | no | Append an entry; body from `-m` or stdin. Author is the configured email. |
| `kalka send <file> <email>` | yes | Sync a blueprint with a peer. |
| `kalka handoff <file> <email>` | yes | Hand ownership over. |
| `kalka peers` | yes | List peers and presence. |
| `kalka verify <email>` | yes | Show the verification code; `--confirm` marks verified. |
| `kalka rendezvous [flags]` | — | Run the server mode. |
| `kalka version` | no | Version and protocol numbers. |

All commands support `--json` for agents. The review log is plain Markdown, so any downstream tool can read it directly without `kalka`.

---

## 13. Local HTTP API

- Bind to `127.0.0.1` only.
- Validate the `Host` header (defeats DNS rebinding).
- Require a per-start random token on every request (delivered in the URL that `kalka` opens, then held as a cookie); state-changing requests also require a matching `Origin`.
- The CLI reads the token from a file in the config directory with user-only permissions.

---

## 14. File system behaviour

- Watch `folder` recursively for `*.md`; ignore `*.conflict-*.md` and dotfiles. Debounce events (≈300 ms); editors save by rename.
- Writes are atomic: temp file in the same directory, then rename; on Windows retry briefly on sharing violations (editors and antivirus hold files open).
- Before writing, re-read the file; if it changed since it was read for this operation, redo the merge against the new content.
- Ignore the watcher event caused by `kalka`'s own write.
- Two files with the same `kalka_id` → both flagged, syncing refused until one is removed.
- Deleting a file simply removes it from the list. A later sync for that `kalka_id` is treated as a new blueprint.

---

## 15. Technology

- Go, no CGO, single static binary per platform: `windows/amd64` first, then `darwin/arm64`, `linux/amd64`.
- WebRTC: `pion/webrtc`; TURN: `pion/turn`.
- File watching: `fsnotify`.
- Frontend: TypeScript with a small framework of the implementer's choice, built to static assets and embedded.
- Releases: reproducible cross-compiled archives containing `kalka` and `kalka-plugin-gh`.
- Pin dependency versions at implementation time; do not assume the versions current when this was written.

---

## 16. Milestones and acceptance criteria

Ordered so the riskiest unknown (transport on real networks) is tested before UI work.

### M0 — Format and merge library
- Parser/serializer for sections 6 and 7.2; `kalka check`, `kalka comment`.
- Property tests: merge idempotent and commutative on entries; no valid entry lost; normalization makes CRLF/LF and trailing-whitespace variants equal; untouched frontmatter keys preserved byte for byte.
- Golden files for: no frontmatter, no review log, sections after the log, unparsed log content, duplicate headings, invalid entry id, frontmatter with arbitrary unknown keys (nested maps, lists, comments, multi-line strings) surviving a full sync round trip unchanged.

### M1 — Transport spike
- Two CLI processes sync a file through (a) stock `peerjs-server` in Docker and (b) the public PeerJS cloud.
- `hello`/`auth` binding implemented; a test rendezvous that swaps SDP fingerprints is detected.
- Written note on `peerjs-go`: adopt or reimplement, and whether the public cloud accepts a Go WebSocket client (a past issue reports handshake rejections).
- Test on a UDP-blocked network with a TURN server.

### M2 — Daemon and end-to-end sync
- Folder watch, config, peer store, `kalka send`, `kalka handoff`, `kalka peers`, `kalka verify`.
- Scenarios pass: all of 16.1.

### M3 — Web UI, linear review
- Setup, list, review, peers; live updates; section 11.2 and 13 hardening with tests (script tag in a received blueprint does not execute; request with wrong `Host` or missing token is refused).

### M4 — Plugins
- Plugin host and `kalka-plugin-gh`; unresolved schemes degrade to text; plugin timeout and crash do not affect the UI.

### M5 — `kalka rendezvous`
- PeerJS-compatible signaling passes the same M1 tests; embedded TURN; `/kalka/ice`.

### M6 — Canvas review

### M7 — Trace blocks
- Parser and validation in the format library; `kalka check` reports trace errors with block and node id.
- Divergence computation with a unit test per class in 6.10, including a block with three lanes and a block with a branch.
- Trace view and inline rendering per 11.1; comments anchored to a node round-trip through a sync and show as orphaned when the node id is removed.
- An invalid block degrades to a code block and does not affect the rest of the document.
- Mermaid blocks render offline.

### 16.1 End-to-end scenarios

1. **Review round trip.** Alice drops a file, sends to Bob (first contact: both confirm the code). Bob adds two comments, syncs. Alice's file contains both; body unchanged; files equal on both sides.
2. **Parallel reviewers.** Bob and Carol each comment on the same base and sync with Alice in either order. Alice ends with both sets in timestamp order. A further sync with each gives all three the same file.
3. **Owner revision.** Alice rewrites a section and removes a heading, then syncs. Bob's copy takes the new body; his entry on the removed heading shows as orphaned, others as outdated.
4. **Non-owner body edit.** Bob changes the body and syncs. Rejected with `not-owner`; Bob accepts convergence; his edit is saved as a conflict file; his entries survive.
5. **Handoff.** Alice hands off to Bob. Bob can now revise; Alice cannot. The log shows the handoff.
6. **Lost acknowledgement.** Kill the connection after Bob writes but before Alice receives the ack. Retry succeeds and is a no-op on Bob's side.
7. **Stale reviewer after handoff.** Carol, holding the pre-handoff copy, syncs with Alice, then with Bob. Carol ends with `owner: bob` and all entries.
8. **Key change.** Bob's identity key is replaced. Alice's client refuses to sync and explains why.
9. **Editor reformat.** Bob's editor converts the file to CRLF and strips trailing spaces. Sync still succeeds as "same plan".
10. **Offline peer.** Sending to an offline peer fails within a few seconds with a clear message; nothing is queued.

---

## 17. Review findings — what changed from the discussion and why

A final pass over the design found the following. Items 1–4 change earlier agreements.

1. **The byte-prefix rule does not survive two reviewers.** After Alice merges Bob's comments, her file is no longer a prefix of Carol's reply, which was written on the original base. Replaced by: *plan (frontmatter + body) must be equal, entry set may only grow* (7.2). Same intent, still one file, still no history.
2. **Server-side email→key binding dropped.** It would give the rendezvous server state and is impossible on stock PeerServer or the public cloud. Trust rests on client-side pinning, the verification code and channel binding (section 8).
3. **`kalka` writes two frontmatter keys on first send.** A blueprint needs an identity that survives renaming, and an owner. This is the one exception to "the tool does not author blueprints".
4. **Handoff must be recognisable by peers who missed it.** A stale reviewer's copy names the old owner, so a file from the new owner would be rejected. The handoff entry in the log is now what legitimises the change (7.2 rule 4), so that entry is required, not optional.
5. **Entry ids are content hashes.** Gives de-duplication and corruption detection without signatures. Authorship between teammates is not cryptographically protected (7.6); stated openly as a v1 limit.
6. **Timestamps order the log.** Clock skew can reorder entries slightly; it cannot lose them.
7. **Three settings stay three.** ICE configuration is discovered from the rendezvous server (9.4); the team salt is optional.
8. **Public PeerJS cloud.** Its documentation says it provides a TURN server, correcting an earlier statement in the discussion that it would not. It remains demo-only: no SLA, guessable IDs, and a reported history of rejecting Go WebSocket clients, hence the M1 check.
9. **Presence has no directory.** PeerJS servers do not list peers; presence is by probing (9.3).
10. **One device per email.** A second device gets `ID-TAKEN`. Multi-device needs a different ID scheme; deferred.
11. **The UI renders hostile input.** A received blueprint is untrusted Markdown displayed by a page that can write files, so sanitization and localhost-API hardening are requirements, not polish (11.2, 13).
12. **No offline queue.** Consistent with the verbal-handoff assumption; an outbox can be added later without format changes.
13. **Workflow-agnostic.** All references to a specific pipeline were removed. The core interprets two frontmatter keys and one section heading; everything else in the file is pass-through. The folder is an exchange area, and a blueprint leaves it once accepted (6.8).
14. **Trace blocks added (6.10).** Blueprints explained at business level are hard to tie back to code, and the details lost in that translation are the ones that matter for implementation. A trace block holds both views as flows and links them; `kalka` computes where they diverge. Lane names are free-form, so the core still knows nothing about "business" or "code". All examples in this document use a neutral, invented domain.
15. **Handoff entries are signed (v0.3).** Unsigned entries plus rule 4 let any teammate write a handoff to themselves and take over the plan on every peer. Only handoff entries carry a signature; other kinds stay unsigned as in 7.6.
16. **Unparsed log content moves with the entry before it (v0.3).** "Preserved in place" and "rewritten in canonical order" could not both hold literally (6.4, 7.2).

### Residual risks

- **Corporate security review.** A tool moving design documents between machines over WebRTC may need sign-off even though nothing is stored centrally. Hosting `kalka rendezvous` on corporate infrastructure is the mitigating position.
- **Networks that block WebRTC entirely.** TURN over TLS/443 covers most; some proxies will still defeat it. M1 measures this on the real network.
- **Other writers of the same file.** Agents or editors may modify a blueprint while it sits in the folder. `kalka` tolerates this by construction (pass-through frontmatter, unparsed log content preserved, re-read before write), and non-owner body edits are caught at sync time (7.5).

---

## 18. Open questions for the product owner

Defaults apply unless answered.

1. **Auto-sync.** Should blueprints re-sync automatically when a known peer comes online? *Default: manual only in v1.*
2. **Multi-repo blueprints.** One `repo`/`base_sha` pair or a list? This only affects the `gh` plugin. *Default: single pair.*
3. **Access control at the rendezvous.** Which mechanism will sit in front of `kalka rendezvous` in the corporate deployment (SSO proxy, network policy, shared `team` string)? *Default: network policy plus `team` string.*
4. **Showing the planned change in a trace.** Should nodes be markable as new, modified or removed, so the diagram shows what the plan changes and not only the current state? *Default: not in v1; unknown node keys are already preserved, so a `change:` key can be added later without breaking files.*
