# kalka — blueprint sharing in teams

*Kalka* (Polish: tracing paper, carbon copy) is the working name. It appears in the binaries, the plugin prefix (`kalka-plugin-<scheme>`), the frontmatter key (`kalka_id`), the URL scheme (`kalka://`) and protocol strings. Keep the name in one constant per component so a later rename is cheap; availability of the name has not been checked.

Specification v0.6 · 2026-11-15 · status: ready for implementation

This document is the hand-over to the implementation agent. Sections 1–15 and 19–23 are normative. Section 16 gives milestones and acceptance criteria. Section 17 records what changed during reviews and why. Section 18 lists open questions and the decisions taken on them; each open question has a default so work is not blocked.

v0.4 added teams with roles and a signed operation log (19), replication between members (20), a threat model (21), seeds (6.11), domain documents (6.12) and the blueprint reference (6.13). v0.5 made kalka a background desktop app with a first-class CLI (4, 22, 23).

v0.6 makes teams the only way to share. One-to-one syncs, key pinning, verification codes, accept prompts and conflict files are gone; every peer is a member of a team you share (8). A person belongs to any number of teams plus a local Personal space (14, 20.1). There are two roles, team owner and member (19.3). Handoff is immediate, and a team owner can reassign any document (7). Documents can be copied or moved between teams, with the destination's acceptance and a countersigned history (20.7). Invites can be opened as `kalka://join/` links (19.7). The app does every workflow, and the CLI does every one too (11, 12). The rendezvous server is its own binary, `kalka-rendezvous` (9.6). The triage behind v0.6 is `docs/plan-teams-only.md`. Section numbers the code cites are kept where the section survives.

Wording: **must** is a requirement, **should** is a strong default the implementer may deviate from with a written reason, **may** is optional.

---

## 1. Purpose and scope

A *blueprint* is a single Markdown file describing an implementation plan. `kalka` lets a team share blueprints, review them, and pass ownership between members, directly between their machines, with comments kept inside the same file. It is the equivalent of a pull request for an implementation plan.

`kalka` is agnostic about how blueprints are produced and consumed. Each member may use a different workflow or agent pipeline to write a blueprint and to act on it afterwards. `kalka` assumes nothing about section names, frontmatter fields or pipeline steps. The folder is an exchange area: a blueprint is drafted in Personal, published to a team for grooming, and moved back out to its owner's own pipeline once accepted.

### Goals

- Use without thinking: one product, three settings, drop a file in a folder.
- No server-side storage. The only server is a stateless rendezvous point with an optional relay.
- One artifact: the blueprint `.md` file is the whole state of a review.
- Only a document's owner changes its plan; everyone else adds entries.
- A team shares its domain language, its seeds and the blueprints it publishes. Every member holds a replica, so nobody has to be online for anyone else to read (19, 20).
- A person can work in several teams, and teams stay apart: nothing crosses from one team to another except by a transfer that the receiving team accepts (20.7).
- Every workflow can be done in the app and with the CLI (11, 12).
- References to code, tickets and docs are resolved by plugins; the core knows nothing about GitHub, Jira or Confluence.

### Non-goals (v1)

- Authoring or editing blueprints. Users and agents write them with their own tools. `kalka` writes only the frontmatter keys listed in 6.2 and the entries it records; the one document it assembles is a seed, from text the user typed (6.11).
- Sharing with someone outside every team you belong to. Create a team, even of two.
- Asynchronous delivery through a server. Team documents travel through whichever members are online (20), never through a server.
- Version history, branching, or any general VCS-like state. Three fixed structures are the exceptions: the team log (19.1), domain snapshots (6.12) and tombstones (20.6).
- Moving a document from a team back to Personal.
- More than one device per email address.
- A Jira or Confluence plugin. Only a GitHub reference plugin ships.
- Real-time co-editing or live cursors.

---

## 2. Design principles

1. **The file is the state.** Everything needed to continue a review is in the `.md` file. `kalka` keeps no per-blueprint database, sidecar or history. Local application state is limited to configuration, the identity key, two small team files in the config directory (open invites and pending joins) and the daemon's settings (5). In the folder, teams add their logs, pending transfers, tombstones and domain snapshots (14).
2. **Grow-only log.** The plan body is changed only by the owner. Everyone else adds entries to a review log. Entries are never edited or removed by `kalka`, with one exception: a tombstone keeps only its release entries (20.6).
3. **The server is not trusted.** The rendezvous server only introduces peers. Peer identity is verified end to end, against the keys team logs bind (8).
4. **Core is concept-free.** The core handles files, teams and entries. Anything domain-specific lives in a plugin. The core knows nothing about the workflow that produced a blueprint: no assumed sections, no pipeline steps, no meaning attached to frontmatter beyond the keys listed in 6.2. The blueprint reference (6.13) is guidance a document opts into, never a requirement.
5. **Windows is a first-class platform.** Primary users work on Windows with PowerShell. Paths, line endings, file locking and `.exe` lookup must work there first.
6. **Content never blocks.** No user action is refused because of what a document says. Content problems are shown as warnings or hints. Checks of signatures and roles are not content validation: an operation, release or entry that fails them is not applied, and the UI says why.
7. **Brainless.** The user configures email, rendezvous and folder. Keys, peer IDs, team state and caches are created and managed by `kalka`.
8. **The app and the CLI are equal.** Every workflow has a control in the app and a command with `--json` and stable error kinds (12). Agents and scripts drive kalka through the CLI.

---

## 3. Glossary

| Term | Meaning |
|---|---|
| Blueprint | One Markdown file with frontmatter, a body and an optional review log. |
| Body | Everything after the frontmatter, excluding the review log section. |
| Plan | Frontmatter and body together: what only the owner changes. |
| Review log | The section headed `## Review log`, containing entries. Managed by `kalka`. |
| Entry | One comment, deviation, release or other record in the review log (6.5). |
| Owner | The one member allowed to change a document's plan and release it (7.1). |
| Team | A group of members defined by a signed operation log (19). Identified by the hash of its `create-team` operation; its name is a label. |
| Team log | The team's append-only set of signed operations, `<team>-team.log` (19.1). |
| Team owner | A member who may change the roster and reassign any document of the team (19.3). A team has at least one. |
| Member | Anyone the team log currently lists. |
| Peer | A member of a team you share, identified by the email and key that team's log binds (8). |
| Personal | The local, never replicated part of the folder, for unshared drafts (14). |
| Team document | A document in a team's folder with `team:` naming that team and a valid release; replicated to every member (20.1). |
| Publish | Moving a document from Personal into a team, with its first release (20.1). |
| Release | A signed entry that makes a version of a team document's plan official (20.4). |
| Handoff | A release by the owner naming another member as owner (7.2). |
| Reassignment | A team owner's signed operation naming a document's next owner (7.3, 19.5). |
| Tombstone | A final release marking a team document deleted or moved away (20.6). |
| Transfer | A copy or move of a document into another team, accepted there by a team owner (20.7). |
| Attested entry | An entry brought in by a transfer: vouched for by the person who started it and the owner who accepted it, not verified against the destination's log (20.7). |
| Waiting item | Something only the local user can move forward, counted per team (11.3). |
| Seed | A blueprint with `status: seed`: an idea split off from a growing blueprint (6.11). |
| Domain document | A team document with `kind: domain` holding a team's shared language (6.12). |
| Rendezvous | The signaling server (PeerJS protocol) that lets two peers find each other. |
| Key ID | The peer ID a daemon registers at the rendezvous, derived from its public key (9.2). |
| Rev | Short hash of the normalized body. Tells which body an entry was written against. |

---

## 4. Architecture

```
 Member A                        Rendezvous                    Member B
┌─────────────────────┐   ┌─────────────────────┐   ┌─────────────────────┐
│ folder/             │   │ kalka-rendezvous    │   │ folder/             │
│   personal/         │   │ (or any PeerJS      │   │   personal/         │
│   parcel-ops/  ◀────┼┄┄┄┼┄ signaling server)  ┼┄┄┄┼──▶ parcel-ops/      │
│   library-desk/     │   │ stateless, + TURN   │   │                     │
│ kalka daemon        │   └─────────────────────┘   │ kalka daemon        │
│ app window / tray   │                             │ app window / tray   │
└─────────┬───────────┘                             └──────────┬──────────┘
          └──────── WebRTC data channel (DTLS, P2P) ───────────┘
                parcel-ops traffic only: A and B share no other team
```

Components:

- **Core** — Go packages with no UI code: identity and keys, the team log, document storage and merge, replication, transfers, P2P networking, the invite flow, the blueprint reference. All networking lives here; the web UI never talks to a peer or to the rendezvous.
- **Daemon** — one long-running process per user and machine that hosts the core. It watches the folder, keeps the rendezvous connection, runs replication, invokes plugins, and serves the local API and the web UI on localhost (13). Closing every window never stops it.
- **CLI** — the `kalka` binary. File commands work without a daemon; daemon commands talk to the running daemon over the local API and start one when none runs (12).
- **Desktop shell** — `kalka-desktop`: a window showing the web UI and a tray icon (22). It hosts the daemon in its own process or attaches to one already running (4.1), and receives `kalka://` links (19.7). It is optional: everything works without it.
- **Web UI** — single-page app embedded in the binary, shown in the desktop window or a browser tab. A pure view over the local API.
- **Plugins** — separate executables named `kalka-plugin-<scheme>`, JSON over stdio.
- **Rendezvous** — any PeerJS-protocol server: the public PeerJS cloud (demo), stock `peerjs-server`, or `kalka-rendezvous`, a separate small binary that adds TURN and key-ID proofs (9.6).

### 4.1 One daemon per user

- The daemon takes an exclusive lock on `<config>/daemon.lock` before anything else, the setup form included: `flock` on macOS and Linux, `LockFileEx` on Windows. The kernel releases it when the process ends, however it ends, so there is no stale lock. The PID in the file is for messages only.
- A second start finds the lock held. `kalka daemon` prints "already running (pid N)" and exits 0. The desktop shell attaches to the running daemon, or asks the shell that already runs to show its window (on a route, for a link), and exits 0.
- **Placement.** The desktop shell runs the daemon in its own process when it gets the lock. When a headless daemon holds the lock, the shell attaches to it as a client. Either way the window and the tray use only the local API.
- A daemon started by the CLI is always headless, also where the desktop shell is installed: an agent's command never makes a window or a tray icon appear. A shell started later attaches.
- Known limit: two config directories naming the same blueprint folder are not prevented. That is a deliberate setup (used in tests); a lock inside the folder would have to work on network shares, where file locks are unreliable.
- **Restricted environments.** Where policy blocks autostart or tray apps, `kalka daemon`, started by hand or by any scheduler, is a fully supported way to run kalka (23.4).

---

## 5. Configuration

File: `%APPDATA%\kalka\config.yaml` on Windows, `$XDG_CONFIG_HOME/kalka/config.yaml` (default `~/.config/kalka/`) elsewhere. If it is missing, the UI opens on a setup form with exactly three required fields.

```yaml
email: ewa@example.org                  # required: who you are
rendezvous: wss://kalka.example.org/    # required: or the literal "peerjs-cloud"
folder: C:\Users\ewa\blueprints         # required: where blueprints live

# optional, never asked for during setup
ui_port: 7717           # localhost port
ice_servers: []         # manual override of ICE configuration
```

Teams are not configured: they are created and joined from the app or the CLI, and each is a folder with its log (14).

Also stored in the config directory, never in the blueprint folder:

- `identity.key` — ed25519 private key, generated by `kalka setup` or on first run, file mode 0600 (on Windows: ACL restricted to the current user).
- `plugins/` — optional directory searched before the executable's folder and `PATH`.
- `invites.yaml` — open invites this installation issued as a team owner (19.7). Owner-only permissions.
- `joins.yaml` — pending joins: tokens redeemed with `kalka join` and not yet completed (19.7). Owner-only permissions.
- `daemon.lock` — the single-daemon lock (4.1).
- `daemon.json` — the running daemon's PID, local API address and per-start token (13). Owner-only permissions. Removed at exit.
- `state.yaml` — settings and marks the daemon owns and writes: `paused`, `notifications`, `keep_running_on_close`, `close_notice_shown`, `autostart_initialized` (22), and the `seen` marks of waiting items (11.3). Changed only through the local API.
- `kalka.log` — log of a daemon started in the background or by the desktop shell.

None of these files is ever asked for or edited by the user.

---

## 6. Blueprint file format

### 6.1 Structure

```markdown
---
kalka_id: 6f1c2a0e-8d1b-4c57-9a43-0e2b7c9d51aa
owner: ewa@example.org
team: parcel-ops
repo: org/depot-service      # opaque to the core; read by plugins
base_sha: 3f9c2a1
---

# Depot routing refactor

## Context
…

## Plan
See [the router](gh:src/routing/plan.py#L120-L180) and [PROJ-123](jira:PROJ-123).

## Review log

### release · ewa@example.org · 2026-11-11T09:00:00Z · § · rev:3fa9c1d2 · id:5b0e7c1d9a20 · sig:<base64url>
v1 · plan:<64 hex> · owner:ewa@example.org

### comment · marek@example.org · 2026-11-11T17:40:03Z · §plan · rev:3fa9c1d2 · id:7c2e91ab04f3 · sig:<base64url>
Doesn't handle the retry path when a delivery attempt is rescheduled halfway.

### deviation · marek@example.org · 2026-11-12T09:12:40Z · §plan · rev:3fa9c1d2 · id:a41b77c09e12 · sig:<base64url>
Moved the weight check into the intake handler because …
```

### 6.2 Frontmatter

- YAML between `---` lines at the top of the file.
- `kalka` attaches meaning to the keys in the table below, and only when the value has the shape given there. A key with another shape is pass-through, as if `kalka` did not know it, and at most produces a warning.
- All other keys are **pass-through**: never interpreted, validated or required by the core, whatever their names, types or nesting. They are preserved byte for byte on disk, travel with the file to members, and are handed to plugins as an opaque map. A blueprint with any frontmatter, or none, is valid.

