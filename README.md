# kalka

**Review implementation plans with your team, peer to peer.**

A *blueprint* is a Markdown file describing an implementation plan, written
by a person or an agent with whatever tools they like. kalka passes it
between the members of a team, collects their comments inside the same
file, and keeps track of who owns it. It is a pull request for a plan:
without a server that stores anything, without accounts, and without
changing how anyone writes.

*Kalka* is Polish for tracing paper.

> **Status:** pre-1.0. It runs on Windows, macOS and Linux. Until version 1
> the wire protocol and the file formats may change between releases
> without a migration.

## How it works

1. **Write a plan** in any editor, or let an agent write it: a Markdown
   file in your kalka folder's `personal/` area. Nobody sees it yet.
2. **Publish it to your team.** Every teammate gets a copy, kept in step
   directly between your machines.
3. **Read their comments beside the text.** Teammates comment on a
   section, or record a *deviation* when the code did something
   differently. You edit the plan and release the next version.

*Needs you* lists what only you can move forward, across your teams: a
blueprint that became yours, an offer to decide on, comments you have not
read yet.

### Under the hood

- **Drop a file in a folder.** Your kalka folder has a `personal/` area
  that never leaves your machine, and one folder per team you belong to.
  Publishing a blueprint moves it from Personal into a team.
- **Every member holds a replica.** Team documents travel directly between
  members' machines over WebRTC, through whoever is online, so nobody has
  to be online for anyone else to read.
- **The owner changes the plan; everyone else adds entries.** Comments and
  deviations are signed entries in a `## Review log` at the end of the
  file. The owner edits the body with their own tools and *releases* a new
  version; unreleased edits do not travel.
- **Teams are signed logs.** Each team has a log of signed operations:
  members, owners, reassignments, transfer decisions. Every member replays
  it and reaches the same roster. There are two roles, team owner and
  member, and team owners can reassign any document.
- **The only server is a rendezvous point.** It introduces machines to
  each other and relays traffic when a direct connection fails. It keeps
  no state, sees no content (everything is end to end encrypted) and
  cannot impersonate anyone. Use the public PeerJS cloud to try kalka, or
  run `kalka-rendezvous` on your own host.

Beyond review: **seeds** (ideas split off a growing plan into the team's
pre-backlog), **domain documents** (a team's shared vocabulary, versioned,
referenced from blueprints), **trace blocks** (a process at business level
and in code side by side, with kalka pointing out where they diverge),
**transfers** of documents between teams.

## Install

Download the installer or archive for your system from the
[releases](https://github.com/golbinski/kalka-blueprint-sharing/releases)
page. [docs/install.md](docs/install.md) has the steps for each system.

- **Desktop app:** a window plus a tray (or menu bar) icon. It starts at
  login and keeps syncing in the background.
- **Command line and headless:** every workflow of the app is also a
  command, with `--json` output. It runs on servers and in containers, and
  for agents ([docs/headless.md](docs/headless.md)).

## Quick start

**You**

1. Install kalka and open it. The setup form asks for your email, your
   team's kalka server (the *rendezvous*; choose the public demo server to
   try kalka) and your blueprint folder.
2. *Create team…* in the switcher at the top left.
3. On the Team page, enter a teammate's email and *Copy invite message*.
   Paste it into any chat: it says how to install kalka and holds the
   invite.

**Your teammate**

1. Installs kalka and clicks the link in your message (or pastes the
   code under *Join with an invite…*).
2. Chooses a folder and clicks *Save and join*. They are in as soon as
   your kalka is running.

**Then**

- You drop a plan into `personal/`, open it and *Publish to team…*.
- Your teammate finds it under *Needs you*, comments beside the text.
- You edit the file and *Release v2*.

The same from the command line:

```sh
# You
kalka setup --email ewa@example.org --rendezvous peerjs-cloud --folder ~/blueprints
kalka team create kalka-core
kalka team invite marek@example.org        # prints a kalka://join/… link; send it any way you like

# Your teammate, after their own setup
kalka join 'kalka://join/…'               # waits for your kalka; `kalka join --list` shows progress

# You: share a blueprint, then release your edits
kalka publish ~/blueprints/personal/push-during-round.md --team kalka-core
kalka release ~/blueprints/kalka-core/push-during-round.md -m "Keep the flag for a paused daemon"

# Your teammate: review it
kalka comment ~/blueprints/kalka-core/push-during-round.md --anchor plan -m "What if the round fails halfway?"

# Either of you: what needs you
kalka waiting
```

`samples/` holds blueprints to try this with, about kalka's own code ([samples/README.md](samples/README.md)).

## Documentation

