# kalka — blueprint sharing in teams

*Kalka* (Polish: tracing paper, carbon copy). The name appears in the binaries, the plugin prefix (`kalka-plugin-<scheme>`), the frontmatter key (`kalka_id`), the URL scheme (`kalka://`) and protocol strings; each component keeps it in one constant.

Specification v0.7 · 2026-11-22

This document describes kalka as built. Sections 1–15 and 19–23 are normative. Section 16 gives the end-to-end scenarios the test suite runs, 17 the reasoning behind choices that are not obvious and the risks that remain, 18 open questions. Before version 1, the wire protocol and the file formats may change between releases without a migration.

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
9. **What a plugin knows stays with the reader.** Whatever a plugin tells (a resolved reference, an issue's title, status or parents) is fetched and cached on the reader's own machine, with their own credentials. It is never written into a blueprint and never sent to a peer. The core enables a feature only for the capabilities a plugin declares and never branches on a plugin's name or scheme. Trackers are one-way: kalka reads their metadata and exports final versions to them, and nothing from a tracker comes back into a file. Only an explicit user action puts tracker information into the shared file: the `kalka_issue` key the author writes, and the `exported` entry an export appends (10.5).

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
| Team document | A document in a team's folder with `kalka_team:` naming that team and a valid release; replicated to every member (20.1). |
| Publish | Moving a document from Personal into a team, with its first release (20.1). |
| Release | A signed entry that makes a version of a team document's plan official (20.4). |
| Handoff | A release by the owner naming another member as owner (7.2). |
| Reassignment | A team owner's signed operation naming a document's next owner (7.3, 19.5). |
| Tombstone | A final release marking a team document deleted or moved away (20.6). |
| Transfer | A copy or move of a document into another team, accepted there by a team owner (20.7). |
| Attested entry | An entry brought in by a transfer: vouched for by the person who started it and the owner who accepted it, not verified against the destination's log (20.7). |
| Waiting item | Something only the local user can move forward, counted per team (11.3). |
| Seed | A blueprint with `kalka_status: seed`: an idea split off from a growing blueprint (6.11). |
| Domain document | A team document with `kalka_kind: domain` holding a team's shared language (6.12). |
| Rendezvous | The signaling server (PeerJS protocol) that lets two peers find each other. |
| Key ID | The peer ID a daemon registers at the rendezvous, derived from its public key (9.2). |
| Rev | Short hash of the normalized body. Tells which body an entry was written against. |

The app shows some of these terms in plainer words; the CLI, the file format and this specification keep the terms. Entries are shown as comments (and deviations); the review log as the comments; an attested entry as "carried over"; an outdated entry as "on vN"; an orphaned one as "section removed"; an incorporated one as "taken up in vN"; a handoff and a reassignment both as *Change owner…*; a waiting item under *Needs you*; the pre-backlog as *Seeds*; promoting a seed as *Turn into a blueprint*; a `budded` entry as "split off a seed"; the rendezvous as "your team's kalka server"; replication and rounds as sync; an invite token as an invite code.

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
kalka_owner: ewa@example.org
kalka_team: parcel-ops
repo: org/depot-service      # opaque to the core; read by plugins
base_sha: 3f9c2a1
---

# Depot routing refactor

## Context
…

## Plan
See [the router](gh:src/routing/plan.py#L120-L180) and [PROJ-123](jira:PROJ-123).

## Review log

### release · ewa@example.org · 2026-11-11T09:00:00Z · § · rev:3fa9c1d2 · id:5b0e7c1d9a2000000000000000000000 · sig:<base64url>
v1 · plan:<64 hex> · owner:ewa@example.org

### comment · marek@example.org · 2026-11-11T17:40:03Z · §plan · rev:3fa9c1d2 · id:7c2e91ab04f300000000000000000000 · sig:<base64url>
Doesn't handle the retry path when a delivery attempt is rescheduled halfway.

### deviation · marek@example.org · 2026-11-12T09:12:40Z · §plan · rev:3fa9c1d2 · id:a41b77c09e1200000000000000000000 · sig:<base64url>
Moved the weight check into the intake handler because …
```

### 6.2 Frontmatter

- YAML between `---` lines at the top of the file.
- `kalka` attaches meaning to the keys in the table below. Each carries the `kalka_` prefix, so a team's own pipeline never collides with one. A `kalka_owner` that is not an email address is an error (the document cannot be released or shared until it is fixed); any other key of the table with another shape is ignored with a warning.
- All other keys are **pass-through**: never interpreted, validated or required by the core, whatever their names, types or nesting. They are preserved byte for byte on disk, travel with the file to members, and are handed to plugins as an opaque map. A blueprint with any frontmatter, or none, is valid.

| Key | Shape | Meaning | Written by `kalka` |
|---|---|---|---|
| `kalka_id` | UUID v4 | Identity of the document | On publish, seed creation, or transfer (20.7) |
| `kalka_owner` | email, compared in its canonical form (ASCII lower case) | The one owner (7.1) | On publish if absent; on handoff; when a new owner releases after a reassignment or an accepted transfer |
| `kalka_team` | team name (19.2) | The team the document belongs to (20.1) | On publish; on a transfer |
| `kalka_status` | `[a-z][a-z0-9-]*` | `seed` marks a seed (6.11); `closed` says the owner confirmed the document delivered (6.18); other values are labels | On seed creation; on promotion; on *Close* and *Reopen* |
| `kalka_parent` | on a blueprint: the parent plan node's `kalka_id`; on a domain document: `<team>/<domain>@v<N>` | The parent in the plan tree (6.18), or the parent domain node's pinned version (6.12) | Never; the author writes it |
| `kalka_breakdown` | `decomposed` or `adopted` | How a plan node's children came about: split in kalka, or adopted from outside it (6.18) | Never |
| `kalka_origin` | `<kalka_id>[#<entry id>]` | Where a seed was split off: the document, and the entry that triggered it (6.11) | On seed creation |
| `kalka_created` | RFC 3339 UTC | When a seed was created | On seed creation |
| `kalka_copied_from` | `<team name>/<kalka_id>` | The source of a copy (20.7) | On a copy |
| `kalka_moved_from` | `<team name>/<kalka_id>` | The source of a move (20.7) | On a move |
| `kalka_moved_to` | `<team name>/<kalka_id>` | Where a moved document went; only on its tombstone stub (20.6) | On the move's tombstone |
| `kalka_kind` | `domain` | A domain document (6.12) | Never; the author writes it |
| `kalka_domain` | on a domain document: a dotted name, each segment `[a-z0-9][a-z0-9-]*`, such as `library.reservations`; on any other document: `<team>/<domain>@v<N>`, or a list of them `[a, b]` | The domain node's name, or the domain node versions the document rests on (6.12) | Never |
| `kalka_version` | positive integer | A domain document's released version | On release (6.12) |
| `kalka_reference` | `kalka-blueprint@v<N>` | Opts the document into hints (6.13) | Never |
| `kalka_issue` | `<scheme>:<payload>`, a plugin reference (10.1) of at most 200 characters, such as `jira:DEV-123` | The one issue the blueprint addresses (10.5) | Never; the author writes it |
| `kalka_archived` | RFC 3339 UTC | Archived since then: lists hide it unless asked (10.5) | On *Archive*; removed on *Unarchive* |

- Every other key, `owner`, `team` or `version` included, is the team's own. Whether a document is deleted is never a key: its deleted release decides (20.6).
- The `repo` and `base_sha` keys in the example above are an instance of this: they mean something to the `gh` plugin only. So does `repos`, its map of further repositories (10.4).
- A file without `kalka_id` is a *local* blueprint: listed in Personal, not yet in any team.
- **Publish** adds `kalka_id`, `kalka_owner: <config email>` if absent, and `kalka_team:`, creating the frontmatter block if the file has none. Whenever `kalka` edits frontmatter it must change only the lines it writes and must not re-serialize the rest (key order and comments stay intact).

### 6.3 Body

Everything after the frontmatter except the review log section. Free-form Markdown; the core gives it no meaning beyond headings (for anchors) and links (for references).

### 6.4 Review log

- The log starts at the first line that is exactly `## Review log` (case-insensitive; spaces allowed after `##` and at the end), at the start of the line and outside a code fence. This is a line rule, not a Markdown parse, so what is hashed never depends on a parser's edge cases. A later such line is ordinary body text, with a warning. Its extent runs to the next H1/H2 line outside a fence, or to end of file. It should be the last section, but `kalka` must cope if another tool has appended sections after it.
- If absent, `kalka` creates it at end of file when the first entry is added.
- Content between entries that does not parse as an entry is preserved and shown as "unparsed" in the UI. It never blocks anything. It belongs to the entry before it (or to the log heading, if it comes before the first entry) and moves with that entry when the log is rewritten in canonical order (20.4). It is not part of that entry's body or `id`.

### 6.5 Entries

Header line, strict:

```
### <kind> · <author-email> · <timestamp> · §<anchor> · rev:<8 hex> · id:<32 hex>[ · <name>:<value>]…[ · sig:[<term>.]<base64url>]
```

- Separator is ` · ` (space, U+00B7, space).
- `kind`: `comment` (a remark), `deviation` (a proposed or actual departure from the plan as written, recorded instead of editing the body), `release` (20.4), `budded` (6.11), `transfer` and `transfer-withdrawn` (20.7), `exported` (10.5). `release` and the transfer kinds have behaviour in the core; `budded` is rendered as a link to the seed; `exported` as a line in the version history; `comment` and `deviation` differ in label and styling alone. The set of `deviation` entries is the document's *deviation log* (6.13). Unknown kinds matching `[a-z][a-z0-9-]*` must be preserved, merged and rendered generically, so a team's own workflow can introduce kinds without changes to `kalka`.
- `timestamp`: UTC, RFC 3339, second precision, `Z` suffix.
- `anchor`: slug of the body heading the entry refers to, or empty (`§`) for a general entry. A header is valid when the slug holds only ASCII lower-case letters, digits, `-` and `_`, and non-ASCII letters and digits; no Unicode tables are consulted, so a toolchain update cannot invalidate a header. Slug rule: lower-case; drop everything except letters, combining marks, digits, `_`, spaces and hyphens; spaces to hyphens; duplicates get `-1`, `-2`… in document order (GitHub's algorithm). A node inside a trace block (6.10) is addressed as `§<slug>#<node-id>`, where the slug is that of the nearest heading above the block.
- `rev`: first 8 hex characters of SHA-256 over the normalized body (6.6) at the time of writing.
- `id`: first 32 hex characters (128 bits) of SHA-256 over `kind \n author \n timestamp \n anchor \n rev \n extension fields \n normalized entry body`, where the extension fields are written as in the header, joined by ` · `, and are an empty line when there are none. 128 bits keep anyone from searching out a second entry with someone else's id. An entry whose `id` does not match its content is *edited*: a warning, not an error, so one edited entry never takes the document out of the replica. Peers do not take it; if the team holds the original, the next merge puts it back, and otherwise the text stays in the file as unparsed content.
- Extension fields: up to 8 fields `<name>:<value>` between `id` and `sig`, with `name` matching `[a-z][a-z0-9-]{0,31}` (not `rev`, `id` or `sig`) and a value without spaces. They are for fields a later `kalka` adds: kept as written, shown, part of the `id` input and so covered by the signature, and otherwise ignored.
- `sig`: an optional last field ` · sig:[<term>.]<base64url>`, with no padding, holding an ed25519 signature by the author's identity key. It is not part of the `id` input. `term` is the first 16 hex of the team log operation (create-team or add-member, 19.1) that bound the signing key to the author in the team whose copy this is; it is absent outside a team. The message is `"kalka-entry-v2\n" + kalka_id + "\n" + term + "\n" + hex(SHA-256 of the id input)`. It covers the full 256-bit digest, so it cannot be moved to another text, and the term, which names one key in one team. `kalka` signs every entry it creates, of every kind, in the UI and in `kalka comment`, under the user's current term in the document's team.

**Checking a signature.** A signature covers the `kalka_id`, so entries written in a file that has none yet stay unsigned. In a team document, a signature is checked against the one key its term names, which must be a term of this team that bound the author's email (19.1); a signature without a term, or with a term of another team, does not verify there. Terms of removed members still resolve, so their entries keep verifying. When a document written in Personal is published, `kalka` signs the user's own entries again under the team's term; it never signs anyone else's. In Personal, it is checked against the local key when the author is the local user, and against the keys any held team log binds to the author otherwise. The UI shows:

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
- `release` body: see 20.4. `budded` body: see 6.11. `transfer` and `transfer-withdrawn` bodies: see 20.7. `exported` body: see 10.5.

Entries are immutable. A correction is a new entry with `re:`.

### 6.6 Normalization and hashing

Used for comparison and hashing only; never changes what is on disk.

1. Decode as UTF-8; strip a leading BOM.
2. Convert CRLF and lone CR to LF.
3. Strip trailing spaces and tabs from every line. Two trailing spaces are a Markdown hard line break: the hash does not cover them, so adding or removing one does not change an `id`.
4. Strip leading and trailing blank lines of the unit being hashed (body, frontmatter, or entry body).

**What a hash covers.** The body is the text before the review log and the text after it, each normalized, joined by one blank line; moving a section from one side of the log to the other does not change it. The plan a release names is the normalized frontmatter, the separator `"\n\x00\n"`, and the body; its hash is SHA-256 of that in UTF-8. Frontmatter is normalized as text only: a tool that re-serializes the YAML (reorders keys, changes quoting) changes the plan, so the owner sees unreleased edits and anyone else's such change is displaced at the next release. The id input joins its fields with newlines; no header field can hold one, so the encoding is unambiguous.

On write, `kalka` preserves the file's existing line-ending style (CRLF if the file predominantly uses it) and writes UTF-8 without BOM.

### 6.7 Derived display states

Computed on the fly, stored nowhere:

- **Outdated** — entry `rev` differs from the current body rev.
- **Orphaned** — entry anchor matches no heading in the current body, or names a trace node that no longer exists.

### 6.8 Leaving the exchange folder

A blueprint is expected to be moved or copied out of the folder once it has been groomed and accepted. Nothing in `kalka` depends on it staying. The file remains ordinary Markdown: the frontmatter keys and the review log are harmless to other tools and can be kept or deleted by whatever consumes the file next. `kalka` offers no clean-up step. Where the work is tracked in an issue tracker, *Export* sends the released version to the blueprint's issue and *Archive* takes it out of the lists, while it keeps replicating (10.5).

Copying a team document out of the folder is reading it, which every member may do (21). Removing it from the team is *Delete for the team* (20.6); deleting the file by hand is not, and replication brings it back (14).

### 6.9 Limits

File at most 2 MiB; entry body at most 64 KiB. Larger inputs are rejected with a clear message. The limits hold for replicated documents too. A team log has no size limit; it is exchanged as missing operations, never as one file (20.3).

### 6.10 Trace blocks

A trace block describes the same process at two or more levels of abstraction and links the levels. Its purpose is to make visible where a high-level description (the idea) and its formalization (typically code) stop lining up, because those are the details a reviewer needs and prose loses. The language is defined in full in [`docs/trace.md`](docs/trace.md); this section summarizes it.

It is a fenced code block with the info string `trace v2`, written in the kalka language v2 (6.14, [`docs/language.md`](docs/language.md)). It is part of the body, so only the owner can change it. It is intended to be written by an agent and read by people; `kalka` renders it and never edits it.

````markdown
```trace v2
lane business

step D1: Extract text
  next D2
step D2: Classify the document

lane code

step I1: Is the file larger than 20 MB?
  next I2: no
  ref gh:src/import/size_guard.py#L12-L48
  why the parser runs out of memory on larger files
step I2: Extract and classify in one pass
  realizes D1
  realizes D2
  ref gh:src/import/parse.py#L200-L310
```
````

**Syntax.** A `lane <name>` line opens each lane, most abstract first; `step <id>: <text>` lines under it are its steps, with their attributes indented by two spaces below them, one per line:

| Keyword | Meaning |
|---|---|
| `lane` | 2 to 5 distinct lanes, most abstract first. The core attaches no meaning to the names. |
| `step` | 1 to 40 steps (20 or fewer advised), each `<id>: <text>`: an id `[A-Za-z][A-Za-z0-9_-]*` of at most 32 characters, unique in the block, and one short line. |
| `next` | A step of the same lane that follows, with an optional label saying when the edge is taken (`next C3: queue not empty`). `next` edges never form a cycle. |
| `back` | A step of the same lane the flow returns to (a loop or retry): the step itself or one before it. Same form as `next`. |
| `realizes` | A step in a more abstract lane that this step implements. |
| `covers` | An acceptance criterion of the document the step demonstrates (6.18). |
| `ref` | A reference target `scheme:payload` (the target of a link as in 10.1, without the Markdown), resolved by plugins. |
| `why` | Why the step exists or differs from the lanes above. Expected on a step that realizes nothing. |
| `change` | `new`, `modified` or `removed`: what the plan does to the step. Absent means unchanged. |
| `guards` | An invariant the step keeps true (6.15): the document's own by id, `parent#<id>` for an inherited one, `<domain>#<id>` for a domain node's. |

A keyword starting with `x-` belongs to the author and is ignored silently; any other unknown keyword is ignored with a warning naming the nearest known one (S020).

**Validation** (`kalka check`, and on render) returns errors and warnings, each a diagnostic with a rule id (`S…` for the shared lexis, `T…` for the trace's own rules, 6.17), a file line and the node. A block with errors is shown as a plain code block with the errors beside it; a block with only warnings is drawn with them. Neither ever stops a sync; an error confirms before Publish and Release (6.17).

**Planned change.** A trace with `change` lines describes the state after the plan. A removed step stays in the flow where it is today, drawn struck through; order passes through it, and it takes no part in divergences. A new step is never unverified for lack of a `ref`.

**Divergences** are computed by `kalka` from the graph, never declared by the author, over live (not removed) nodes. "A precedes B" means a path of `next` edges leads from A to B; `back` edges take no part.

| Class | Definition |
|---|---|
| Implementation-only | A node outside the most abstract lane that realizes nothing. |
| Unrealized | A node outside the least abstract lane that nothing realizes. |
| Merged | One node realizes two or more nodes of one lane above. |
| Expanded | One node is realized by two or more nodes of one lane below. |
| Scattered | Another step of the realizers' lane, which realizes something itself, lies between two realizers of one node. |
| Reordered | A precedes B above, but in a lane below a realizer of B precedes a realizer of A and never the other way round; only the minimal pairs are reported. |
| Skipped | A node realizes M directly across a middle lane that has no step between them. |
| Unverified | A live node not marked new with no `ref` in the least abstract lane, or whose `ref`, in any lane, has no plugin or fails to resolve. |
| Stale | A plugin returned a warning for the node's `ref`. |

The last two depend on the reader's plugins and may differ between peers. The exact definitions and a reference algorithm are in `docs/trace.md`, section 5.

A trace shows that a mapping was claimed and that referenced code exists, not that the mapping is correct. The divergence list exists to make each claim quick for a reviewer to check.

Fenced blocks with the info string `mermaid` are rendered as diagrams by a bundled renderer (no network access) and carry no semantics for `kalka`. They remain the place for free-form sketches.

### 6.11 Seeds

A *seed* is a blueprint with `kalka_status: seed`: an idea split off from a blueprint whose scope is growing, kept small until someone takes it up. There is no new document type. Everything that holds for a blueprint holds for a seed: one `owner`, append-only entries, releases, handoff and reassignment as usual.

- Optional keys: `kalka_origin: <kalka_id>[#<entry id>]`, the document it was split from and the entry that triggered it, usually a deviation; `kalka_created` (6.2). A seed's origin is not a place in the plan tree: `kalka_parent` (6.18) is left for the author to set when the seed becomes a plan of its own.
- Body: an H1 title, `## Problem` and `## Why it was split off`. `kalka` writes exactly this when it creates a seed, from the text the user typed. This is the one document `kalka` assembles.

**Budding.** From any blueprint a member holds, as owner or not, *Split off a seed…* (UI) or `kalka seed` (CLI) does the following:

1. If the parent has no `kalka_id`, it gets one first.
2. The seed file is written, with a fresh `kalka_id`, `kalka_owner:` the budding user, `kalka_status: seed`, `kalka_origin` (with `#<entry id>` when started from an entry), and `kalka_created`.
3. A signed `budded` entry is appended to the parent, anchored like the entry it came from, with `re: <origin id>` when there is one. Body: the line `budded: <seed kalka_id>`, then the seed's title.
4. If step 3 fails, the UI offers "add the backlink" (`kalka seed backlink`) and never creates a second seed.
5. The seed is written in the parent's directory, named after its title. A seed of a team document is published to the same team at once (its first release); a seed of a Personal document stays in Personal.

The parent's frontmatter and body are not touched. The backlink reaches the parent's owner through replication, like any entry.

**Pre-backlog.** A view, *Seeds* in the app, per team and for Personal, of every seed held there: title, owner, where it was split off (linked when held, also after the parent was moved, through `moved-from`), and last activity: the later of `created` and the newest entry.

**Promotion.** The owner (in Personal, the local user, as for any document there: 7.1) changes `status` to another value, typically `draft`: *Promote* in the UI or `kalka promote`, which for a team document also releases the change. A team owner promotes someone else's seed by taking it over first (19.6). A non-owner's edit of `status` is a stray plan edit and does not travel (20.4).

### 6.12 Domain documents

A *domain document* holds a team's shared language for one domain: glossary, bounded contexts, invariants, domain events. Agents read it as context when they work on blueprints. A team may have several domains.

`kalka` interprets only its typed blocks: `glossary v2` (6.16) and `invariants v2` (6.15). Its prose, however it is organised, is for people (6.14). The review view of a domain document lists its invariants with the held blueprints that reference each one.

- File name: `<team>-<domain>-domain.md`, created by its author. `kalka` finds domain documents by frontmatter, never by file name.
- Frontmatter: `kalka_kind: domain`, `team`, `domain`, `owner`, `version`. It is always a team document (20.1).
- It is an ordinary document. Its owner releases versions, hands it off, and can have it reassigned by a team owner, exactly as for any document (7). There is no separate domain role.
- **One document per domain name in a team.** Publishing a domain document, or accepting one by transfer, when the team already holds one with the same `kalka_domain:` is refused (`domain-exists`). Two published concurrently are both kept and both flagged "two documents claim this domain"; a reference to that name is shown as ambiguous until a team owner deletes one.
- Members propose changes as signed comments, exactly as in blueprint review.

**Releasing.** The owner edits the body with their own tools and then releases: *Release* in the UI or `kalka release`. `kalka` increments `version`, appends a signed `release` entry whose body lists the incorporated comment ids and a summary, and replicates it. Before releasing, the owner sees, as information, the held blueprints that reference an invariant the new version no longer has (`I040`) and those that use a word the new version makes an alias (`G030`). Edits that are not released do not travel. Comments are never removed; the UI folds those a release incorporated under "incorporated in v<N>".

**Snapshots.** Each released version, made locally or received, is kept as `<team>-<domain>-domain@v<N>.md` beside the live document, so pinned references keep resolving. The index ignores files matching `*@v<N>.md` (14). A member who joins later holds the versions released since they joined, not the older ones. Snapshots are named after the live file, wherever it is in the team folder; a snapshot is written once and never changed. The review view shows a kept version read only, and comments always go to the live document. A transfer does not carry snapshots, and `version` restarts at 1 in the destination (20.7).

A blueprint's reference to a version that is not kept here is shown as such ("not kept here"), as is a reference to a domain of which no document is held.

**References from blueprints.** A blueprint names the domain version it was written against with `kalka_domain: <team>/<domain>@v<N>`. When a newer version of that domain is held locally, the review view shows a dismissible "newer domain version available (v<M>)" indicator. `kalka check` reports it as information (`R003`) only when the newer version changes something the document uses: a term it uses, an invariant it names, or the parent pin of a node on its path. It never blocks anything. A reference to a team name held twice (19.2) is shown as ambiguous.

**The domain tree.** Domains form a tree per team: a root domain, its subdomains, theirs. A domain node's name is its path, `library.reservations`, and starts with its parent's name followed by a dot (`D001`). A subdomain names its parent's version:

```yaml
kalka_kind: domain
kalka_domain: library.reservations
kalka_parent: team-lending/library@v2
```

- **Pins, not bumps.** A new version of a parent changes nothing below it. The child shows "parent newer" (`D030`, information) until its owner moves the pin and releases. A pin to a version not kept here is `D002`; pins that lead back are `D003`.
- **A path, not a node.** A blueprint pinning `library.reservations@v1` rests on the whole path to the root at the pinned versions: it is linked against the terms of every node on it, and may name the invariants of every node on it (`<domain>#<id>`). A domain off the path is `I020`.
- **Extend only.** A subdomain adds terms and invariants; it may not redefine a term, form or alias defined above it (`D010`). To narrow an inherited term, an invariant says so with `refines <term>`; the term then shows the narrowing on hover. An invariant that speaks only of inherited terms without saying which it refines is `D011`; `refines` naming no term on the path is `D012`. When two branches need different meanings of one word, use two terms, or move the term down into each branch.
- **Siblings.** Two children of one node defining a term the same way is information (`D031`): lift it to their common parent.
- **Several nodes.** A plan node may name several domain nodes, `kalka_domain: [team/library.reservations@v1, team/library.fines@v1]`. Where they pin different versions of a common ancestor, that is `D021`. Where two of them define a word differently, a use of it must be qualified, `library.fines#hold` (`D020`).

### 6.13 Blueprint reference and hints

`kalka` ships `BLUEPRINT-REFERENCE.md`, embedded in the binary and versioned as `kalka-blueprint@v<N>` (v2 now, written for the kalka language v2). `kalka reference export` writes it out. It is written to be dropped in unchanged as an agent skill or instruction file: it opens with frontmatter holding `name` and `description`.

It tells agents to run `kalka check --json` before handing a document over, and lists the rules of 6.17, generated from the registry. It describes what a blueprint useful to agents usually contains, as guidance, never as a requirement. Each section comes with a short reason it helps agents, and examples use invented everyday services (parcel logistics, library lending):

1. **Domain reference**: which domain document and version applies (`kalka_domain:`, 6.12).
2. **Scope**: the bounded contexts and modules expected to be touched. Work beyond the scope becomes a seed (6.11), not a larger change.
3. **Verification plan**: an `invariants v2` block (6.15) whose invariants say how each is verified (`verified-by`), with edge cases and what a human reviewer must specifically check. Written at planning time, not at review time. Trace steps name the invariants they keep with `guards`.
4. **Critical sections**: where the design deliberately departs from the clean model, for example for performance, and why. Agents must not "clean them up"; human review is required there.
5. **Operations**: failure modes, what to observe, how to roll back.
6. **Deviation log**: the `deviation` entries of the review log (6.5), recorded with `kalka comment --kind deviation`, never a body section.

It also describes plan nodes, goals and acceptance criteria (6.18), the domain tree (6.12), the language v2 (6.14), and tells agents to read `kalka context <file>` before starting.

**Hints** are opt-in. Only a document whose frontmatter says `kalka_reference: kalka-blueprint@v2` gets them: one hint per recommended body section (1 to 5) that has no heading with the matching slug (at any level; a `-N` duplicate suffix is ignored), for example "no verification plan". A frontmatter `kalka_domain:` key of the form `<team>/<domain>@v<N>` also satisfies the domain-reference hint. The hints are rules `H001` (no domain reference), `H002` (scope), `H003` (verification plan, also satisfied by an invariants block), `H004` (critical sections) and `H005` (operations); an unknown version gives `H010`. There is no hint for the deviation log, since a plan not yet implemented has none. Hints are information only: they never change the exit code of `kalka check`, never stop a sync, and can be dismissed per document in the UI. Dismissal is kept in the browser, not in the file or the config. A document naming an unknown reference version gets one information line and no hints. Every other document gets no hints at all.

### 6.14 Conventions live in typed blocks

`kalka` takes meaning only from the frontmatter and from typed fenced blocks: `trace v2` (6.10), `invariants v2` (6.15), `glossary v2` (6.16) and `goals v2` (6.18). It never infers meaning from prose or from headings, with two exceptions: headings give comment anchors (6.5), and the opt-in hints of 6.13 look for headings by slug. Everything else in the body is for people and is rendered, never interpreted.

**The kalka language v2.** The typed blocks share one line-oriented language, defined in full in [`docs/language.md`](docs/language.md) (lexis, EBNF, the keyword table, references, ids and anchors):

- A header line starts in column 0: an item `<keyword> <name>: <text>`, a section `<keyword> <text>`, or a declaration `<keyword> <ref>`. An attribute line is indented by exactly two spaces, `<keyword> <value>`, and belongs to the item above. There is no third level.
- One line is one value; a list is a repeated keyword. No line continuation, no quoting, no escaping: free text may hold commas, colons and quotes.
- Keywords are lower case, singular, whole words, only at the start of a line, never followed by a colon. No tabs and no comments.
- References are `<id>` within the document, `parent#<id>` for the parent plan node (6.18) and `<domain>#<id>` for a domain node (6.12).
- A keyword starting with `x-` belongs to the author and is ignored silently; another unknown keyword is ignored with a warning naming the nearest known one.
- Drift into familiar formats, Gherkin (`Scenario:`, `Given`, `And`) or YAML (`- id:`, `lanes:`), is an error naming the v2 form to write.

The fence names the block and the language version (`invariants v2`); a block with no version is read as v2 with a warning, and any other version is shown as code: kalka is not yet at 1.0 and keeps no compatibility between its versions. The lexical rules are the `S` group (6.17), shared by every block; each block's own rules follow. Each lexical diagnostic says what to write instead, and most carry the replacement line as a fix.

What `kalka` finds in a document is reported as **diagnostics** (6.17), the same from `kalka check` and in the app.

### 6.15 Invariants

An *invariant* is a property that must hold whatever the code does: "a parcel is never handed to two couriers at once". Invariants are first-class items: they have ids, take comments, and are checked against the trace that should guard them.

````markdown
```invariants v2
affects parcel-delivery#I3

invariant I1: A parcel has at most three redelivery attempts
  why the depot keeps a parcel for seven days; three attempts fit
  verified-by gh:test/redelivery_test.py#L40-L90
  verified-by Reviewer: run the migration on a copy of last week's attempts
  status new
```
````

The block is allowed in domain documents and in blueprints, anywhere in the body; a document may hold several, read as one list.

| Keyword | Level | Meaning |
|---|---|---|
| `invariant <id>: <statement>` | item | One invariant, 1 to 100 per document. The id `[A-Za-z][A-Za-z0-9_-]*`, at most 32 characters, unique in the document (`I1`, `I2`… is the advised form); the statement one sentence saying what holds, at most 300 characters advised. |
| `affects <ref>` | declaration | An invariant of another document the plan touches: a domain node's `<domain>#<id>`, or the parent plan node's `parent#<id>`. Blueprints only. |
| `why` | attribute | Why it must hold. |
| `verified-by` | attribute, repeatable | How it is checked: a reference target `scheme:payload` resolved by plugins (10.1), or free text such as a manual check (`Reviewer: …`). |
| `refines` | attribute, repeatable | An inherited term the invariant narrows (6.12). |
| `status` | attribute, blueprints only | `new`, `changed` or `removed`: what the plan does to it, as `change` does for a trace step. Absent means unchanged. |

**References.** An invariant of the same document is named by its id (`I1`). An invariant of a domain node is named `<domain>#<id>` (`parcel-delivery#I3`) and resolves against the node of that name on the path of the domain version the blueprint references with `kalka_domain: <team>/<domain>@v<N>` (6.12), read from that version's snapshot, never from another version. An invariant of the parent plan node, or of a plan node above it, is named `parent#<id>` (6.18). References appear in `affects` and in a trace step's `guards` (6.10); a reference anywhere in prose is only text.

**Anchors.** Each invariant is a comment anchor, `§<slug>#<id>`, where the slug is the heading above its block, as for trace steps. An invariant id that is also a trace node id under the same heading is a warning: comments on it would be ambiguous.

**Computed checks** (rules in 6.17):

- a reference that does not resolve (an unknown id, a domain other than the one `kalka_domain:` names, or no `kalka_domain:` at all) is an error;
- an invariant the document introduces (not `removed`) or affects that no trace node guards is a warning, only when the document has a valid trace block;
- an invariant with no `verified-by` is a warning (not for `removed` ones);
- a domain invariant that a blueprint references and that a newer held version of the domain no longer has is information, for readers of the blueprint and for the domain's owner when releasing (11.1);
- when the referenced domain version is not held, the rules that need it say so once, as information ("can't check"), and are not run.

### 6.16 Glossary and linking

A domain document defines its terms in typed blocks. So may a blueprint that names no domain (no `kalka_domain:`): a sketch or a small piece of work that has no domain document yet keeps its words beside the plan, and they work there as a domain's do in its blueprints.

````markdown
```glossary v2
term parcel: One item the depot accepts, labelled and tracked on its own
  form parcels
  alias package
  alias consignment
  see delivery attempt
  context delivery

term delivery attempt: One visit by a courier to hand a parcel over
  form delivery attempts
```
````

| Keyword | Level | Meaning |
|---|---|---|
| `term <term>: <definition>` | item | The canonical term, at most 60 characters, which may hold spaces and ends at the first `: `; and what it means here, at most 400 characters advised. |
| `form` | attribute, repeatable | Another form of the term itself (plural, other inflections); it links like the term. |
| `alias` | attribute, repeatable | A word people also use for it. Using one is a warning naming the term. |
| `see` | attribute, repeatable | A related term of the same glossary, or of a domain node above. |
| `context` | attribute | The bounded context it belongs to. |

The block is a list of 1 to 500 entries. Several glossary blocks in one document are read as one glossary; the same term twice, or a term, form or alias that names two entries, is an error. A glossary block in a blueprint that names a domain is a warning and is shown as code: that blueprint is linked against its domain's glossary, never two at once.

**Linking.** A blueprint's text is linked against the glossary of **the domain version it references**, read from that version's snapshot, together with the glossaries of every node above it on its domain path at their pinned versions (6.12). When that version is not kept here the view says "glossary v3 not kept here" and nothing is linked or checked; another version is never used instead. A domain document, and a blueprint that names no domain, link their text against their own glossary.

- Matching is case-insensitive (Unicode lower case), on whole words (the characters on either side are not letters or digits), against each entry's term, forms and aliases. A space in a term matches any run of white space, line breaks included.
- Text is matched within one run of text: body paragraphs, headings, list items, table cells and link text, a trace step's text, an invariant's statement, and a goal's or criterion's text and steps. A term interrupted by markup (`par*cel*`) does not match.
- Nothing is matched in code spans, code blocks other than trace blocks, link targets, raw HTML, the frontmatter or the review log.
- At each position the longest match wins; matches never overlap.

The app underlines a linked term subtly and shows its definition and context on hover or keyboard focus; a side panel lists the terms the document uses with their counts. An alias is underlined as a warning and its diagnostic offers the term as a replacement.

Not in this version: flagging words that are not in the glossary. A possible later form is an inline marker the author writes, such as `[[term]]`, so that only marked words are checked (18.2).

### 6.17 Diagnostics and rules

Every finding of `kalka check` and of the app is a diagnostic: a file and position (line, and where known column and end, columns counted in characters from 1), a **rule id**, a severity (`error`, `warning`, `info`) and a message saying what to write instead. A diagnostic may carry a `fix`: replacement text and the range it replaces. Fixes are suggestions; `kalka` never rewrites a file to apply them.

**One registry.** The rules are declared once, in Go (`internal/rules`), with their id, severity, title, meaning, why the rule exists and a wrong and a right example. The checker emits only registered ids; the app shows the registry's title and meaning beside a diagnostic; `kalka check --explain <id>` prints an entry; [`docs/rules.md`](docs/rules.md) and the rule list in `BLUEPRINT-REFERENCE.md` are generated from it. A test fails when a rule is emitted but not registered, registered but never emitted, or the generated lists are out of date.

**Ids** are a letter and three digits, stable across versions:

| Group | Covers |
|---|---|
| `F` | Frontmatter: identity, owner, domain keys, the issue (`F020`–`F022`). |
| `B` | Structure and the review log: fences, entries, anchors. |
| `S` | The lexis of the kalka language v2, shared by every typed block (6.14, `docs/language.md`). |
| `T` | Trace blocks (6.10, `docs/trace.md`). |
| `G` | Glossary blocks and term use (6.16). |
| `I` | Invariants blocks and references (6.15). |
| `P` | Goals blocks and the plan graph (6.18). |
| `D` | The domain tree (6.12). |
| `R` | References to other documents: domain versions, and with `--with-plugins` reference targets. |
| `H` | The opt-in hints of 6.13. |

**One set of rules.** `kalka check` runs every rule this `kalka` knows. kalka is pre-1.0: a release may add, change or remove rules, and there is no switch to run an earlier release's rules; the `--json` output names the `kalka` version that produced it.

**The issue key needs no plugin.** `kalka_issue` that is not a `<scheme>:<payload>` reference is a warning (`F020`); a scheme no plugin shipped with kalka handles is information (`F021`), decided from a fixed list in the code, never from what is installed; a malformed `kalka_archived` is a warning (`F022`). None of these asks a tracker.

**Determinism.** The checker reads only the files it is given and the domain versions and plan nodes it is pointed at (12.4). It uses no network, no daemon and no configuration beyond finding the local folder, and reads no keys: the same input and `kalka` version give byte-identical `--json` output. Plugins run only with `--with-plugins`.

**Severity and the network.** Diagnostics never stop a sync. A few structural errors (`F001`–`F004`, `B001`, `B012`, `B013`) make a file one that cannot be signed or merged safely: publishing, releasing and transfers refuse such a file, as before. Any other error is shown before Publish or Release with a confirmation, and never blocks.

### 6.18 Plan graph

Plans form a tree: a *plan node* (an epic, say) holds goals and invariants; the blueprints below it hold acceptance criteria and are what gets implemented. There are no level names, only one edge and any depth, and all of it is optional: a single blueprint may hold goals, criteria, invariants and a trace together.

- **The edge.** A document names its parent plan node with `kalka_parent: <the parent's kalka_id>` (6.2). In the text the parent is always `parent`: `parent#G1`, `parent#E1`. A parent, or a node further up, that is not held here is `P010`, and the invariants inherited from above the gap are not checked against; when the checker read only the files it was given, a parent or children not among them are not looked for, and `P011` says so once instead of `P010`, `P030` or `P031`; a `kalka_parent` chain that leads back is `P012`, which says where the loop is. Plan nodes are looked up in the document's scope (14): a parent elsewhere is found when none is in the scope, but only children in the scope claim its goals, so a Personal draft does not change a team plan's states. `parent#<id>` in a document with no `kalka_parent` is `P021`.
- **Goals and criteria** are a `goals v2` block (6.14):

  ```goals v2
  goal G1: The queue head learns that a copy is waiting for them

  criterion AC1: The queue head is notified exactly once
    realizes G1
    given a copy whose title has two open reservations
    when the copy is returned
    then the queue head gets one email
  ```

  A goal at a middle level realizes goals of its own parent, `realizes parent#G1` (`P004` for any other form); a criterion realizes a goal of its parent or of its own document. A criterion's steps are given, then when, then then (`P002`); one with no `then` is `P003`. An id twice is `P001`. A goal of the grandparent, named as if it were the parent's, is `P020`, and the message names the goal of the parent to realize instead.
- **States are computed, never written.** A criterion is *open* until its document is closed, then *delivered*. A goal is *unclaimed* when nothing realizes it, *claimed* when something does, *ready* when everything claiming it is delivered or ready, and *delivered* when it is ready and its own document is closed. A leaf is delivered by its owner's explicit confirmation: *Close as delivered* in the UI or `kalka close <file>`, which writes `kalka_status: closed` and, for a team document, releases it at once, so, as with an archive (10.5), only the owner closes it and not over edits that are not released; a failed release is reported. A domain document is not closed; *Reopen* (`kalka close --reopen`) takes it back to open.
- **Coverage.** An unclaimed goal is a warning while its document is open (`P030`) and an error once it is closed (`P031`), as is a closed document whose goals are claimed but not delivered. `kalka_breakdown: adopted` says the children came from outside kalka (the stories existed before the epic was written here): coverage is then a finding to decide on (`P032`, information). `kalka_breakdown: decomposed` says the split was made in kalka.
- **Inherited invariants.** The invariants of every plan node above a document hold in it. It may add its own; it may not loosen or remove an inherited one, so reusing an inherited id for an invariant of its own is `P023`. It names an inherited invariant with `affects parent#<id>` and `guards parent#<id>`, which resolve on the whole plan path (`I020` when none has it).
- **One chain.** A business step names the criterion it demonstrates with `covers` (6.10; `P022` for a name that is not a criterion of the document). The chain then runs from the epic's goal through the criterion and the business steps to code with a `ref`.
- **Context for agents.** `kalka context <file>` (12.1), and *Context for an agent* in the document view, print the document's flattened context: the plan path from the root to the document, with each level's goals and invariants, and the domain path, with each node's terms (and the invariants that narrow them) and invariants, at the pinned versions. Siblings are never included. It is what an agent reads before working on the document.
- **The lens.** Context and the *Graph* tab (11.1) select their documents the same way, by one rule: the plan path from the root to the document (none for a domain document), the domain path at the versions pinned, and the document's children (the plan nodes naming it as their parent, or a domain node's subdomains), never siblings. Context leaves the children out. `kalka graph <file>` (12.1) prints the lens as a graph of documents and their items, with what needs attention.

---

## 7. Ownership

Every document has one owner. In a team, ownership is decided by the document's release chain together with the team log (20.4): the `kalka_owner:` line is what people read, the chain is the authority.

### 7.1 Owner

- The owner of a team document is the member the head of its release chain names, or the new owner an effective reassignment names (7.3). Only the owner changes the plan and signs the next release.
- In Personal, the owner is the local user, whatever `kalka_owner:` says; nothing there leaves the machine.
- The UI shows non-owners a read-only body. Edits by a non-owner on their own disk are not blocked; they never travel, and the next replication that brings a release replaces them, keeping a copy (7.5).

### 7.2 Handoff

*Change owner…* in the UI, or `kalka handoff <file> <email>`, by the owner:

1. The recipient must be a current member of the document's team in the local replay. A Personal document cannot be handed off; publish it first.
2. `kalka` writes `kalka_owner: <recipient>` and releases the next version, naming the recipient in the release (20.4). This is the whole handoff: there is no prompt, no connection and no acknowledgement. It works offline and while paused, and replicates when it can.
3. The recipient gets a notice and a waiting item (11.3) when the release reaches them, and can hand it back the same way.

The release that changes `kalka_owner:` is the record; the UI renders it as "handed off to <recipient>".

### 7.3 Reassignment by a team owner

A team owner can make any member the owner of any document of the team, without the current owner's consent: *Change owner…* (or *Choose owner…* when there is none) in the UI, or `kalka reassign <file> <email>`.

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
4. Each side checks that the peer ID it dialed or was dialed from is the key ID of the presented key (9.2), so a squatter on someone's key ID fails at once. This needs only the `hello`, so it is checked before sending `auth`.
5. The responder then checks the key against its team logs as in 8.2; the initiator dialed a key it took from a team log or an invite.

Any failure closes the connection. This makes a malicious or compromised rendezvous server unable to sit in the middle.

---

## 9. Rendezvous and transport

### 9.1 Signaling protocol

The client must speak the PeerJS server WebSocket protocol so that one client works against the public PeerJS cloud, stock `peerjs-server` and `kalka-rendezvous`. Message types include `OPEN`, `OFFER`, `ANSWER`, `CANDIDATE`, `HEARTBEAT`, `LEAVE`, `EXPIRE`, `ERROR`, `ID-TAKEN`.

- The rendezvous can be switched while kalka runs (11.1, 13, 18.2 decisions 21–23): the registration closes, the cached ICE servers are dropped and kalka registers on the new server under the same key id. Status and the public-cloud notice follow at once. An installation still uses one server.
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

TURN over TCP/TLS on 443 must be supported; many managed networks block UDP.

### 9.5 Wire protocol (data channel)

One reliable, ordered data channel labelled `kalka/1` per session. Messages are JSON objects, each sent as a 4-byte big-endian length followed by the payload; writes to the channel are chunked to at most 16 KiB so no single SCTP message exceeds common limits.

| Message | Fields | Notes |
|---|---|---|
| `hello` | `proto`, `proto_min`, `features`, `email`, `pubkey`, `nonce`, optional `invite` | Both directions, first. `invite` carries a join proof (19.7). |
| `auth` | `sig` | Both directions, second. |
| `team_sync` | `team`, `heads`, `digest` | Opens a replication session (20.3): the log heads and one digest over the inventory. |
| `team_log` | `team`, `ops` | Log operations the other side is missing. Also sent once by an owner who admits a joiner (19.7 step 5). |
| `team_inv` | `team`, `items` | Inventory lines (20.3), pending transfers included. |
| `team_want` | `team`, `ids` | The documents the sender wants; each follows as a `team_doc`. |
| `team_doc` | `team`, `kalka_id`, `filename`, `content` | One wanted document; empty `content` means unavailable. |
| `bye` | optional `reason`, `detail` | Either side. The reasons are listed below. |
| `keepalive` | — | Sent at least every 10 s by a side busy applying what it received, so the other does not take it for silence. |

**Versions.** The channel label `kalka/1` never changes; versions live in `hello`. Each side announces the highest protocol version it speaks (`proto`) and the lowest (`proto_min`, equal to `proto` when absent). The session runs at the lower of the two `proto` values; if that is below either side's `proto_min`, the side that sees it sends `bye` with reason `proto` and `detail` `<min>-<max>` (its own range) and both show which side needs to update. `features` lists optional capabilities by name; names a side does not know are ignored. This version of `kalka` speaks protocol 1 only and announces no features.

Unknown message types are ignored, also as the first message after `auth`.

| `bye` reason | Meaning |
|---|---|
| `done` | The session finished. |
| `not-member` | The responder does not count the caller as a member of that team (also its answer for a team it does not hold). |
| `invalid` | The peer broke the protocol (an answer for another team, too many wanted documents, a join message that does not decode); `detail` says how. |
| `proto` | No protocol version both sides speak; `detail` is the sender's range, `<min>-<max>`. |
| `joined` | A join completed (19.7). |
| `invite-unknown`, `invite-used`, `invite-expired`, `invite-proof`, `invite-team`, `invite-email`, `invite-not-owner`, `invite-key-clash` | A join was refused (19.7 step 6). |

**Limits.** A message is at most 16 MiB; a document at most 2 MiB and an entry body at most 64 KiB (6.9); a team log line at most 8 KiB. One `team_want` names at most 200 documents; a side that wants more asks for the rest in another session, which starts at once. A side refuses a `team_want` of more than 1,000 as `invalid`, and a caller it does not count as a member may send at most 64 log operations. The 30 s of silence that ends a session count from the last byte received, not from the start of a message, so a large document arrives over a slow link as long as it keeps moving. A session closes when both sides are done or after 30 s of silence; the initiator waits for the responder's close as long as keepalives arrive. Nothing on the wire asks a person anything.

The responder dispatches on the first message after `auth`: `team_sync` starts a session; a `hello` with an invite proof is a join (19.7); anything else gets `bye` `not-member`.

### 9.6 `kalka-rendezvous`

A separate binary for a team or organisation to host, with no client code, no configuration directory and no UI:

- PeerJS-compatible signaling, state in memory only.
- Embedded TURN relay (`pion/turn`) with time-limited credentials exposed at `/kalka/ice`.
- `--allow-origin` / reverse-proxy friendly, so SSO or network policy can sit in front. No built-in user database.
- TLS via flags or behind a proxy.
- **Key-ID proof.** A client registering a key ID (9.2) adds the query parameter `kproof = base64url(pubkey) "." unix-seconds "." base64url(sig)`, where `sig` signs `"kalka-keyid-v1\n" + id + "\n" + unix-seconds`. The server checks that the ID matches the key, the signature verifies and the timestamp is within a few minutes, and refuses the registration otherwise. Other servers ignore the parameter.

### 9.7 Public cloud mode

`rendezvous: peerjs-cloud` is for demos. The UI must show a permanent notice in this mode: the service is community-run with no availability guarantee, IDs can be claimed by anyone who knows a public key, which keeps its owner offline, and only the team logs protect the exchange. Not for sensitive blueprints.

---

## 10. Plugins

### 10.1 References

A reference is a Markdown link whose target has a non-web scheme: `[label](scheme:payload)`. Scheme matches `[a-z][a-z0-9+.-]*`. The schemes `http`, `https`, `mailto` and `file` are never dispatched to plugins. A reference with no installed plugin renders as plain text with its label.

Blueprints carry references only. Resolved content is never written into the file and never sent to a peer; each peer resolves with their own plugins and credentials.

**Context from the frontmatter.** Where a reference needs context that is not in the reference itself (which repository, at which commit; which site), the plugin reads it from the frontmatter, which every request carries. A blueprint that spans several such places names each one in a map in the frontmatter, and a reference picks one with a `<name>:` prefix on its payload; a reference without a prefix uses the plugin's default keys. `gh` does this with `repos` (10.4). Plugins for other services should follow the same convention, so that one blueprint can reference several of them.

### 10.2 Discovery

Executable named `kalka-plugin-<scheme>` (plus `.exe`, `.cmd` on Windows), searched in `<config>/plugins`, then the folder of the running executable, then `PATH`.

### 10.3 Protocol

One process per request. Request as one JSON object on stdin, response as one JSON object on stdout, exit code 0. Stderr is logged. Arguments from blueprints are never placed on the command line. The protocol is version 2; a plugin answering `describe` with another version is not used, and the reason is logged.

```json
{ "protocol": 2, "op": "describe" }
→ { "protocol": 2, "name": "github", "version": "0.2.0", "schemes": ["gh"],
    "capabilities": { "resolve": true } }

{ "protocol": 2, "op": "resolve",
  "context": { "frontmatter": { "repo": "org/doc-service", "base_sha": "3f9c2a1" } },
  "refs": [ { "ref": "gh:src/import/parse.py#L120-L180", "payload": "src/import/parse.py#L120-L180" } ] }
→ { "protocol": 2, "results": [
    { "ref": "gh:src/import/parse.py#L120-L180", "ok": true,
      "title": "src/import/parse.py · L120–180",
      "url": "https://github.com/org/doc-service/blob/3f9c2a1/src/import/parse.py#L120-L180",
      "status": "",
      "snippet": { "lang": "python", "start_line": 120, "text": "…" },
      "warnings": [ { "text": "File changed in 7 commits since base_sha" } ] } ] }
```

Failed items return `{ "ref": …, "ok": false, "error": "…" }`. All refs of one scheme in a blueprint are batched into one call. Results are cached in memory for 5 minutes, keyed by ref and frontmatter hash.

The core renders `title`, `url` (http/https only), `status`, `snippet` and `warnings` generically. It must not branch on plugin name or scheme.

**Capabilities.** `describe` declares what a plugin does; the core offers a feature only when the plugin of the scheme declares it, and drops what it does not know:

```json
"capabilities": {
  "resolve": true,
  "issue": {
    "meta": true,
    "hierarchy": true,
    "fields": ["title", "type", "status", "assignee", "labels", "sprint", "parents", "updated"],
    "export": { "attachment": true, "comment": true } } }
```

`resolve` enables references (10.1); `issue.meta` the issue's metadata (`issue_meta`); `issue.hierarchy` its parents and grouping by them; `issue.fields` the fields the core keeps from a result (others are dropped); `issue.export` the export (`export`), with what it writes. A plugin's description is cached until its executable changes, and re-read at most once a minute after a failure.

**`issue_meta`** takes the payloads of one scheme in one batch and returns one result per issue, in order:

```json
{ "protocol": 2, "op": "issue_meta", "issues": ["DEV-123", "DEV-140"] }
→ { "protocol": 2, "results": [
    { "issue": "DEV-123", "ok": true, "title": "Redelivery after a failed attempt",
      "url": "https://example.atlassian.net/browse/DEV-123", "type": "Story",
      "status": { "name": "In Progress", "category": "in_progress" },
      "assignee": "Ewa Nowak", "labels": ["delivery"], "sprint": "Sprint 41",
      "parents": [ { "issue": "DEV-90", "title": "Failed-delivery handling", "type": "Epic", "url": "…" } ],
      "updated": "2026-11-19T14:02:00Z" },
    { "issue": "DEV-140", "ok": false, "error": "not found or no permission" } ] }
```

`status.category` is normalized to `todo`, `in_progress` or `done` (an unknown value reads as `todo`), so grouping never knows a tracker's workflow. `parents` is nearest first, at most 5, and kept only from a plugin that declares `hierarchy`. A response with a top-level `"error"` fails the whole batch (not logged in, the tracker unreachable): the core keeps what it had. An item with `ok: false` is the tracker's answer and replaces what was cached.

**`export`** sends a released version to an issue:

```json
{ "protocol": 2, "op": "export", "issue": "DEV-123",
  "document": { "title": "Redelivery", "version": 3, "file_name": "redelivery-v3.md", "markdown": "<the whole file>" },
  "summary_markdown": "<the released plan and the accepted deviations>",
  "previous": { "attachment_id": "10046", "comment_id": "20311" } }
→ { "protocol": 2, "ok": true, "url": "https://example.atlassian.net/browse/DEV-123?focusedCommentId=20311",
    "attachment_id": "10047", "comment_id": "20311" }
```

It is idempotent per issue: with `previous`, the plugin replaces that attachment and edits that comment, and writes new ones where they are gone. Ids are at most 64 characters of `[A-Za-z0-9._:-]`. A failure is `ok: false` with `error`, and must leave the issue as it was where the tracker allows.

**Timeouts:** 10 s for `describe`, `resolve` and `issue_meta`; 60 s for `export`.

### 10.4 Reference plugin: `kalka-plugin-gh`

Separate binary in the same repository, shipped alongside `kalka`. Uses the user's `gh` CLI authentication (`gh api`); no token handling of its own.

- Frontmatter keys it reads: `repo`, `base_sha`, optional `host` (GitHub Enterprise), the default repository; and `repos`, a map from names to entries with the same three keys, for further repositories. A name is `[a-z][a-z0-9_-]{0,31}`.
- `gh:[<name>:]<path>[#L<a>[-L<b>]]` → permalink at that repository's `base_sha` and snippet via the contents API. Without a name, the default repository; with one, the entry of `repos`, and the result's title starts with the name. A path segment cannot contain `:`, so a prefix is never mistaken for a path.
- A reference whose repository is missing or invalid fails on its own, with the frontmatter key at fault (`repos.api.base_sha`); the other references resolve.
- Staleness: one compare call `base_sha...<default branch>` per repository and batch; any referenced path among that repository's changed files gets a warning.
- If `base_sha` is unknown to the remote (unpushed spike), every result for that repository carries a warning saying so.

```yaml
repo: org/doc-service
base_sha: 3f9c2a1
repos:
  web: {repo: org/web-app, base_sha: 8d0e5b2}
  api: {repo: org/api, base_sha: 9b07e44, host: github.example.org}
```

`[parser](gh:src/import/parse.py#L120-L180)` reads `org/doc-service`; `[form](gh:web:src/upload/Form.tsx#L10-L60)` reads `org/web-app` at `8d0e5b2`.
- Paths and line numbers from the payload are validated and passed as API parameters, never through a shell.
- It declares `resolve` only.

### 10.5 Issue trackers

A blueprint names the one issue it addresses in its frontmatter: `kalka_issue: jira:DEV-123`. The key is the author's, written by hand or by an agent; kalka never writes it. References to issues in the body (`[the story](jira:DEV-123)`) keep working through `resolve`, independent of the key.

**Metadata is local.** What the tracker says about the issue (title, status, parents and the declared fields) is fetched by the reader's own plugin, with the reader's own credentials, and kept in a cache per installation: `issue-cache.json` in the config directory, keyed by scheme and issue, owner-only. An entry is fresh for 15 minutes; a failed fetch is retried after a minute. The page never waits for a plugin: it gets what is cached at once, fetches in the background, one batch per scheme, and is told on the event stream (`issues`) when the answer arrives. When the plugin fails or the machine is offline, the cached metadata is shown with "as of <time>". The cache is never written into a blueprint, into the shared configuration, or replicated. Without a plugin for the scheme, or without `meta`, only the raw key is shown.

**Export.** The owner of a team document, or a team owner, exports the released version to its issue, when the plan on disk is the latest release (no unreleased edits) and the scheme's plugin declares `export`. It sends the whole file as the attachment, `<file name>-v<N>.md`, and as the summary the released plan with its accepted deviations (the deviation entries a valid release took up) and a line "Exported from kalka: <title>, version N, on <date> by <email>". After the plugin succeeds, kalka appends an `exported` entry, signed by the exporter, which replicates like any entry:

```
### exported · ewa@parcels.example · 2026-11-20T10:12:00Z · § · rev:… · id:… · sig:…
exported: jira:DEV-123 · v3
url: https://example.atlassian.net/browse/DEV-123?focusedCommentId=20311
attachment: 10047
comment: 20311
```

The newest `exported` entry for the issue gives the next export's `previous`, so exporting again (by anyone allowed) replaces the same attachment and updates the same comment, and writes a new entry. A failed export changes nothing in the file. A team owner exports someone else's document without taking it over: an export appends an entry and does not change the plan. Domain documents are not exported.

**Archive.** After an export the page offers *Archive*. Archiving writes `kalka_archived: <time>` into the frontmatter, a plan edit, so only the owner archives, and for a team document it is released at once as the next version; a team owner takes the document over first (7.4). It is refused while the plan has unreleased edits, which would otherwise ride along. *Unarchive* removes the key the same way. `kalka_status` is left alone, so an archived seed is still a seed. An archived document keeps replicating and can be opened, commented and exported; the lists and the rail hide it unless *Show archived* is on.

**`kalka-plugin-jira`.** A separate binary shipped beside `kalka` and `kalka-plugin-gh`, for Jira Cloud (REST API v3, an email and an API token) and Jira Data Center (REST API v2, a personal access token). `kalka-plugin-jira login` asks for the site and the token, checks them against the site, and stores the token in the system's credential store (macOS Keychain, Windows Credential Manager, the Secret Service through `secret-tool`), or, where there is none, in the plugin's own `config.json`, readable by the user only. Credentials are never in a blueprint and never in kalka's configuration. One site per user. Keys are validated as `[A-Z][A-Z0-9_]+-<number>` and sent as API parameters, never through a shell. Metadata is read in batches (one JQL search per 100 keys); parents follow the `parent` field up to 5 levels, so levels above Epic come from the site's hierarchy (Jira Cloud plans or premium hierarchy levels); on Data Center only sub-tasks have a parent. Rate limits (429, 503 with `Retry-After`) are waited out within the request's time, then reported. The comment is written in Atlassian Document Format on Cloud and as Markdown text on Data Center. [`docs/jira.md`](docs/jira.md) has the setup.

---

## 11. Web UI

Served from the binary (`embed.FS`). The frontend is prebuilt; end users need no Node toolchain. It is shown in the desktop window (22) or in a browser tab (`kalka open`); it is the same page from the same localhost address either way, a pure view over the local API (13). It is the primary interface: every workflow has a control in it (12).

### 11.1 Views

The window follows the system's light or dark appearance; Mermaid diagrams follow it too. Banners that appear (paused, offline, lost connection) are announced to screen readers.

**The team switcher** sits at the top of the window: the current scope's name, and a dropdown listing Personal first, then every team by name, each with a badge counting its waiting items (11.3). It also lists each join in progress ("docs-site · joining, waiting for kasia") or failed ("could not join"), each a link to its progress, then *Create team…* and *Join with an invite…*. Up and Down move through it; Escape closes it. *Create team…* takes any text and shows the name it becomes (19.2), then opens the new team's Team page. Two teams with one name show a short hash.

**The header** also carries the sync state at all times, as `kalka status` and the tray do: "Synced 11:02 · 2 of 6 teammates online", "Not synced yet", "Paused" or "Offline". It opens a panel with each team's last sync and *Sync now*. The selected scope is part of the route (`#/t/<team>/…`, `personal` for Personal) and is remembered in the page's storage. Opening a document by link switches to the document's scope.

Every view below except Needs you shows the selected scope. In Personal the Team and Domains views are hidden.

**Top bar, document bar and rail.** The top bar is the same on every screen, a `<nav>` named "Sections": the team switcher, *Needs you*, *Blueprints*, *Seeds*, *Domains* and *Team* (the last two not in Personal), the sync state and *Settings*. While a document is open, the section it belongs to stays highlighted. Under it, only in the document, trace and graph views, the document bar (`<nav>` "Document") holds a breadcrumb, the section's name linking to its list, then the plan nodes from the root to the document's parent (6.18), each by its issue key and title and linked when held here ("parent not here" when it is not), then the document's title after its issue key, and on the right the tabs *Document*, *Trace* and *Graph* (*Trace* is shown disabled for a document with no trace blocks). A blueprint with `kalka_issue` shows its issue after the breadcrumb (10.5): a chip with the key, the title and the status, linked to the issue, with "as of <time>" when the tracker could not be reached, and before it the parent chain as a small breadcrumb of keys; without a plugin, or without access, the raw key only. Where the plugin can export, *Export…* sits left of the tabs, disabled with the reason when you cannot export (not released, unreleased edits, not the owner). It confirms what will be sent, then offers *Archive*. Both bars stay at the top as the page scrolls.

Which section a document belongs to is one rule, `sectionOf`, used for the highlight, the breadcrumb, the rail and the lists themselves, so a document is highlighted in the section whose list holds it: a domain document with a valid `kalka_domain:` that is not a duplicate is in *Domains*, in a team (Personal has no Domains, so there it is in *Blueprints*); a seed (`kalka_status: seed`) is in *Seeds*; anything else is in *Blueprints*. A duplicate domain document stays in *Blueprints*, where the duplicate is flagged.

Left of the document, the **document rail** (`<nav>` "<section> in <scope>") lists the documents of the open document's section, the same set and order as that section's list, each by title with a dot when it needs you (comments by others not seen yet, or a waiting item; 11.3, 11.4); the open one is marked as the current page and kept scrolled into view. The rail follows the section list's grouping, with a header per group, and leaves out archived documents unless the list shows them (the open document is always listed). A row opens that document in the same view where it has one: from a trace, its first trace, else its document; from the document or its map, its document. A « / » button collapses the rail to a thin strip, and the choice is remembered in the page's storage (open when storage is unavailable). On a window narrower than 1000 px the rail starts collapsed, and opening it there is not remembered. The rail takes its own column and never covers the document; below 1340 px, while it is open, the section outline gives way to it. The list pages stay as they are: the rail is their compact form. Focus moves through the top bar, the document bar, the rail, then the document.

- **Needs you** — route `#/needs`, the first tab, over every team at once: the waiting items (11.3) grouped as *Decide* (transfers, disputes, documents without an owner) and *Yours now*, each a sentence naming who is involved, with its buttons (*Accept*, *Decline*, *Choose owner…* with a member picker, *Open*); then what is new since you last looked (11.4); then what is in progress with nothing to do (your joins, your offers to other teams with *Withdraw*); then the last notices. The tab's badge counts the waiting items and the documents with something new.
- **Setup** — the three settings: email, rendezvous, folder, with *Choose…* for the folder in the desktop window (the system's folder dialog, `POST /api/pick-folder`). Opened from a join link with no configuration, it is filled in from the token: the email and the server are shown as facts with *Change*, only the folder is asked, and *Save and join <team>* joins once kalka has restarted, without a second click.
- **Join screen** — route `#/join/<token>`: the team name, the inviter, the invited email and the expiry, with one *Join* button. In place of the button it says "this invite is for <email>, you are <email>", "expired", "you are already a member", or that the invite is for a team on another kalka server. Opening it never joins. While the join waits it shows its steps (set up, invite accepted here, waiting for the inviter's kalka to run, getting the team's blueprints), the attempts so far and the expiry, that it keeps trying with the window closed, *Copy a message for <inviter>* and *Cancel this join*. A failed join shows its reason and a box for a new invite, until dismissed. `#/join` without a token lists the joins in progress or failed.
- **Blueprints** — the blueprints in the scope, the most recently active first; seeds are listed under *Seeds* and domain documents under *Domains* only (`sectionOf`, above), and a scope holding only those says where they are: title (first H1) with "N new" (11.4), "unreleased edits" and validity problems as badges, the owner ("you" or the name before `@`), the team's version, and the last activity (the newest entry, else the file's time). In Personal each offers *Publish to team…*. In a team folder, one that does not replicate says why: "not shared: publish it or move it to Personal". An empty list says what to do next: in Personal, where to put a file, with *Open folder*; in a team, *Publish one from Personal* and *Invite a teammate*. A row with `kalka_issue` carries the issue's key and status. Where an installed plugin declares it, *Group by* offers *parent issue* (with a level picker named by the issue types, Epic and above, when there are several levels) and *issue status* (to do, in progress, done); each group has a header, a parent's key and title linked to it; *No <level>*, *Issue not available* and *No issue* come last. Order within a group is the list's order. Archived blueprints are hidden, with *Show archived (N)* when there are any. Both choices are remembered per team in the page's storage, and fall back to no grouping and hidden when storage fails or a plugin no longer offers the grouping.
- **Review (linear)** — a header card, then the rendered body with entries next to the section they anchor to, general entries and releases at the end, entries on removed sections last. The header names the state in one sentence ("You own this · version 2 is with <team>", "<owner> owns this · you are reviewing version 1", "Yours · not shared yet") with what follows from it ("You have edits your teammates cannot see yet. Releasing sends them as version 3.") and at most one primary action: *Release vN* with an optional note, *Publish to team…*, or *Choose owner…*. Problems take the sentence's place: two owners (a disputed version), no owner (7.4), being moved to another team ("comments added now will not travel"); a pending change of owner is said on the second line. Everything else is under *More*: *Publish to team…* for a Personal blueprint, *Context for an agent* (6.18), *Change owner…* (a hand-off for the owner, 7.2; a reassignment for a team owner, 7.3), *Split off a seed…*, *Copy to another team…*, *Move to another team…*, *Archive* or *Unarchive* (10.5), and *Delete for the team…* last; a team owner looking at someone else's blueprint also gets *Take over and release…* and *Take over and delete…* (7.4). Each destructive action is confirmed in the card. Entry cards carry no kind label for a comment (a deviation is labelled), the author's name before `@` with the address on hover, and badges whose explanation shows when clicked: "on vN" (outdated, with the version current when it was written), "section removed" (orphaned), "damaged", "not signed", "signature invalid: ignored", "only in your copy", "carried over" (attested, 20.7), "taken up in vN" (incorporated, 6.12), "former member". A thread written wholly on an earlier text folds into "N comments on an earlier version". Releases show as a version line (version, time, owner, note), and so do exports (version, the issue linked, time, who). A seed split off from an entry is a line under that entry, not a card. Selecting text in the body offers *Comment* beside the selection. The composer explains comment and deviation and lists the sections by their headings. Resolved references show inline with snippet and warnings; hints for documents opted into the reference (6.13) and the "newer domain version" line (6.12) stay below the header. The document bar above it carries the breadcrumb and the tabs *Document*, *Trace* and *Graph*. Beside the document, on a wide enough window, an outline rail lists its sections with their comment counts and a dot where there is a deviation, and *General*.
- **Plan** — in the review view of a document in the plan graph (6.18), a line in the header card sums up its goals by state ("3 goals: 1 unclaimed, 2 claimed"), its criteria and its children, with *Close as delivered* or *Reopen* for the owner. A goals block is drawn in the body as cards, one per goal and criterion, each a comment anchor with its state as a badge (unclaimed, claimed, ready, delivered, open); a criterion lists its given, when and then lines and the trace steps that cover it; a goal lists, as a tree, the child goals and criteria that claim it, each with its document's issue key and title, linked when held, and its state. A trace step's detail lists the criteria it covers. A domain node's header names the parent version it extends, with a "parent newer" badge when a newer one is out (`D030`), and its subdomains.
- **Invariants** — a panel in the review view of a document with invariants blocks (6.15): one row per invariant, its own and those it affects, with the statement, *guarded by* (links to the trace steps), *verified by* (each reference as the plugins resolved it) and the number of comments; an unguarded or unverified invariant is marked as a divergence is in a trace. An invariant id is a link wherever it appears in the body, in trace steps and in comments, with its statement on hover; each invariant takes comments like a section. A domain document's panel lists, per invariant, the held blueprints that reference it.
- **Terms** — in the review view of a blueprint that references a domain version, of a blueprint with a glossary of its own (no `kalka_domain:`, 6.16), and of a domain document: the glossary's terms in the text are underlined (6.16), with the definition and context on hover or focus, the domain node that defines the term, and the invariants that narrow it (`refines`, 6.12), which a domain node's glossary also marks as "refined"; an invariant that refines a term carries a "refines" marker, and a side panel lists the terms used with their counts. "Glossary v3 not kept here" when that version is not held.
- **Diagnostics** — the review view lists the document's diagnostics (6.17) by severity, each with its rule's title and meaning; *Publish to team…* and *Release* first show the errors and ask "Publish anyway?" or "Release anyway?".
- **Graph** — the *Graph* tab, route `#/t/<team>/graph/<kalka_id>` (`#/graph/<rel>` for a document with no `kalka_id`): the document in the plan graph and the domain tree, read-only, as one canvas. It draws the lens (6.18), the plan path, the domain path and the document's children, never siblings, in two bands: the domain tree on the left, the plan tree on the right, roots at the top. The layout is computed from the files, the same every time, and never saved. *Focus here* on a selected card opens the graph of that document, and the browser's Back returns to the previous one. Zoom is by level: a card (level 1) shows its kind, title, issue key, owner or version, and counts of errors, warnings and open items; opened (level 2) it lists its items, goals, criteria and invariants for a plan node, terms and invariants for a domain node, each with its state; a criterion opens (level 3) into the trace steps that cover it, and those into the steps that realize them, down to code. What is open is kept per document in the page's storage; by default every card is closed but the document's own. Edges are the tree (`kalka_parent`, the domain parent with the version pinned), pins from a plan node to its domain version, and the links between items: realizes, covers, realizes between steps, guards, affects, refines. Each item and edge carries what the document views say of it: a goal's or criterion's state, a criterion no step covers, an invariant not guarded or not verified, a step's trace divergence in the Trace view's words, a stale or unverified reference, a parent domain newer than the pinned one (6.12), ancestors pinning inconsistent domain versions (`D021`), and on cards the diagnostics count. The panel beside the canvas lists the open items, "N places need attention" in the documents held here: unclaimed goals, uncovered criteria, unguarded or unverified invariants, and trace divergences that come from the files (not a reader's own unopened links); selecting one opens what holds it and moves to it. Review is shown in the canvas too: an item's comment threads as a count, a dot for a deviation, a card's "N comments in prose", General and orphaned counts, and "new" for what arrived since you last looked (11.4); selecting an item shows its threads in the panel, read-only, with *Open in Document* and *Open in Trace*. A document not held here is a placeholder card with dashed edges; a domain whose name more than one document claims is marked ambiguous; an archived document is dimmed; seeds are not drawn; a block with errors marks its card; a loop in `kalka_parent` is cut and the edge back marked. Past about 40 items in a level, the first are shown with "+N more". Every card and item is a button in reading order, cards say whether they are open, and the open-items heading is announced. `kalka graph <file>` (12.1) prints the same graph.
- **Trace** — each trace block rendered as lanes side by side, most abstract on the left, flow running top to bottom within a lane. A lower-lane node is placed level with the first node it realizes; realization links are drawn across lanes in a style distinct from flow arrows, and `back` edges around the side of the lane in a style of their own, with their labels. New, modified and removed steps are marked, removed ones struck through. Nodes with a divergence are visibly marked, and the abstract lane is left empty opposite an implementation-only node, with no label: the flow arrow across the empty row already joins the steps on either side. Divergences are shown in plain words with the block's own lane names, never the class names: on a node "code only", "no code yet", "does J3 + J4", "split into K2 + K3", "split, not adjacent", "out of order", "link changed", "link not opened"; as a card head "Code only", "No code for this step", "Two business steps, one code step", "One business step, two code steps", "Its code steps are apart in the flow", "Different order in business and code", "The linked lines changed since this plan was written". The side panel is titled "N places where <lanes> differ", says that kalka worked them out from the arrows and that they are questions for review, not errors, and lists link problems (stale, unverified) in a group of their own; selecting a node replaces it with that node's detail: `why`, what it realizes, the resolved reference with snippet and warnings, entries anchored to the node, and a comment box that anchors to `§<slug>#<node-id>`. Validation warnings are listed in the side panel and under the inline block. A Diagram / Source toggle shows the block's source. Layout is computed, nothing is saved. The same block appears inline in the linear review view, never so small that a step cannot be read: a wide trace scrolls. Realization is shown as "carries out" / "carried out by".
- **Seeds** — seeds held in the scope (6.11), the pre-backlog: title, owner, where it was split off from, last activity; *Turn into a blueprint* (promote), and in Personal *Publish to team…*, since seeds are listed here only. Archived seeds are hidden as in Blueprints, with *Show archived*.
- **Team** — in this order: the sync in words ("Last sync 11:02 with marek, ola · piotr offline") with *Sync now*; one sentence per blueprint the last sync refused, and why; **Invite a teammate** (email, validity, *Create invite*) with *Copy invite message* (how to install kalka, the link, and the code for when the link is not clickable), *Copy link*, *Copy code*, and the open invites with *Show* and *Cancel*; the members with role, last seen (online, or when this kalka last found them online, 9.3) and how many blueprints they own, each with a ⋯ menu: *Make team owner…*, *Remove as team owner…* (each confirmed, saying what it allows), *Give their blueprints to someone…*, *Remove from team…* (with a preview of the blueprints that become yours); transfers in and out (20.7), open while one waits; then folded the team history (the log as a readable history, with discarded operations and their reasons) and the technical details (team id, folder, key fingerprints, files not shared, deleted blueprints). Wherever a member is named (hand-off, reassignment, the owner of an accepted transfer) it is picked from the team's members, not typed. *Decline* of a transfer asks once.
- **Domains** — "your team's shared vocabulary": the team's domain documents with version and owner; *Release* for the owner, with a checklist of open comments to mark as taken up (incorporated); a version switcher over local snapshots in the document header. With none yet, the start of a domain document to copy and *Open Personal folder*.
- **Settings** — who you are: your email, your folder with *Open folder* (`POST /api/open-folder`), and your server with *Change…* (an inline form: the public demo server or a kalka server address, with what the change means for the team above the button that applies it, and *Switch anyway* when the new server did not answer; 9.1); *Check setup*, the results of `kalka doctor` (12.5); start at login, keep running when the window is closed, notifications, pause (22); *Updates*: install updates automatically, *Check for updates*, and *Restart to update* or *Download and restart* (24); *Blueprint writing guide for agents: save a copy* (6.13); the version, with the protocol versions and your key under its details. The two window settings are hidden in a browser tab.

The UI updates live from the daemon's event stream when files change or replication delivers. A notice shown in the window (22.4) is a link to its subject. Dialogs are modal for the keyboard: focus moves in and stays in, Escape closes them, focus returns to what opened them.

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

- Not waiting items: your own pending join (it waits for the inviter; the switcher and Needs you show it), a queued push, new comments (a notice, 22.4, and 11.4).
- The `seen` marks are a small list in `state.yaml`: document id and what made it yours, the handoff release's id or the reassignment's operation. A mark is dropped when that is no longer what makes the document yours.
- After a reassignment or an accept, the new owner's kalka releases on its own (7.3); that release is not "a release of yours", so the item stays until the document is opened or a release is made by hand.
- Timestamps come from other people's clocks. "Oldest" orders the list and nothing else.

### 11.4 New since you last looked

Needs you also lists the team documents with comments or deviations by others that the user has not seen, and other people's documents that arrived and were never opened here. A document counts as seen when the page shows it (`POST /api/read`). The marks are the entry ids seen per document, in `read.json` in the config directory; they stay on this machine. The first start marks everything held as seen. The Blueprints list shows the same count as "N new".

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
| `kalka join --list` | view | Joins in progress or failed, with attempts, expiry and reason. | Join screen, Needs you |
| `kalka join --forget <id, link or token>` | daemon | Stop a pending join or clear a failed one. | Join screen *Cancel this join*, *Dismiss* |
| `kalka team members\|log\|invites` | view | The roster with presence, the log, or the invites made here. | Team |
| `kalka team remove <email> [--dry-run]` | daemon | Remove a member; `--dry-run` lists the documents that would be reassigned. | Team ⋯ *Remove from team…* |
| `kalka team add-owner\|remove-owner <email>` | daemon | Roster changes (19.3). | Team ⋯ *Make team owner…*, *Remove as team owner…* |
| `kalka team sync` | daemon | Start a replication round now. | Team *Sync now* |
| `kalka publish <file> --team <team> [-m <note>]` | daemon | Move a Personal document into a team with its first release (20.1). | Blueprints (Personal) *Publish to team…* |
| `kalka release <file> [-m <text>] [--incorporates <id>,...]` | daemon | Release the next version of a team document (20.4). | Review *Release vN*, *Take over and release…*, Domains *Release* |
| `kalka handoff <file> <email>` | daemon | Make another member the owner (7.2). | Review More *Change owner…* (as the owner) |
| `kalka reassign <file> <email>` | daemon | Team owners: make any member the owner (7.3). | Review More *Change owner…* (as team owner), *Choose owner…*; Needs you *Choose owner…*; Team ⋯ *Give their blueprints to someone…* |
| `kalka copy <file> --to <team>` | daemon | Offer a copy to another team (20.7). | Review More *Copy to another team…* |
| `kalka move <file> --to <team>` | daemon | Offer a move to another team (20.7). | Review More *Move to another team…* |
| `kalka transfers [--team <t>]` | view | Transfers into and out of the team. | Team, transfers; Needs you |
| `kalka transfer accept <id> [--owner <email>]` | daemon | Team owners: accept a pending transfer. | Needs you *Accept* |
| `kalka transfer decline <id>` | daemon | Team owners: decline it. | Needs you *Decline* |
| `kalka transfer withdraw <id>` | daemon | Withdraw your own pending transfer. | Team, transfers, *Withdraw* |
| `kalka export <file>` | daemon | Export the released version to the blueprint's issue (10.5). | Review *Export…* |
| `kalka archive <file> [--undo]` | daemon | Archive a blueprint or seed, or unarchive it with `--undo` (10.5). A team owner takes it over first. | Review More *Archive*, *Unarchive*; *Archive* after an export |
| `kalka delete <file>` | daemon | Delete a team document for the team (20.6). | Review More *Delete for the team…*, *Take over and delete…* |
| `kalka comment <file> [--anchor <slug>] [--kind deviation] [--re <id>]` | file | Append an entry; body from `-m` or stdin. Author is the configured email. | Review comment, deviation, reply |
| `kalka seed <parent> --title <t> [--from <entry id>] [-m <text>] [--why <text>]` | file | Bud a seed (6.11). | Review More *Split off a seed…*, a deviation's *Split off a seed* |
| `kalka seed backlink <seed>` | file | Add a missing `budded` entry to the seed's parent. | Review *Link it from its parent* |
| `kalka promote <file> [--status <s>]` | daemon | Change a seed's `status` (default `draft`). | Seeds, Review *Turn into a blueprint* |
| `kalka close <file> [--reopen]` | daemon | The owner confirms the document delivered (`kalka_status: closed`), or reopens it (6.18). | Review *Close as delivered*, *Reopen* |
| `kalka context <file> [--json] [--domain <path>]` | view | The document's flattened context for an agent: the plan path with goals and invariants, the domain path with terms and invariants (6.18). Reads files as `check` does. | Review More *Context for an agent* |
| `kalka graph <file> [--json] [--domain <path>]` | view | The lens of the document (6.18) as a graph: cards for the plan path, the domain path and the children, their items, the edges between them, and the open items in the documents read. The same files give the same output, byte for byte. Reads files as `check` does. | The Graph tab of the document bar |
| `kalka seeds [--team <t>\|personal]` | view | The pre-backlog of a team or Personal. | Seeds |
| `kalka domains [--team <t>]` | view | Domain documents and versions. | Domains |
| `kalka check <files or folders…> [--json] [--explain <id>] [--strict] [--with-plugins] [--domain <path>]` | file | Run the rules of 6.17 (12.4). | Diagnostics in Blueprints and Review; the confirmation before Publish and Release |
| `kalka invariants <file> [--json] [--domain <path>]` | file | The invariants matrix (12.4). | Review *Invariants* |
| `kalka doctor [--json]` | view | Check this installation (12.5). | Settings *Check setup* |
| `kalka samples` | file | Copy the sample blueprints, built into kalka, into `personal/samples/`, keeping files already there. Before setup it asks for them to be added once kalka is set up: the daemon adds them when it first starts. | Blueprints in Personal when empty, and Settings, *Add sample blueprints* |
| `kalka waiting [--team <t>] [--seen <file>]` | view (daemon with `--seen`) | Waiting items (11.3); `--seen` clears a handed-to-you item. | Switcher badges, Needs you, tray |
| `kalka pause` / `kalka resume` | daemon | Go offline and back (22.3). | Settings, tray |
| `kalka settings [--notifications on\|off] [--keep-running on\|off] [--auto-update on\|off]` | daemon (a view with no option) | The settings of 22.2; with no option it shows them. | Settings |
| `kalka update [--check] [--json]` | daemon | Look for a new release and, unless `--check`, install it when this kalka can (24), which restarts it. | Settings, *Updates* |
| `kalka settings --rendezvous <url\|peerjs-cloud> [--force]` | daemon, or `config.yaml` when none runs | Switch the rendezvous server (9.1). Unless `--force`, the server must answer; an invalid value exits 2, a server that does not answer exits 1 with kind `rendezvous-silent`. | Settings, *Change…* |
| `kalka autostart on [--headless]\|off\|status` | file | Start kalka at login (22.2). | Settings *Start at login* |
| `kalka reference export [-o <file>] [--force]` / `reference version` | file | Write or name `BLUEPRINT-REFERENCE.md` (6.13). Never overwrites without `--force`. | Settings *Blueprint writing guide for agents* |
| `kalka version` | file | Version and protocol numbers. | Settings, version details |

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

With `--json`, a failure is `{"error": "...", "kind": "..."}` on stdout, and the exit code is 1 (2 for usage errors; `check` and `doctor` have their own codes, 12.4, 12.5). A view's JSON is an object carrying `"daemon"`, with live fields `null` when no daemon runs (unknown, not zero). The kinds are stable:

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
| `rendezvous-silent` | The server to switch to did not answer; `--force` (or *Switch anyway*) switches regardless. |
| `error` | Anything else (a file that cannot be read, a network failure); the message says what. |
<!-- /error-kinds -->

A test checks this list against the kinds the code declares, and that each is returned somewhere.

### 12.4 `kalka check`

`kalka check <files or folders…>` runs the rules of 6.17. A folder is searched for `*.md`, leaving out dot-directories and domain snapshots (`*@v<N>.md`); files are checked in path order.

| Option | Meaning |
|---|---|
| `--json` | One object: `rules` (the set), `kalka` (the version), `files`, each with `file`, `diagnostics` and `traces` (each block's divergences, 6.10), and `summary` (counts per severity). A diagnostic has `line`, `column`, `end_line`, `end_column` (0 when unknown, then left out), `rule`, `severity`, `message`, and an optional `fix` with `text` and the `line`, `column`, `end_line`, `end_column` it replaces. Diagnostics are ordered by line, column and rule. |
| `--explain <id>` | Print what the rule means, why it exists, and a wrong and a right example. Takes no files. |
| `--strict` | Warnings fail the check as errors do. |
| `--with-plugins` | Resolve reference targets (trace `ref`, invariant `verified-by`) with the installed plugins and report those that do not resolve (`R010`) or that warn (`R011`). Off by default, since plugins may use the network. |
| `--domain <path>` | Where domain versions and plan nodes are read from: a document, or a folder searched like the files. The files checked are always read too. Without it the local `kalka` folder is used when one is set up; otherwise the rules that need a domain or a parent report "can't check" (`R001`, `P011`). |
| `--verbose` | Also print information in the text output (`--json` always has it). |

A domain version is found as its snapshot `*@v<N>.md` first, then as a domain document whose `kalka_version:` is that version (a repository that keeps only the live file).

**Exit codes:** 0 when there are no errors or warnings (information is allowed), 1 when there is an error, 2 when there are warnings only (1 with `--strict`), 3 for a usage error, such as an unknown option or an option without its value. A file that cannot be read is an error diagnostic (`B000`), not a usage error.

`kalka check` never writes: there is no formatter and no `--fix`. Agents run `kalka check --json` before handing a document over (6.13).

`kalka invariants <file> [--json] [--domain <path>]` prints the invariants matrix of 11.1, one row per invariant: the document's own, then the inherited and domain invariants it affects or guards, each with its statement (from the referenced version or the plan node above), status, what it refines, the trace steps guarding it, its `verified-by` and its gaps (`unguarded`, `unverified`). It prints the invariant diagnostics of `kalka check` (`I…`, and `R001` when the domain version is not here) and exits on them as `check` does. It reads domain versions as `check` does.

### 12.5 `kalka doctor`

`kalka doctor [--json]` checks that this installation can work, and says for each check `ok`, `warn` or `fail` with one line saying how to fix it, or `skip` when a check it depends on failed (no configuration, no answer from the rendezvous). Unlike `check`, it reads the configuration, talks to the daemon and reaches the network. It only reads: it registers, writes and starts nothing, but for a temporary file in the folder, removed at once.

| Check | Fails or warns when |
|---|---|
| `config` | There is no configuration, or it does not load (fail: run `kalka setup`). |
| `key` | The identity key is missing or unreadable (fail). |
| `daemon` | No daemon holds the single-daemon lock (warn: run `kalka`). |
| `local-api` | A daemon runs but its API does not answer with the token (fail). |
| `rendezvous` | The rendezvous server does not answer within 5 seconds (fail). A kalka rendezvous is asked for `/kalka/ice`; a stock PeerJS server answers 404, which counts as an answer. |
| `turn` | The rendezvous lists no TURN server (warn: peers behind strict networks will not connect). The PeerJS cloud has its own. |
| `clock` | The local clock differs from the rendezvous server's by more than 2 minutes (warn) or 10 (fail): invites and TURN credentials are time-bound. |
| `folder` | The folder is missing or not writable (fail). |
| `webview2` | Windows only: the WebView2 runtime is missing (fail: the window cannot open). |
| `autostart` | The start-at-login entry cannot be read, or runs a program that no longer exists (warn: turn it off and on in Settings). The system's entry is the setting (22.2), so an entry that is off is `ok`. |
| `url-scheme` | `kalka://` links do not open this installation (warn: open Kalka once to register it). |

Exit code 0 when all are `ok` (or `skip`), 2 when some only warn, 1 when one fails. The app shows the same results in Settings under *Check setup* (`GET /api/doctor`), run by the daemon: there `daemon` and `local-api` are `ok`, since it answers.

---

## 13. Local HTTP API

One local API, used by the web UI (in the desktop window or a browser tab), the desktop shell's tray, and the CLI. There is no second transport.

- Bind to `127.0.0.1` only, on `ui_port` (default 7717). The port is fixed so that the page's per-origin storage survives restarts.
- Validate the `Host` header against the loopback address and port (defeats DNS rebinding).
- Require the per-start random token on every `/api/` request. The page gets it in the URL that `kalka open` or the shell loads, which the daemon exchanges for an HTTP-only, same-site cookie; the CLI sends it as a bearer header.
- Requests that change state must carry an `Origin` equal to the daemon's own origin, or none (the CLI). When a `Sec-Fetch-Site` header is present on an `/api/` request it must be `same-origin` or `none`, for reads too. A web page elsewhere that aims requests at `localhost` is refused. The token link itself is not checked: opened from a mail or chat page it arrives as `cross-site`, and a page that does not know the token gains nothing from it. The UI's static files carry no data and are served without these checks (the sandboxed Mermaid frame loads them from an opaque origin).
- The token is in `daemon.json` in the config directory, written owner-only (mode 0600; on Windows an ACL restricted to the current user). Another local user cannot read it. The user's own processes can, as they can read the identity key: they are trusted with the user's files.
- `--no-ui` stops the daemon from serving the web UI (every page path answers 404 and no cookie is set); the API stays up for the CLI and the shell.
- **Scope.** Lists take `?team=<name or hash>` or `?team=personal`: `GET /api/blueprints`, `/api/seeds`, `/api/domains`, `/api/waiting`, `/api/transfers`. `GET /api/teams` lists the teams with their waiting counts, and each member's `presence` as this daemon last saw it (`online`, `offline`, `unknown`; none for the local user, 9.3) with `last_seen`, when it last found them online since it started. `GET /api/blueprints` gives each blueprint's `activity` (its newest entry's time, else the file's), `new` (11.4), and for a published one the team's `version` and `unreleased` when this copy's plan differs from it. `GET /api/waiting` lists the items oldest first; `POST /api/waiting/seen {path}` clears a handed-to-you item, as opening the document with `GET /api/doc` does. `GET /api/summary` carries `waiting: {total, oldest: {team, at, target}, teams: [{team, name, count}]}`; `target` is `#/needs` for a transfer. `GET /api/unread` lists what is new (11.4); `POST /api/read {path}` marks a document seen.
- **Issues** (10.5). `GET /api/blueprints` carries `issue` (the `kalka_issue` value) and `archived`; `/api/seeds` carries `archived`. `GET /api/issues?team=` returns `caps` (`meta`, `hierarchy`, `export`: whether any installed plugin declares them) and `issues`, keyed by `kalka_issue`, each with `scheme`, `payload`, `caps`, the cached `meta`, `as_of`, `stale`, `pending` and `error`; it starts a background fetch for what is stale and never waits for it, and the event stream says `issues` when one finishes. A document's view carries `issue` likewise, `export` (`can`, `why`, `version`, the `last` export with `last_at` and `last_by`) and `archived`; an `exported` entry carries `exported`, parsed. `POST /api/export {path}` exports and returns `{path, issue, version, url, updated}`; `POST /api/archive {path, archived, take_over}` archives or unarchives.
- `GET /api/graph?path=<file>` or `?team=<name or hash>&id=<kalka_id>` returns `{graph, rel}`: what `kalka graph --json` prints for the document, with where each card is held (`rel`, `kept`, `scope`), the reader's own reference divergences and what is new since they last looked; `rel` is the document's.
- `GET /api/about` returns what `kalka version` prints (`version`, `wire_protocol`, `plugin_protocol`, `reference`); `GET /api/reference` returns `BLUEPRINT-REFERENCE.md` as a download, for Settings *save a copy* (6.13). The page never writes the reference into the folder.
- `POST /api/joins/preview {token}` returns what the join screen shows (11.1) and never joins: the token as normalized, the team name and hash, the inviter, the invited email, the expiry and the rendezvous; `problem` with `message` when the join cannot go ahead here (`invite-email`, `invite-expired`, `rendezvous-mismatch`, `already-member`, or `own-invite` when the owner opens their own link); and `joining` when this installation is already redeeming it. Before setup it answers from the token alone, so the setup form can fill itself in. `POST /api/joins` refuses with the same kinds. `DELETE /api/joins/<id or token>` stops a pending join or clears a failed one.
- **Diagnostics and review aids.** A document's view carries `problems`: its diagnostics (6.17) from the same checker as `kalka check`, without plugins, reading domain versions from the folder; `invariants`, the matrix of 12.4 with, for a domain document, the held blueprints referencing each invariant; and `glossary`: the referenced version (`ref`), its state (`held`, `not-kept`, `none`) and its entries. A release entry carries `release`, its first line as the core reads it (version, plan, owner, deleted, how many comments it takes up, note), so the page never parses it. `GET /api/rules` returns the registry; `GET /api/domain-impact?path=<domain document>` the held blueprints a release of its current text would affect (`I040`, `G030`); `GET /api/doctor` the results of `kalka doctor`.
- `POST /api/rendezvous {rendezvous, force}` switches the rendezvous server while kalka runs and returns the status. An empty or unparsable value is `400`. Unless `force`, the server is asked for its ICE configuration (the check of `kalka doctor`, 5 s); if it does not answer the reply is `409` with `"kind": "rendezvous-silent"` and the message says it did not answer, and the page offers *Switch anyway*. The same server as now changes nothing. Otherwise `config.yaml` is saved with only the rendezvous changed, the node drops its registration and registers on the new server, and the status shows it at once.
- `POST /api/pick-folder {start}` shows the system's folder dialog (setup and the daemon both answer it) and returns `{folder}`, empty when canceled. `POST /api/open-folder {path}` shows the blueprint folder, or a folder inside it, in the system's file manager; a path outside the folder is refused.
- Links the page opens to the outside go through `POST /api/open-external` when it runs in the desktop window: only `http` and `https` are accepted, and the system browser opens them. The window never navigates away from the loopback origin.
- **The desktop shell** (22) is a client like the others. It holds an event stream with `GET /api/events?shell=1`; while one is open, `GET /api/summary` reports `"shell": "desktop"` and the shell, not the daemon, presents notices. `POST /api/ui/show {"path"}` or `{"route"}` asks it to show its window, on a document's route or on a route of the page (`#/` and URL-safe characters only: Needs you, the join screen); with no shell attached it fails with kind `no-shell`, and bare `kalka` opens the browser instead. The window loads the token link with `window=1`, which also sets a cookie the page can read (`kalka_window=1`): the page then sends links outside, leaves notifications to the shell, and shows the window settings.

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
- **A team document** lies in the team's folder, has `kalka_team:` naming that team, and has a valid release; or is a valid pending transfer in `.kalka/incoming/`. Only such files replicate (20.1). One function decides this.
- **Mismatches are shown, never replicated:** a file in a team folder without that `kalka_team:` or without a release is "not shared"; a file in Personal with `kalka_team:` shows "has `kalka_team:` but lies in Personal".
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
- Desktop shell: Wails v3, pinned to an exact release. It is built only with the `desktop` build tag, so `go build ./...`, `go vet ./...` and `go test ./...` never need it. On Windows it needs no CGO; on macOS and Linux it does (23).
- `kalka-rendezvous` (`cmd/kalka-rendezvous`) imports only the signaling, peer ID and TURN packages; a test keeps it free of the core, the daemon, the UI and WebRTC.
- URL scheme registration lives in `internal/urlscheme`, beside `internal/autostart`, with the registry, data directory and command runner as parameters so tests use fakes (23).
- What the user installs is one product per platform (23.1): on Windows a per-user installer holding `kalka.exe` and `kalka-desktop.exe`; on macOS `Kalka.app` with one executable that is also the CLI; on Linux one archive with `kalka` and `kalka-desktop`.
- No bundled Node.js or Chromium: Node is a build-time tool for the web UI only, and the window uses the system webview.
- WebRTC: `pion/webrtc`; TURN: `pion/turn`.
- File watching: `fsnotify`.
- Frontend: TypeScript with a small framework of the implementer's choice, built to static assets and embedded.
- Releases (`tools/release`): per target a reproducible headless archive (`kalka`, `kalka-plugin-gh`; on Windows also `kalka-desktop.exe`), a `kalka-rendezvous` archive; the Windows installer (when NSIS is installed), `Kalka.app` in a zip (built on a Mac) and a Linux desktop archive (built on Linux with GTK 4 and WebKitGTK 6.0), each skipped with a reason when the host cannot build it; `SHA256SUMS`. In CI the Linux job publishes the release and a macOS job adds the app zips and their `SHA256SUMS` lines (24). See 23.5 for signing.
- Pin dependency versions at implementation time; do not assume the versions current when this was written.

---

## 16. Scenarios

The test suite runs each of these between several daemons (`internal/core/scenario_test.go` and the tests beside it).

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

## 17. Design notes

Choices that are not obvious from the rules alone, and why they were made.

1. **Only the owner changes the plan; everyone else adds entries.** A rule such as "a reply must extend the file it answers" does not survive two reviewers: once the owner merges one reviewer's comments, the file is no longer a prefix of the other's reply (7, 20.4).
2. **The rendezvous server binds no email to a key.** That would give it state, and it is impossible on a stock PeerJS server or the public cloud. Trust rests on team logs (8).
3. **`kalka` writes a few frontmatter keys.** A blueprint needs an identity that survives renaming, an owner and a team. This is the one exception to "the tool does not author blueprints" (6.2).
4. **Entry ids are content hashes.** They give de-duplication and corruption detection; signatures add authorship (6.5). Signatures cover the full digest: the 12-hex id is short enough to be matched by brute force.
5. **Timestamps order the log.** Clock skew can reorder entries slightly; it cannot lose them.
6. **Three settings stay three.** ICE configuration is discovered from the rendezvous server (9.4); teams are created and joined, not configured (5).
7. **The public PeerJS cloud is for trying kalka out.** It provides a TURN server, but has no SLA, and any holder of a public key can claim that key's id there (9.7).
8. **Presence has no directory.** PeerJS servers do not list peers; presence is learned from probes and replication (9.3).
9. **One device per identity.** A second device is a second identity (1).
10. **The UI renders hostile input.** A received blueprint is untrusted Markdown shown by a page that can write files, so sanitization and the hardening of the local API are requirements, not polish (11.2, 13).
11. **No offline queue on a server.** Team documents travel through whichever members are online (20.3).
12. **Workflow-agnostic.** The core interprets the frontmatter keys of 6.2 and one section heading; everything else in the file is pass-through.
13. **Trace blocks (6.10).** Blueprints explained at business level are hard to tie back to code, and the details lost in that translation are the ones that matter for implementation. A trace block holds both views as flows and links them; `kalka` computes where they diverge instead of trusting the author to say so. The language is line-oriented, one value per line with no quoting (6.14), because the common failures of YAML, a text cut at a comma in a flow mapping, a colon that turns a text into a mapping, or a misspelt key silently ignored, make the tool state something false about the code; v1 was a restricted YAML and showed those failures in practice. Loops are written as `back` edges so that "comes before" is a partial order the classes can rely on. The version is in the fence, so a later version may change the syntax entirely.
14. **Unparsed log content moves with the entry before it.** "Preserved in place" and "rewritten in canonical order" cannot both hold literally (6.4).
15. **A key-derived peer id does not stop posing on a stock server:** any holder of the public key can register it. Channel binding prevents posing; on `kalka-rendezvous` a proof of the key also prevents squatting (8.3, 9.2, 9.6).
16. **Team documents carry signed releases,** so whoever relays a document does not matter (20.4). Judging "body edits only by the owner" by the sender would refuse every relay.
17. **"Removal wins; the senior owner wins a mutual removal."** A rule like "the lower hash wins" would let a removed owner undo the removal by back-dating an operation and grinding its hash (19.4).
18. **Team owners act by taking a document over** (7.4). Letting a team owner do anything a document owner can would let two people sign one version and make disputes ordinary.
19. **A reassignment cites the version its signer saw.** Document releases are not in the team log's graph, so "releases not causally before a reassignment" cannot be decided; everything above the cited version by anyone else is void, and every reassignment is an anchor (7.3, 20.4).
20. **Location decides what replicates** (14). Choosing documents by the `kalka_team:` name anywhere in the folder would let two teams with one name leak into each other.
21. **Countersigned history is attested, not verified.** The destination of a transfer cannot check source signatures; the badge says who vouches (20.7, 21).

### Residual risks

- **Security review.** An organisation may want to sign off a tool that moves design documents between machines over WebRTC, even though nothing is stored centrally. Hosting `kalka-rendezvous` on its own infrastructure is the mitigating position.
- **Networks that block WebRTC entirely.** TURN over TLS/443 covers most; some proxies will still defeat it.
- **Other writers of the same file.** Agents or editors may modify a blueprint while it sits in the folder. `kalka` tolerates this by construction (pass-through frontmatter, unparsed log content preserved, re-read before write), and stray plan edits never travel (7.5).
- **Team log rules are hard to change later.** Logs created under one rule set must replay the same way forever. Each operation carries a rule version (19.1), and no real team should be created before the harness passes for the current version.
- **Rendezvous load from replication.** Gossip with a small fan-out and a digest that ends idle sessions in one round trip keep it low (20.3).
- **A move can hang** until the person who started it is online after the accept (20.7).

---

## 18. Open questions

### 18.1 Open questions

Each question has the answer kalka currently implements.

1. **Access control at the rendezvous.** Which mechanism will sit in front of a hosted `kalka-rendezvous` (SSO proxy, network policy)? *Default: network policy.*
2. **Manual accept for joins.** Should a team owner be able to require a manual "accept" for each join? *Default: not in this version; the invite is the approval (19.7).*
3. **Key rotation.** Every signature check would need to know which key was valid when. The format has the two hooks it needs: a newer rule version's operation (such as a key rotation) travels through older peers (19.1), and an entry's signature names the member term it was made under (6.5), so a rule such as "a release must be signed under the author's current term" can be added without changing the entry format. *Default: future work; a lost key means removal and a new invite. Until then a removed or replaced key still signs entries that verify under its old term.*
4. **Teams across rendezvous servers.** An installation uses one rendezvous, so a person cannot be in two teams on two servers. With several teams per person this limit is met sooner. *Default: accepted limit; the join screen names the mismatch.*
5. **Several devices per person.** With key IDs a second device is a second identity. *Default: still a non-goal (1).*
6. **Team-wide invite links.** An invite names one email, so a link posted in a channel works for one person. *Default: named and single use (19.7).*
7. **From a team back to Personal.** *Default: not supported; "delete for the team" keeps the last copy in the team's trash.*
8. **One signature algorithm.** Keys and signatures carry no algorithm tag; everything is ed25519. *Default: accepted. Another algorithm would need a new rule version, and peers that cannot verify it could not pass its operations on.*
9. **Finality.** A senior owner's removal signed from an old view voids what the removed owner did after that view (19.4 rule 2), even when nobody meant harm. *Default: `kalka` runs a replication round before signing a removal, a demotion or a reassignment, and warns when no other member was reached; a checkpoint operation is future work for a later rule version.*

### 18.2 Decisions

Decisions taken where the design left a choice, with the reason.

1. **Structural errors still refuse a publish.** `F001`–`F004`, `B001`, `B012` and `B013` mean the file cannot be signed or merged safely, so Publish, Release and transfers keep refusing it. Every other error, including trace, invariant and glossary errors, is shown before Publish and Release with a confirmation and never blocks (6.17).
2. **Trace errors count in `kalka check`.** A trace block with errors makes `kalka check` exit 1, like any other error. Divergences are not diagnostics: they are review questions, listed per block in `--json`.
3. **No signature counts in `check`.** Whether a signature verifies depends on the keys a machine holds, not on the file, so `check` does not report it; the app shows each entry's signature.
4. **One "can't check" per document.** When the referenced domain version is not held, a single information diagnostic (`R001`) names the version and the rules not run, instead of one per reference.
5. **Snapshots first, then a live file.** A domain version is read from its snapshot; a domain document whose `kalka_version:` matches is used when no snapshot exists, so a repository holding only the live file can be checked in CI (12.4).
6. **No check of a term's context against the Scope section.** Reading the Scope section would mean interpreting prose, which 6.14 rules out. A later version may add a typed `scope` key.
7. **Invariant ids are free-form** like trace node ids, with `I1`, `I2`… advised, so a team can keep ids it already uses. They share the comment-anchor space of trace nodes under the same heading, so a clash is a warning (`I016`).
8. **The verification plan hint** (`H003`) is satisfied by an invariants block or by a "Verification plan" heading.
9. **Glossary matching in the browser.** The app links terms in the rendered page with its own matcher; `kalka check` uses the Go matcher. Both are run over the same corpus (`testdata/glossary/corpus.json`) in the Go and the web tests, which must give the same matches.
10. **Doctor exit codes** follow `check`: 0 ok, 2 warnings only, 1 a failure.
11. **The page computes no diagnostic.** It shows the daemon's `problems`. Two other pieces of logic the page had of its own are settled: a release entry is read by the core and sent parsed (`release` on an entry, 13), and what counts as a plugin reference in a link (10.1) is checked by both sides against one corpus (`testdata/refs/corpus.json`). Turning a typed team name into a name (19.2) stays in the page as a preview; the daemon validates the result.

12. **An invariants block may hold only `affects`.** A plan that changes nothing of its own but touches domain invariants says so without inventing one.
13. **`affects` and `guards` resolve the same way.** Both name domain invariants as `<domain>#<id>` against the version in `kalka_domain:`; a domain document may name its own invariants with or without its domain name.
14. **A reference wins over a term.** Where an invariant reference such as `delivery#I3` contains a glossary term, the page links the reference.
15. **Typed blocks with errors are not read.** A glossary or invariants block with an error other than a duplicate is ignored as a whole and shown as code, as a trace block is; duplicates drop only the later entry, so one mistake does not hide a glossary.
16. **`G030` and `I040` at release list each blueprint once per word or invariant.** They are information for the owner and never stop the release.
17. **Doctor has a `skip` state.** A check that cannot run because an earlier one failed says so instead of failing twice; it does not change the exit code.
18. **No doctor check for a start-at-login setting.** kalka keeps no such setting apart from the system's entry (22.2), so doctor reports an entry it cannot read or that runs a missing program, never one that is merely off.
19. **Samples go to `personal/samples/` and are never overwritten.** Seeding twice adds only what is missing, so a sample someone edited stays as they left it, and Personal's own files are never touched.
20. **The installer asks for the samples, the daemon adds them.** The installer runs before setup, when there is no folder yet; `kalka samples` then leaves a request in the configuration folder that the daemon honours at its first start after setup.
21. **The server changes live, and is checked first.** Settings and `kalka settings --rendezvous` switch the rendezvous of a running kalka without a restart: `config.yaml` is saved with only the rendezvous changed and the network loops restart on the new server. The new server is asked to answer before the switch unless forced, so a typing mistake does not take kalka offline. The key id is the same on any server, so no re-invite is needed: teammates reach you once they use the same server.
22. **The email stays a setup setting.** It is bound to the key in the team logs, so changing it means teammates must invite you again; the folder is where the data is. Both change with `kalka setup`, not live.
23. **Open invites are not rewritten.** An invite carries the rendezvous of its inviter (19.7), so after a switch the open ones name the old server and the join check refuses them (`rendezvous-mismatch`); the owner creates new ones. A join still in progress waits for an owner who may still be on the old server and completes once they move too.

24. **Archive is its own key.** `kalka_archived: <time>` leaves `kalka_status` alone, so unarchiving a seed gives back a seed, and the time says when. It is a plan edit, released by the owner like any other, so it replicates and is signed; a team owner takes the document over first (7.4), as for any release. It is refused over unreleased edits, which would otherwise be released unseen.
25. **Export needs no take-over.** It appends an entry and leaves the plan alone, so a team owner exports a document someone else owns as is; the entry names who exported.
26. **"Unknown scheme" is a fixed list.** `F021` compares the scheme with the plugins kalka ships (`jira`), not with what is installed, so `kalka check` gives the same answer on every machine.
27. **Accepted deviations are those a release took up.** The summary lists the deviation entries named in a valid release's `incorporates` (6.12), since that is the one place the plan's owner said a deviation was accepted.
28. **The cache survives restarts.** `issue-cache.json` in the config directory holds the last answers, so a restart offline still shows titles "as of" their time. It is per installation, like `read.json`.
29. **A whole-batch failure keeps what was cached;** an item the tracker answered `ok: false` (deleted, no access) replaces it, so a lost permission is shown, not hidden behind old data.
30. **The export entry follows the tracker.** It is appended only after the plugin succeeded; if writing it fails, the error says that the issue was updated, so the next export (with no `previous`) writes a new attachment and comment rather than losing track silently.
31. **Jira: new attachment first.** The plugin uploads the new attachment, writes or edits the comment, and only then deletes the previous attachment; a failure on the way removes what it added. One site per user; Data Center gets a plain Markdown comment and its hierarchy is sub-tasks only.

Future work:

- **Several issues per blueprint**, and more than one site per tracker. One key keeps grouping and export unambiguous.
- **Plan diffs between versions** ("what changed since you last read it"). A release stores only the plan's hash, so showing the change needs the text of the version a reader last read, which `kalka` would have to keep locally for each reader.
- **Words missing from the glossary.** Flagging a word the glossary does not define needs to know which words are meant as domain terms; an inline marker such as `[[term]]` would make that explicit.

---

## 19. Teams and roles

### 19.1 Team log

A team is a **signed, append-only set of operations**, not an editable document. The current state (members, owners, reassignments, transfer decisions) is computed from it by a deterministic replay (19.4).

- File: `<folder>/<team>/<team>-team.log`, one operation per line, as JSON. It is not Markdown, because hashes must cover exact bytes and editors change whitespace. It is not meant to be edited; the UI and `kalka team log` render it as a readable history. The index ignores it (it is not `*.md`).
- One operation: `{v, team, op, args, prev, signer, at, sig}`.
  - `v`: the rule version that replays it. All current operations are `v: 2`. A later rule change uses a new version and never changes how an existing operation replays. An operation of a newer version than a `kalka` reads is verified, kept byte for byte and passed on, and stays pending with everything built on it until that `kalka` is updated (`docs/teamlog.md`).
  - `team`: the team hash (19.2); empty in `create-team` itself.
  - `op` and `args`: one of the operations below.
  - `prev`: the hashes of the operations it builds on: the heads of the signer's log when it was made. These links make the log a causal graph.
  - `signer`: the signer's public key. `at`: the signer's clock, for display only; it plays no part in replay.
  - `sig`: an ed25519 signature over `"kalka-teamlog-sig\n" + hash`, with the same context for every rule version.
- An operation's hash is SHA-256 over its canonical encoding without `sig`: UTF-8 JSON with keys in lexicographic order, no insignificant whitespace, no floating-point numbers, `prev` sorted. A line must be in exactly that form, so the hash can be taken from the line without knowing the operation's kind. The exact encoding is fixed in `docs/teamlog.md`. Keys and signatures are base64url without padding.
- An operation is *known* once a peer holds it and has checked its signature. Operations are exchanged by hash (20.3), never as a whole file.

| Operation | `args` | Who may sign |
|---|---|---|
| `create-team` | `name`, `email` (the creator's) | Anyone; the signer becomes the first owner and member |
| `add-member` | `email`, `pubkey`, optional `invite` (id) | A team owner |
| `remove-member` | `email` | A team owner |
| `add-owner` | `email` (must be a member) | A team owner |
| `remove-owner` | `email` | A team owner |
| `reassign-document` | `doc` (`kalka_id`), `from` (email), `owner` (email), `after` (version) | A team owner (19.5) |
| `accept-transfer` | `doc` (the new `kalka_id`), `owner` (email), `transfer` (64 hex digest of the `transfer` entry) | A team owner (19.6) |
| `decline-transfer` | `doc`, `transfer` | A team owner (19.6) |

Every operation is a team owner's; members sign entries and releases in documents, never log operations.

### 19.2 Team identity

A team's identity is the hash of its `create-team` operation. Its name is a label: `[a-z0-9][a-z0-9-]*`, used in file names and in `kalka_domain:` references. Two different teams with the same name can exist; the second one held locally gets a suffix on its folder, and a reference to that name is shown as ambiguous. A member's identity within a team is the pair of email and key bound by `add-member`.

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

- `reassign-document {doc, from, owner, after}` is how a team owner names a document's next owner (7.3). `after` is the highest valid release of the document the signer holds. Releases above `after` that the signer had not seen become void; `kalka` therefore runs a replication round before signing a reassignment or a removal, and warns when none succeeded. `from` is the owner the signer saw: it is for the history and the notices, and replay does not judge it.
- **Removing a member.** Together with `remove-member`, the remover's `kalka` emits one `reassign-document` per team document it holds whose owner is the removed member, giving each to the remover. They do not depend on the removal: if the remover loses a mutual removal, their term ends and rule 2 discards the reassignments with it; if the removal is discarded for another reason (rule 6), the reassignments stand, since a team owner could have made them anyway.
- A document whose owner is no longer a member and that no reassignment covers (one the remover did not hold) is a waiting item for the team's owners (11.3).
- The `kalka_owner:` line follows: when the new owner's `kalka` holds the operation and the release at `after`, it rewrites `kalka_owner:` and releases version `after`+1.

### 19.6 Transfer decisions

A transfer into the team arrives as a pending document (20.7). Its decision is a log operation, because it needs a team owner's authority, "removal wins" for a decider who is being removed, and one winner among concurrent decisions; replay gives all three.

- `accept-transfer {doc, owner, transfer}`: `transfer` is the digest of the offer's `transfer` entry, so the signature on the operation is the accepting owner's countersignature of the plan and the history it covers. `owner` is the accepting owner, unless they name another member. The operation is an anchor with cut-off 0: `owner` is the only valid signer of version 1 (20.4).
- `decline-transfer {doc, transfer}`: the offer is discarded on every peer (20.7).
- Rule 8 settles concurrent decisions: one accept counts; an accept beats a decline.

### 19.7 Invites and joining

1. A team owner runs `kalka team invite <email>` or *Invite* in the Team view. The token is one line of base32 holding: the team name and hash, the inviting owner's public key and email (`inviter`), the invitee's email, a 32-byte one-time secret, the expiry (default 72 hours, `--expires`), and the rendezvous address. The open invite is stored in `invites.yaml`. `kalka` does not send the token; the owner passes it on through any channel, as a link `kalka://join/<token>` or as the bare token.
2. The invitee opens the link, pastes the link or the code into *Join with an invite…*, or runs `kalka join <link or token>`. The join screen (11.1) shows the team, the inviter, the invited email and the expiry; only *Join* or `kalka join` joins. The owner's key becomes their trust anchor for the team. The pending join is stored in `joins.yaml`. If the token names another rendezvous than the configured one, `kalka` says so.
3. The joiner's daemon probes the owner's key ID once a minute until it meets the owner or the invite expires; this survives restarts. Only the joiner can dial: the owner does not know the joiner's key. The owner's part is to be online. Nothing is queued on the rendezvous server.
4. On meeting, the joiner's `hello` carries `invite: {id, team, proof}`, where `team` is the team hash from the token and `proof` is an HMAC-SHA-256 under the secret over both public keys and the team hash. The owner checks it after `auth`. A valid proof is the verification.
5. The owner's `kalka` approves automatically (the invite is the approval), appends `add-member {email, pubkey, invite}` with the email *from the invite*, marks the invite used, and sends the log on the same connection. The joiner checks that the log starts at the team hash in its token and contains its own `add-member`. A folder `<folder>/<team>/` is created for the team.
6. A bad proof, an expired invite, a used invite, or an email other than the one invited ends the connection with a `bye` naming the reason (`invite-unknown`, `invite-used`, `invite-expired`, `invite-proof`, `invite-team`, `invite-email`, `invite-not-owner` or `invite-key-clash`, 9.5), and the joiner's UI shows it. A token that does not parse never reaches the wire: the joiner's own `kalka` refuses it as `invite-token`.

Single use holds in two places: the issuing owner's `invites.yaml`, and the replay rule that discards a second `add-member` for the same invite id (19.4 rule 8). Only the issuing installation can redeem an invite, because the joiner dials its key ID.

**Parsing.** One function parses what a user gives: a `kalka://join/<token>` link or a bare token, in any letter case, with surrounding whitespace or angle brackets as chat tools add them, at most 4 KiB. Line breaks inside are ignored (mail wraps long lines), and so is one trailing slash, which some systems add to a link. Another scheme, another path, extra path parts, a query, a fragment or a damaged checksum are refused with `invite-token`. The team name and the inviter are claims until the join completes, and are rendered as text.

**Links.** The `kalka` URL scheme is registered per user (23). The program that receives a link hands it to the running app with `POST /api/ui/show` on the join route and exits; with no shell running, the shell starts on that route; without a desktop shell, `kalka open <link>` opens the browser there and `kalka join <link>` joins directly. A link can only open the join screen.

---

## 20. Team replication

### 20.1 Scope and publishing

Every member keeps a full replica of the team's documents: the team log, the team documents (blueprints, seeds, domain documents and their snapshots), tombstones and pending transfers. **Replication never crosses teams**: a session is for one team, and a peer you share one team with gets nothing of another unless they are a member of it too. **Personal never takes part**: no Personal file enters an inventory, and a request for its `kalka_id` gets the same answer as a request for an id that does not exist (14).

A document enters a team when its owner publishes it: *Publish to team…* in Personal, or `kalka publish <file> --team <team>`. Only a member can publish, only to a team they belong to. `kalka` adds `kalka_id` and `owner` if missing, writes `kalka_team:`, makes the first release (20.4), and moves the file into the team's folder; the result carries the new path. No acceptance is needed and the member stays the owner. A document belongs to exactly one team or to Personal. Leaving a team is a tombstone: deletion (20.6) or a move (20.7); copies already replicated cannot be recalled.

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
### release · ewa@example.org · 2026-11-20T09:00:00Z · § · rev:<8 hex> · id:<32 hex> · sig:<term>.<base64url>
v7 · plan:<64 hex SHA-256 of the normalized frontmatter and body> · owner:ewa@example.org
incorporates: 7c2e91ab04f3d81a6b0e55c2f9a41b77, a41b77c09e12f3d8e6a0b5c4d2e1f907
Renamed "consignment" to "parcel"; added the invariant on redelivery attempts.
```

- The first line is `v<N> · plan:<hash> · owner:<email>`, followed by ` · deleted` for a deletion and ` · deleted · moved:<team name>/<kalka_id>` for a move (20.6), and then by any number of ` · <name>:<value>` tokens a later `kalka` may add, which are signed with the body and otherwise ignored. `owner` is the `kalka_owner:` of the released plan: the one who may release the next version. `incorporates` and the note are optional.
- The signature covers the entry's full digest, which includes the version, the plan hash and the owner. Every release needs a signature under a term of this team that bound its author (6.5).

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

- Domain documents also carry `kalka_version:` in frontmatter; blueprints and seeds carry the version only in their releases.
- **Releases are made on an action, not on save:** *Publish*, *Release*, *Hand off*, *Promote*, *Delete for the team*, and the release a new owner's `kalka` makes after a reassignment or an accepted transfer. An owner with unreleased edits sees a banner.

**Entries.** The union of all valid entries from both sides, de-duplicated by `id`, sorted by `(timestamp, id)`; where both sides hold an entry with the same `id`, the copy whose `sig` verifies is kept, so a relay cannot strip a signature. Unparsed log content from the side whose plan was kept is preserved. An entry enters the replica only if its signature verifies under a term of this team that bound the author's email (6.5), or it is attested by an effective transfer (20.7). Others stay in the file that holds them, *local only*. The merge is idempotent, commutative on the entry set, and never drops a valid entry.

### 20.5 Where received documents go

Documents received from members are created in `<folder>/<team>/`, with the sanitized name the sender suggests and a numeric suffix if taken. Documents are found by `kalka_id` within their team, so a user may move a received file into a subfolder of the team folder. Domain snapshots are written beside their live document. Pending transfers are kept in `<folder>/<team>/.kalka/incoming/<kalka_id>.md` until decided.

### 20.6 Deletion

- Only a document's owner deletes it for the team: *Delete for the team* or `kalka delete <file>`. This makes a release with the next version, marked as a deletion, whose plan is a **stub**: the frontmatter and nothing else. The release decides that a document is deleted; no frontmatter key does.
- It spreads like any release, and no release above a tombstone is valid. A tombstone beats any older copy, in both directions of a merge.
- On each peer the file leaves the folder. The stub goes to `<folder>/<team>/.kalka/tombstones/<kalka_id>.md` and the last full copy to `<folder>/<team>/.kalka/trash/`. Replication reads the tombstone directory, so an offer of an older copy is answered with the tombstone.
- The stub keeps only its release entries, the tombstone included, so that every peer can check the chain that leads to it. This is the one place where valid entries are dropped (2).
- **A move's tombstone** (20.7) is the same, with `moved:<team name>/<kalka_id>` in the release line and `kalka_moved_to:` in the stub. The source team shows "moved to <team>" in place of the document.
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
2. A new document is built: a fresh `kalka_id`; `kalka_team: <destination>`; `kalka_copied_from: <source team name>/<source kalka_id>`, or `kalka_moved_from:` for a move; `kalka_owner:` the actor for now; the plan of the source's head release (unreleased edits do not travel: a file that differs from its head release is refused until its edits are released or undone); every valid entry of the source, byte for byte, with their signatures.
3. The actor's `kalka` appends a signed `transfer` entry and writes the file to `<folder>/<destination>/.kalka/incoming/<new id>.md`, from where destination replication carries it.
4. For a move, a signed `transfer` entry is also appended to the source document, so the source team sees "being moved to <team>; comments added now will not travel". A copy leaves the source unchanged byte for byte.

**The `transfer` entry** is the actor's countersignature:

```
### transfer · ewa@example.org · 2026-12-01T10:00:00Z · § · rev:<8 hex> · id:<32 hex> · sig:<term>.<base64url>
move · from:parcel-ops/<source kalka_id> · source:<64 hex team hash> · to:library-desk/<64 hex team hash>
plan:<64 hex>
history:<64 hex> · entries: 7c2e91ab04f300000000000000000000, a41b77c09e1200000000000000000000
```

- The first body line is `copy` or `move`, the source document, and both teams by hash; `to` also names the destination, so source members who are not in it can read the banner. `plan` is the plan hash of the offered plan.
- `history` is SHA-256 over the lines `<64 hex digest of the entry's id input> <sig or "-">\n`, one per listed entry, in `(timestamp, id)` order. It covers the full digests, not the 48-bit ids, and the original signature text.
- `entries` lists the ids of the attested set, so every peer knows which entries the digest speaks for, also after members add their own.
- The entry's own signature, under the actor's term in the destination team, covers its body and the new `kalka_id`. So the actor signs the mode, both teams, the plan and the exact history.
- In the source document of a move, the same entry (with the source's `kalka_id`) records the start.

**Reaching the owners.** The pending document is part of the destination's replica: its inventory line is marked pending, and it is offered and fetched like a team document, so any destination member relays it and the actor can go offline once one other member holds it. It stays outside the index and is shown only to the team's owners (to decide) and to the actor (to watch or withdraw). A peer takes a pending document only if all of this holds:

- the `transfer` entry's signature verifies against a key the destination log binds to its author, who is a member;
- `to` is this team's hash and `plan` is the document's plan hash;
- the listed entries are present and hash to `history`;
- the document has no release.

Anything else is refused and listed in the Team view with the reason.

**Deciding.** Any destination team owner, in Needs you or with `kalka transfer accept|decline <id>`:

- **Accept** signs `accept-transfer {doc, owner, transfer}` (19.6), with `owner` the accepting owner unless `--owner <member>` names another member. The named owner's `kalka` then rewrites `kalka_owner:`, makes release 1, and moves the file from `.kalka/incoming/` into the team folder. From there it is an ordinary team document; its owner can hand it off to anyone in the team.
- **Decline** signs `decline-transfer {doc, transfer}`. Every peer replaces the pending file by a stub in `.kalka/tombstones/` holding the `transfer` entry only, so a lagging replica cannot bring the offer back; the full offer is kept in `.kalka/trash/<kalka_id>.offer.md`, so an accept that wins after all (below) restores it. A copy is thereby discarded. For a move, the actor's `kalka` appends a `transfer-withdrawn` entry answering the source's `transfer` entry (`re: <id>`, body `move declined`), and nothing else changes there.
- **Withdraw:** the actor signs a `transfer-withdrawn` entry on the pending document (`re: <id>`, body `withdrawn`). It has the effect of a decline unless an accept counts: the stub keeps the withdrawal, so it travels as one. For a move, the source gets `move withdrawn` in the same way.
- Concurrent decisions: an accept beats a decline or a withdrawal; of two accepts the first in replay order counts (19.4 rule 8). So there is never a second version 1.

**Attested entries.** Entries listed by an accepted transfer are *attested*. They are not checked against destination keys, take no part in the destination's release chain (source releases are shown as history, "v3 in parcel-ops"), and are shown as "from parcel-ops, attested by Ewa and Jan": the actor and the accepting owner. Before the accept the badge names the actor only. An attested entry never changes: one that no longer matches `history` is dropped with a warning, as an entry with a wrong id is. Entries written after the transfer are ordinary destination entries.

**The source tombstone.** For a move, the actor's `kalka` writes the tombstone (20.6) when its destination replica shows the effective accept. The trigger is derived from files: a live source document whose `transfer` entry of a move, by the local user, is not answered by a `transfer-withdrawn`, and an effective accept of that entry's digest in the destination log (the entry in the source and the offer's entry have one digest; only their signatures differ). It needs no state file and survives restarts. Until then the source is live and unchanged, with its banner.

- If the actor is offline, the tombstone waits for them; both documents exist meanwhile.
- If the actor can no longer sign it (the source was reassigned, or the actor removed, in the meantime), the move ends as a copy: the actor's `kalka` answers the entry with `move ended as a copy`. The actor gets a notice; the source's owner may delete it.
- A move withdrawn and then accepted all the same (an accept beats a withdrawal) also ends as a copy: the source's entry is already answered.
- Entries added in the source after the transfer started are not carried. They stay in the source and in its trash copy after the tombstone.

**Identity.** Copy and move both get a new `kalka_id`. A tombstone is final for its id on every peer of its team, so with the id kept a document could never move back; and a member of both teams would hold two live files with one id. The store indexes `moved-from` and `copied-from` (14), so a seed's `kalka_origin:`, a `kalka_parent:` and a `budded:` link still resolve to a moved document that is held.

**Domain documents and seeds.** `kalka_version:` restarts with release 1 in the destination; snapshots are not carried. An accept is refused (`domain-exists`) when the destination holds a document for that domain. A seed keeps `kalka_origin`; a plan node's children keep their `kalka_parent`, which resolves through `moved-from` while the moved parent is held.

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
- **Tray menu:** *Open kalka*; a status line ("Online · 4 of 6 members reachable · last sync 14:02", "Paused", or "Offline: rendezvous not reachable"); "N waiting for you" when any team has waiting items, opening the window on Needs you; *Pause sync* / *Resume sync*; *Start at login* with a check mark; *Quit kalka*.
- The status comes from `GET /api/summary`: distinct members reached out of all members of all teams; the latest replication round that reached a member; the waiting total and the oldest item's target (13). The tray refreshes on the daemon's events, at most once a second.
- **Quit** asks first: "kalka will stop syncing with your teams until you start it again." It then stops the daemon after its running rounds finish. When the shell is attached to a headless daemon, quitting stops that daemon too, and the question says so.
- A `kalka://` link opens the window on the join screen (19.7).

### 22.2 Settings

| Setting | Default | Meaning |
|---|---|---|
| Start at login | on, set once at the shell's first start | A per-user entry, no admin rights: Windows `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, macOS a LaunchAgent in `~/Library/LaunchAgents`, Linux an XDG autostart file. The entry starts the shell with `--hidden`, or `kalka daemon --background` where no shell is installed. The system's entry is the truth; it is not copied into `state.yaml`. |
| Keep running when closed | on | Closing the window hides it. The first time, one notification says that kalka keeps syncing and where its icon is. Off: closing the window quits the shell, and an in-process daemon stops with it. |
| Notifications | on | One switch for the notices of 22.4. Waiting items still show in the window when it is off. |
| Install updates automatically | on | 24.2. |
| Pause | off | 22.3. |

- `--hidden` starts the shell with no window shown; autostart passes it. Starting the shell again always shows the window.
- On Linux without a tray host (stock GNOME without an extension), closing minimizes instead of hiding, so the window stays reachable.
- `kalka settings` changes the same settings from the CLI (12.1).
- The rendezvous server is not in `state.yaml`: it is `config.yaml`'s, and Settings changes it live (9.1); the email and the folder change only with `kalka setup`.

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
| A kalka update is ready | An update was downloaded and verified (24.2). One notice per version; a click opens Settings. |

On macOS, an unsigned build cannot post to the notification centre; notices show only in the window. In a browser tab the page's own browser notifications remain; in the desktop window they are not used. `kalka daemon --no-notifications` shows none.

---

## 23. Platforms and distribution

### 23.1 What is installed

| Platform | Package | Contents |
|---|---|---|
| Windows (primary) | Per-user installer, no elevation, into `%LOCALAPPDATA%\Programs\kalka`; adds the folder to the user's `PATH`, a Start-menu shortcut, and the `kalka` URL scheme; its last page offers *Start kalka* and *Add sample blueprints to Personal* (`kalka samples`) | `kalka.exe` (console: CLI and headless daemon), `kalka-desktop.exe` (GUI: the shell; with arguments it runs the CLI without a console window), `kalka-plugin-gh.exe` |
| macOS | `Kalka.app` in `~/Applications`, or anywhere | One executable, which is the shell without arguments and the CLI with them; `kalka` on the `PATH` is a symlink to it |
| Linux | One archive | `kalka` (static), `kalka-desktop`, a `.desktop` file and an icon, and the list of system packages the shell needs |
| Servers | `kalka-rendezvous_<v>_<os>_<arch>` archive | `kalka-rendezvous` (9.6) |

The headless archives of 15 remain for servers, containers and machines without a desktop.

The Windows installer changes the user's `PATH` only when it can read the whole value and the result fits in an installer string (1024 characters): otherwise it leaves `PATH` as it is and says to add the folder by hand. A `PATH` it could not read is never written back. The uninstaller removes its folder under the same rule.

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

---

## 24. Updates

The desktop app updates itself from the GitHub releases of the project; nothing is installed without a verified checksum, and nothing restarts without a click.

### 24.1 Looking for a release

- The daemon lists the releases (`GET /repos/<owner>/<repo>/releases` on the GitHub API) 30 seconds after it starts and every 6 hours, and on *Check for updates* or `kalka update`. The answer is cached by its ETag; a failed check (no network, GitHub's rate limit) is logged and shown in Settings, never fatal.
- A build whose version is `dev` never checks. Pre-releases (a hyphen in the version) are ignored unless the running build is itself a pre-release.
- **Policy**, caret-style as npm's `^`: a release is *compatible* with the running version when it has the same major version from 1.0.0 on, or, below 1.0.0, the same minor version (0.1.1 to 0.1.5, not to 0.2.0). Of the releases newer than the running one the newest compatible one is offered; with none, the newest of all, marked not compatible.
- The package is `kalka_<v>_windows_<arch>_setup.exe`, `kalka_<v>_darwin_<arch>_app.zip` or `kalka_<v>_linux_<arch>_desktop.tar.gz`. A release without the package for this system is announced and not installed.

### 24.2 Download

- With *Install updates automatically* on (the default, `auto_update` in `state.yaml`), a compatible update is downloaded in the background to `<config>/updates/<version>/` and verified against the `SHA256SUMS` of the same release; a release without that file, or a package that does not match, is refused and removed. It is then *ready*: Settings and the tray say *Restart to update*, and one notice per version is raised (22.4).
- With the setting off, or for an update that is not compatible, the update is only *available*: Settings and the tray show it with a link to the release notes and a *Download and restart* button. Nothing is downloaded before the click.
- `GET /api/update` is `{current, checks, latest, state, compatible, notes_url, can_install, checked_at, error}`; `state` is `none`, `available`, `downloading`, `ready` or `error`. `POST /api/update/check` checks now; `POST /api/update/install` downloads if need be, installs and restarts. The settings API carries `auto_update`.

### 24.3 Installing

Only the desktop shell installs, and only an installation it recognises; anything else (the command line or headless daemon, a package manager, a folder it cannot write) is told about the release and never written to (`can_install` is false).

| System | Recognised as | Install |
|---|---|---|
| Windows | `kalka-desktop.exe` beside `uninstall.exe` (the installer's folder) | The downloaded setup runs with `/S`. A silent install waits until the running `kalka-desktop.exe` can be replaced, installs, and starts kalka again. |
| macOS | The program is inside `Kalka.app`, whose folder is writable | The new `Kalka.app` is unpacked beside the old one, the old one renamed aside, the new one renamed in, the old one removed; a failed swap puts the old one back. kalka is opened again once this process has ended. |
| Linux | `kalka-desktop` with `kalka` and `kalka-plugin-gh` beside it, in a writable folder | The three programs are written beside the old ones and renamed over them, then `kalka-desktop` starts again once this process has ended. |

Installing stops the daemon the usual way: running replication rounds finish first, then the shell ends.

### 24.4 Tray

The tray menu has one more item while there is something to offer: *Restart to update to X* (ready), *Download X and restart*, or *kalka X is available* (opens Settings when this kalka cannot install it).
