# kalka — peer-to-peer blueprint sharing

*Kalka* (Polish: tracing paper, carbon copy) is the working name. It appears in the binary, the plugin prefix (`kalka-plugin-<scheme>`), the frontmatter key (`kalka_id`) and protocol strings. Keep the name in one constant per component so a later rename is cheap; availability of the name has not been checked.

Specification v0.5 · 2026-11-14 · status: ready for implementation

This document is the hand-over to the implementation agent. Sections 1–15 and 19–23 are normative. Section 16 gives milestones and acceptance criteria. Section 17 records what changed during the final review and why. Section 18 lists open questions and the decisions taken on them; each open question has a default so work is not blocked.

v0.4 adds teams with roles and a signed operation log (19), replication of team documents between members (20), a threat model (21), seeds (6.11), team domain documents (6.12), and the blueprint reference (6.13). The new sections were appended so that existing section numbers, which the code cites, stay valid. The triage behind v0.4 is `docs/plan-teams-replication.md`.

v0.5 makes kalka a background desktop app: one daemon per user that runs from login whether or not a window is open, a CLI that stays first class, and a desktop shell with a window and a tray icon (4, 22). It adds the daemon lock and `kalka daemon` (4.1), hardens the local API (13), and covers platforms and distribution (23). The triage behind v0.5 is `docs/plan-desktop-app.md`.

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
- A team shares its domain language, its seeds and the blueprints it publishes. Every member holds a replica, so nobody has to be online for anyone else to read (19, 20).
- References to code, tickets and docs are resolved by plugins; the core knows nothing about GitHub, Jira or Confluence.

### Non-goals (v1)

- Authoring or editing blueprints. Users and agents write them with their own tools. `kalka` writes only the frontmatter keys listed in 6.2 and the entries it records; the one document it assembles is a seed, from text the user typed (6.11).
- Asynchronous delivery through a server. A one-to-one sync needs both peers online; handoff is agreed verbally first. Team documents travel through whichever members are online (20), never through a server.
- Version history, branching, or any general VCS-like state. Three fixed structures are the exceptions: the team log (19.1), domain snapshots (6.12) and tombstones (20.6).
- More than one device per email address.
- A Jira or Confluence plugin. Only a GitHub reference plugin ships.
- Real-time co-editing or live cursors.

---

## 2. Design principles

1. **The file is the state.** Everything needed to continue a review is in the `.md` file. `kalka` keeps no per-blueprint database, sidecar or history. Local application state is limited to configuration, the identity key, the list of known peers, and two small team files in the config directory (open invites and pending joins; 5). In the folder, teams add their logs, tombstones and domain snapshots (14).
2. **Grow-only log.** The plan body is changed only by the owner. Everyone else adds entries to a review log. Entries are never edited or removed by `kalka`, with one exception: a tombstone keeps only its own entry (20.6).
3. **The server is not trusted.** The rendezvous server only introduces peers. Peer identity is verified end to end.
4. **Core is concept-free.** The core handles files, peers, teams and entries. Anything domain-specific lives in a plugin. The core knows nothing about the workflow that produced a blueprint: no assumed sections, no pipeline steps, no meaning attached to frontmatter beyond the keys listed in 6.2. The blueprint reference (6.13) is guidance a document opts into, never a requirement.
5. **Windows is a first-class platform.** Primary users work on Windows with PowerShell. Paths, line endings, file locking and `.exe` lookup must work there first.
6. **Content never blocks.** No user action is refused because of what a document says. Content problems are shown as warnings or hints. Checks of signatures and roles are not content validation: an operation, release or entry that fails them is not applied, and the UI says why.
7. **Brainless.** The user configures email, rendezvous and folder. Keys, peer IDs, team state and caches are created and managed by `kalka`.

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
| Team | A group of members defined by a signed operation log (19). Identified by the hash of its `create-team` operation; its name is a label. |
| Team log | The team's append-only set of signed operations, `<team>-team.log` (19.1). |
| Team owner | A member allowed to change the roster and assign domain owners (19.3). A team has at least one. |
| Domain owner | The member assigned to a domain document by a team owner (19.3). Releases its versions. |
| Member | Anyone the team log currently lists. |
| Published document | A document with `team:` in its frontmatter and a valid release, replicated to the team's members (20.1). Also *team-shared*. |
| Private document | Any document that is not published. It never takes part in replication. |
| Release | A signed entry that makes a version of a published document's plan official (20.4). |
| Tombstone | A final release marking a published document deleted (20.6). |
| Seed | A blueprint with `status: seed`: an idea split off from a growing blueprint (6.11). |
| Domain document | A published document with `kind: domain` holding a team's shared language (6.12). |
| Email ID / key ID | The two peer IDs a daemon registers at the rendezvous (9.2). |

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

- **Core** — Go packages with no UI code: identity and keys, the team log, document storage and merge, replication, P2P networking, the invite flow, the blueprint reference. All networking and sync live here; the web UI never talks to a peer or to the rendezvous.
- **Daemon** — one long-running process per user and machine that hosts the core. It watches the folder, keeps the rendezvous connection, runs syncs and replication, invokes plugins, and serves the local API and the web UI on localhost (13). Closing every window never stops it.
- **CLI** — the `kalka` binary. File commands work without a daemon; daemon commands talk to the running daemon over the local API and start one when none runs (12).
- **Desktop shell** — `kalka-desktop`: a window showing the web UI and a tray icon (22). It hosts the daemon in its own process or attaches to one already running (4.1). It is optional: everything works without it.
- **Web UI** — single-page app embedded in the binary, shown in the desktop window or a browser tab. A pure view over the local API.
- **Plugins** — separate executables named `kalka-plugin-<scheme>`, JSON over stdio.
- **Rendezvous** — any PeerJS-protocol server: the public PeerJS cloud (demo), stock `peerjs-server`, or `kalka rendezvous` (same binary, adds TURN).

### 4.1 One daemon per user

- The daemon takes an exclusive lock on `<config>/daemon.lock` before anything else, the setup form included: `flock` on macOS and Linux, `LockFileEx` on Windows. The kernel releases it when the process ends, however it ends, so there is no stale lock. The PID in the file is for messages only.
- A second start finds the lock held. `kalka daemon` prints "already running (pid N)" and exits 0. The desktop shell attaches to the running daemon, or asks the shell that already runs to show its window, and exits 0.
- **Placement.** The desktop shell runs the daemon in its own process when it gets the lock. When a headless daemon holds the lock, the shell attaches to it as a client. Either way the window and the tray use only the local API.
- A daemon started by the CLI is always headless, also where the desktop shell is installed: an agent's command never makes a window or a tray icon appear. A shell started later attaches.
- Known limit: two config directories naming the same blueprint folder are not prevented. That is a deliberate setup (used in tests); a lock inside the folder would have to work on network shares, where file locks are unreliable.
- **Restricted environments.** Where policy blocks autostart or tray apps, `kalka daemon`, started by hand or by any scheduler, is a fully supported way to run kalka (23.4).

---

## 5. Configuration

File: `%APPDATA%\kalka\config.yaml` on Windows, `$XDG_CONFIG_HOME/kalka/config.yaml` (default `~/.config/kalka/`) elsewhere. If it is missing, the UI opens on a setup form with exactly three required fields.

```yaml
email: gracjan@corp.com                 # required: who you are
rendezvous: wss://kalka.corp.example/      # required: or the literal "peerjs-cloud"
folder: C:\Users\gracjan\blueprints     # required: where blueprints live

# optional, never asked for during setup
team: ""                # shared string mixed into email IDs (9.2); unrelated to teams (19)
ui_port: 7717           # localhost port
ice_servers: []         # manual override of ICE configuration
```

Also stored in the config directory, never in the blueprint folder:

- `identity.key` — ed25519 private key, generated by `kalka setup` or on first run, file mode 0600 (on Windows: ACL restricted to the current user).
- `peers.yaml` — known peers: email, public key, `verified` flag, first-seen date.
- `plugins/` — optional directory searched before `PATH`.
- `invites.yaml` — open invites this installation issued as a team owner (19.7). Owner-only permissions.
- `joins.yaml` — pending joins: tokens redeemed with `kalka join` and not yet completed (19.7). Owner-only permissions.
- `daemon.lock` — the single-daemon lock (4.1).
- `daemon.json` — the running daemon's PID, local API address and per-start token (13). Owner-only permissions. Removed at exit.
- `state.yaml` — settings the daemon owns and writes: `paused`, `notifications`, `keep_running_on_close`, `close_notice_shown`, `autostart_initialized` (22). Changed only through the local API.
- `kalka.log` — log of a daemon started in the background or by the desktop shell.

None of these files is ever asked for or edited by the user.

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
- `kalka` attaches meaning to the keys in the table below, and only when the value has the shape given there. A key with another shape is pass-through, as if `kalka` did not know it, and at most produces a warning.
- All other keys are **pass-through**: never interpreted, validated or required by the core, whatever their names, types or nesting. They are preserved byte for byte on disk, travel with the file to peers, and are handed to plugins as an opaque map. A blueprint with any frontmatter, or none, is valid.

| Key | Shape | Meaning | Written by `kalka` |
|---|---|---|---|
| `kalka_id` | UUID v4 | Identity of the document | On first send, publish, or seed creation |
| `owner` | email, case-insensitive | The one owner (7.3) | On first send if absent; on handoff; after a reassignment (19.5) |
| `status` | `[a-z][a-z0-9-]*` | `seed` marks a seed (6.11); other values are labels | On seed creation; on promotion from the UI |
| `parent` | UUID | The blueprint a seed was split from | On seed creation |
| `origin` | 12 hex | The entry id that triggered a seed | On seed creation |
| `created` | RFC 3339 UTC | When a seed was created | On seed creation |
| `team` | team name (19.2) | The team a document is published to (20.1) | On publish |
| `kind` | `domain` | A domain document (6.12) | Never; the author writes it |
| `domain` | on a domain document: a name `[a-z0-9][a-z0-9-]*`; on any other document: `<team>/<domain>@v<N>` | The domain's name, or a reference to a domain version | Never |
| `version` | positive integer | A domain document's released version | On release (6.12) |
| `deleted` | `true` | Marks a tombstone stub (20.6) | On deletion |
| `reference` | `kalka-blueprint@v<N>` | Opts the document into hints (6.13) | Never |