| Document | What it covers |
|---|---|
| [specification.md](specification.md) | What kalka does and why: file format, ownership, trust, transport, teams, replication, UI, CLI, API, threat model, packaging |
| [docs/install.md](docs/install.md) | Installing on Windows, macOS and Linux |
| [docs/headless.md](docs/headless.md) | Running without the desktop app, for servers and agents |
| [docs/rendezvous.md](docs/rendezvous.md) | Hosting `kalka-rendezvous`, with TURN and TLS |
| [docs/teamlog.md](docs/teamlog.md) | The team log's encoding and replay rules |
| [docs/peerjs.md](docs/peerjs.md) | How the PeerJS servers behave where kalka depends on it |
| [samples/README.md](samples/README.md) | Sample blueprints for trying kalka |

## For agents

- `kalka reference export` writes the **blueprint reference**: a short
  guide, in the form of an agent skill, to writing blueprints agents can
  act on safely (scope, verification plan, critical sections, operations,
  the deviation log). A blueprint opts in with `kalka_reference:
  kalka-blueprint@v1` and then gets soft hints such as "no verification
  plan".
- Every command takes `--json`, and errors carry a stable `kind`.
  `kalka waiting --json` is what an agent polls.
- An agent that is not acting for the owner adds entries
  (`kalka comment`, `--kind deviation`) instead of editing the body.

## Plugins

A link such as `[handler](gh:src/upload/handlers.py#L40-L120)` is a
*reference*. A plugin named `kalka-plugin-<scheme>` resolves it, with the
reader's own credentials, into a snippet shown beside the text. Resolved
content is never written into the file or sent to anyone. kalka ships
`kalka-plugin-gh` for GitHub; the protocol is in the specification (§10).

The repository and commit come from the blueprint's frontmatter (`repo`,
`base_sha`). A plan that spans several repositories names the others under
`repos:` and picks one per link: `[form](gh:web:src/upload/Form.tsx#L10-L60)`.

### Jira

`kalka_issue: jira:DEV-123` in the frontmatter names the issue a blueprint
addresses. With `kalka-plugin-jira login` done, the document shows the
issue's title, status and parents, the Blueprints list groups by parent or
status, and the released version can be exported to the issue (attachment
and comment), after which the blueprint can be archived. Setup and limits:
[docs/jira.md](docs/jira.md).

## Building from source

You need Go (the version in `go.mod`) and Node.js 24. The desktop app also
needs a C toolchain on macOS and Linux, and on Linux the GTK 4 and
WebKitGTK 6.0 development packages (`libgtk-4-dev`,
`libwebkitgtk-6.0-dev`).

```sh
make web          # build the web UI into internal/ui/dist (embedded in the binary)
make build        # kalka and the plugins (kalka-plugin-gh, kalka-plugin-jira)
make desktop      # kalka-desktop, the desktop shell, for this system
make rendezvous   # kalka-rendezvous
make test         # web tests, then go test -race ./...
make release VERSION=x.y.z   # every artifact this system can build, into dist/
```

The desktop shell is behind the `desktop` build tag, so `go build ./...`
and `go test ./...` never need its toolchain. Its tests run with
`go test -tags desktop ./cmd/kalka-desktop ./internal/desktop`. Tests
that reach the public PeerJS cloud or GitHub are off unless
`KALKA_CLOUD_TEST=1` or `KALKA_GH_LIVE=1` is set.

CI ([.github/workflows/ci.yml](.github/workflows/ci.yml)) runs the web
checks, `go vet` for every system, and the tests with the race detector on
Linux, macOS and Windows. A `v*` tag builds the release.

### Layout

| Path | What |
|---|---|
| `cmd/kalka` | The CLI and headless daemon: one static binary, no cgo |
| `cmd/kalka-desktop` | The desktop shell (Wails v3): window, tray, notifications |
| `cmd/kalka-rendezvous` | The rendezvous server: PeerJS-compatible signaling and TURN |
| `cmd/kalka-plugin-gh` | The GitHub reference plugin |
| `cmd/kalka-plugin-jira` | The Jira plugin: issue metadata and export |
| `internal/blueprint`, `internal/merge` | The file format, signed entries, releases and merging |
| `internal/teamlog` | Team logs: operations, replay, invites |
| `internal/core` | The node: teams, replication, ownership, transfers, seeds, domains |
| `internal/daemon`, `internal/cli` | The local HTTP API and the commands |
| `internal/transport`, `internal/peerjs`, `internal/rendezvous` | WebRTC, signaling and the server side |
| `web` | The web UI (React, TypeScript), served by the daemon |
| `tools/release`, `tools/winicon` | The reproducible release build; the Windows icon resources of the desktop app |
| `samples`, `testdata` | Sample blueprints, and golden files for the format |

## License

[Apache License 2.0](LICENSE); see also [NOTICE](NOTICE).