| Key | Shape | Meaning | Written by `kalka` |
|---|---|---|---|
| `kalka_id` | UUID v4 | Identity of the document | On publish, seed creation, or transfer (20.7) |
| `owner` | email, case-insensitive | The one owner (7.1) | On publish if absent; on handoff; when a new owner releases after a reassignment or an accepted transfer |
| `team` | team name (19.2) | The team the document belongs to (20.1) | On publish; on a transfer |
| `status` | `[a-z][a-z0-9-]*` | `seed` marks a seed (6.11); other values are labels | On seed creation; on promotion |
| `parent` | UUID | The blueprint a seed was split from | On seed creation |
| `origin` | 12 hex | The entry id that triggered a seed | On seed creation |
| `created` | RFC 3339 UTC | When a seed was created | On seed creation |
| `copied-from` | `<team name>/<kalka_id>` | The source of a copy (20.7) | On a copy |
| `moved-from` | `<team name>/<kalka_id>` | The source of a move (20.7) | On a move |
| `moved-to` | `<team name>/<kalka_id>` | Where a moved document went; only on its tombstone stub (20.6) | On the move's tombstone |
| `kind` | `domain` | A domain document (6.12) | Never; the author writes it |
| `domain` | on a domain document: a name `[a-z0-9][a-z0-9-]*`; on any other document: `<team>/<domain>@v<N>` | The domain's name, or a reference to a domain version | Never |
| `version` | positive integer | A domain document's released version | On release (6.12) |
| `deleted` | `true` | Marks a tombstone stub (20.6) | On deletion or move |
| `reference` | `kalka-blueprint@v<N>` | Opts the document into hints (6.13) | Never |

- Key names are bare, as above. A team whose own pipeline already uses one of these names with another meaning must rename its key, or accept that `kalka` reads it when the shape matches.
- The `repo` and `base_sha` keys in the example above are an instance of this: they mean something to the `gh` plugin only.
- A file without `kalka_id` is a *local* blueprint: listed in Personal, not yet in any team.
- **Publish** adds `kalka_id`, `owner: <config email>` if absent, and `team:`, creating the frontmatter block if the file has none. Whenever `kalka` edits frontmatter it must change only the lines it writes and must not re-serialize the rest (key order and comments stay intact).

### 6.3 Body

Everything after the frontmatter except the review log section. Free-form Markdown; the core gives it no meaning beyond headings (for anchors) and links (for references).

### 6.4 Review log

- Exactly one H2 with the text `Review log` (case-insensitive). Its extent runs to the next H1/H2 or to end of file. It should be the last section, but `kalka` must cope if another tool has appended sections after it.
- If absent, `kalka` creates it at end of file when the first entry is added.
- Content between entries that does not parse as an entry is preserved and shown as "unparsed" in the UI. It never blocks anything. It belongs to the entry before it (or to the log heading, if it comes before the first entry) and moves with that entry when the log is rewritten in canonical order (20.4). It is not part of that entry's body or `id`.

### 6.5 Entries

Header line, strict:

```
### <kind> · <author-email> · <timestamp> · §<anchor> · rev:<8 hex> · id:<12 hex>
```

