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
**transfers** of documents between teams, and a **waiting list** of what
only you can move forward.

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

In the app, the first window is the setup form: your email, the rendezvous
(`peerjs-cloud` to try it out), and your blueprint folder. Then *Create
team…* in the switcher, *Invite* on the Team page, and send the
`kalka://join/…` link to a teammate.

The same from the command line:

```sh
# You
kalka setup --email ewa@example.org --rendezvous peerjs-cloud --folder ~/blueprints
kalka team create kalka-core
kalka team invite marek@example.org        # prints a kalka://join/… link; send it any way you like

# Your teammate, after their own setup
kalka join 'kalka://join/…'

# You: share a blueprint, then release your edits
kalka publish ~/blueprints/personal/push-during-round.md --team kalka-core
kalka release ~/blueprints/kalka-core/push-during-round.md -m "Keep the flag for a paused daemon"

# Your teammate: review it
kalka comment ~/blueprints/kalka-core/push-during-round.md --anchor plan -m "What if the round fails halfway?"

# Either of you: what needs you
kalka waiting
```

`samples/` holds blueprints to try this with, about kalka's own code ([samples/README.md](samples/README.md)).

## For agents

- `kalka reference export` writes the **blueprint reference**: a short
  guide, in the form of an agent skill, to writing blueprints agents can
  act on safely (scope, verification plan, critical sections, operations,
  the deviation log). A blueprint opts in with `reference:
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

## Documentation

| Document | What it covers |
|---|---|
| [specification.md](specification.md) | What kalka does and why: file format, ownership, trust, transport, teams, replication, UI, CLI, API, threat model, packaging |
| [docs/install.md](docs/install.md) | Installing on Windows, macOS and Linux |
| [docs/headless.md](docs/headless.md) | Running without the desktop app, for servers and agents |
| [docs/rendezvous.md](docs/rendezvous.md) | Hosting `kalka-rendezvous`, with TURN and TLS |
| [docs/teamlog.md](docs/teamlog.md) | The team log's encoding and replay rules |
| [docs/peerjs.md](docs/peerjs.md) | How the PeerJS servers behave where kalka depends on it |
| [docs/desktop-testing.md](docs/desktop-testing.md) | The manual test pass of the desktop app |
| [samples/README.md](samples/README.md) | Sample blueprints for trying kalka |

## Building from source

You need Go (the version in `go.mod`) and Node.js 24. The desktop app also
needs a C toolchain on macOS and Linux, and on Linux the GTK 4 and
WebKitGTK 6.0 development packages (`libgtk-4-dev`,
`libwebkitgtk-6.0-dev`).

```sh
make web          # build the web UI into internal/ui/dist (embedded in the binary)
make build        # kalka and kalka-plugin-gh
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
| `internal/blueprint`, `internal/merge` | The file format, signed entries, releases and merging |
| `internal/teamlog` | Team logs: operations, replay, invites |
| `internal/core` | The node: teams, replication, ownership, transfers, seeds, domains |
| `internal/daemon`, `internal/cli` | The local HTTP API and the commands |
| `internal/transport`, `internal/peerjs`, `internal/rendezvous` | WebRTC, signaling and the server side |
| `web` | The web UI (React, TypeScript), served by the daemon |
| `tools/release` | The reproducible release build |
| `samples`, `testdata` | Sample blueprints, and golden files for the format |

## License

[MIT](LICENSE)