- Key names are bare, as above. A team whose own pipeline already uses one of these names with another meaning must rename its key, or accept that `kalka` reads it when the shape matches.
- The `repo` and `base_sha` keys in the example above are an instance of this: they mean something to the `gh` plugin only.
- A file without `kalka_id` is a *local* blueprint: listed in the UI, not yet shareable.
- **First send:** `kalka` adds `kalka_id` and, if absent, `owner: <config email>`, creating the frontmatter block if the file has none. Whenever `kalka` edits frontmatter it must change only the lines it writes and must not re-serialize the rest (key order and comments stay intact).

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
- `kind`: `comment` (a remark), `deviation` (a proposed or actual departure from the plan as written, recorded instead of editing the body), `handoff`, `release` (20.4), or `budded` (6.11). `handoff` and `release` have behaviour in the core; `budded` is rendered as a link to the seed; `comment` and `deviation` differ in label and styling alone. The set of `deviation` entries is the document's *deviation log* (6.13). Unknown kinds matching `[a-z][a-z0-9-]*` must be preserved, merged and rendered generically, so a team's own workflow can introduce kinds without changes to `kalka`.
- `timestamp`: UTC, RFC 3339, second precision, `Z` suffix.
- `anchor`: slug of the body heading the entry refers to, or empty (`§`) for a general entry. Slug rule: lower-case; drop everything except letters, digits, spaces and hyphens; spaces to hyphens; duplicates get `-1`, `-2`… in document order (GitHub's algorithm). A node inside a trace block (6.10) is addressed as `§<slug>#<node-id>`, where the slug is the section containing the block.
- `rev`: first 8 hex characters of SHA-256 over the normalized body (6.6) at the time of writing.
- `id`: first 12 hex characters of SHA-256 over `kind \n author \n timestamp \n anchor \n rev \n normalized entry body`. An entry whose `id` does not match its content is invalid and is dropped on merge with a visible warning.
- `sig`: an optional seventh field ` · sig:<base64url>`, with no padding, holding an ed25519 signature by the author's identity key. It is not part of the `id` input. Two messages exist:
  - **v2**, written by `kalka` on every entry it creates, of every kind, in the UI and in `kalka comment`: `"kalka-entry-v2\n" + kalka_id + "\n" + hex(SHA-256 of the id input)`. It covers the full 256-bit digest, so it cannot be moved to another text.

  A signature covers the `kalka_id`, so entries written in a file that has none yet stay unsigned. An entry without `sig` is legal and shown as *unsigned*. An entry whose `sig` does not verify against the key `kalka` holds for its author (8.2, 19) is shown as *signature invalid* and has no effect. `kalka` never signs an entry it did not write, except when the user explicitly asks it to sign their own earlier entries; otherwise a teammate could write an entry in my name and my own `kalka` would sign it.

Entry body: Markdown up to the next entry header or the end of the log.

- Headings inside an entry body must be level 4 or deeper. The UI and `kalka comment` demote shallower headings on input.
- Optional first line `re: <id>` marks a reply to another entry.
- Optional leading blockquote (`> …`) quotes the body text the entry targets; the UI uses it to highlight the passage if it still exists.
- `handoff` body is the single line `→ <new-owner-email>`.
- `release` body: see 20.4. `budded` body: see 6.11.

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

File at most 2 MiB; entry body at most 64 KiB. Larger inputs are rejected with a clear message. The limits hold for replicated documents too. A team log has no size limit; it is exchanged as missing operations, never as one file (20.3).

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

### 6.11 Seeds

A *seed* is a blueprint with `status: seed`: an idea split off from a blueprint whose scope is growing, kept small until someone takes it up. There is no new document type. Everything that holds for a blueprint holds for a seed: one `owner`, append-only entries, sync and handoff as usual.

- Optional keys: `parent` (the `kalka_id` of the blueprint it was split from), `origin` (the `id` of the entry that triggered it, usually a deviation), `created` (6.2).
- Body: an H1 title, `## Problem` and `## Why it was split off`. `kalka` writes exactly this when it creates a seed, from the text the user typed. This is the one document `kalka` assembles.

**Budding.** From any blueprint a member holds, as owner or reviewer, "Create seed" (UI) or `kalka seed` (CLI) does the following:

1. If the parent has no `kalka_id`, it gets one first, as on a first send.
2. The seed file is written, with a fresh `kalka_id`, `owner:` the budding user, `status: seed`, `parent`, `origin` when started from an entry, and `created`.
3. A signed `budded` entry is appended to the parent, anchored like the entry it came from, with `re: <origin id>` when there is one. Body: the line `budded: <seed kalka_id>`, then the seed's title.
4. If step 3 fails, the UI offers "add the backlink" and never creates a second seed.
5. The seed is written in the parent's directory, named after its title.

The parent's frontmatter and body are not touched. The backlink reaches the parent's owner on the next sync, like any entry. A seed budded from a published parent is published to the same team by default (20.1).

**Pre-backlog.** A view of every seed the local folder holds: the user's own, those received by one-to-one sync, and, for teams, the whole team's published seeds (20). For each: title, owner, parent (linked when held), and age. Age is the time since the later of `created` and the newest entry; a seed without `created` shows "age unknown".

**Promotion.** The owner changes `status` to another value, typically `draft`: "Promote" in the UI, or by editing the frontmatter. A non-owner's change is rejected at sync as any non-owner plan change is. Handing a seed over before promotion is an ordinary handoff.

### 6.12 Domain documents

A *domain document* holds a team's shared language for one domain: glossary, bounded contexts, invariants, domain events. Agents read it as context when they work on blueprints. A team may have several domains.

- File name: `<team>-<domain>-domain.md`, created by its author. `kalka` finds domain documents by frontmatter, never by file name.
- Frontmatter: `kind: domain`, `team`, `domain`, `owner`, `version`. It is always a published document (20.1).
- The domain owner is the member a team owner assigned with `assign-domain` (19.3), not whoever the `owner:` line names. Releases by anyone else are not applied (20.4). Until a domain owner is assigned, nobody can release the document, not even its author. A domain document is never handed off: a team owner assigns the domain again.
- Members propose changes as signed comments, exactly as in blueprint review.

**Releasing.** The domain owner edits the body with their own tools, as owners of any blueprint can, and then makes a release: "Release" in the UI or `kalka release`. `kalka` increments `version`, appends a signed `release` entry whose body lists the incorporated comment ids and a summary, and replicates it. Edits that are not released do not travel. Comments are never removed; the UI folds those a release incorporated under "incorporated in v<N>".

**Snapshots.** Each released version, made locally or received, is kept as `<team>-<domain>-domain@v<N>.md` beside the live document, so pinned references keep resolving. The index ignores files matching `*@v<N>.md` (14). A member who joins later holds the versions released since they joined, not the older ones. Snapshots are named after the live file, wherever it is in the folder; a snapshot is written once and never changed. The review view shows a kept version read only, and comments always go to the live document.

A blueprint's reference to a version that is not kept here is shown as such ("not kept here"), as is a reference to a domain of which no document is held.

**References from blueprints.** A blueprint names the domain version it was written against with `domain: <team>/<domain>@v<N>`. When a newer version of that domain is held locally, the review view shows a dismissible "newer domain version available (v<M>)" indicator. It never blocks anything. A reference to a team name held twice (19.2) is shown as ambiguous.

### 6.13 Blueprint reference and hints

`kalka` ships `BLUEPRINT-REFERENCE.md`, embedded in the binary and versioned as `kalka-blueprint@v<N>` (v1 at first). `kalka reference export` writes it out. It is written to be dropped in unchanged as an agent skill or instruction file: it opens with frontmatter holding `name` and `description`.

It describes what a blueprint useful to agents usually contains, as guidance, never as a requirement. Each section comes with a short reason it helps agents, and examples use a neutral domain (parcel logistics, library lending):

1. **Domain reference**: which domain document and version applies (`domain:`, 6.12).
2. **Scope**: the bounded contexts and modules expected to be touched. Work beyond the scope becomes a seed (6.11), not a larger change.
3. **Verification plan**: invariants, edge cases, and what a human reviewer must specifically check. Written at planning time, not at review time.
4. **Critical sections**: where the design deliberately departs from the clean model, for example for performance, and why. Agents must not "clean them up"; human review is required there.
5. **Operations**: failure modes, what to observe, how to roll back.
6. **Deviation log**: the `deviation` entries of the review log (6.5), recorded with `kalka comment --kind deviation`, never a body section.

**Hints** are opt-in. Only a document whose frontmatter says `reference: kalka-blueprint@v1` gets them: one hint per recommended body section (1 to 5) that has no heading with the matching slug (at any level; a `-N` duplicate suffix is ignored), for example "no verification plan". A frontmatter `domain:` key of the form `<team>/<domain>@v<N>` also satisfies the domain-reference hint. The hint codes are `hint-no-domain-reference`, `hint-no-scope`, `hint-no-verification-plan`, `hint-no-critical-sections` and `hint-no-operations`; an unknown version gives `hint-unknown-reference`. There is no hint for the deviation log, since a plan not yet implemented has none. Hints are information only: they never change the exit code of `kalka check`, never stop a sync, and can be dismissed per document in the UI. Dismissal is kept in the browser, not in the file or the config. A document naming an unknown reference version gets one information line and no hints. Every other document gets no hints at all.

---

## 7. Sync, ownership and handoff

### 7.1 One operation

Every exchange is a **sync**: the initiator sends its file, the responder merges and replies with the merged file, the initiator merges the reply and acknowledges. Sending for review, returning comments, pushing a revised plan and handing off are all the same operation. After a successful sync both files are equal under normalization.

### 7.2 Merge algorithm

`merge(local L, incoming I, sender S)` → new local file or a rejection. `L` may be absent.

This is the rule for one-to-one syncs and for any document the local peer does not hold as a team document. For a published document of a team whose log the local peer holds, rule 4 is replaced by the release rule of 20.4, and a tombstone wins (20.6). Steps 1, 3 and 6 apply to both.

1. **Parse** `I`. Not parseable, over limits, or missing `kalka_id`/`owner` → reject `malformed`.
2. **New blueprint** (no local file with this `kalka_id`): ask the user to accept (shows sender, title, size). Declined → reject `declined`. Accepted → result is `I`; file name is the sender's suggested name, sanitized, with a numeric suffix if taken.
3. **Same plan** (frontmatter and body of `I` equal those of `L` after normalization): result is `L` with the entry union (step 6).
4. **Owner change of plan** — accept `I`'s frontmatter and body if either holds:
   - `S` equals `L.owner`; or
   - `S` equals `I.owner` and `I`'s log contains a chain of `handoff` entries leading from `L.owner` to `S`.
   Result is `I`'s frontmatter and body with the entry union.

   **Handoff chain:** walk the handoff entries of the entry union in canonical order, starting with `current = L.owner`. An entry is a link only if its author equals `current` and its `sig` verifies against the key pinned for that author (8.2). A link sets `current` to the entry's target. The chain leads to `S` if `current` equals `S` at any point of the walk. A handoff entry that is not a link stays in the log as an ordinary entry; the UI marks it "unverified handoff". If a signature cannot be checked because no key is pinned for its author, the rejection says so and names the author to sync with.
5. **Otherwise** reject `not-owner`. The rejection carries the responder's current file so the sender can converge (7.5).
6. **Entry union**: all valid entries from both sides, de-duplicated by `id`, sorted by `(timestamp, id)`. Where both sides hold an entry with the same `id`, the copy whose `sig` verifies is kept, so a relay cannot strip a signature. Unparsed log content from the side whose body was kept is preserved.

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

`kalka` signs every entry it writes (6.5). What a signature changes depends on where the document lives:

- **Private documents (one-to-one).** A peer accepts any well-formed entry from any authenticated peer, signed or not, including entries by third parties (that is how reviewers see each other's comments). A signature that verifies against the author's pinned key shows as *signed*; a missing one as *unsigned*; a wrong one as *signature invalid*. Handoff entries move ownership, so they count only when their signature verifies (7.2). Entries written by hand or by a tool other than `kalka` stay legal and unsigned.
- **Published documents.** An entry replicates within the team only if its v2 signature verifies against a key the team log has *ever* bound to the author's email (20.4). Unsigned entries, entries by people outside the team stay in the files that hold them, marked *local only*, and do not enter the replica. Entries by removed members replicate and are marked *by a removed member*: refusing them would depend on the order in which each peer learned of the removal, and peers would never agree.

The `id` hash catches accidental corruption; signatures catch forgery.

---

## 8. Identity and trust

### 8.1 Keys

Each installation generates an ed25519 key pair on first run. The user never handles it.

### 8.2 Pinning

- First contact with an email: the peer's public key is stored in `peers.yaml` as unverified.
- Later contacts must present the same key. A different key blocks all syncs with that email until the user removes the old pin and re-verifies. The UI must make this state loud.
- A key the team log binds to a member's email (19.1) counts as verified for that team's traffic. If `peers.yaml` pins another key for the same email, `kalka` uses the log's key for team traffic, the pinned key for one-to-one syncs, and shows the mismatch loudly in the Peers and Team views. It never picks silently.
- A key is a member's identity together with their email; an email alone is never trusted.

### 8.3 Verification code

For each peer pair: `code = base32(SHA-256("kalka-sas-v1" ‖ min(pkA, pkB) ‖ max(pkA, pkB)))`, first 8 characters shown as `XXXX-XXXX`. Both sides see the same code.

- First sync with an unverified peer requires both users to confirm the code once (they are on a call anyway, per the verbal-handoff assumption). Confirmation sets `verified: true`.
- Members of a team do not compare codes with each other: the team log carries their keys. A join is verified by the invite (19.7) instead of a code.
- `kalka verify <email>` shows the code on the CLI.

### 8.4 Channel binding

After the data channel opens and before any blueprint data flows:

1. Each side sends `hello {proto, email, pubkey, nonce}`.
2. Each side sends `auth {sig}` where `sig` signs `"kalka-auth-v1" ‖ own DTLS certificate fingerprint ‖ peer DTLS certificate fingerprint ‖ peer nonce`.
3. Each side verifies the signature against the pinned (or first-seen) key, using fingerprints read from the DTLS transport itself, not from the SDP relayed by the server.
4. Each side checks that the peer ID it dialed or was dialed from is one of the claimed identity's IDs (9.2): the email ID derived from the claimed email, or the key ID derived from the presented key. On a key ID this check comes before pinning, so a squatter on someone's key ID fails without leaving a changed-key record.

Any failure closes the connection. This makes a malicious or compromised rendezvous server unable to sit in the middle.

---

## 9. Rendezvous and transport

### 9.1 Signaling protocol

The client must speak the PeerJS server WebSocket protocol so that one client works against the public PeerJS cloud, stock `peerjs-server` and `kalka rendezvous`. Message types include `OPEN`, `OFFER`, `ANSWER`, `CANDIDATE`, `HEARTBEAT`, `LEAVE`, `EXPIRE`, `ERROR`, `ID-TAKEN`.

- Only the signaling protocol is PeerJS. What flows over the data channel is `kalka`'s own protocol (9.5); there is no interoperability with browser PeerJS clients and none is required.
- Treat the `peers/peerjs-server` source as the authority for URL shape, query parameters, heartbeat interval and ID character rules. Do not rely on this document for those details.
- `github.com/muka/peerjs-go` is an existing Go port of client and server on Pion (Apache-2.0). Evaluate it in M1; adopt, vendor or reimplement the signaling part. It is small.

### 9.2 Peer IDs

Each daemon registers two IDs, on two signaling connections:

```
email_id = "kalka-"  + hex(SHA-256("kalka-peer-v1|" + team + "|" + lowercase(email)))[0:32]
key_id   = "kalka-k" + hex(SHA-256("kalka-key-v1|" + pubkey))[0:32]
```

`team` in `email_id` is the optional config salt (5), not a team (19). `pubkey` is the raw 32-byte ed25519 key.

- **Dialing.** If a key is known for the peer (from a team log or a pin), dial its key ID, from one's own key ID; if that ID does not answer, fall back to the email ID. A failed handshake never falls back. Otherwise dial its email ID. All team traffic uses key IDs. A first one-to-one send to a typed email uses the email ID, since no key is known yet.
- **What a key ID gives.** A squatter on someone's key ID is detected before pinning (8.4), so they cannot lock a user out of their pinned peers. On `kalka rendezvous` a key ID also cannot be claimed without the key (9.6). On other servers anyone holding the public key, which every team member does, can still occupy the ID and keep its owner offline; impersonation was already prevented by channel binding and pinning.
- No directory is needed: to reach a teammate, derive an ID and connect. The server refuses a second connection with an ID already in use (`ID-TAKEN`); the UI reports this as "this email is already online elsewhere".

### 9.3 Presence

PeerJS servers do not list peers by default. Presence is learned by probing: a connection attempt to an offline ID fails quickly. The daemon probes the peers in `peers.yaml` plus addresses typed this session, at most once a minute each, and on demand before a sync. Team members are probed only while a replication round needs them (20.3). A pending join probes the inviting owner's key ID once a minute until the invite expires (19.7).

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
| `hello` | `proto`, `email`, `pubkey`, `nonce`, optional `invite` | Both directions, first. `invite` carries a join proof (19.7). |
| `auth` | `sig` | Both directions, second. |
| `sync_offer` | `kalka_id`, `filename`, `intent` (`sync` \| `handoff` \| `replicate`), `content` | Initiator. `replicate` is used inside a team session only and raises no prompt. |
| `sync_reply` | `kalka_id`, `status` (`merged` \| `rejected`), `reason`, `content` | `reason`: `malformed`, `declined`, `not-owner`, `diverged`. `content` present when merged or when offering convergence. |
| `sync_ack` | `kalka_id`, `rev` | Initiator confirms it holds the merged file. |
| `bye` | optional `reason` | Either side. |
| `team_sync` | `team`, `heads`, `digest` | Opens a replication session (20.3): the log heads and one digest over the inventory. |
| `team_log` | `team`, `ops` | Log operations the other side is missing. Also sent once by an owner who admits a joiner (19.7 step 5). |
| `team_inv` | `team`, `items` | Inventory lines: `kalka_id`, latest release version and plan hash, entry-set digest. |
| `team_want` | `team`, `ids` | The documents the sender wants; each follows as a `sync_offer` with `intent: replicate`. |

Unknown message types are ignored. A `proto` mismatch ends the session with a human-readable error. Connections close after `sync_ack` or 30 s of silence; a user prompt (accept, verify) extends the wait to 5 min with keep-alives.

The responder dispatches on the type of the first message after `auth`.

### 9.6 `kalka rendezvous`

Server mode of the same binary for corporate hosting:

- PeerJS-compatible signaling, state in memory only.
- Embedded TURN relay (`pion/turn`) with time-limited credentials exposed at `/kalka/ice`.
- `--allow-origin` / reverse-proxy friendly, so corporate SSO or network policy can sit in front. No built-in user database.
- TLS via flags or behind a proxy.
- **Key-ID proof.** A client registering a key ID (9.2) adds the query parameter `kproof = base64url(pubkey) "." unix-seconds "." base64url(sig)`, where `sig` signs `"kalka-keyid-v1\n" + id + "\n" + unix-seconds`. The server checks that the ID matches the key, the signature verifies and the timestamp is within a few minutes, and refuses the registration otherwise (within 5 minutes). Nothing is stored. Email IDs register as before. Other PeerJS servers ignore the parameter (checked live on the public cloud, 2026-11-14).

### 9.7 Public cloud mode

`rendezvous: peerjs-cloud` is for demos. The UI must show a permanent notice in this mode: the service is community-run with no availability guarantee, IDs can be claimed by anyone who knows an email or a public key, which keeps their owner offline, and only the pinned keys and the team log protect the exchange. Not for sensitive blueprints.

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

Served from the binary (`embed.FS`). The frontend is prebuilt in CI; end users need no Node toolchain. It is shown in the desktop window (22) or in a browser tab (`kalka open`); it is the same page from the same localhost address either way, a pure view over the local API (13).

### 11.1 Views

- **Setup** — the three settings.
- **Blueprints** — files in the folder: title (first H1), role (owner / reviewer / local), owner, entry count, validity problems.
- **Review (linear)** — rendered body; entries shown next to the section they anchor to; general entries at the end; badges for outdated and orphaned; resolved references shown inline with snippet and warnings. Actions: add comment, add deviation, reply, *Sync with…*, *Hand off to…* (owner only).
- **Review (canvas)** — the same data as an annotated graph: each body section is a fixed read-only card laid out in reading order; each entry is a note connected to its section's card; references are small cards beside the section. Replies hang under the note they answer; the General and Orphaned groups of the linear view get cards of their own after the body. Layout is computed from the text, not saved, so there is no canvas state to persist. Selecting a note or a section card opens the linear view scrolled to it. Freehand drawing is out of scope.
- **Trace** — each trace block rendered as lanes side by side, most abstract on the left, flow running top to bottom within a lane. A lower-lane node is placed level with the first node it realizes; realization links are drawn across lanes in a style distinct from flow arrows. Nodes with a divergence are visibly marked, and a gap is left (and labelled) in the abstract lane opposite an implementation-only node. A side panel lists the computed divergences; selecting a node replaces it with that node's detail: `why`, what it realizes, the resolved reference with snippet and warnings, entries anchored to the node, and a comment box that anchors to `§<slug>#<node-id>`. A Diagram / Source toggle shows the raw YAML. Layout is computed, nothing is saved. The same block appears inline in the linear review view at reduced size. Reference mockups: the "bp — trace view mockups" design canvas.
- **Peers** — add by email, presence, verification code, pin status, key fingerprint.
- **Review additions (v0.4)** — signature badges on entries (*unsigned*, *signature invalid*, *by a removed member*, *local only*); a hint strip for documents opted into the reference (6.13), each hint dismissible; the "newer domain version available" indicator (6.12); "Create seed" on deviation entries and in the document menu, and `budded` entries as links (6.11); for owners of published documents, a banner when edits are not yet released, and *Publish*, *Publish update*, *Delete for the team*.
- **Pre-backlog** — seeds held locally, including the team's (6.11): title, owner, parent, age; *Promote* for the owner.
- **Team** — members, owners, domain owners, open invites, the log rendered as a readable history, a replication status line (last round, peers reached, documents refused and why), and *Join* (paste a token), *Invite*, *Remove* (with a preview of the documents to be reassigned), *Add owner*, *Assign domain*.
- **Domains** — the team's domain documents with version and owner; *Release* for the domain owner, with a checklist of open comments to mark as incorporated; a version switcher over local snapshots.
- **Settings (v0.5)** — start at login, keep running when the window is closed, notifications, pause (22). The two window settings are hidden in a browser tab.

The UI updates live from the daemon (SSE or WebSocket) when files change or syncs arrive.

### 11.2 Rendering untrusted content

Blueprints from peers are untrusted input rendered in a page that can trigger file writes and syncs.

- Raw HTML in Markdown must be disabled or sanitized.
- Link targets limited to http, https and plugin schemes.
- Plugin output is treated as text; `snippet.text` is never interpreted as HTML.
- Strict Content-Security-Policy: no inline script, no remote origins.

---

## 12. CLI

The CLI is a first-class interface: agents and scripts use it, it works without the desktop shell, and on machines where the shell is not installed. Commands fall into four classes.

| Class | With a daemon running | With no daemon |
|---|---|---|
| **File** | Not used | Not used, and none is started. Writes follow 14 (re-read and redo on change), so they are safe beside a running daemon. |
| **Daemon** | Over the local API (13) | One is started in the background (`kalka daemon --background`) and the command runs once it answers. `--no-start` or `KALKA_NO_START=1` fails instead, with exit code 1 and `"kind": "not-running"` in JSON. |
| **View** | Over the local API, with live data (presence, online state) | Read from the local files in process; nothing is started. Live fields are reported as unknown and JSON carries `"daemon": false`. |
| **Control** | `daemon`: exits 0 (4.1). `stop`: asks it to stop and waits. | `daemon`: runs. `stop`: nothing to do, exits 0. |

| Command | Class | Purpose |
|---|---|---|
| `kalka` | — | Make sure a daemon runs, then open the UI: the desktop window if a shell is attached, else the browser. |
| `kalka daemon [--background] [--no-ui] [--no-popups]` | control | Run the daemon in the foreground (prompts are asked in the terminal), or as at login with `--background` (no browser, notifications, log to `kalka.log`). `--no-ui` serves the API but not the web UI. `--no-popups` shows no popups or notifications: prompts wait in the UI and the terminal. |
| `kalka stop [--wait <dur>]` | control | Stop the running daemon after its running rounds finish; waits up to `--wait` (default 1m). |
| `kalka status` | view | Online state, peers reachable, last sync, pending items, paused. |
| `kalka pause` / `kalka resume` | daemon | Go offline and back (22.3). |
| `kalka open [--print]` | daemon | Open the UI in the browser, or print its URL. |
| `kalka autostart on [--headless]\|off\|status` | file | Start kalka at login (22.2): the desktop app where installed, else (or with `--headless`) `daemon --background`. |
| `kalka setup --email … --rendezvous … --folder …` | file | Write the three settings. |
| `kalka check <file>` | file | Validate format; exit code for use in scripts and agents. |
| `kalka comment <file> --anchor <slug> [--kind deviation] [--re <id>]` | file | Append an entry; body from `-m` or stdin. Author is the configured email. |
| `kalka send <file> <email>` | daemon | Sync a blueprint with a peer. |
| `kalka handoff <file> <email>` | daemon | Hand ownership over. |
| `kalka peers` | view | List peers and presence. |
| `kalka peers add\|forget <email>` | daemon | Change the known peers. |
| `kalka verify <email>` | daemon | Show the verification code; `--confirm` marks verified. |
| `kalka rendezvous [flags]` | file | Run the server mode. |
| `kalka version` | file | Version and protocol numbers. |
| `kalka reference export [-o <file>] [--force]` | file | Write `BLUEPRINT-REFERENCE.md` (6.13), to stdout by default. Never overwrites without `--force`. |
| `kalka reference version` | file | Print the embedded reference version. |
| `kalka seed <parent> [--from <entry id>] --title <t> [-m <text>]` | file | Bud a seed (6.11). |
| `kalka seeds` | file | List the pre-backlog. |
| `kalka team create <name>` | daemon | Create a team; you are its first owner. |
| `kalka team invite <email> [--expires <dur>]` | daemon | Print an invite token (19.7). Team owners only. |
| `kalka join <token> [--wait <dur>]` | daemon | Redeem an invite; waits briefly for the owner, then leaves the retry to the daemon. |
| `kalka team members\|log\|invites` | view | Show the roster, the log, or the invites made on this installation. |
| `kalka team remove\|add-owner\|remove-owner <email>` | daemon | Roster changes (19.3). |
| `kalka team assign-domain <domain> <email>` | daemon | Appoint a domain owner. |
| `kalka publish <file> --team <team>` | daemon | Publish a document to a team (20.1); again later to publish an update. |
| `kalka release <file> [-m <text>] [--incorporates <id>,...]` | daemon | Release a domain version (6.12). |
| `kalka delete <file>` | daemon | Delete a published document for the team (20.6). |
| `kalka domains` | view | List domain documents and versions. |

All commands support `--json` for agents. The review log is plain Markdown, so any downstream tool can read it directly without `kalka`. `kalka comment` signs with the identity key from the config directory (6.5); it still needs no daemon.

The rule behind the classes: one writer for everything shared beyond a single file. Daemon commands are those that write the team log, the config directory, a release or a tombstone, or need the network. File commands touch one blueprint file under the rules of 14. Views exist so that an agent's read in a sandbox does not register on a rendezvous server as a side effect.

A daemon command with no configuration fails with "kalka is not set up" (`"kind": "not-set-up"`); it does not start a daemon that would wait for the setup form.

With `--json`, a failure is `{"error": "...", "kind": "..."}` on stdout. A view's JSON is an object carrying `"daemon"`: `peers --json` is `{"daemon", "peers"}`, `domains --json` is `{"daemon", "domains"}`, the team views put `daemon` beside the team's fields, and `status --json` reports `online`, `peers_online` and `pending_prompts` as `null` when no daemon runs (unknown, not zero).

---

## 13. Local HTTP API

One local API, used by the web UI (in the desktop window or a browser tab), the desktop shell's tray, and the CLI. There is no second transport (18.3, decision 7).

- Bind to `127.0.0.1` only, on `ui_port` (default 7717). The port is fixed so that the page's per-origin storage survives restarts.
- Validate the `Host` header against the loopback address and port (defeats DNS rebinding).
- Require the per-start random token on every `/api/` request. The page gets it in the URL that `kalka open` or the shell loads, which the daemon exchanges for an HTTP-only, same-site cookie; the CLI sends it as a bearer header.
- Requests that change state must carry an `Origin` equal to the daemon's own origin, or none (the CLI). When a `Sec-Fetch-Site` header is present on an `/api/` request it must be `same-origin` or `none`, for reads too. A web page elsewhere that aims requests at `localhost` is refused. The token link itself is not checked: opened from a mail or chat page it arrives as `cross-site`, and a page that does not know the token gains nothing from it. The UI's static files carry no data and are served without these checks (the sandboxed Mermaid frame loads them from an opaque origin).
- The token is in `daemon.json` in the config directory, written owner-only (mode 0600; on Windows an ACL restricted to the current user). Another local user cannot read it. The user's own processes can, as they can read the identity key: they are trusted with the user's files.
- `--no-ui` stops the daemon from serving the web UI (every page path answers 404 and no cookie is set); the API stays up for the CLI and the shell. It also turns popups off, since there is no page for them to open.
- Links the page opens to the outside go through `POST /api/open-external` when it runs in the desktop window: only `http` and `https` are accepted, and the system browser opens them. The window never navigates away from the loopback origin.
- **The desktop shell** (22) is a client like the others. It holds an event stream with `GET /api/events?shell=1`; while one is open, `GET /api/summary` reports `"shell": "desktop"` and the shell, not the daemon, presents prompts and notices. `POST /api/ui/show {"path"}` asks it to show its window (on a document, if given); with no shell attached it fails with kind `no-shell`, and bare `kalka` opens the browser instead. The window loads the token link with `window=1`, which also sets a cookie the page can read (`kalka_window=1`): the page then sends links outside, leaves notifications to the shell, and shows the window settings.

---

## 14. File system behaviour

- Watch `folder` recursively for `*.md`; ignore `*.conflict-*.md`, domain snapshots `*@v<N>.md` (6.12), dotfiles and dot-directories. Debounce events (≈300 ms); editors save by rename.
- Team files live in `<folder>/<team>/`: the log `<team>-team.log`, documents received from members, and `.kalka/tombstones/` and `.kalka/trash/` (20.6). A document the user published from elsewhere in the folder stays where it is; documents are always found by `kalka_id`, never by path.
- Writes are atomic: temp file in the same directory, then rename; on Windows retry briefly on sharing violations (editors and antivirus hold files open).
- Before writing, re-read the file; if it changed since it was read for this operation, redo the merge against the new content.
- Ignore the watcher event caused by `kalka`'s own write.
- Two files with the same `kalka_id` → both flagged, syncing refused until one is removed.
- Deleting a private file simply removes it from the list. A later sync for that `kalka_id` is treated as a new blueprint.
- Deleting a published document's file with the file manager is not a deletion for the team: replication fetches it again, and the UI says how to delete it for the team (20.6).

---

## 15. Technology

- Go. The `kalka` binary (CLI and headless daemon) has no CGO: a single static binary per platform, `windows/amd64` first, then `darwin/arm64`, `linux/amd64`.
- Desktop shell: Wails v3, pinned to an exact release (18.3, decision 8). It is built only with the `desktop` build tag, so `go build ./...`, `go vet ./...` and `go test ./...` never need it. On Windows it needs no CGO; on macOS and Linux it does (23).
- What the user installs is one product per platform (23.1): on Windows a per-user installer holding `kalka.exe` and `kalka-desktop.exe`; on macOS `Kalka.app` with one executable that is also the CLI; on Linux one archive with `kalka` and `kalka-desktop`.
- No bundled Node.js or Chromium: Node is a build-time tool for the web UI only, and the window uses the system webview.
- WebRTC: `pion/webrtc`; TURN: `pion/turn`.
- File watching: `fsnotify`.
- Frontend: TypeScript with a small framework of the implementer's choice, built to static assets and embedded.
- Releases (`tools/release`): per target a reproducible headless archive (`kalka`, `kalka-plugin-gh`; on Windows also `kalka-desktop.exe`); the Windows installer (when NSIS is installed), `Kalka.app` in a zip (built on a Mac) and a Linux desktop archive (built on Linux with GTK 4 and WebKitGTK 6.0), each skipped with a reason when the host cannot build it; `SHA256SUMS`. See 23.5 for signing.
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
- Every section and entry of the linear view appears; the layout is the same for the same input; nothing is saved; selecting a note scrolls the linear view to it.

### M7 — Trace blocks
- Parser and validation in the format library; `kalka check` reports trace errors with block and node id.
- Divergence computation with a unit test per class in 6.10, including a block with three lanes and a block with a branch.
- Trace view and inline rendering per 11.1; comments anchored to a node round-trip through a sync and show as orphaned when the node id is removed.
- An invalid block degrades to a code block and does not affect the rest of the document.
- Mermaid blocks render offline.

### R1 — Blueprint reference and hints
- `kalka reference export` writes exactly the embedded bytes, to stdout or `-o`, and refuses to overwrite without `--force`; the file declares its version.
- No hints without `reference:`; each hint appears alone when its section is missing; hints never change an exit code or stop a sync.

### R2 — Team log library (no network)
- Operations from non-owners and from removed signers are rejected; concurrent owner operations converge identically on all replicas, including mutual owner removal; reassignment goes to the removing owner and is discarded when the removal loses.
- A randomized harness over many replicas, with hostile, back-dated and hash-ground operations, random order, duplication and partitions, runs 100 000 histories without divergence. Replay does not depend on input order.
- Invite tokens: expired, tampered, wrong team.

### R3 — Signed entries, releases, tombstones (no network)
- A domain release signed by a non-assigned owner is not applied; a tombstone beats a stale copy in both directions; a relay's copy of a release is applied, a relay's unreleased edit is not; a signature stripped by a relay is restored.
- Old unsigned files parse and merge as before; the merge stays idempotent and commutative on entries; the ten scenarios of 16.1 still pass.

### R4 — Seeds
- Budding gives a seed with `status: seed`, one owner, `parent`, `origin`, `created` and a new `kalka_id`; the parent gains exactly one signed `budded` entry and its plan is unchanged; the backlink survives a sync both ways.

### R5 — Key IDs and capabilities
- Known peers connect over key IDs, first contact over email IDs, with fallback to the email ID when a key ID does not answer; a squatter on a key ID fails without a changed-key record; `kalka rendezvous` refuses a key ID without a valid proof.

### R6 — Teams and invites
- The invite flow end to end over real WebRTC: the join completes with no prompt on either side; an expired token and a reused token are refused with a clear reason, and a second `add-member` for one invite is discarded at replay.

### R7 — Team replication
- With three or more daemons: private documents never leave the local peer (no private id, title or byte on the wire; a request for a private id is answered as unknown); a tombstone survives sync with a stale peer; concurrent owner operations converge over the network; reassignment reaches every peer and is undone everywhere when the removal loses.

### R8 — Domain documents
- A domain release signed by a non-assigned owner is applied by no peer, and each says why; a release reaches an offline member through a relay; snapshots never show as duplicates; the newer-version indicator appears and can be dismissed.

### D1 — Core and daemon split
- No behaviour change. A test walks the import graph: the core imports no HTTP, UI or notification package.

### D2 — Daemon lock
- A second `kalka daemon` exits 0 and names the first; the lock is released when the holder is killed; the macOS LaunchAgent restarts a crashed daemon without starting two.

### D3 — Local API hardening
- Requests without the token, with a wrong token, with a foreign `Origin` or `Sec-Fetch-Site: cross-site` on a write, or with a wrong `Host` are refused with 403; `daemon.json` is owner-only; `--no-ui` serves no page.

### D4 — CLI with and without a daemon
- Each class behaves as in 12 with and without a running daemon; a daemon command starts a headless daemon and runs; `--no-start` fails with `not-running`; views read local files and report `"daemon": false`.

### D5 — Sync controls, settings and notices (headless)
- Replication and incoming syncs continue with no window and no browser page connected; pause takes the daemon offline and survives a restart; each notice of 22.4 is raised once, and not when notifications are off.

### D6 — Desktop shell
- Closing the window hides it and the daemon keeps syncing; the one-time notice appears once; `--hidden` starts with no window; the shell attaches to a headless daemon; start at login toggles per platform.

### D7 — Builds and packaging
- One build entry point produces the headless binaries for all targets and the desktop shell for the targets the host can build; the Windows installer installs per user without elevation; unsigned builds succeed and say so.

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
11. **Seed backlink.** Bob buds a seed from a deviation on Alice's blueprint and syncs. Alice's copy has the `budded` entry; her plan is unchanged.
12. **Join.** Alice invites Carol; Carol runs `kalka join` while Alice is offline; when Alice comes online the join completes in the background and both logs list Carol.
13. **Relay.** Alice publishes a seed and goes offline. Bob, online with Alice earlier, passes it to Carol. Carol verifies Alice's release and holds the seed.
14. **Deleted stays deleted.** Carol is offline holding v3; Alice deletes the document; Carol returns and meets only Bob. The file does not come back anywhere.
15. **Removal.** Alice removes Dave; Dave's published seeds become Alice's on every member; Dave's later releases are not applied.

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

17. **Teams, replication and signatures (v0.4).** The v0.4 request was triaged against the code before this revision (`docs/plan-teams-replication.md`). Four points of it could not work as written, and the spec takes a different route for each:
    - *A key-derived peer ID does not stop posing on a stock server*: any holder of the public key can register it. Posing was already prevented by channel binding and pinning; what key IDs add is that a squatter can no longer lock someone out, and on `kalka rendezvous` a proof of the key (9.2, 9.6).
    - *A signature over the 12-hex entry id does not bind the text*: 48 bits can be matched by brute force, which also weakened v0.3 handoff signatures. All new signatures cover the full digest (6.5).
    - *"Body edits only by the owner" judged by the sender refuses every relay.* Published documents carry signed releases instead, and the sender no longer matters (20.4).
    - *"The lower hash wins" lets a removed owner undo the removal*, by back-dating an operation and grinding its hash. Replaced by "removal wins; the senior owner wins a mutual removal" (19.4). The product owner accepted this change.
18. **Two IDs per daemon (v0.4).** Keeping the email ID keeps one-to-one sends to a typed email working (9.2).
19. **Replication overrides "manual only" for team documents (v0.4).** One-to-one syncs keep every v0.3 rule, prompts included (18.2).

### Residual risks

- **Corporate security review.** A tool moving design documents between machines over WebRTC may need sign-off even though nothing is stored centrally. Hosting `kalka rendezvous` on corporate infrastructure is the mitigating position.
- **Networks that block WebRTC entirely.** TURN over TLS/443 covers most; some proxies will still defeat it. M1 measures this on the real network.
- **Other writers of the same file.** Agents or editors may modify a blueprint while it sits in the folder. `kalka` tolerates this by construction (pass-through frontmatter, unparsed log content preserved, re-read before write), and non-owner body edits are caught at sync time (7.5).
- **Team log rules are hard to change later.** Logs created under one rule set must replay the same way forever. Each operation carries a rule version (19.1), and no real team should be created before the R2 harness passes.
- **Rendezvous load from replication.** Unknown limits on the public cloud; gossip with a small fan-out and a digest that ends idle sessions in one round trip keep it low (20.3).

---

## 18. Open questions and decisions

### 18.1 Open questions

Defaults apply unless answered.

1. ~~**Auto-sync.**~~ Answered for team documents (18.2, decision 3); private documents stay manual only.
2. **Multi-repo blueprints.** One `repo`/`base_sha` pair or a list? This only affects the `gh` plugin. *Default: single pair.*
3. **Access control at the rendezvous.** Which mechanism will sit in front of `kalka rendezvous` in the corporate deployment (SSO proxy, network policy, shared `team` string)? *Default: network policy plus `team` string.*
4. **Showing the planned change in a trace.** Should nodes be markable as new, modified or removed, so the diagram shows what the plan changes and not only the current state? *Default: not in v1; unknown node keys are already preserved, so a `change:` key can be added later without breaking files.*
5. **Manual accept for joins.** Should a team owner be able to require a manual "accept" for each join, as a setting? *Default: not in this version; the invite is the approval (19.7).*
6. **Key rotation.** Every signature check would need to know which key was valid when, so rotation is not cheap. *Default: future work; a lost key means removal and a new invite.*
7. **Teams across rendezvous servers.** An installation uses one rendezvous, so a person cannot serve two teams on two servers. *Default: accepted limit; the invite token names the server and a mismatch is shown at join time.*
8. **Several devices per person.** With key IDs a second device is a second identity, and an invite belongs to the device that made it. *Default: still a non-goal (1).*

### 18.2 Decisions (v0.4, answered by the product owner 2026-11-13)

| # | Question | Decision |
|---|---|---|
| 1 | Peer IDs | Both: key ID for known keys and team traffic, email ID for first contact (9.2). |
| 2 | Team log conflicts | "Removal wins; the senior owner wins a mutual removal; hash as the last tiebreak" replaces "the lower hash wins" (19.4). |
| 3 | Automatic replication | Team documents replicate in the background between members, with no click, accept prompt or code, into `<folder>/<team>/` (20). |
| 4 | Installed peers | None needed before the first release: every milestone up to v1 may change the wire protocol and formats without keeping older builds working (product owner, 2026-11-14). |
| 5 | Joining | No manual accept in this version (18.1 question 5); an invite must name the invitee's email (19.7). |
| 6 | Blueprint updates | An owner's edits reach the team on *Publish update*, not on save (20.4). |
| 7 | Entries by removed members | Accepted if signed by a key the log ever bound to the author, and marked (7.6). |
| 8 | Reassignment power | A team owner may reassign only documents of removed or absent members (19.5). |
| 9 | Domain comments | Kept forever, folded when incorporated (6.12). |
| 10 | Snapshots | Beside the live document, ignored by the index; a late joiner lacks older versions (6.12). |
| 11 | Frontmatter key names | Bare, read only when the value has the expected shape (6.2). |
| 12 | Deviation log | The set of `deviation` entries, not a body section (6.13). |
| 13 | Team log format | `<team>-team.log`, one JSON operation per line; the team is identified by the hash of `create-team` (19.1). |
| 14 | Publishing | A `team:` frontmatter key plus a first release, by the owner, one team per document (20.1). |
| 15 | Deletion | A tombstone is a final release; the stub and the last full copy are kept under `.kalka/` (20.6). |

### 18.3 Decisions (v0.5, desktop app, product owner 2026-11-14)

| # | Question | Decision |
|---|---|---|
| 1 | Networking in the front end | None: WebRTC, signaling and sync were already in Go (`internal/transport`, `internal/peerjs`); the web UI only calls the local API and its event stream. Nothing moved. |
| 2 | Shell and daemon | The shell runs the daemon in process when it takes the lock, and attaches when a headless daemon holds it (4.1). Wails' own single-instance feature is not used; the lock answers the question. |
| 3 | Commands and the daemon | Four classes (12). The request's "read-only commands work on local files" became "file commands never need a daemon": `comment` and `seed` already wrote safely without one. |
| 4 | Pause | Fully offline: both rendezvous registrations closed, replication, joins and probes stopped. Peers see a closed laptop; no wire change. It survives a restart and is always visible (22.3). |
| 5 | Invite joins | No approval step; the invite is the approval. "A pending invite join" notifies the owner that someone joined with their invite, and the joiner that the join completed or failed (22.4). |
| 6 | One product per platform | One per-user installer or bundle per platform (23.1). On Windows it holds two executables built from the same code, because Windows fixes a program as console or GUI at link time: a console program opens a console window at login, a GUI program prints nothing to a terminal. On Linux the desktop shell links WebKitGTK and would not start where it is missing, so the static `kalka` stays separate. |
| 7 | Local IPC | The same loopback HTTP API for the page, the shell and the CLI, with token, `Host`, `Origin` and `Sec-Fetch-Site` checks (13). No Unix socket and no named pipe: one transport is simpler, the window needs loopback HTTP anyway (the Wails asset server on Windows delivers only complete responses, which breaks the event stream), and owner-only permissions on `daemon.json` protect the token. |
| 8 | Wails version | v3, pinned to an exact release: it has the tray, a cancellable close, a hidden start and notifications built in; v2 has no tray and is in maintenance. Fallback tray: `fyne.io/systray`, only if the Wails tray fails a platform. Reasoning and sources: `docs/plan-desktop-app.md` §3. |
| 9 | Signing | No certificates yet. Signing is a configuration step; unsigned builds succeed and say so (23.5). |
| 10 | Windows testing | Done by the product owner on a real Windows PC after the work; until then Windows-specific code is compiled and unit tested only. |
| 11 | Updates | No auto-updater until builds are signed. |

---

## 19. Teams and roles

### 19.1 Team log

A team is a **signed, append-only set of operations**, not an editable document. The current state (members, owners, domain assignments, reassignments) is computed from it by a deterministic replay (19.4).

- File: `<folder>/<team>/<team>-team.log`, one operation per line, as JSON. It is not Markdown, because hashes must cover exact bytes and editors change whitespace. It is not meant to be edited; the UI and `kalka team log` render it as a readable history. The index ignores it (it is not `*.md`).
- One operation: `{v, team, op, args, prev, signer, at, sig}`.
  - `v`: the rule version that replays it. All operations of v0.4 are `v: 1`. A later rule change uses a new version and never changes how an existing operation replays.
  - `team`: the team hash (19.2); empty in `create-team` itself.
  - `op` and `args`: one of the operations below.
  - `prev`: the hashes of the operations it builds on: the heads of the signer's log when it was made. These links make the log a causal graph.
  - `signer`: the signer's public key. `at`: the signer's clock, for display only; it plays no part in replay.
  - `sig`: an ed25519 signature over `"kalka-teamlog-v1\n" + hash`.
- An operation's hash is SHA-256 over its canonical encoding without `sig`: UTF-8 JSON with keys in lexicographic order, no insignificant whitespace, no floating-point numbers, `prev` sorted. The exact encoding is fixed in `docs/teamlog.md` and is part of rule version 1. Keys and signatures are base64url without padding.
- An operation is *known* once a peer holds it and has checked its signature. Operations are exchanged by hash (20.3), never as a whole file.

| Operation | `args` | Who may sign |
|---|---|---|
| `create-team` | `name`, `email` (the creator's) | Anyone; the signer becomes the first owner and member |
| `add-member` | `email`, `pubkey`, optional `invite` (id) | A team owner |
| `remove-member` | `email` | A team owner |
| `add-owner` | `email` (must be a member) | A team owner |
| `remove-owner` | `email` | A team owner |
| `assign-domain` | `domain`, `owner` (email), `after` (version) | A team owner |
| `reassign-document` | `doc` (`kalka_id`), `from` (the removed owner's email), `owner` (email), `after` (version) | A team owner (19.5) |

### 19.2 Team identity

A team's identity is the hash of its `create-team` operation. Its name is a label: `[a-z0-9][a-z0-9-]*`, used in file names and in `domain:` references. Two different teams with the same name can exist; the second one held locally gets a suffix on its folder, and a reference to that name is shown as ambiguous. A member's identity within a team is the pair of email and key bound by `add-member`.

### 19.3 Roles

- **Team owner.** A team has at least one. Any team owner may add and remove members, add and remove owners, assign domain owners, and reassign documents (19.5). Every owner is also a member.
- **Domain owner.** The member a team owner appointed with `assign-domain`. Releases versions of that domain document and incorporates comments (6.12). Cannot change the roster.
- **Member.** Comments on domain documents, owns blueprints and seeds, publishes to the team, sees the team's pre-backlog.

The roles answer who may *change* what: the roster, domain releases, ownership. They do not limit reading (21).

### 19.4 Replay and validity

The state is a pure function of the set of known operations, so every peer holding the same set computes the same state, without coordination. `docs/teamlog.md` gives the precise rules and how they are computed; in short:

1. **Authority in view.** An operation counts only if its signer holds an owner term granted by one of its ancestors and not ended by one. Operations whose predecessors are not all known are pending, not discarded.
2. **Removal wins.** When the removal of X is effective, every non-removal operation X signed under the ended term that is not an ancestor of the removal is discarded, including operations concurrent with it. Back-dating an operation therefore gains nothing.
3. **Seniority decides between removals.** Removals are decided in order of their signers' seniority. A removal is blocked if an already accepted removal ended its signer's term without having seen it. An accepted removal stands even if a less senior owner removes its signer concurrently.
4. **Support, not ancestry.** An operation stands or falls on its own support: the signer's owner term, the target's member term, and the removal a reassignment relies on must be effective and in its view. Having a discarded operation among one's predecessors does not discard an operation. Otherwise a removed owner's back-dated operation, once merged, would void every later honest operation.
5. **Seniority** is the chain of owner grants from the creator: the creator first; an appointee directly after their appointer, ordered among the appointer's other appointees by causal depth and then hash. Nobody can appoint an owner senior to themselves. With two grants for one owner, the better counts. Re-adding starts a new term, and seniority counts from the new grant.
6. **Never without an owner.** If the effective removals together would leave no owner, the least senior of them is discarded.
7. **No self-removal.** An owner cannot remove themselves; another owner removes them.
8. **Same subject.** An `add-member` for an email that is a member in the signer's view is discarded. Of concurrent `add-member` operations for one email, one key, or one invite id, the first in replay order counts. For `assign-domain` and `reassign-document` on one subject, the last counts. The replay order is causal depth, then hash. These are the only places where a hash decides, and nothing of value is gained by searching for one.

Resulting behaviour: in a mutual removal the senior owner stays. In a ring of three, the most senior remover's removal stands, and at least one owner remains. A member added by an owner who is concurrently being removed is not a member. A reassignment falls when the removal it relies on is discarded.

Replay reports the discarded operations with their reasons, and the Team view shows them.

### 19.5 Removing a member and reassigning documents

- A team owner removes a member with `remove-member`. Together with it, the owner's `kalka` emits one `reassign-document` per published document it holds whose effective owner is the removed member, giving each to the removing owner. These operations name the removal in their `prev`, so if the removal is discarded (19.4), they are discarded with it, and ownership stays where it was.
- Each `reassign-document` names `from`, the removed owner. Replay accepts it only with an effective `remove-member` of `from` among its ancestors (19.4 rule 4). The document layer checks that `from` is the document's effective owner.
- `after` names the last released version of the document the removing owner had seen. Releases by the previous owner above that version are void.
- A team owner may reassign a document only with the removal of its current owner as an ancestor, or when its current owner is not a member (a document the remover had not received at removal time). A team owner cannot take a present member's document; that member hands it off as usual.
- **Effective owner of version n** of a published document: start from its first owner; signed handoff entries move ownership as in 7.2; an effective `reassign-document` (or, for a domain document, `assign-domain`) moves it for all versions above its `after`. For a published document this chain, log included, is the authority. The `owner:` line is what people read: when the new owner's `kalka` sees an effective reassignment, it rewrites the `owner:` line and makes a release. Until then the UI shows "owner: X, reassigned to Y, pending". A document whose effective owner is not a member shows "owner is no longer a member".
- If a reassignment is later discarded, releases made under it become void. A peer holding one marks the document "ownership disputed" and takes the highest valid release it is offered. It does not rewrite its file without a valid replacement.

### 19.6 Domain assignment

`assign-domain {domain, owner, after}` makes `owner` the domain owner for releases above version `after`. Releases of that domain signed by anyone else above `after` are not applied (20.4). The `owner:` line of the domain document follows as in 19.5: when the new domain owner's `kalka` holds the release at the cut-off, it rewrites `owner:` and releases the next version.

- Unlike a reassignment of a blueprint, which names the signer of one version and lets the chain go on from there, an assignment covers every version above `after`: a domain owner cannot hand releasing to someone else by naming them in `owner:`.
- `after` is the highest release of the domain's documents the assigning owner holds.
- Removing a member who owns a domain also assigns the domain to the remover, with the removal as the only predecessor, as for reassigned documents (19.5).

### 19.7 Invites and joining

1. A team owner runs `kalka team invite <email>`. The token is one line of base32 holding: the team name and hash, the inviting owner's public key, the invitee's email, a 32-byte one-time secret, the expiry (default 72 hours, `--expires`), and the rendezvous address. The open invite is stored in `invites.yaml`. `kalka` does not send the token; the owner passes it on through any channel.
2. The invitee runs `kalka join <token>`. The owner's key becomes their trust anchor for the team and is pinned as verified. The pending join is stored in `joins.yaml`. If the token names another rendezvous than the configured one, `kalka` says so.
3. The joiner's daemon probes the owner's key ID once a minute until it meets the owner or the invite expires; this survives restarts. Only the joiner can dial: the owner does not know the joiner's key. The owner's part is to be online. Nothing is queued on the rendezvous server.
4. On meeting, the joiner's `hello` carries `invite: {id, team, proof}`, where `team` is the team hash from the token and `proof` is an HMAC-SHA-256 under the secret over both public keys and the team hash. The owner checks it after `auth`, before any code prompt. A valid proof counts as verification, so neither side compares a code.
5. The owner's `kalka` approves automatically (the invite is the approval), appends `add-member {email, pubkey, invite}` with the email *from the invite*, marks the invite used, and sends the log on the same connection. The joiner checks that the log starts at the team hash in its token and contains its own `add-member`.
6. A bad proof, an expired invite, a used invite, or an email other than the one invited ends the connection with a `bye` naming the reason, and the joiner's UI shows it.

Single use holds in two places: the issuing owner's `invites.yaml`, and the replay rule that discards a second `add-member` for the same invite id (19.4 rule 8). Only the issuing installation can redeem an invite, because the joiner dials its key ID.

---

## 20. Team replication

### 20.1 Scope and publishing

Every member keeps a full replica of the team's **published documents**: the team log, domain documents and their snapshots, and the blueprints and seeds published to the team. **Private documents and one-to-one handoffs never take part**: they enter no inventory, and a request for their `kalka_id` gets the same answer as a request for an id that does not exist.

A document is published when its owner runs *Publish* or `kalka publish <file> --team <team>`: `kalka` writes `team: <team>` into the frontmatter and makes the first release (20.4). Only a member can publish, only to a team they belong to, and a document belongs to at most one team. A seed budded from a published parent is published to the same team by default. Unpublishing is a tombstone (20.6); copies already replicated cannot be recalled.

A published document can still be synced one-to-one with someone outside the team. That peer has no team log, so it applies the rules of 7.2 unchanged. Handing a published document off to a non-member is refused; the owner unpublishes it first.

### 20.2 Trust between members

Members trust each other through the team log: the keys it binds are verified for team traffic (8.2), so members never compare codes and received documents raise no accept prompt. Every operation, release and entry is checked before it is written. Nothing fails silently: a team document that fails a check is not written, and the Team view lists it with the reason.

### 20.3 Sessions and scheduling

- **When.** At daemon start, when a member comes online, after a local change to a team document, and every ten minutes with jitter. Each round picks at most three online members; a local change is pushed to at most five. Team members are probed only while a round needs them.
- **A session**, over a key-ID connection with another member: exchange log heads and one digest over the inventory (`team_sync`). If both match, the session ends after one round trip. Otherwise: missing log operations first (`team_log`), then inventory lines (`team_inv`), then the wanted documents (`team_want`, each followed by a `sync_offer` with `intent: replicate`).
  - The responder tells a session apart from a one-to-one sync by its first message: `team_sync` starts replication, anything else a sync (10).
  - The responder merges the initiator's log operations before it checks membership, so a member just added by an operation the initiator brings is admitted in the same session. A peer that is not a member after the merge gets `bye` with reason `not-member`.
  - An inventory line is `kalka_id`, the head version, the head's plan hash, whether it is a tombstone, and a digest over the document's entries that verify against team keys. A line is wanted when the document is not held, when it is a tombstone, when its version is higher, when the entry digests differ, or when the versions are equal and the plan hashes differ (a dispute to learn about). Nothing is wanted for a document held as a tombstone.
  - The initiator sends the documents the responder wants first, then receives the ones it wants. A document that is not published, or not held, is answered with an offer with empty content: unavailable, not an error.
  - Each received document is merged with the rules of 20.4 and written only if the merge changes the local file. A refusal (no valid release, a chain that does not lead to the incoming plan) is recorded with its reason for the Team view, and the session goes on. When the entries are taken but the incoming plan is not, because the release naming it does not count, the document is written and the reason is recorded too: each peer says why it did not apply a release.
- **One-to-one syncs** of a published document between members still use the merge of 7.2. Replication corrects any difference at the next round: the release chain decides the plan.
- **Relaying.** Any member can pass on any published document it holds. No author or owner needs to be online.
- Peers that are not members never see a `team_*` message.

### 20.4 Merge rules for published documents

**Team log.** The union of valid operations, replayed (19.4).

**Releases.** A `release` entry makes a version of a published document's plan official:

```
### release · alice@corp.com · 2026-11-20T09:00:00Z · § · rev:<8 hex> · id:<12 hex> · sig:<base64url>
v7 · plan:<64 hex SHA-256 of the normalized frontmatter and body> · owner:alice@corp.com
incorporates: 7c2e91ab04f3, a41b77c09e12
Renamed "consignment" to "parcel"; added the invariant on redelivery attempts.
```

- The first line is `v<N> · plan:<hash> · owner:<email>`, followed by ` · deleted` for a tombstone (20.6). `owner` is the `owner:` of the released plan: the one who may release the next version. `incorporates` and the note are optional.
- The v2 signature covers the entry's full digest, which includes the version, the plan hash and the owner.
- **The release chain.** The valid releases of a document form a chain:
  - version 1 counts if it is signed by the owner it names (the publisher);
  - version n+1 counts if it is signed by the owner that version n names;
  - an effective reassignment (19.5) or domain assignment (19.6) with cut-off `after` makes its new owner the signer of version `after`+1, and voids releases by anyone else from that version on;
  - every release needs a v2 signature by a key the team log has bound to its author;
  - nothing above a gap, or above a tombstone, counts.

  This makes the chain the effective owner of 19.5. A handoff of a published document is a release naming the new owner, made by `kalka` when the owner hands off.
- **Disputed versions.** If one version has two different valid releases, neither counts, and nothing above them does; the document is shown as *ownership disputed*. Choosing one would let the signer of the second win by searching for a lower id. For version 1, that signer could be any member. A team owner settles a dispute by removing the member who caused it and reassigning the document (19.5); with `after: 0` the reassignment names the signer of version 1.
- **Rule.** For a published document of a team whose log the local peer holds, the incoming plan (frontmatter and body) replaces the local one when the head of the chain names the incoming plan's hash and has a higher version than the local plan's release. The sender does not matter. A local plan that is not released (the owner's unreleased edits) counts as the highest version the local file already held a release of. A local plan whose only release is void (a discarded reassignment) is replaced by the head. An incoming plan that is not the head is not taken, and its entries are still merged. The version never goes down. A new document is accepted only with a valid release of its plan.
- Domain documents also carry `version:` in frontmatter; blueprints and seeds carry the version only in their releases.
- **Releases are made on an action, not on save.** *Release* for a domain document, *Publish* and *Publish update* for a blueprint or seed. `kalka`'s own changes to a published document (promotion, handoff, reassignment) release automatically. An owner with unreleased edits sees a banner.

**Entries.** The union of 7.2 step 6, sorted by `(timestamp, id)`, with the copy whose signature verifies kept. An entry enters the replica only if its v2 signature verifies against a key the log has ever bound to the author's email (7.6). Others stay local only.

### 20.5 Where received documents go

Documents received from members are created in `<folder>/<team>/`, with the sanitized name the sender suggests. Documents are always found by `kalka_id`, so a user may move a received file within the folder. Domain snapshots are written beside their live document.

### 20.6 Deletion

- Only a document's effective owner deletes it for the team: *Delete for the team* or `kalka delete <file>`. This makes a release with the next version, marked as a deletion, whose plan is a **stub**: the frontmatter with `deleted: true` and nothing else.
- It spreads like any release, and no release above a tombstone is valid. A tombstone beats any older copy, in both directions of a merge.
- On each peer the file leaves the folder. The stub goes to `<folder>/<team>/.kalka/tombstones/<kalka_id>.md` and the last full copy to `<folder>/<team>/.kalka/trash/`. Replication reads the tombstone directory, so an offer of an older copy is answered with the tombstone.
- The stub keeps only its release entries, the tombstone included, so that every peer can check the chain that leads to it. This is the one place where valid entries are dropped (2).
- Tombstones are kept forever; each is a few hundred bytes.
- A plain file deletion is not a tombstone (14).

---

## 21. Threat model

What the design protects, and what it does not:

- **Roles protect against unauthorized changes, not against reading.** The roster, domain releases and ownership can only be changed by the roles allowed to (19.3), and every peer checks this itself. Reading is not restricted within a team: every member holds a full replica. Replicas cannot be recalled: a removed member keeps everything they already received.
- **A leaked invite token** lets its holder join until it is used or expires, but only under the email the owner named (19.7), and only once. Short expiry and single use limit the window; an owner who suspects a leak removes the member.
- **A team owner's key compromises the team.** Its holder can change the roster and reassign documents. The most senior owner's key is the root of the team: an owner can be removed by a more senior one, but a removed owner who is senior to the remover can still counter-remove (19.4). Nobody can appoint an owner senior to themselves, so the creator's key outranks every other. Key rotation is future work (18.1).
- **The rendezvous server is not trusted.** It can refuse service, keep a peer offline by occupying its ID (on servers other than `kalka rendezvous`, 9.2), and see who connects to whom. It cannot read or alter a sync (channel binding, 8.4) or impersonate a member (pinned keys and the team log).
- **Members can comment in their own name only.** Entries are signed (6.5); a forged entry does not enter the replica (7.6). Entries written outside `kalka` stay unsigned and local.
- **Private documents stay private.** One function decides what replicates (20.1); a private document never appears in an inventory, and requests for it look like requests for unknown ids.
- **The local API is reachable by local processes and by web pages aimed at `localhost`.** It answers only with the per-start token, checks `Host` against DNS rebinding, and refuses state changes from foreign origins (13). Another local user cannot read the token (owner-only `daemon.json`). Processes of the same user are trusted, as they can read the identity key anyway.
- **Out of scope:** a member's machine being compromised; traffic analysis; denial of service by a member flooding operations, which other members see in the log and can answer by removal.

---

## 22. Desktop app

Team replication works only while members are online at the same time, so kalka runs in the background from login, independent of any window. The desktop shell gives that background process a window and a tray icon.

### 22.1 Window and tray

- The window shows the web UI (11) from the daemon's localhost address. Nothing is bound into the page; it behaves exactly as in a browser tab.
- **Tray menu:** *Open kalka*; a status line ("Online · 4 of 6 peers reachable · last sync 14:02", "Paused", or "Offline: rendezvous not reachable"); "N waiting for you" when prompts or joins are pending, opening the window on them; *Pause sync* / *Resume sync*; *Start at login* with a check mark; *Quit kalka*.
- The status comes from `GET /api/summary`: peers whose last probe says online out of all known peers and members; the latest replication round that reached a member or completed one-to-one sync; prompts waiting for an answer, joins not completed, and teams with a push still queued. The tray refreshes on the daemon's events, at most once a second.
- **Quit** asks first: "kalka will stop syncing with your team until you start it again." It then stops the daemon after its running rounds finish. When the shell is attached to a headless daemon, quitting stops that daemon too, and the question says so.

### 22.2 Settings

| Setting | Default | Meaning |
|---|---|---|
| Start at login | on, set once at the shell's first start | A per-user entry, no admin rights: Windows `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, macOS a LaunchAgent in `~/Library/LaunchAgents`, Linux an XDG autostart file. The entry starts the shell with `--hidden`, or `kalka daemon --background` where no shell is installed. The system's entry is the truth; it is not copied into `state.yaml`. |
| Keep running when closed | on | Closing the window hides it. The first time, one notification says that kalka keeps syncing and where its icon is. Off: closing the window quits the shell, and an in-process daemon stops with it. |
| Notifications | on | One switch for the notices of 22.4. Prompts still show in the window when it is off. |
| Pause | off | 22.3. |

- `--hidden` starts the shell with no window shown; autostart passes it. Starting the shell again always shows the window.
- On Linux without a tray host (stock GNOME without an extension), closing minimizes instead of hiding, so the window stays reachable.

### 22.3 Pause

Pause takes kalka offline: the daemon closes its rendezvous registrations and stops replication, joins and presence probes. Local work continues (comments, seeds, publish and release write locally and are pushed after resume). While paused, commands that need the network (`send`, `handoff`, `join`) fail with `"kind": "paused"`. The pause is kept in `state.yaml` and survives a restart; the tray icon has a paused state, the UI shows a banner, and a start in the paused state raises one notification.

### 22.4 Notifications

The core decides; the shell or the headless daemon shows them as OS notifications. A click opens the window (or the browser) on the subject.

| Notice | When |
|---|---|
| Handoff waiting | An incoming handoff needs an answer (as today, with the verification and new-blueprint prompts). |
| New comments on your document | A sync or replication adds entries by someone else (not releases, handoffs or `budded`) to a document the local user owns. One notice per document per sync or round; a first copy raises none. |
| Someone joined with your invite | The owner's side, when a join is admitted. |
| Your join completed or failed | The joiner's side, when a join reaches a final state. Retries raise nothing. |
| Newer domain version | A received release raises a domain to a version above the one a document owned by the local user references. One notice per domain and version. |

On macOS, an unsigned build cannot post to the notification centre; it shows prompts as dialogs and other notices only in the window. In a browser tab the page's own browser notifications remain; in the desktop window they are not used.

---

## 23. Platforms and distribution

### 23.1 What is installed

| Platform | Package | Contents |
|---|---|---|
| Windows (primary) | Per-user installer, no elevation, into `%LOCALAPPDATA%\Programs\kalka`; adds the folder to the user's `PATH` and a Start-menu shortcut with kalka's AppUserModelID | `kalka.exe` (console: CLI and headless daemon), `kalka-desktop.exe` (GUI: the shell; with arguments it runs the CLI without a console window), `kalka-plugin-gh.exe` |
| macOS | `Kalka.app` in `~/Applications`, or anywhere | One executable, which is the shell without arguments and the CLI with them; `kalka` on the `PATH` is a symlink to it |
| Linux | One archive | `kalka` (static), `kalka-desktop`, a `.desktop` file and an icon, and the list of system packages the shell needs |

The headless archives of 15 remain for servers, containers and machines without a desktop.

### 23.2 Windows

- WebView2 is preinstalled on Windows 10 and 11, but can be missing or removed by policy. Before opening a window the shell reads the runtime version from the registry keys Microsoft documents for the Evergreen runtime. If it is missing, a native message names the download and offers "Open kalka in the browser instead", which keeps the daemon running and uses `kalka open`. The window is never blank.
- Notifications need an AppUserModelID registered for the sender. Wails' notification service registers its own under `HKCU\Software\Classes\AppUserModelId` at first start, so the installer's Start-menu shortcut is a plain one. Untested on real Windows until the D8 pass.

### 23.3 macOS and Linux

- macOS: notifications and a clean tray need a signed bundle (23.5). Unsigned bundles are ad-hoc signed so they start on Apple Silicon.
- Linux: the shell needs GTK 4 and WebKitGTK 6.0 (Ubuntu 24.04 or newer as the stated base). The tray uses StatusNotifierItem; stock GNOME shows it only with an extension (22.2).
- Webview rendering differences found during the work are listed here. None recorded yet: the window has only been checked on macOS, by its network traffic and the smoke test, not by eye.

### 23.3a Building without cgo

Wails v3.1.0 has experimental cgo-free back ends (`purego` tag; GTK 4 on Linux by default). With them `kalka-desktop` builds for macOS and Linux from any host, and the macOS build passes the shell's smoke test (hidden window, attach, stop). The release build keeps cgo on macOS and Linux and offers `-purego` as an option, until the cgo-free window has been used by hand on both systems.

### 23.4 Restricted environments

Where IT policy blocks autostart entries, tray apps or the webview, run `kalka daemon` by hand, from a login script, or from any scheduler, and use the browser through `kalka open`. Every feature except the tray and the window works this way. What IT needs to allow: outgoing WebSocket to the rendezvous server, UDP or TURN for WebRTC (9), and a loopback TCP port for the local API (13).

### 23.5 Code signing

Signing is a configuration step of the release build, and its absence is not an error: an unsigned build succeeds and says "unsigned" per artifact.

- Windows: an Authenticode code-signing certificate (on a hardware token or a cloud signing service, as certificate authorities issue them today) and a timestamp server. The build runs a configured signing command on every executable. Unsigned builds trigger SmartScreen; `docs/install.md` explains how to run them.
- macOS: Apple Developer Program membership, a Developer ID Application certificate, the hardened runtime, notarization and stapling. The build reads the identity and the notary profile from the environment.
- Linux: no signing; `SHA256SUMS` as today.