- Separator is ` · ` (space, U+00B7, space).
- `kind`: `comment` (a remark), `deviation` (a proposed or actual departure from the plan as written, recorded instead of editing the body), `release` (20.4), `budded` (6.11), `transfer` and `transfer-withdrawn` (20.7). `release` and the transfer kinds have behaviour in the core; `budded` is rendered as a link to the seed; `comment` and `deviation` differ in label and styling alone. The set of `deviation` entries is the document's *deviation log* (6.13). Unknown kinds matching `[a-z][a-z0-9-]*` must be preserved, merged and rendered generically, so a team's own workflow can introduce kinds without changes to `kalka`.
- `timestamp`: UTC, RFC 3339, second precision, `Z` suffix.
- `anchor`: slug of the body heading the entry refers to, or empty (`§`) for a general entry. Slug rule: lower-case; drop everything except letters, digits, spaces and hyphens; spaces to hyphens; duplicates get `-1`, `-2`… in document order (GitHub's algorithm). A node inside a trace block (6.10) is addressed as `§<slug>#<node-id>`, where the slug is the section containing the block.
- `rev`: first 8 hex characters of SHA-256 over the normalized body (6.6) at the time of writing.
- `id`: first 12 hex characters of SHA-256 over `kind \n author \n timestamp \n anchor \n rev \n normalized entry body`. An entry whose `id` does not match its content is invalid and is dropped on merge with a visible warning.
- `sig`: an optional seventh field ` · sig:<base64url>`, with no padding, holding an ed25519 signature by the author's identity key. It is not part of the `id` input. The message is `"kalka-entry-v2\n" + kalka_id + "\n" + hex(SHA-256 of the id input)`. It covers the full 256-bit digest, so it cannot be moved to another text. `kalka` signs every entry it creates, of every kind, in the UI and in `kalka comment`.

**Checking a signature.** A signature covers the `kalka_id`, so entries written in a file that has none yet stay unsigned. In a team document, a signature is checked against the keys the team log has ever bound to the author's email (19.1). In Personal, it is checked against the local key when the author is the local user, and against the keys any held team log binds to the author otherwise. The UI shows:

| State | When |
|---|---|
| *signed* | The signature verifies. |
| *unsigned* | No `sig`. |
| *signature invalid* | A `sig` that does not verify against any key held for the author. The entry has no effect. |
| *unknown signer* | No key is held for the author (Personal only). |
| *local only* | In a team document, an entry that does not verify against the team's keys; it does not replicate (20.4). |
| *by a removed member* | Verifies against a key the log bound to someone no longer a member. |
| *from <team>, attested by <names>* | Brought in by a transfer (20.7). |

`kalka` never signs an entry it did not write, except when the user explicitly asks it to sign their own earlier entries; otherwise a teammate could write an entry in my name and my own `kalka` would sign it.

Entry body: Markdown up to the next entry header or the end of the log.

- Headings inside an entry body must be level 4 or deeper. The UI and `kalka comment` demote shallower headings on input.
- Optional first line `re: <id>` marks a reply to another entry.
- Optional leading blockquote (`> …`) quotes the body text the entry targets; the UI uses it to highlight the passage if it still exists.
- `release` body: see 20.4. `budded` body: see 6.11. `transfer` and `transfer-withdrawn` bodies: see 20.7.

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

A blueprint is expected to be moved or copied out of the folder once it has been groomed and accepted. Nothing in `kalka` depends on it staying. The file remains ordinary Markdown: the frontmatter keys and the review log are harmless to other tools and can be kept or deleted by whatever consumes the file next. `kalka` offers no export or clean-up step in v1.

Copying a team document out of the folder is reading it, which every member may do (21). Removing it from the team is *Delete for the team* (20.6); deleting the file by hand is not, and replication brings it back (14).

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

**Validation** (`kalka check`, and on render): YAML parses; ids unique; every `lane`, `next` and `realizes` target exists; `next` stays within one lane; `realizes` points to a more abstract lane; at most 40 nodes per block (authors should aim for 20 or fewer and use several blocks). An invalid block is shown as a plain code block with the error beside it. It never blocks anything.

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

A *seed* is a blueprint with `status: seed`: an idea split off from a blueprint whose scope is growing, kept small until someone takes it up. There is no new document type. Everything that holds for a blueprint holds for a seed: one `owner`, append-only entries, releases, handoff and reassignment as usual.

- Optional keys: `parent` (the `kalka_id` of the blueprint it was split from), `origin` (the `id` of the entry that triggered it, usually a deviation), `created` (6.2).
- Body: an H1 title, `## Problem` and `## Why it was split off`. `kalka` writes exactly this when it creates a seed, from the text the user typed. This is the one document `kalka` assembles.

**Budding.** From any blueprint a member holds, as owner or not, "Create seed" (UI) or `kalka seed` (CLI) does the following:

1. If the parent has no `kalka_id`, it gets one first.
2. The seed file is written, with a fresh `kalka_id`, `owner:` the budding user, `status: seed`, `parent`, `origin` when started from an entry, and `created`.
3. A signed `budded` entry is appended to the parent, anchored like the entry it came from, with `re: <origin id>` when there is one. Body: the line `budded: <seed kalka_id>`, then the seed's title.
4. If step 3 fails, the UI offers "add the backlink" (`kalka seed backlink`) and never creates a second seed.
5. The seed is written in the parent's directory, named after its title. A seed of a team document is published to the same team at once (its first release); a seed of a Personal document stays in Personal.

The parent's frontmatter and body are not touched. The backlink reaches the parent's owner through replication, like any entry.

**Pre-backlog.** A view, per team and for Personal, of every seed held there: title, owner, parent (linked when held, also after the parent was moved, through `moved-from`), and age. Age is the time since the later of `created` and the newest entry; a seed without `created` shows "age unknown".

**Promotion.** The owner changes `status` to another value, typically `draft`: *Promote* in the UI or `kalka promote`, which for a team document also releases the change. A team owner promotes someone else's seed by taking it over first (19.6). A non-owner's edit of `status` is a stray plan edit and does not travel (20.4).

### 6.12 Domain documents

A *domain document* holds a team's shared language for one domain: glossary, bounded contexts, invariants, domain events. Agents read it as context when they work on blueprints. A team may have several domains.

- File name: `<team>-<domain>-domain.md`, created by its author. `kalka` finds domain documents by frontmatter, never by file name.
- Frontmatter: `kind: domain`, `team`, `domain`, `owner`, `version`. It is always a team document (20.1).
- It is an ordinary document. Its owner releases versions, hands it off, and can have it reassigned by a team owner, exactly as for any document (7). There is no separate domain role.
- **One document per domain name in a team.** Publishing a domain document, or accepting one by transfer, when the team already holds one with the same `domain:` is refused (`domain-exists`). Two published concurrently are both kept and both flagged "two documents claim this domain"; a reference to that name is shown as ambiguous until a team owner deletes one.
- Members propose changes as signed comments, exactly as in blueprint review.

**Releasing.** The owner edits the body with their own tools and then releases: *Release* in the UI or `kalka release`. `kalka` increments `version`, appends a signed `release` entry whose body lists the incorporated comment ids and a summary, and replicates it. Edits that are not released do not travel. Comments are never removed; the UI folds those a release incorporated under "incorporated in v<N>".

**Snapshots.** Each released version, made locally or received, is kept as `<team>-<domain>-domain@v<N>.md` beside the live document, so pinned references keep resolving. The index ignores files matching `*@v<N>.md` (14). A member who joins later holds the versions released since they joined, not the older ones. Snapshots are named after the live file, wherever it is in the team folder; a snapshot is written once and never changed. The review view shows a kept version read only, and comments always go to the live document. A transfer does not carry snapshots, and `version` restarts at 1 in the destination (20.7).

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

## 7. Ownership

Every document has one owner. In a team, ownership is decided by the document's release chain together with the team log (20.4): the `owner:` line is what people read, the chain is the authority.

### 7.1 Owner

- The owner of a team document is the member the head of its release chain names, or the new owner an effective reassignment names (7.3). Only the owner changes the plan and signs the next release.
- In Personal, the owner is the local user, whatever `owner:` says; nothing there leaves the machine.
- The UI shows non-owners a read-only body. Edits by a non-owner on their own disk are not blocked; they never travel, and the next replication that brings a release replaces them, keeping a copy (7.5).

### 7.2 Handoff

*Hand off…* in the UI, or `kalka handoff <file> <email>`, by the owner:

1. The recipient must be a current member of the document's team in the local replay. A Personal document cannot be handed off; publish it first.
2. `kalka` writes `owner: <recipient>` and releases the next version, naming the recipient in the release (20.4). This is the whole handoff: there is no prompt, no connection and no acknowledgement. It works offline and while paused, and replicates when it can.
3. The recipient gets a notice and a waiting item (11.3) when the release reaches them, and can hand it back the same way.

The release that changes `owner:` is the record; the UI renders it as "handed off to <recipient>".

### 7.3 Reassignment by a team owner

A team owner can make any member the owner of any document of the team, without the current owner's consent: *Reassign…* in the UI, or `kalka reassign <file> <email>`.

- It is a signed `reassign-document` operation in the team log (19.1, 19.5). It names the document, the owner the signer saw (`from`), the new owner, and `after`: the highest valid release of the document the signer holds, with that release's plan hash.
- Its `owner` is the only valid signer of version `after`+1. Everything else above `after` is void, whoever signed it and whenever (20.4). The team owner's decision wins.
- The new owner's `kalka` releases version `after`+1 in their name as soon as it holds the operation; until then peers show "reassigned to <owner>, pending".
- The previous and the new owner each get one notice when their `kalka` replays the operation.
- **The race.** A former owner who releases version `after`+1 before learning of the reassignment has made a void release. A peer that took it first replaces it when the new owner's release arrives. Every peer holding the same operations and entries computes the same chain, in any order of arrival.
- Removing a member reassigns their documents to the remover (19.5).

### 7.4 Team owners acting on others' documents

A team owner's one direct power over a document they do not own is reassignment, to anyone, themselves included. Releasing, promoting, deleting for the team and moving to another team are owner actions: when a team owner chooses one on someone else's document, the UI says "you will become the owner first", and `kalka` signs a reassignment to the actor and then the action. On the CLI these are two commands, `kalka reassign <file> <me>` and the action. Copying to another team needs no take-over, because it changes nothing in the source (20.7).

The reason is one signer per version: if the owner and every team owner could all sign the next release, ordinary concurrent work would fork the chain.

### 7.5 Displaced edits

When replication replaces a local plan that is not the plan of any release (a former owner's unreleased edits after a reassignment, or a non-owner's stray edit), the displaced file is first written to `<folder>/<team>/.kalka/trash/<name>.displaced-<timestamp>.md`, and a notice names it. No question is asked, and the copy never returns to the index.

---

## 8. Identity and trust

### 8.1 Keys

Each installation generates an ed25519 key pair on first run. The user never handles it.

### 8.2 Trust comes from team logs

- **A peer** is a pair of email and key that a team log you hold binds, for a team you are both members of. Nothing else is a peer, and there is no list of peers outside team logs.
- **A connection from someone who shares no team with you is refused.** After the handshake (8.3), the first message must be a `team_sync` for a team in which, once the log operations it brings are merged, both sides are members (20.3), or the `hello` must carry an invite proof (19.7). Anything else ends with `bye`, reason `not-member`. No file is read and nothing is recorded about the caller.
- **First contact** rests on the invite token alone: it carries the inviter's key, and the joiner dials that key's ID (19.7). Whoever passes the token on is trusted with that.
- **One email with different keys in two teams** (someone reinstalled and was re-invited to one of them) blocks nothing: each team uses its own binding, and the Team view shows one information line.
- A key is a member's identity together with their email; an email alone is never trusted.

### 8.3 Channel binding

After the data channel opens and before any blueprint data flows:

1. Each side sends `hello {proto, email, pubkey, nonce}`.
2. Each side sends `auth {sig}` where `sig` signs `"kalka-auth-v1" ‖ own DTLS certificate fingerprint ‖ peer DTLS certificate fingerprint ‖ peer nonce`.
3. Each side verifies the signature against the presented key, using fingerprints read from the DTLS transport itself, not from the SDP relayed by the server.
4. Each side checks that the peer ID it dialed or was dialed from is the key ID of the presented key (9.2), so a squatter on someone's key ID fails at once.
5. The responder then checks the key against its team logs as in 8.2; the initiator dialed a key it took from a team log or an invite.

Any failure closes the connection. This makes a malicious or compromised rendezvous server unable to sit in the middle.

---

## 9. Rendezvous and transport

### 9.1 Signaling protocol

The client must speak the PeerJS server WebSocket protocol so that one client works against the public PeerJS cloud, stock `peerjs-server` and `kalka-rendezvous`. Message types include `OPEN`, `OFFER`, `ANSWER`, `CANDIDATE`, `HEARTBEAT`, `LEAVE`, `EXPIRE`, `ERROR`, `ID-TAKEN`.

- Only the signaling protocol is PeerJS. What flows over the data channel is `kalka`'s own protocol (9.5); there is no interoperability with browser PeerJS clients and none is required.
- Treat the `peers/peerjs-server` source as the authority for URL shape, query parameters, heartbeat interval and ID character rules. Do not rely on this document for those details.
- `github.com/muka/peerjs-go` is an existing Go port of client and server on Pion (Apache-2.0). Evaluate it in M1; adopt, vendor or reimplement the signaling part. It is small.

### 9.2 Peer IDs

Each daemon registers one ID, on one signaling connection:

```
key_id = "kalka-k" + hex(SHA-256("kalka-key-v1|" + pubkey))[0:32]
```

`pubkey` is the raw 32-byte ed25519 key.

- **Dialing.** A member is dialed at the key ID of the key the team log binds; a joiner dials the key ID of the inviter's key from the token. A failed handshake is never retried at another ID.
- **What a key ID gives.** A squatter on someone's key ID fails channel binding (8.3). On `kalka-rendezvous` a key ID cannot be claimed without the key (9.6). On other servers anyone holding the public key, which every team member does, can occupy the ID and keep its owner offline; impersonation is still prevented by channel binding and the team log.
- No directory is needed: to reach a member, derive the ID and connect. The server refuses a second connection with an ID already in use (`ID-TAKEN`); the UI reports this as "this identity is already online on another machine".
- `hello` still carries the email: it is the name the log binds to the key.

### 9.3 Presence

PeerJS servers do not list peers. Presence is learned from replication: members are probed only while a round needs them (20.3), and a pending join probes the inviting owner's key ID once a minute until the invite expires (19.7). The Team view shows each member of the selected team as online, or when they were last reached. `kalka status` and the tray count distinct members reached across all teams.

### 9.4 ICE

Order of precedence:

1. `ice_servers` from config, if set.
2. `GET <rendezvous-http-origin>/kalka/ice` — served by `kalka-rendezvous`, returns ICE servers with short-lived TURN credentials. A 404 is normal for other servers.
3. Built-in default: a public STUN server; for `peerjs-cloud`, also whatever ICE defaults the current PeerJS client library ships.

TURN over TCP/TLS on 443 must be supported; corporate networks commonly block UDP.

### 9.5 Wire protocol (data channel)

One reliable, ordered data channel labelled `kalka/1` per session. Messages are JSON objects, each sent as a 4-byte big-endian length followed by the payload; writes to the channel are chunked to at most 16 KiB so no single SCTP message exceeds common limits.

| Message | Fields | Notes |
|---|---|---|
| `hello` | `proto`, `email`, `pubkey`, `nonce`, optional `invite` | Both directions, first. `invite` carries a join proof (19.7). |
| `auth` | `sig` | Both directions, second. |
| `team_sync` | `team`, `heads`, `digest` | Opens a replication session (20.3): the log heads and one digest over the inventory. |
| `team_log` | `team`, `ops` | Log operations the other side is missing. Also sent once by an owner who admits a joiner (19.7 step 5). |
| `team_inv` | `team`, `items` | Inventory lines (20.3), pending transfers included. |
| `team_want` | `team`, `ids` | The documents the sender wants; each follows as a `team_doc`. |
| `team_doc` | `team`, `kalka_id`, `filename`, `content` | One wanted document; empty `content` means unavailable. |
| `bye` | optional `reason`, `detail` | Either side. Reasons include `not-member`, `invite-*` (19.7) and `proto`. |
| `keepalive` | — | Keeps a slow session open. |

Unknown message types are ignored. A `proto` mismatch ends the session with a human-readable error. A session closes when both sides are done or after 30 s of silence. Nothing on the wire asks a person anything.

The responder dispatches on the first message after `auth`: `team_sync` starts a session; a `hello` with an invite proof is a join (19.7); anything else gets `bye` `not-member`.

### 9.6 `kalka-rendezvous`

A separate binary for corporate hosting, with no client code, no configuration directory and no UI:

- PeerJS-compatible signaling, state in memory only.
- Embedded TURN relay (`pion/turn`) with time-limited credentials exposed at `/kalka/ice`.
- `--allow-origin` / reverse-proxy friendly, so corporate SSO or network policy can sit in front. No built-in user database.
- TLS via flags or behind a proxy.
- **Key-ID proof.** A client registering a key ID (9.2) adds the query parameter `kproof = base64url(pubkey) "." unix-seconds "." base64url(sig)`, where `sig` signs `"kalka-keyid-v1\n" + id + "\n" + unix-seconds`. The server checks that the ID matches the key, the signature verifies and the timestamp is within a few minutes, and refuses the registration otherwise. Other servers ignore the parameter.

### 9.7 Public cloud mode

`rendezvous: peerjs-cloud` is for demos. The UI must show a permanent notice in this mode: the service is community-run with no availability guarantee, IDs can be claimed by anyone who knows a public key, which keeps its owner offline, and only the team logs protect the exchange. Not for sensitive blueprints.

---

## 10. Plugins

### 10.1 References

A reference is a Markdown link whose target has a non-web scheme: `[label](scheme:payload)`. Scheme matches `[a-z][a-z0-9+.-]*`. The schemes `http`, `https`, `mailto` and `file` are never dispatched to plugins. A reference with no installed plugin renders as plain text with its label.

Blueprints carry references only. Resolved content is never written into the file and never sent to a peer; each peer resolves with their own plugins and credentials.

### 10.2 Discovery

Executable named `kalka-plugin-<scheme>` (plus `.exe`, `.cmd` on Windows), searched in `<config>/plugins`, then the folder of the running executable, then `PATH`.

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

Served from the binary (`embed.FS`). The frontend is prebuilt; end users need no Node toolchain. It is shown in the desktop window (22) or in a browser tab (`kalka open`); it is the same page from the same localhost address either way, a pure view over the local API (13). It is the primary interface: every workflow has a control in it (12).

### 11.1 Views

**The team switcher** sits at the top of the window: the current scope's name, and a dropdown listing Personal first, then every team by name, each with a badge counting its waiting items (11.3). It also lists "joining <team>…" for each pending join, then *Create team…* and *Join with link or token…*. Two teams with one name show a short hash. The selected scope is part of the route (`#/t/<team>/…`, `personal` for Personal) and is remembered in the page's storage. Opening a document by link switches to the document's scope.

Every view below shows the selected scope. In Personal the Team and Domains views are hidden.

- **Setup** — the three settings: email, rendezvous, folder. Opened from a join link with no configuration, it is filled in from the token, so a new teammate types only the folder, and goes on to the join screen.
- **Join screen** — route `#/join/<token>`: the team name, the inviter, the invited email and the expiry, with one *Join* button. In place of the button it says "this invite is for <email>, you are <email>", "expired", "you are already a member", or "this invite uses another rendezvous server". Opening it never joins.
- **Blueprints** — files in the scope: title (first H1), role (owner / member), owner, entry count, validity problems. In Personal each file offers *Publish to team…*. In a team folder, a file that does not replicate says why: "not shared: publish it or move it to Personal".
- **Review (linear)** — rendered body; entries shown next to the section they anchor to; general entries at the end; badges for outdated and orphaned and for signature states (6.5), attested entries included; resolved references shown inline with snippet and warnings; hints for documents opted into the reference (6.13); the "newer domain version available" indicator (6.12); `budded` entries as links. Actions: add comment, add deviation, reply, *Create seed*, *Promote*, and for the owner *Release*, *Hand off…*, *Copy to team…*, *Move to team…*, *Delete for the team*; for team owners *Reassign…*, with the take-over wording of 7.4 on owner actions. Banners: unreleased edits; "reassigned to <owner>, pending"; "being moved to <team>; comments added now will not travel"; ownership disputed; last release withdrawn.
- **Review (canvas)** — the same data as an annotated graph: each body section is a fixed read-only card laid out in reading order; each entry is a note connected to its section's card; references are small cards beside the section. Replies hang under the note they answer; the General and Orphaned groups of the linear view get cards of their own after the body. Layout is computed from the text, not saved, so there is no canvas state to persist. Selecting a note or a section card opens the linear view scrolled to it. Freehand drawing is out of scope.
- **Trace** — each trace block rendered as lanes side by side, most abstract on the left, flow running top to bottom within a lane. A lower-lane node is placed level with the first node it realizes; realization links are drawn across lanes in a style distinct from flow arrows. Nodes with a divergence are visibly marked, and a gap is left (and labelled) in the abstract lane opposite an implementation-only node. A side panel lists the computed divergences; selecting a node replaces it with that node's detail: `why`, what it realizes, the resolved reference with snippet and warnings, entries anchored to the node, and a comment box that anchors to `§<slug>#<node-id>`. A Diagram / Source toggle shows the raw YAML. Layout is computed, nothing is saved. The same block appears inline in the linear review view at reduced size.
- **Pre-backlog** — seeds held in the scope (6.11): title, owner, parent, age; *Promote*.
- **Team** — members and owners with presence (9.3), open invites with *Copy link*, *Copy token* and *Cancel*, the log rendered as a readable history with discarded operations and their reasons, a replication status line (last round, members reached, documents refused and why) with *Sync now*, transfers in and out (20.7), and *Invite*, *Remove* (with a preview of the documents to be reassigned), *Make owner*, *Remove as owner*.
- **Domains** — the team's domain documents with version and owner; *Release* for the owner, with a checklist of open comments to mark as incorporated; a version switcher over local snapshots.
- **Waiting** — the selected team's waiting items (11.3), each with its kind, who and when, and its buttons: *Accept*, *Decline*, *Open*, *Reassign…*.
- **Settings** — start at login, keep running when the window is closed, notifications, pause (22); *Blueprint reference: save a copy* (6.13); the version line. The two window settings are hidden in a browser tab.

The UI updates live from the daemon's event stream when files change or replication delivers.

### 11.2 Rendering untrusted content

Blueprints from peers are untrusted input rendered in a page that can trigger file writes and syncs.

- Raw HTML in Markdown must be disabled or sanitized.
- Link targets limited to http, https and plugin schemes.
- Plugin output is treated as text; `snippet.text` is never interpreted as HTML.
- Strict Content-Security-Policy: no inline script, no remote origins.

### 11.3 Waiting items

A waiting item is something only the local user can move forward. Each has a team, a time and a target in the UI. All are derived from files and logs, except the one `seen` mark.

| Item | For whom | Since | Cleared by |
|---|---|---|---|
| Pending transfer into the team (20.7) | The team's owners | The `transfer` entry's timestamp | Accept, decline or withdrawal |
| Document handed off or reassigned to you, or accepted in your name | The new owner | The release's or operation's timestamp | Opening the document, `kalka waiting --seen <file>`, or any release of yours |
| Ownership disputed (20.4) | The team's owners | The later of the two releases | A reassignment |
| Owner is no longer a member | The team's owners | The removal | A reassignment |

- Not waiting items: your own pending join (it waits for the inviter; the switcher shows "joining <team>…"), a queued push, new comments (a notice, 22.4).
- The `seen` marks are a small list in `state.yaml`: document id and what made it yours, the handoff release's id or the reassignment's operation. A mark is dropped when that is no longer what makes the document yours.
- After a reassignment or an accept, the new owner's kalka releases on its own (7.3); that release is not "a release of yours", so the item stays until the document is opened or a release is made by hand.
- Timestamps come from other people's clocks. "Oldest" chooses which team the tray opens (22.1) and nothing else.

---

## 12. CLI

The CLI is a first-class interface: agents and scripts use it, it works without the desktop shell, and on machines where the shell is not installed. Commands fall into four run classes.

| Class | With a daemon running | With no daemon |
|---|---|---|
| **File** | Not used | Not used, and none is started. Writes follow 14 (re-read and redo on change), so they are safe beside a running daemon. |
| **Daemon** | Over the local API (13) | One is started in the background (`kalka daemon --background`) and the command runs once it answers. `--no-start` or `KALKA_NO_START=1` fails instead, with exit code 1 and `"kind": "not-running"` in JSON. |
| **View** | Over the local API, with live data (presence, online state) | Read from the local files in process; nothing is started. Live fields are reported as unknown and JSON carries `"daemon": false`. |
| **Control** | `daemon`: exits 0 (4.1). `stop`: asks it to stop and waits. | `daemon`: runs. `stop`: nothing to do, exits 0. |

### 12.1 Commands

Team commands take `--team <name or hash>`; with exactly one team it may be omitted. Commands that take a file read the team from where the file lies (14). The last column is the app's control for the same workflow (12.2).

| Command | Class | Purpose | In the app |
|---|---|---|---|
| `kalka` | control | Make sure a daemon runs, then open the UI: the desktop window if a shell is attached, else the browser. | — |
| `kalka open [--print] [<link>]` | control | Open the UI in the browser, or print its URL; with a `kalka://` link, on its route. | — |
| `kalka daemon [--background] [--no-ui] [--no-notifications]` | control | Run the daemon in the foreground, or as at login with `--background` (log to `kalka.log`). `--no-ui` serves the API but not the web UI. `--no-notifications` shows no OS notifications. | — |
| `kalka stop [--wait <dur>]` | control | Stop the running daemon after its running rounds finish; waits up to `--wait` (default 1m). | Tray *Quit* |
| `kalka setup --email … --rendezvous … --folder …` | file | Write the three settings and create `personal/`. | Setup |
| `kalka status` | view | Online state, members reached, last round, waiting count, paused. | Header line, tray |
| `kalka teams` | view | The teams held, with role and waiting count. | Team switcher |
| `kalka team create <name>` | daemon | Create a team; you are its first owner. | Switcher *Create team…* |
| `kalka team invite <email> [--expires <dur>]` | daemon | Print an invite link and token (19.7). Team owners only. | Team *Invite* |
| `kalka team uninvite <id>` | daemon | Cancel an open invite. | Team, open invites, *Cancel* |
| `kalka join <link or token> [--wait <dur>]` | daemon | Redeem an invite; waits briefly for the owner, then leaves the retry to the daemon. | Join screen *Join* |
| `kalka join --show <link or token>` | view | What the join screen shows; never joins. | Join screen |
| `kalka team members\|log\|invites` | view | The roster with presence, the log, or the invites made here. | Team |
| `kalka team remove <email> [--dry-run]` | daemon | Remove a member; `--dry-run` lists the documents that would be reassigned. | Team *Remove* |
| `kalka team add-owner\|remove-owner <email>` | daemon | Roster changes (19.3). | Team *Make owner*, *Remove as owner* |
| `kalka team sync` | daemon | Start a replication round now. | Team *Sync now* |
| `kalka publish <file> --team <team> [-m <note>]` | daemon | Move a Personal document into a team with its first release (20.1). | Blueprints (Personal) *Publish to team…* |
| `kalka release <file> [-m <text>] [--incorporates <id>,...]` | daemon | Release the next version of a team document (20.4). | Review *Release*, Domains *Release* |
| `kalka handoff <file> <email>` | daemon | Make another member the owner (7.2). | Review *Hand off…* |
| `kalka reassign <file> <email>` | daemon | Team owners: make any member the owner (7.3). | Review *Reassign…*, Waiting |
| `kalka copy <file> --to <team>` | daemon | Offer a copy to another team (20.7). | Review *Copy to team…* |
| `kalka move <file> --to <team>` | daemon | Offer a move to another team (20.7). | Review *Move to team…* |
| `kalka transfers [--team <t>]` | view | Transfers into and out of the team. | Team, transfers; Waiting |
| `kalka transfer accept <id> [--owner <email>]` | daemon | Team owners: accept a pending transfer. | Waiting *Accept* |
| `kalka transfer decline <id>` | daemon | Team owners: decline it. | Waiting *Decline* |
| `kalka transfer withdraw <id>` | daemon | Withdraw your own pending transfer. | Team, transfers, *Withdraw* |
| `kalka delete <file>` | daemon | Delete a team document for the team (20.6). | Review *Delete for the team* |
| `kalka comment <file> [--anchor <slug>] [--kind deviation] [--re <id>]` | file | Append an entry; body from `-m` or stdin. Author is the configured email. | Review comment, deviation, reply |
| `kalka seed <parent> --title <t> [--from <entry id>] [-m <text>] [--why <text>]` | file | Bud a seed (6.11). | *Create seed* |
| `kalka seed backlink <seed>` | file | Add a missing `budded` entry to the seed's parent. | Review *Add the backlink* |
| `kalka promote <file> [--status <s>]` | daemon | Change a seed's `status` (default `draft`). | Pre-backlog, Review *Promote* |
| `kalka seeds [--team <t>\|personal]` | view | The pre-backlog of a team or Personal. | Pre-backlog |
| `kalka domains [--team <t>]` | view | Domain documents and versions. | Domains |
| `kalka check <file>` | file | Validate format; exit code for use in scripts and agents. | Problems in Blueprints and Review |
| `kalka waiting [--team <t>] [--seen <file>]` | view (daemon with `--seen`) | Waiting items (11.3); `--seen` clears a handed-to-you item. | Switcher badges, Waiting, tray |
| `kalka pause` / `kalka resume` | daemon | Go offline and back (22.3). | Settings, tray |
| `kalka settings [--notifications on\|off] [--keep-running on\|off]` | daemon (a view with no option) | The settings of 22.2; with no option it shows them. | Settings |
| `kalka autostart on [--headless]\|off\|status` | file | Start kalka at login (22.2). | Settings *Start at login* |
| `kalka reference export [-o <file>] [--force]` / `reference version` | file | Write or name `BLUEPRINT-REFERENCE.md` (6.13). Never overwrites without `--force`. | Settings *Blueprint reference* |
| `kalka version` | file | Version and protocol numbers. | Settings, version line |

All commands support `--json` for agents. The review log is plain Markdown, so any downstream tool can read it directly without `kalka`. `kalka comment` signs with the identity key from the config directory (6.5); it still needs no daemon.

The rule behind the run classes: one writer for everything shared beyond a single file. Daemon commands are those that write the team log, the config directory, a release, a tombstone or a transfer, or need the network. File commands touch one blueprint file under the rules of 14. Views exist so that an agent's read in a sandbox does not register on a rendezvous server as a side effect.

While paused, everything except `join` works and is pushed after resume; `join` fails with `paused`.

### 12.2 Parity

Every workflow has both a command and a control in the app. The CLI's command table is the list; `internal/cli/parity.json` maps each command to a parity class and the app's action id:

- **workflow**: must have an action id that a control in the web UI carries;
- **view**: must have a place in the UI;
- **process**: exempt, with the reason (`kalka`, `open`, `daemon`, `stop`: they start, show or stop the app itself).

A Go test checks that every command has a row, every workflow row has an action id, and every workflow command accepts `--json`. A control carries its action id as `data-action` (`web/src/actions.ts` declares the ids). A web test checks that every action id in the table is declared and used by a control, and that every declared action appears in the table, so a UI workflow without a command fails too.

### 12.3 Errors

With `--json`, a failure is `{"error": "...", "kind": "..."}` on stdout, and the exit code is 1 (2 for usage errors). A view's JSON is an object carrying `"daemon"`, with live fields `null` when no daemon runs (unknown, not zero). The kinds are stable:

<!-- error-kinds -->
| Kind | Meaning |
|---|---|
| `not-set-up` | No configuration; a daemon command does not start a daemon that would wait for the setup form. |
| `not-running` | No daemon, and `--no-start` said not to start one. |
| `paused` | `join` while paused. |
| `no-shell` | `ui/show` with no desktop shell attached. |
| `format` | The file does not parse or is over limits (6.9). |
| `duplicate` | Two files hold this `kalka_id` (14). |
| `no-team` | The command needs a team and none is held. |
| `unknown-team` | No held team has that name or hash. |
| `ambiguous-team` | Several teams match; give the hash. |
| `not-member` | The person is not a member of that team (or you are not). |
| `not-team-owner` | Only a team owner may do this. |
| `not-owner` | Only the document's owner may do this; a team owner reassigns first (7.4). |
| `last-owner` | The change would leave the team without an owner. |
| `in-personal` | The document is in Personal; publish it first. |
| `not-shared` | The file lies in a team folder but is not a team document. |
| `disputed` | Ownership is disputed; a team owner reassigns first. |
| `domain-exists` | The team already holds a document for that domain. |
| `transfer-pending` | A transfer of this document is already pending. |
| `transfer-decided` | The transfer was already accepted, declined or withdrawn. |
| `unknown-transfer` | No pending transfer has that id. |
| `invite-token` | The link or token does not parse. |
| `invite-expired`, `invite-used`, `invite-email` | The invite has expired, was used, or names another email. |
| `rendezvous-mismatch` | The invite names another rendezvous server. |
| `error` | Anything else (a file that cannot be read, a network failure); the message says what. |
<!-- /error-kinds -->

A test checks this list against the kinds the code declares, and that each is returned somewhere.

---

## 13. Local HTTP API

One local API, used by the web UI (in the desktop window or a browser tab), the desktop shell's tray, and the CLI. There is no second transport (18.3, decision 7).

- Bind to `127.0.0.1` only, on `ui_port` (default 7717). The port is fixed so that the page's per-origin storage survives restarts.
- Validate the `Host` header against the loopback address and port (defeats DNS rebinding).
- Require the per-start random token on every `/api/` request. The page gets it in the URL that `kalka open` or the shell loads, which the daemon exchanges for an HTTP-only, same-site cookie; the CLI sends it as a bearer header.
- Requests that change state must carry an `Origin` equal to the daemon's own origin, or none (the CLI). When a `Sec-Fetch-Site` header is present on an `/api/` request it must be `same-origin` or `none`, for reads too. A web page elsewhere that aims requests at `localhost` is refused. The token link itself is not checked: opened from a mail or chat page it arrives as `cross-site`, and a page that does not know the token gains nothing from it. The UI's static files carry no data and are served without these checks (the sandboxed Mermaid frame loads them from an opaque origin).
- The token is in `daemon.json` in the config directory, written owner-only (mode 0600; on Windows an ACL restricted to the current user). Another local user cannot read it. The user's own processes can, as they can read the identity key: they are trusted with the user's files.
- `--no-ui` stops the daemon from serving the web UI (every page path answers 404 and no cookie is set); the API stays up for the CLI and the shell.
- **Scope.** Lists take `?team=<name or hash>` or `?team=personal`: `GET /api/blueprints`, `/api/seeds`, `/api/domains`, `/api/waiting`, `/api/transfers`. `GET /api/teams` lists the teams with their waiting counts, and each member's `presence` as this daemon last saw it (`online`, `offline`, `unknown`; none for the local user, 9.3). `GET /api/waiting` lists the items oldest first; `POST /api/waiting/seen {path}` clears a handed-to-you item, as opening the document with `GET /api/doc` does. `GET /api/summary` carries `waiting: {total, oldest: {team, at, target}, teams: [{team, name, count}]}`.
- `GET /api/about` returns what `kalka version` prints (`version`, `wire_protocol`, `plugin_protocol`, `reference`); `GET /api/reference` returns `BLUEPRINT-REFERENCE.md` as a download, for Settings *save a copy* (6.13). The page never writes the reference into the folder.
- `POST /api/joins/preview {token}` returns what the join screen shows (11.1) and never joins: the token as normalized, the team name and hash, the inviter, the invited email, the expiry and the rendezvous; `problem` with `message` when the join cannot go ahead here (`invite-email`, `invite-expired`, `rendezvous-mismatch`, `already-member`, or `own-invite` when the owner opens their own link); and `joining` when this installation is already redeeming it. Before setup it answers from the token alone, so the setup form can fill itself in. `POST /api/joins` refuses with the same kinds.
- Links the page opens to the outside go through `POST /api/open-external` when it runs in the desktop window: only `http` and `https` are accepted, and the system browser opens them. The window never navigates away from the loopback origin.
- **The desktop shell** (22) is a client like the others. It holds an event stream with `GET /api/events?shell=1`; while one is open, `GET /api/summary` reports `"shell": "desktop"` and the shell, not the daemon, presents notices. `POST /api/ui/show {"path"}` or `{"route"}` asks it to show its window, on a document's route or on a route of the page (`#/` and URL-safe characters only: the waiting list, the join screen); with no shell attached it fails with kind `no-shell`, and bare `kalka` opens the browser instead. The window loads the token link with `window=1`, which also sets a cookie the page can read (`kalka_window=1`): the page then sends links outside, leaves notifications to the shell, and shows the window settings.

---

## 14. File system behaviour

```
<folder>/
  personal/                      Personal: unshared drafts, never replicated
  parcel-ops/                    one folder per team
    parcel-ops-team.log
    depot-routing.md
    parcel-ops-routing-domain.md
    parcel-ops-routing-domain@v3.md
    .kalka/incoming/             pending transfers into the team (20.7)
    .kalka/tombstones/           tombstone stubs (20.6)
    .kalka/trash/                last copies and displaced edits (7.5, 20.6)
  library-desk/
```

- **Location decides scope.** A document belongs to the team whose folder it lies in (at any depth below it), or to Personal. Files directly in `<folder>/` count as Personal too, so "drop a file in the folder" keeps working. `setup` creates `personal/`.
- **A team document** lies in the team's folder, has `team:` naming that team, and has a valid release; or is a valid pending transfer in `.kalka/incoming/`. Only such files replicate (20.1). One function decides this.
- **Mismatches are shown, never replicated:** a file in a team folder without that `team:` or without a release is "not shared"; a file in Personal with `team:` shows "has `team:` but lies in Personal".
- `personal` is a reserved team name. Two teams with one name: the second folder held gets a suffix (`-2`), and the switcher shows a short hash.
- Within its team folder a user may move a file into subfolders; documents are found by `kalka_id` within their scope, never by path. The store also indexes `moved-from` and `copied-from`, so links to a document's former id still resolve.
- Watch `folder` recursively for `*.md`; ignore domain snapshots `*@v<N>.md` (6.12), dotfiles and dot-directories (so `.kalka/` is never indexed). Debounce events (≈300 ms); editors save by rename.
- Writes are atomic: temp file in the same directory, then rename; on Windows retry briefly on sharing violations (editors and antivirus hold files open).
- Before writing, re-read the file; if it changed since it was read for this operation, redo the merge against the new content.
- Ignore the watcher event caused by `kalka`'s own write.
- Two files with the same `kalka_id` in one scope → both flagged, and neither replicates until one is removed.
- Deleting a Personal file simply removes it from the list. Deleting a team document's file with the file manager is not a deletion for the team: replication fetches it again, and the UI says how to delete it for the team (20.6).

---

## 15. Technology

- Go. The `kalka` binary (CLI and headless daemon) has no CGO: a single static binary per platform, `windows/amd64` first, then `darwin/arm64`, `linux/amd64`.
- Desktop shell: Wails v3, pinned to an exact release (18.3, decision 8). It is built only with the `desktop` build tag, so `go build ./...`, `go vet ./...` and `go test ./...` never need it. On Windows it needs no CGO; on macOS and Linux it does (23).
- `kalka-rendezvous` (`cmd/kalka-rendezvous`) imports only the signaling, peer ID and TURN packages; a test keeps it free of the core, the daemon, the UI and WebRTC.
- URL scheme registration lives in `internal/urlscheme`, beside `internal/autostart`, with the registry, data directory and command runner as parameters so tests use fakes (23).
- What the user installs is one product per platform (23.1): on Windows a per-user installer holding `kalka.exe` and `kalka-desktop.exe`; on macOS `Kalka.app` with one executable that is also the CLI; on Linux one archive with `kalka` and `kalka-desktop`.
- No bundled Node.js or Chromium: Node is a build-time tool for the web UI only, and the window uses the system webview.
- WebRTC: `pion/webrtc`; TURN: `pion/turn`.
- File watching: `fsnotify`.
- Frontend: TypeScript with a small framework of the implementer's choice, built to static assets and embedded.
- Releases (`tools/release`): per target a reproducible headless archive (`kalka`, `kalka-plugin-gh`; on Windows also `kalka-desktop.exe`), a `kalka-rendezvous` archive; the Windows installer (when NSIS is installed), `Kalka.app` in a zip (built on a Mac) and a Linux desktop archive (built on Linux with GTK 4 and WebKitGTK 6.0), each skipped with a reason when the host cannot build it; `SHA256SUMS`. See 23.5 for signing.
- Pin dependency versions at implementation time; do not assume the versions current when this was written.

---

## 16. Milestones and acceptance criteria

M0–M7 (format and merge, transport, daemon, web UI, plugins, rendezvous, canvas, trace), R1–R8 (reference, team log, signed entries and releases, seeds, key IDs, teams and invites, replication, domains) and D1–D7 (the daemon, the local API, the CLI classes, settings and notices, the desktop shell, packaging) are done. Their acceptance criteria are kept in `implementation-plan.md`; where they named one-to-one syncs, v0.6 replaces them by the criteria below. The work order and sizes are in `docs/plan-teams-only.md`.

### T1 — Remove one-to-one sharing
- A connection from a peer that shares no team is closed with `not-member`, and no file is read. A member presenting another key than the log binds is refused. One signaling registration per daemon; the transport suite runs on key IDs; the public-cloud check still passes. Nothing refers to the deleted commands, messages, files or prompts.

### T2 — Personal and team folders
- **Isolation:** Ewa and Marek share `parcel-ops`; Ewa and Olga share `library-desk`. Over full rounds in both directions, Marek receives no byte, id or title of a `library-desk` document or log, and Olga none of `parcel-ops`; a request for a foreign id is answered like an unknown one. Two teams with one name on one machine exchange nothing across.
- **Personal never leaves:** with Personal files present, including one that carries `team:` and one with a forged release, no inventory line, content or id of them appears on any channel.
- Publish moves the file and reports the new path; mismatches are listed and not replicated; `personal` is refused as a team name.

### T3 — Roles and ownership
- **Immediate handoff:** with the recipient offline, the owner hands off; the release names the recipient; the recipient's first round delivers it with a notice and no question; the recipient hands it back. Handoff to a non-member and of a Personal file is refused.
- **Reassignment without consent:** a team owner reassigns a present member's document while that member is offline; previous and new owner each get one notice; a member's attempt fails with `not-team-owner`.
- **The race:** a former owner releases version n+1 concurrently with a reassignment at cut-off n. Over every delivery order of the operation and the releases to three peers (enumerated, not sampled), all peers end with the same chain, plan and owner. Two team owners reassigning one document concurrently converge. A document reassigned twice keeps a valid chain through both cut-offs. A dispute is ended by a reassignment.
- The team log harness holds for 100 000 histories under rule version 2, with reassignments generated at random. Displaced edits land in the trash with a notice. A domain document is released by its owner, handed off and reassigned; a second document for one domain is refused locally and flagged when concurrent.

### T4 — Transfers between teams
- Copy accepted: new id, `copied-from`, source unchanged byte for byte, the accepting owner is the owner and can hand it off. Move accepted: no source tombstone before the accept, the `moved:` tombstone after it, and the source team sees the document gone.
- Decline of a copy: every destination peer discards it, and a lagging peer cannot bring it back. Decline of a move: the source keeps the document, no tombstone.
- **Countersigned history:** a destination member who is not in the source team takes the pending document, shows its entries as attested by the actor and then by both; a document with one attested entry altered, removed or added is refused.
- **Non-member:** a transfer by someone who is not a member of the destination is refused on their machine with `not-member`, and a pending document whose `transfer` entry is signed by a key the destination log does not bind is refused by every receiving peer, with the reason listed.
- The actor goes offline right after starting; an owner reached only through a third member still gets the item. Two owners accept concurrently: one version 1 everywhere. Accept beats decline and withdrawal. A move whose actor lost the source document ends as a copy with a notice.

### T5 — Join links
- `kalka://join/<token>` and a bare token parse, in any case, with whitespace or angle brackets; another scheme or path, extra parts, a query, more than 4 KiB or a damaged checksum are refused.
- With a shell attached, a start with a link calls `ui/show` with the join route and exits 0; without one, the shell starts on the route; `kalka open --print <link>` prints the join URL. The preview reports a wrong email, an expired invite, another rendezvous and an existing membership, and never joins.
- Registration is tested against a fake registry, a temporary data directory and a recording command runner; the installer script, `Info.plist` and `.desktop` text carry the scheme. No test registers a real handler.

### T6 — Waiting items, scoped API, commands
- Each waiting item appears for the right people only and clears as 11.3 says; the summary's total, per-team counts and oldest target are right over three teams; the tray opens the oldest team. Every command of 12.1 runs with `--json`. Each error kind of 12.3 is produced by at least one command.

### T7 — The app
- The parity tests of 12.2 pass. The switcher, scoped routes, waiting list, join screen and attested badge have unit tests. One workflow of each family runs through the API the page uses. No fixture or mockup holds a term from the excluded domains (finance, trading).

### T8 — Rendezvous binary and packaging
- `kalka-rendezvous` passes the signaling, TURN and key-ID proof tests; the import test holds; the release lists its archives and `SHA256SUMS` covers them.

### 16.1 End-to-end scenarios

1. **Review round trip.** Ewa publishes a blueprint to `parcel-ops`. Marek gets it without a question, adds two comments; Ewa's copy has both; her plan is unchanged; the files are equal on both sides.
2. **Parallel reviewers.** Marek and Olga comment on the same version in either order; every member ends with both sets in timestamp order.
3. **Owner revision.** Ewa rewrites a section, removes a heading and releases. Marek's copy takes the new plan; his entry on the removed heading shows as orphaned, others as outdated.
4. **Stray edit.** Marek edits the body of Ewa's document. Nothing of it travels; the next release replaces it, and his edit is kept in the trash with a notice.
5. **Handoff.** Ewa hands off to Marek while he is offline. Marek can release; Ewa cannot. The log shows "handed off to Marek".
6. **Reassignment race.** Olga, a team owner, reassigns Ewa's document to Marek while Ewa, offline, releases another version. After everyone meets, every peer holds Marek's version and Ewa's is void.
7. **Editor reformat.** Marek's editor converts the file to CRLF and strips trailing spaces. Replication still treats it as the same plan.
8. **Isolation.** Ewa is in `parcel-ops` and `library-desk`; Marek only in `parcel-ops`. Nothing of `library-desk` reaches Marek, and Personal reaches nobody.
9. **Seed backlink.** Marek buds a seed from a deviation on Ewa's blueprint. The seed is a team document; Ewa's copy has the `budded` entry; her plan is unchanged.
10. **Join by link.** Ewa invites Olga and posts the link. Olga clicks it while Ewa is offline, sees the team and the inviter, and joins; when Ewa comes online the join completes in the background and both logs list Olga.
11. **Relay.** Ewa publishes a seed and goes offline. Marek, online with Ewa earlier, passes it to Olga. Olga verifies Ewa's release and holds the seed.
12. **Deleted stays deleted.** Olga is offline holding v3; Ewa deletes the document; Olga returns and meets only Marek. The file does not come back anywhere.
13. **Removal.** Ewa removes Dawid; Dawid's documents become Ewa's on every member; Dawid's later releases are not applied.
14. **Copy.** Ewa copies a blueprint from `parcel-ops` to `library-desk`. Jan, an owner there who is not in `parcel-ops`, accepts; the copy is his, its old comments show as attested by Ewa and Jan, and `parcel-ops` is unchanged.
15. **Move and decline.** Ewa moves a seed to `library-desk`; Jan declines; the seed stays in `parcel-ops` and nothing is tombstoned. She moves it again; Jan accepts; it disappears from `parcel-ops` as "moved to library-desk".

---

## 17. Review findings — what changed and why

A final pass over the design found the following. Items 1–4 change earlier agreements.

1. **The byte-prefix rule does not survive two reviewers.** After Ewa merges Marek's comments, her file is no longer a prefix of Olga's reply, which was written on the original base. Replaced by: *the plan may change only by its owner; the entry set may only grow* (20.4). Same intent, still one file, still no history.
2. **Server-side email→key binding dropped.** It would give the rendezvous server state and is impossible on stock PeerServer or the public cloud. Trust rested on client-side pinning until v0.6, and now on team logs (8, item 20).
3. **`kalka` writes a few frontmatter keys.** A blueprint needs an identity that survives renaming, an owner and a team. This is the one exception to "the tool does not author blueprints" (6.2).
4. **Handoff must be recognisable by peers who missed it.** Overtaken by v0.6: a handoff is a release naming the new owner, so the chain carries it (7.2, item 20).
5. **Entry ids are content hashes.** Gives de-duplication and corruption detection; signatures (6.5) add authorship.
6. **Timestamps order the log.** Clock skew can reorder entries slightly; it cannot lose them.
7. **Three settings stay three.** ICE configuration is discovered from the rendezvous server (9.4); teams are created and joined, not configured (5).
8. **Public PeerJS cloud.** It provides a TURN server. It remains demo-only: no SLA, IDs anyone holding a public key can claim, and a reported history of rejecting Go WebSocket clients.
9. **Presence has no directory.** PeerJS servers do not list peers; presence is learned from replication (9.3).
10. **One device per email.** A second device gets `ID-TAKEN`. Multi-device needs a different ID scheme; deferred.
11. **The UI renders hostile input.** A received blueprint is untrusted Markdown displayed by a page that can write files, so sanitization and localhost-API hardening are requirements, not polish (11.2, 13).
12. **No offline queue on a server.** Team documents travel through whichever members are online (20.3).
13. **Workflow-agnostic.** All references to a specific pipeline were removed. The core interprets the frontmatter keys of 6.2 and one section heading; everything else in the file is pass-through.
14. **Trace blocks added (6.10).** Blueprints explained at business level are hard to tie back to code, and the details lost in that translation are the ones that matter for implementation. A trace block holds both views as flows and links them; `kalka` computes where they diverge. Lane names are free-form. All examples in this document use neutral, invented domains.
15. **Handoff entries were signed (v0.3).** Overtaken by v0.6: there are no handoff entries.
16. **Unparsed log content moves with the entry before it (v0.3).** "Preserved in place" and "rewritten in canonical order" could not both hold literally (6.4).
17. **Teams, replication and signatures (v0.4).** Four points of the v0.4 request could not work as written:
    - *A key-derived peer ID does not stop posing on a stock server*: any holder of the public key can register it. Channel binding prevents posing; on `kalka-rendezvous` a proof of the key prevents squatting (9.2, 9.6).
    - *A signature over the 12-hex entry id does not bind the text*: 48 bits can be matched by brute force. All signatures cover the full digest (6.5).
    - *"Body edits only by the owner" judged by the sender refuses every relay.* Team documents carry signed releases instead, and the sender no longer matters (20.4).
    - *"The lower hash wins" lets a removed owner undo the removal*, by back-dating an operation and grinding its hash. Replaced by "removal wins; the senior owner wins a mutual removal" (19.4).
18. **Two IDs per daemon (v0.4).** Overtaken by v0.6: key IDs only (9.2).
19. **Replication overrode "manual only" for team documents (v0.4).** Overtaken by v0.6: there is nothing else.
20. **Teams only (v0.6).** The v0.6 request was triaged against the code (`docs/plan-teams-only.md`). What it changed, and the points where the spec takes its own route:
    - *Two trust sources and two merge rules* (pins and the handoff chain for one-to-one, team logs and releases for teams) became one: team logs and releases.
    - *"A team owner can do anything a document owner can"*, taken literally, lets two people sign one version and makes disputes ordinary. Team owners act by taking the document over (7.4).
    - *"Releases not causally before a reassignment are rejected"*: document releases are not in the team log's graph. The reassignment cites the version its signer saw, and everything above it by anyone else is void (7.3, 20.4).
    - *Replay kept only the last reassignment of a document*, so a document reassigned twice lost its chain at the first cut-off. Every reassignment is now an anchor (20.4).
    - *Replication chose documents by team name anywhere in the folder*, so two teams with one name would have leaked into each other. Location now decides (14).
    - *Countersigned history is attested, not verified*: the destination cannot check source signatures. The badge says who vouches (20.7, 21).

### Residual risks

- **Corporate security review.** A tool moving design documents between machines over WebRTC may need sign-off even though nothing is stored centrally. Hosting `kalka-rendezvous` on corporate infrastructure is the mitigating position.
- **Networks that block WebRTC entirely.** TURN over TLS/443 covers most; some proxies will still defeat it.
- **Other writers of the same file.** Agents or editors may modify a blueprint while it sits in the folder. `kalka` tolerates this by construction (pass-through frontmatter, unparsed log content preserved, re-read before write), and stray plan edits never travel (7.5).
- **Team log rules are hard to change later.** Logs created under one rule set must replay the same way forever. Each operation carries a rule version (19.1), and no real team should be created before the harness passes for the current version.
- **Rendezvous load from replication.** Gossip with a small fan-out and a digest that ends idle sessions in one round trip keep it low (20.3).
- **A move can hang** until the person who started it is online after the accept (20.7).

---

## 18. Open questions and decisions

### 18.1 Open questions

Defaults apply unless answered.

1. **Multi-repo blueprints.** One `repo`/`base_sha` pair or a list? This only affects the `gh` plugin. *Default: single pair.*
2. **Access control at the rendezvous.** Which mechanism will sit in front of `kalka-rendezvous` in the corporate deployment (SSO proxy, network policy)? *Default: network policy.*
3. **Showing the planned change in a trace.** Should nodes be markable as new, modified or removed? *Default: not in v1; unknown node keys are already preserved, so a `change:` key can be added later.*
4. **Manual accept for joins.** Should a team owner be able to require a manual "accept" for each join? *Default: not in this version; the invite is the approval (19.7).*
5. **Key rotation.** Every signature check would need to know which key was valid when. *Default: future work; a lost key means removal and a new invite.*
6. **Teams across rendezvous servers.** An installation uses one rendezvous, so a person cannot be in two teams on two servers. With several teams per person this limit is met sooner. *Default: accepted limit; the join screen names the mismatch.*
7. **Several devices per person.** With key IDs a second device is a second identity. *Default: still a non-goal (1).*
8. **Team-wide invite links.** An invite names one email, so a link posted in a channel works for one person. *Default: named and single use (18.4, decision 18).*
9. **From a team back to Personal.** *Default: not in this work; "delete for the team" keeps the last copy in the team's trash.*

### 18.2 Decisions (v0.4, answered by the product owner 2026-11-13)

| # | Question | Decision |
|---|---|---|
| 1 | Peer IDs | Reversed in v0.6: key ID only (18.4, decision 2). |
| 2 | Team log conflicts | "Removal wins; the senior owner wins a mutual removal; hash as the last tiebreak" (19.4). |
| 3 | Automatic replication | Team documents replicate in the background between members, with no click, accept prompt or code, into `<folder>/<team>/` (20). |
| 4 | Installed peers | None needed before the first release: every milestone up to v1 may change the wire protocol and formats without keeping older builds working. |
| 5 | Joining | No manual accept; an invite names the invitee's email (19.7). |
| 6 | Blueprint updates | An owner's edits reach the team on *Release*, not on save (20.4). |
| 7 | Entries by removed members | Accepted if signed by a key the log ever bound to the author, and marked (6.5). |
| 8 | Reassignment power | Reversed in v0.6: any document, at any time (18.4, decision 6). |
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
| 1 | Networking in the front end | None: WebRTC, signaling and replication are in Go; the web UI only calls the local API and its event stream. |
| 2 | Shell and daemon | The shell runs the daemon in process when it takes the lock, and attaches when a headless daemon holds it (4.1). Wails' own single-instance feature is not used; the lock answers the question, and links are handed over through `ui/show` (19.7). |
| 3 | Commands and the daemon | Four run classes (12). File commands never need a daemon. |
| 4 | Pause | Fully offline: the rendezvous registration closed, replication, joins and probes stopped. It survives a restart and is always visible (22.3). |
| 5 | Invite joins | No approval step; the invite is the approval. The owner is told someone joined with their invite, the joiner that the join completed or failed (22.4). |
| 6 | One product per platform | One per-user installer or bundle per platform (23.1). On Windows it holds two executables built from the same code, because Windows fixes a program as console or GUI at link time. On Linux the desktop shell links WebKitGTK and would not start where it is missing, so the static `kalka` stays separate. |
| 7 | Local IPC | The same loopback HTTP API for the page, the shell and the CLI, with token, `Host`, `Origin` and `Sec-Fetch-Site` checks (13). |
| 8 | Wails version | v3, pinned to an exact release. Reasoning and sources: `docs/plan-desktop-app.md` §3. |
| 9 | Signing | No certificates yet. Signing is a configuration step; unsigned builds succeed and say so (23.5). |
| 10 | Windows testing | Done by the product owner on a real Windows PC after the work; until then Windows-specific code is compiled and unit tested only. |
| 11 | Updates | No auto-updater until builds are signed. |

### 18.4 Decisions (v0.6, teams only, product owner 2026-11-15)

| # | Question | Decision |
|---|---|---|
| 1 | Sharing | Teams only. One-to-one sync, pins, verification codes, accept prompts and conflict files are deleted. |
| 2 | Peer IDs | Key ID only, on one signaling connection; the email ID and the config `team` salt are deleted (9.2). |
| 3 | Roles | Team owner and member; no domain owner (19.3). |
| 4 | Owner tiebreak | The seniority chain: an appointee directly below the appointer; siblings by causal depth of the grant, then hash (19.4 rule 5). |
| 5 | Handoff | A release naming the new owner; immediate; no connection (7.2). |
| 6 | Reassignment | Any document, any time, by a team owner; an anchor with a cut-off (7.3, 20.4). |
| 7 | Race rule | A release above a reassignment's cut-off by anyone but its new owner is void (20.4). |
| 8 | Team owner as document owner | By take-over: reassign to oneself, then act; one signer per version (7.4). |
| 9 | Disputes | Only two different valid releases of one version; settled by a reassignment (20.4). |
| 10 | Displaced edits | Kept in the team's trash, with a notice (7.5). |
| 11 | Layout | Location decides; Personal; publish moves the file and reports the new path (14, 20.1). |
| 12 | Transfers | The offer is a pending document; the decision is in the destination log; copy and move get a new id; the move's tombstone comes after acceptance (20.7). |
| 13 | Attestation | A `transfer` entry by the actor and `accept-transfer` by the owner; the badge names both (20.7). |
| 14 | Who sees a pending transfer | It replicates to every destination member so it can be relayed; only the team's owners and the actor see it in the app. |
| 15 | Source entries during a pending move | Not carried; the source shows a banner (20.7). |
| 16 | Waiting items | Transfers, documents handed or reassigned to you, disputes, documents whose owner left (11.3). Handed-to-you items count until opened. |
| 17 | Join links | `kalka://join/<token>`, registered per user; the link only opens the join screen (19.7, 23). |
| 18 | Invite scope | Named and single use, as before (18.1 question 8). |
| 19 | Rendezvous | Its own binary, `kalka-rendezvous` (9.6). |
| 20 | Parity | A tested table; stable error kinds (12.2, 12.3). |
| 21 | Windows pass | Once, after the v0.6 work, with a rewritten `docs/desktop-testing.md`. |

---

## 19. Teams and roles

### 19.1 Team log

A team is a **signed, append-only set of operations**, not an editable document. The current state (members, owners, reassignments, transfer decisions) is computed from it by a deterministic replay (19.4).

- File: `<folder>/<team>/<team>-team.log`, one operation per line, as JSON. It is not Markdown, because hashes must cover exact bytes and editors change whitespace. It is not meant to be edited; the UI and `kalka team log` render it as a readable history. The index ignores it (it is not `*.md`).
- One operation: `{v, team, op, args, prev, signer, at, sig}`.
  - `v`: the rule version that replays it. All operations of v0.6 are `v: 2`. A later rule change uses a new version and never changes how an existing operation replays.
  - `team`: the team hash (19.2); empty in `create-team` itself.
  - `op` and `args`: one of the operations below.
  - `prev`: the hashes of the operations it builds on: the heads of the signer's log when it was made. These links make the log a causal graph.
  - `signer`: the signer's public key. `at`: the signer's clock, for display only; it plays no part in replay.
  - `sig`: an ed25519 signature over `"kalka-teamlog-v2\n" + hash`.
- An operation's hash is SHA-256 over its canonical encoding without `sig`: UTF-8 JSON with keys in lexicographic order, no insignificant whitespace, no floating-point numbers, `prev` sorted. The exact encoding is fixed in `docs/teamlog.md` and is part of rule version 2. Keys and signatures are base64url without padding.
- An operation is *known* once a peer holds it and has checked its signature. Operations are exchanged by hash (20.3), never as a whole file.

| Operation | `args` | Who may sign |
|---|---|---|
| `create-team` | `name`, `email` (the creator's) | Anyone; the signer becomes the first owner and member |
| `add-member` | `email`, `pubkey`, optional `invite` (id) | A team owner |
| `remove-member` | `email` | A team owner |
| `add-owner` | `email` (must be a member) | A team owner |
| `remove-owner` | `email` | A team owner |
| `reassign-document` | `doc` (`kalka_id`), `from` (email), `owner` (email), `after` (version), `plan` (64 hex, or empty when `after` is 0) | A team owner (19.5) |
| `accept-transfer` | `doc` (the new `kalka_id`), `owner` (email), `transfer` (64 hex digest of the `transfer` entry) | A team owner (19.6) |
| `decline-transfer` | `doc`, `transfer` | A team owner (19.6) |

Every operation is a team owner's; members sign entries and releases in documents, never log operations.

### 19.2 Team identity

A team's identity is the hash of its `create-team` operation. Its name is a label: `[a-z0-9][a-z0-9-]*`, used in file names and in `domain:` references. Two different teams with the same name can exist; the second one held locally gets a suffix on its folder, and a reference to that name is shown as ambiguous. A member's identity within a team is the pair of email and key bound by `add-member`.

### 19.3 Roles

- **Team owner.** A team has at least one; the creator is the first. Any team owner may invite, add and remove members, add and remove owners, reassign any document of the team (7.3), and decide transfers into the team (19.6). Every owner is also a member. The last owner cannot be removed (19.4 rule 6), and an owner cannot remove themselves (rule 7).
- **Member.** Publishes documents to the team, owns documents, comments, buds seeds, hands off the documents they own, and starts transfers of them to other teams they belong to.

The roles answer who may *change* what: the roster, ownership, and what enters the team. They do not limit reading (21).

### 19.4 Replay and validity

The state is a pure function of the set of known operations, so every peer holding the same set computes the same state, without coordination. `docs/teamlog.md` gives the precise rules and how they are computed; in short:

1. **Authority in view.** An operation counts only if its signer holds an owner term granted by one of its ancestors and not ended by one. Operations whose predecessors are not all known are pending, not discarded.
2. **Removal wins.** When the removal of X is effective, every non-removal operation X signed under the ended term that is not an ancestor of the removal is discarded, including operations concurrent with it. Back-dating an operation therefore gains nothing.
3. **Seniority decides between removals.** Removals are decided in order of their signers' seniority. A removal is blocked if an already accepted removal ended its signer's term without having seen it. An accepted removal stands even if a less senior owner removes its signer concurrently.
4. **Support, not ancestry.** An operation stands or falls on its own support: the signer's owner term and the target's member term must be effective and in its view. Having a discarded operation among one's predecessors does not discard an operation. Otherwise a removed owner's back-dated operation, once merged, would void every later honest operation.
5. **Seniority** is the chain of owner grants from the creator: the creator first; an appointee directly after their appointer, ordered among the appointer's other appointees by causal depth of the grant and then hash. Nobody can appoint an owner senior to themselves. With two grants for one owner, the better counts. Re-adding starts a new term, and seniority counts from the new grant. This is also the tiebreak for owners added concurrently: the hash orders only siblings under one appointer, who could have appointed them in either order, so grinding a hash gains nothing.
6. **Never without an owner.** If the effective removals together would leave no owner, the least senior of them is discarded.
7. **No self-removal.** An owner cannot remove themselves; another owner removes them.
8. **Same subject.**
   - An `add-member` for an email that is a member in the signer's view is discarded. Of concurrent `add-member` operations for one email, one key, or one invite id, the first in replay order counts.
   - A `reassign-document` whose `after` is lower than that of an effective reassignment of the same document in its view is discarded ("stale cut-off"). Of effective reassignments of one document with the same `after`, the last in replay order counts. Reassignments with different `after` all count: each is an anchor (20.4).
   - Of `accept-transfer` operations for one `transfer`, or for one `doc`, the first in replay order counts and the others are discarded ("already decided"). A `decline-transfer` is discarded when an `accept-transfer` for the same `transfer` counts: accept beats decline.

   The replay order is causal depth, then hash. These are the only places where a hash decides, and any team owner can always sign a later operation, so the hash gives nobody anything they did not have.
9. **Targets must be members.** The `owner` of a `reassign-document` or `accept-transfer` must be a member in the signer's view.

Resulting behaviour: in a mutual removal the senior owner stays. In a ring of three, the most senior remover's removal stands, and at least one owner remains. A member added by an owner who is concurrently being removed is not a member. A reassignment or transfer decision signed by an owner whose term a removal ended without seeing it falls with that term (rule 2).

Replay reports the discarded operations with their reasons, and the Team view shows them.

### 19.5 Reassignment and removal

- `reassign-document {doc, from, owner, after, plan}` is how a team owner names a document's next owner (7.3). `after` is the highest valid release of the document the signer holds; `plan` is that release's plan hash. `from` is the owner the signer saw: it is for the history and the notices, and replay does not judge it.
- **Removing a member.** Together with `remove-member`, the remover's `kalka` emits one `reassign-document` per team document it holds whose owner is the removed member, giving each to the remover. They do not depend on the removal: if the remover loses a mutual removal, their term ends and rule 2 discards the reassignments with it; if the removal is discarded for another reason (rule 6), the reassignments stand, since a team owner could have made them anyway.
- A document whose owner is no longer a member and that no reassignment covers (one the remover did not hold) is a waiting item for the team's owners (11.3).
- The `owner:` line follows: when the new owner's `kalka` holds the operation and the release at `after`, it rewrites `owner:` and releases version `after`+1.

### 19.6 Transfer decisions

A transfer into the team arrives as a pending document (20.7). Its decision is a log operation, because it needs a team owner's authority, "removal wins" for a decider who is being removed, and one winner among concurrent decisions; replay gives all three.

- `accept-transfer {doc, owner, transfer}`: `transfer` is the digest of the offer's `transfer` entry, so the signature on the operation is the accepting owner's countersignature of the plan and the history it covers. `owner` is the accepting owner, unless they name another member. The operation is an anchor with cut-off 0: `owner` is the only valid signer of version 1 (20.4).
- `decline-transfer {doc, transfer}`: the offer is discarded on every peer (20.7).
- Rule 8 settles concurrent decisions: one accept counts; an accept beats a decline.

### 19.7 Invites and joining

1. A team owner runs `kalka team invite <email>` or *Invite* in the Team view. The token is one line of base32 holding: the team name and hash, the inviting owner's public key and email (`inviter`), the invitee's email, a 32-byte one-time secret, the expiry (default 72 hours, `--expires`), and the rendezvous address. The open invite is stored in `invites.yaml`. `kalka` does not send the token; the owner passes it on through any channel, as a link `kalka://join/<token>` or as the bare token.
2. The invitee opens the link, pastes the link or the token into *Join with link or token…*, or runs `kalka join <link or token>`. The join screen (11.1) shows the team, the inviter, the invited email and the expiry; only *Join* or `kalka join` joins. The owner's key becomes their trust anchor for the team. The pending join is stored in `joins.yaml`. If the token names another rendezvous than the configured one, `kalka` says so.
3. The joiner's daemon probes the owner's key ID once a minute until it meets the owner or the invite expires; this survives restarts. Only the joiner can dial: the owner does not know the joiner's key. The owner's part is to be online. Nothing is queued on the rendezvous server.
4. On meeting, the joiner's `hello` carries `invite: {id, team, proof}`, where `team` is the team hash from the token and `proof` is an HMAC-SHA-256 under the secret over both public keys and the team hash. The owner checks it after `auth`. A valid proof is the verification.
5. The owner's `kalka` approves automatically (the invite is the approval), appends `add-member {email, pubkey, invite}` with the email *from the invite*, marks the invite used, and sends the log on the same connection. The joiner checks that the log starts at the team hash in its token and contains its own `add-member`. A folder `<folder>/<team>/` is created for the team.
6. A bad proof, an expired invite, a used invite, or an email other than the one invited ends the connection with a `bye` naming the reason (`invite-token`, `invite-expired`, `invite-used`, `invite-email`), and the joiner's UI shows it.

Single use holds in two places: the issuing owner's `invites.yaml`, and the replay rule that discards a second `add-member` for the same invite id (19.4 rule 8). Only the issuing installation can redeem an invite, because the joiner dials its key ID.

**Parsing.** One function parses what a user gives: a `kalka://join/<token>` link or a bare token, in any letter case, with surrounding whitespace or angle brackets as chat tools add them, at most 4 KiB. Line breaks inside are ignored (mail wraps long lines), and so is one trailing slash, which some systems add to a link. Another scheme, another path, extra path parts, a query, a fragment or a damaged checksum are refused with `invite-token`. The team name and the inviter are claims until the join completes, and are rendered as text.

**Links.** The `kalka` URL scheme is registered per user (23). The program that receives a link hands it to the running app with `POST /api/ui/show` on the join route and exits; with no shell running, the shell starts on that route; without a desktop shell, `kalka open <link>` opens the browser there and `kalka join <link>` joins directly. A link can only open the join screen.

---

## 20. Team replication

### 20.1 Scope and publishing

Every member keeps a full replica of the team's documents: the team log, the team documents (blueprints, seeds, domain documents and their snapshots), tombstones and pending transfers. **Replication never crosses teams**: a session is for one team, and a peer you share one team with gets nothing of another unless they are a member of it too. **Personal never takes part**: no Personal file enters an inventory, and a request for its `kalka_id` gets the same answer as a request for an id that does not exist (14).

A document enters a team when its owner publishes it: *Publish to team…* in Personal, or `kalka publish <file> --team <team>`. Only a member can publish, only to a team they belong to. `kalka` adds `kalka_id` and `owner` if missing, writes `team:`, makes the first release (20.4), and moves the file into the team's folder; the result carries the new path. No acceptance is needed and the member stays the owner. A document belongs to exactly one team or to Personal. Leaving a team is a tombstone: deletion (20.6) or a move (20.7); copies already replicated cannot be recalled.

### 20.2 Trust between members

Members trust each other through the team log: the keys it binds are the team's keys (8.2), so members never compare codes and received documents raise no prompt. Every operation, release and entry is checked before it is written. Nothing fails silently: a team document that fails a check is not written, and the Team view lists it with the reason.

### 20.3 Sessions and scheduling

- **When.** At daemon start, when a member comes online, after a local change to a team document or the log, and every ten minutes with jitter, per team. Each round picks at most three online members; a local change is pushed to at most five. Members are probed only while a round needs them.
- **A session**, over a key-ID connection with another member: exchange log heads and one digest over the inventory (`team_sync`). If both match, the session ends after one round trip. Otherwise: missing log operations first (`team_log`), then inventory lines (`team_inv`), then the wanted documents (`team_want`, each answered by a `team_doc`).
  - The responder merges the initiator's log operations before it checks membership, so a member just added by an operation the initiator brings is admitted in the same session. A peer that is not a member after the merge gets `bye` with reason `not-member`, the same answer as for a team the responder does not hold.
  - An inventory line is `kalka_id`, the head version, the head's plan hash, whether it is a tombstone, whether it is a pending transfer, and a digest over the document's entries that verify against team keys (attested entries included, 20.7). A line is wanted when the document is not held, when it is a tombstone, when its version is higher, when the entry digests differ, or when the versions are equal and the plan hashes differ (a dispute to learn about). Nothing is wanted for a document held as a tombstone.
  - The initiator sends the documents the responder wants first, then receives the ones it wants. A document that is not a team document of this team, or not held, is answered with empty content: unavailable, not an error.
  - Each received document is merged with the rules of 20.4 and written only if the merge changes the local file. A refusal (no valid release, a chain that does not lead to the incoming plan, a bad transfer) is recorded with its reason for the Team view, and the session goes on. When the entries are taken but the incoming plan is not, the document is written and the reason is recorded too.
- **Relaying.** Any member can pass on any team document it holds, pending transfers included. No author or owner needs to be online.
- Peers that are not members never see a `team_*` message for that team.

### 20.4 Merge rules for team documents

**Team log.** The union of valid operations, replayed (19.4).

**Releases.** A `release` entry makes a version of a team document's plan official:

```
### release · ewa@example.org · 2026-11-20T09:00:00Z · § · rev:<8 hex> · id:<12 hex> · sig:<base64url>
v7 · plan:<64 hex SHA-256 of the normalized frontmatter and body> · owner:ewa@example.org
incorporates: 7c2e91ab04f3, a41b77c09e12
Renamed "consignment" to "parcel"; added the invariant on redelivery attempts.
```

- The first line is `v<N> · plan:<hash> · owner:<email>`, followed by ` · deleted` for a deletion and ` · deleted · moved:<team name>/<kalka_id>` for a move (20.6). `owner` is the `owner:` of the released plan: the one who may release the next version. `incorporates` and the note are optional.
- The v2 signature covers the entry's full digest, which includes the version, the plan hash and the owner. Every release needs a v2 signature by a key the team log has bound to its author.

**The release chain.** The valid releases of a document form a chain. An *anchor* is an effective `reassign-document` of the document (its `after` and `owner`) or an effective `accept-transfer` for it (cut-off 0 and its `owner`). The *expected signer* of version n+1 is:

- the `owner` of the anchor with cut-off n, if there is one;
- otherwise the owner that the valid release of version n names;
- for version 1 with no anchor at 0: the owner that version 1 itself names (the publisher).

A release of version n+1 counts if its signer is the expected signer and, unless an anchor has cut-off n, version n counts. So an anchor restarts the chain: a gap, a dispute or a void release below it does not void what stands above it, and everything above its cut-off signed by anyone else is void, whenever it was signed. That is the race rule of 7.3: a release is *causally before* a reassignment exactly when its version is at most the reassignment's `after`. Releases need not cite log heads, because the only question is whether the reassignment had seen the release. Nothing above a tombstone counts.

A handoff (7.2) is a release naming another owner. A team owner acting on someone else's document reassigns it to themselves first (7.4), so there is exactly one expected signer for every version.

**Rule.** For a team document of a team whose log the local peer holds, the incoming plan (frontmatter and body) replaces the local one when the head of the chain names the incoming plan's hash and has a higher version than the local plan's release. The sender does not matter. A local plan that is not released (the owner's unreleased edits) counts as the highest version the local file already held a release of. Replacing a plan that is not the plan of any release keeps the displaced file (7.5).

**Disputes.** If one version has two different valid releases, neither counts, and nothing above them does until an anchor; the document is shown as *ownership disputed*, and it is a waiting item for the team's owners (11.3). With one expected signer per version this can only happen in two ways:

1. One key signs two different releases of the same version: an identity key copied to a second machine, a restore from a backup, or bad intent. Picking one by hash would let that signer take a handed-off document back by grinding.
2. Two members publish the same `kalka_id`, each naming themselves in version 1: a Personal file passed around by hand and published twice.

Any team owner settles either with one reassignment, whose anchor restarts the chain.

**Last release withdrawn.** A release made by the `owner` of a reassignment that replay later discards (its signer lost an owner term concurrently) becomes void. Peers that took its plan keep the file, mark it "last release withdrawn", and take the next valid release they are offered.

- Domain documents also carry `version:` in frontmatter; blueprints and seeds carry the version only in their releases.
- **Releases are made on an action, not on save:** *Publish*, *Release*, *Hand off*, *Promote*, *Delete for the team*, and the release a new owner's `kalka` makes after a reassignment or an accepted transfer. An owner with unreleased edits sees a banner.

**Entries.** The union of all valid entries from both sides, de-duplicated by `id`, sorted by `(timestamp, id)`; where both sides hold an entry with the same `id`, the copy whose `sig` verifies is kept, so a relay cannot strip a signature. Unparsed log content from the side whose plan was kept is preserved. An entry enters the replica only if its v2 signature verifies against a key the log has ever bound to the author's email, or it is attested by an effective transfer (20.7). Others stay in the file that holds them, *local only*. The merge is idempotent, commutative on the entry set, and never drops a valid entry.

### 20.5 Where received documents go

Documents received from members are created in `<folder>/<team>/`, with the sanitized name the sender suggests and a numeric suffix if taken. Documents are found by `kalka_id` within their team, so a user may move a received file into a subfolder of the team folder. Domain snapshots are written beside their live document. Pending transfers are kept in `<folder>/<team>/.kalka/incoming/<kalka_id>.md` until decided.

### 20.6 Deletion

- Only a document's owner deletes it for the team: *Delete for the team* or `kalka delete <file>`. This makes a release with the next version, marked as a deletion, whose plan is a **stub**: the frontmatter with `deleted: true` and nothing else.
- It spreads like any release, and no release above a tombstone is valid. A tombstone beats any older copy, in both directions of a merge.
- On each peer the file leaves the folder. The stub goes to `<folder>/<team>/.kalka/tombstones/<kalka_id>.md` and the last full copy to `<folder>/<team>/.kalka/trash/`. Replication reads the tombstone directory, so an offer of an older copy is answered with the tombstone.
- The stub keeps only its release entries, the tombstone included, so that every peer can check the chain that leads to it. This is the one place where valid entries are dropped (2).
- **A move's tombstone** (20.7) is the same, with `moved:<team name>/<kalka_id>` in the release line and `moved-to:` in the stub. The source team shows "moved to <team>" in place of the document.
- Tombstones are kept forever; each is a few hundred bytes.
- A plain file deletion is not a tombstone (14).

### 20.7 Transfers between teams

A transfer copies or moves a document into another team. It is available to the document's owner, or to a team owner of the source team (a move by a team owner takes the document over first, 7.4; a copy does not need to). The person acting, the *actor*, must be a member of both teams. Moving from Personal into a team is publishing (20.1), not a transfer.

A transfer has three parts in three places:

| Part | Where | Signed by |
|---|---|---|
| The **offer**: a new document carrying a `transfer` entry | The destination's replica, as a pending document | The actor |
| The **decision**: `accept-transfer` or `decline-transfer` | The destination's log (19.6) | A destination team owner |
| The **source tombstone** (move only) | The source team, a final release | The actor, as the document's owner in the source |

**Starting.** `kalka copy <file> --to <team>` or `kalka move <file> --to <team>`, or *Copy to team…* / *Move to team…*:

1. Checks, all local: the actor is a member of the source and of the destination; the actor is the document's owner or a source team owner; the document is not disputed; no transfer of it is pending (`transfer-pending`). A non-member of the destination is refused with `not-member`.
2. A new document is built: a fresh `kalka_id`; `team: <destination>`; `copied-from: <source team name>/<source kalka_id>`, or `moved-from:` for a move; `owner:` the actor for now; the plan of the source's head release (unreleased edits do not travel: a file that differs from its head release is refused until its edits are released or undone); every valid entry of the source, byte for byte, with their signatures.
3. The actor's `kalka` appends a signed `transfer` entry and writes the file to `<folder>/<destination>/.kalka/incoming/<new id>.md`, from where destination replication carries it.
4. For a move, a signed `transfer` entry is also appended to the source document, so the source team sees "being moved to <team>; comments added now will not travel". A copy leaves the source unchanged byte for byte.

**The `transfer` entry** is the actor's countersignature:

```
### transfer · ewa@example.org · 2026-12-01T10:00:00Z · § · rev:<8 hex> · id:<12 hex> · sig:<base64url>
move · from:parcel-ops/<source kalka_id> · source:<64 hex team hash> · to:library-desk/<64 hex team hash>
plan:<64 hex>
history:<64 hex> · entries: 7c2e91ab04f3, a41b77c09e12
```

- The first body line is `copy` or `move`, the source document, and both teams by hash; `to` also names the destination, so source members who are not in it can read the banner. `plan` is the plan hash of the offered plan.
- `history` is SHA-256 over the lines `<64 hex digest of the entry's id input> <sig or "-">\n`, one per listed entry, in `(timestamp, id)` order. It covers the full digests, not the 48-bit ids, and the original signature text.
- `entries` lists the ids of the attested set, so every peer knows which entries the digest speaks for, also after members add their own.
- The entry's own v2 signature covers its body and the new `kalka_id`. So the actor signs the mode, both teams, the plan and the exact history.
- In the source document of a move, the same entry (with the source's `kalka_id`) records the start.

**Reaching the owners.** The pending document is part of the destination's replica: its inventory line is marked pending, and it is offered and fetched like a team document, so any destination member relays it and the actor can go offline once one other member holds it. It stays outside the index and is shown only to the team's owners (to decide) and to the actor (to watch or withdraw). A peer takes a pending document only if all of this holds:

- the `transfer` entry's signature verifies against a key the destination log binds to its author, who is a member;
- `to` is this team's hash and `plan` is the document's plan hash;
- the listed entries are present and hash to `history`;
- the document has no release.

Anything else is refused and listed in the Team view with the reason.

**Deciding.** Any destination team owner, in the Waiting view or with `kalka transfer accept|decline <id>`:

- **Accept** signs `accept-transfer {doc, owner, transfer}` (19.6), with `owner` the accepting owner unless `--owner <member>` names another member. The named owner's `kalka` then rewrites `owner:`, makes release 1, and moves the file from `.kalka/incoming/` into the team folder. From there it is an ordinary team document; its owner can hand it off to anyone in the team.
- **Decline** signs `decline-transfer {doc, transfer}`. Every peer replaces the pending file by a stub in `.kalka/tombstones/` holding the `transfer` entry only, so a lagging replica cannot bring the offer back; the full offer is kept in `.kalka/trash/<kalka_id>.offer.md`, so an accept that wins after all (below) restores it. A copy is thereby discarded. For a move, the actor's `kalka` appends a `transfer-withdrawn` entry answering the source's `transfer` entry (`re: <id>`, body `move declined`), and nothing else changes there.
- **Withdraw:** the actor signs a `transfer-withdrawn` entry on the pending document (`re: <id>`, body `withdrawn`). It has the effect of a decline unless an accept counts: the stub keeps the withdrawal, so it travels as one. For a move, the source gets `move withdrawn` in the same way.
- Concurrent decisions: an accept beats a decline or a withdrawal; of two accepts the first in replay order counts (19.4 rule 8). So there is never a second version 1.

**Attested entries.** Entries listed by an accepted transfer are *attested*. They are not checked against destination keys, take no part in the destination's release chain (source releases are shown as history, "v3 in parcel-ops"), and are shown as "from parcel-ops, attested by Ewa and Jan": the actor and the accepting owner. Before the accept the badge names the actor only. An attested entry never changes: one that no longer matches `history` is dropped with a warning, as an entry with a wrong id is. Entries written after the transfer are ordinary destination entries.

**The source tombstone.** For a move, the actor's `kalka` writes the tombstone (20.6) when its destination replica shows the effective accept. The trigger is derived from files: a live source document whose `transfer` entry of a move, by the local user, is not answered by a `transfer-withdrawn`, and an effective accept of that entry's digest in the destination log (the entry in the source and the offer's entry have one digest; only their signatures differ). It needs no state file and survives restarts. Until then the source is live and unchanged, with its banner.

- If the actor is offline, the tombstone waits for them; both documents exist meanwhile.
- If the actor can no longer sign it (the source was reassigned, or the actor removed, in the meantime), the move ends as a copy: the actor's `kalka` answers the entry with `move ended as a copy`. The actor gets a notice; the source's owner may delete it.
- A move withdrawn and then accepted all the same (an accept beats a withdrawal) also ends as a copy: the source's entry is already answered.
- Entries added in the source after the transfer started are not carried. They stay in the source and in its trash copy after the tombstone.

**Identity.** Copy and move both get a new `kalka_id`. A tombstone is final for its id on every peer of its team, so with the id kept a document could never move back; and a member of both teams would hold two live files with one id. The store indexes `moved-from` and `copied-from` (14), so a seed's `parent:` and a `budded:` link still resolve to a moved document that is held.

**Domain documents and seeds.** `version:` restarts with release 1 in the destination; snapshots are not carried. An accept is refused (`domain-exists`) when the destination holds a document for that domain. A seed keeps `parent` and `origin`.

---

## 21. Threat model

What the design protects, and what it does not:

- **Only members are peers.** A connection from someone who shares no team is refused before any file is read (8.2). There is no other way in.
- **Roles protect against unauthorized changes, not against reading.** The roster, ownership and what enters a team can only be changed by the roles allowed to (19.3), and every peer checks this itself. Reading is not restricted within a team: every member holds a full replica. Replicas cannot be recalled: a removed member keeps everything they already received.
- **Teams are kept apart, and Personal stays local.** One function decides what replicates, by location (14, 20.1). A peer in one of your teams learns nothing of another; a Personal file never appears in an inventory, and requests for it look like requests for unknown ids.
- **A team owner can take any document.** Reassignment needs no consent (7.3), so a team owner's key controls every document of the team. Displaced edits are kept (7.5), and the history shows who reassigned what.
- **A team owner's key compromises the team.** Its holder can change the roster and reassign documents. The most senior owner's key is the root of the team: an owner can be removed by a more senior one, but a removed owner who is senior to the remover can still counter-remove (19.4). Nobody can appoint an owner senior to themselves, so the creator's key outranks every other. Key rotation is future work (18.1).
- **A leaked invite token or link** lets its holder join until it is used or expires, but only under the email the owner named (19.7), and only once. Short expiry and single use limit the window; an owner who suspects a leak removes the member. Anyone can raise a `kalka://` link from a web page, but a link can only open the join screen.
- **Attested history is two people's word.** The destination of a transfer cannot check source signatures, nor that the actor had the right to take the document out of the source: the source-side role check is a product rule, not a security boundary, since any source member already holds the text. An actor can attribute any text to anyone "from the source team"; the badge names who vouches for it, and the accepting owner sees the whole history before accepting (20.7).
- **A pending transfer is readable by destination members before the decision.** It travels through their replicas so that it reaches the owners; offering a document to a team discloses it to that team.
- **The rendezvous server is not trusted.** It can refuse service, keep a peer offline by occupying its ID (on servers other than `kalka-rendezvous`, 9.2), and see who connects to whom. It cannot read or alter a session (channel binding, 8.3) or impersonate a member (the team log).
- **Members can comment in their own name only.** Entries are signed (6.5); a forged entry does not enter the replica (20.4). Entries written outside `kalka` stay unsigned and local.
- **The local API is reachable by local processes and by web pages aimed at `localhost`.** It answers only with the per-start token, checks `Host` against DNS rebinding, and refuses state changes from foreign origins (13). Another local user cannot read the token (owner-only `daemon.json`). Processes of the same user are trusted, as they can read the identity key anyway.
- **Out of scope:** a member's machine being compromised; traffic analysis; denial of service by a member flooding operations or transfers, which other members see and can answer by removal.

---

## 22. Desktop app

Team replication works only while members are online at the same time, so kalka runs in the background from login, independent of any window. The desktop shell gives that background process a window and a tray icon.

### 22.1 Window and tray

- The window shows the web UI (11) from the daemon's localhost address. Nothing is bound into the page; it behaves exactly as in a browser tab.
- **Tray menu:** *Open kalka*; a status line ("Online · 4 of 6 members reachable · last sync 14:02", "Paused", or "Offline: rendezvous not reachable"); "N waiting for you" when any team has waiting items, opening the window on the waiting list of the team holding the oldest item; *Pause sync* / *Resume sync*; *Start at login* with a check mark; *Quit kalka*.
- The status comes from `GET /api/summary`: distinct members reached out of all members of all teams; the latest replication round that reached a member; the waiting total and the oldest item's target (13). The tray refreshes on the daemon's events, at most once a second.
- **Quit** asks first: "kalka will stop syncing with your teams until you start it again." It then stops the daemon after its running rounds finish. When the shell is attached to a headless daemon, quitting stops that daemon too, and the question says so.
- A `kalka://` link opens the window on the join screen (19.7).

### 22.2 Settings

| Setting | Default | Meaning |
|---|---|---|
| Start at login | on, set once at the shell's first start | A per-user entry, no admin rights: Windows `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, macOS a LaunchAgent in `~/Library/LaunchAgents`, Linux an XDG autostart file. The entry starts the shell with `--hidden`, or `kalka daemon --background` where no shell is installed. The system's entry is the truth; it is not copied into `state.yaml`. |
| Keep running when closed | on | Closing the window hides it. The first time, one notification says that kalka keeps syncing and where its icon is. Off: closing the window quits the shell, and an in-process daemon stops with it. |
| Notifications | on | One switch for the notices of 22.4. Waiting items still show in the window when it is off. |
| Pause | off | 22.3. |

- `--hidden` starts the shell with no window shown; autostart passes it. Starting the shell again always shows the window.
- On Linux without a tray host (stock GNOME without an extension), closing minimizes instead of hiding, so the window stays reachable.
- `kalka settings` changes the same settings from the CLI (12.1).

### 22.3 Pause

Pause takes kalka offline: the daemon closes its rendezvous registration and stops replication, joins and presence probes. Local work continues: comments, seeds, publish, release, handoff, reassignment and transfers write locally and are pushed after resume. While paused, `join` fails with `"kind": "paused"`. The pause is kept in `state.yaml` and survives a restart; the tray icon has a paused state, the UI shows a banner, and each start while paused raises one notice.

### 22.4 Notifications

The core decides; the shell or the headless daemon shows them as OS notifications. A click opens the window (or the browser) on the subject.

| Notice | When |
|---|---|
| Handed off or reassigned to you | A release or reassignment makes the local user the owner of a document. |
| Your document was reassigned | A reassignment takes a document from the local user. |
| A transfer waits for your team | A pending transfer arrives in a team the local user owns. |
| Your transfer was decided | A transfer the local user started is accepted or declined; a move finished, or ended as a copy. |
| Your edits were kept aside | Replication displaced a local plan (7.5). |
| New comments on your document | Replication adds entries by someone else (not releases or `budded`) to a document the local user owns. One notice per document per round; a first copy raises none. |
| Someone joined with your invite | The owner's side, when a join is admitted. |
| Your join completed or failed | The joiner's side, when a join reaches a final state. Retries raise nothing. |
| Newer domain version | A received release raises a domain to a version above the one a document owned by the local user references. One notice per domain and version. |

On macOS, an unsigned build cannot post to the notification centre; notices show only in the window. In a browser tab the page's own browser notifications remain; in the desktop window they are not used. `kalka daemon --no-notifications` shows none.

---

## 23. Platforms and distribution

### 23.1 What is installed

| Platform | Package | Contents |
|---|---|---|
| Windows (primary) | Per-user installer, no elevation, into `%LOCALAPPDATA%\Programs\kalka`; adds the folder to the user's `PATH`, a Start-menu shortcut, and the `kalka` URL scheme | `kalka.exe` (console: CLI and headless daemon), `kalka-desktop.exe` (GUI: the shell; with arguments it runs the CLI without a console window), `kalka-plugin-gh.exe` |
| macOS | `Kalka.app` in `~/Applications`, or anywhere | One executable, which is the shell without arguments and the CLI with them; `kalka` on the `PATH` is a symlink to it |
| Linux | One archive | `kalka` (static), `kalka-desktop`, a `.desktop` file and an icon, and the list of system packages the shell needs |
| Servers | `kalka-rendezvous_<v>_<os>_<arch>` archive | `kalka-rendezvous` (9.6) |

The headless archives of 15 remain for servers, containers and machines without a desktop.

### 23.1a The `kalka` URL scheme

Registered per user, never with administrator rights, and never by a test (tests use a fake registry, a temporary data directory and a recording command runner):

| System | Registration | How a link arrives |
|---|---|---|
| Windows | `HKCU\Software\Classes\kalka` with `URL Protocol` and `shell\open\command` = `"…\kalka-desktop.exe" "%1"`. Written by the installer, removed by the uninstaller, and re-checked by the shell at start | A new `kalka-desktop.exe` with the link as its only argument. A single `kalka://` argument starts the shell on a route instead of running the CLI; if a shell runs, it hands over through `ui/show` and exits 0 |
| macOS | `CFBundleURLTypes` with the scheme `kalka` in `Kalka.app`'s `Info.plist`; the system registers it when the bundle is first opened | An Apple Event to the running app, or to a newly started one; the shell shows the window on the join route |
| Linux | `MimeType=x-scheme-handler/kalka;` and `Exec=kalka-desktop %u` in `kalka.desktop`. The shell writes the file to `$XDG_DATA_HOME/applications` and runs `xdg-mime default kalka.desktop x-scheme-handler/kalka` at start | As on Windows: a new process with one argument |

Chat tools may not make `kalka://` clickable; the invite dialog therefore offers *Copy link* and *Copy token*, and the paste box takes either.

### 23.2 Windows

- WebView2 is preinstalled on Windows 10 and 11, but can be missing or removed by policy. Before opening a window the shell reads the runtime version from the registry keys Microsoft documents for the Evergreen runtime. If it is missing, a native message names the download and offers "Open kalka in the browser instead", which keeps the daemon running and uses `kalka open`. The window is never blank.
- Notifications need an AppUserModelID registered for the sender. Wails' notification service registers its own under `HKCU\Software\Classes\AppUserModelId` at first start, so the installer's Start-menu shortcut is a plain one. Untested on real Windows until the product owner's pass.

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
