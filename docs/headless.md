# kalka without the desktop app

The desktop app is a window and a tray icon around the kalka daemon. Everything else (sync, teams, replication, the waiting list, notices) is the daemon's, and it runs fine on its own:

- on servers, containers and machines without a desktop;
- where IT policy blocks autostart entries, tray apps or the webview;
- for agents and scripts, which only need the command line.

Use the headless archive for your system (`kalka_<v>_<os>_<arch>.tar.gz`, or the Windows `.zip`). It holds `kalka`, `kalka-plugin-gh` and `kalka-plugin-jira`, plus `kalka-desktop.exe` on Windows.

## Running the daemon

| How | Command | Notes |
|---|---|---|
| In a terminal | `kalka daemon` | Its log goes to the terminal. Ctrl+C stops it after running rounds. |
| In the background, now | `kalka` | Starts `kalka daemon --background` detached if none runs, then opens the page in the browser. |
| At login, no window | `kalka autostart on --headless` | macOS: a LaunchAgent; Linux: `~/.config/autostart`; Windows: the Run key, starting the console-less `kalka-desktop.exe daemon --background`. |
| From anything else | `kalka daemon --background` | A login script, a scheduler, a container entry point. Output goes to `kalka.log` in the config folder. |

A second start finds the running daemon and exits with status 0, so starting it from several places is safe. `kalka stop` stops it; `kalka status` shows its state.

The command line works with or without a running daemon (spec §12). Views (`status`, `teams`, `team members|log|invites`, `waiting`, `seeds`, `transfers`, `domains`, `settings`) read the local files and start nothing; their JSON says `"daemon": false`. File commands (`check`, `comment`, `seed`, `seed backlink`, `reference`) never need it. Commands that write the team log, a release or a transfer start the daemon in the background unless told not to (`--no-start`, or `KALKA_NO_START=1`), which agents in sandboxes should use. Every command takes `--json`; `kalka waiting --json` is what an agent polls for work that waits on the user.

## The page

`kalka open` opens the web UI in the browser; `kalka open --print` prints its address with the session token, for opening it elsewhere on the same machine. It is the same UI the desktop window shows.

Options for the daemon:

- `--no-notifications`: no OS notifications; notices show only in the page.
- `--no-ui`: no web UI at all (page paths answer 404); the local API stays up for the CLI. Implies `--no-notifications`.

Without a page open, notices (new comments on your documents, joins, newer domain versions) reach you as notification-centre toasts where the system has them for kalka (Windows, after `kalka autostart on`). Nothing waits for an answer: kalka never asks a question during replication.

## Plugins without a window

`kalka plugins` lists the installed plugins and their state; with kalka
running it reports what the running kalka sees, which is what resolves
references (its `PATH`, its access to the credential store). Set a plugin up
with `kalka plugin set`, the secret on standard input:

```
printf '%s\n' "$TOKEN" | kalka plugin set jira site=https://example.atlassian.net email=ewa@example.org --secret-stdin token
```

A secret is never taken as an argument, so it stays out of the shell history
and the process list. On a machine with no credential store (a server without
a Secret Service), the Jira plugin keeps the token in its own `config.json`,
readable by you only; `kalka plugin settings jira` says which. When kalka runs
in a session without the credential store your shell has, `kalka plugin set`
warns that the running kalka sees the plugin otherwise. `kalka doctor` has a
`plugin:<scheme>` row for each plugin; one that is not set up is fine.

## What IT needs to allow

- Outgoing WebSocket (`wss://` or `ws://`) to the team's rendezvous server.
- WebRTC to peers: UDP, or TURN over TCP/TLS on 443 when UDP is blocked (`kalka-rendezvous`, the server a team hosts, hands out short-lived TURN credentials; other rendezvous servers need their own TURN; see [rendezvous.md](rendezvous.md)).
- Outgoing HTTPS to `api.github.com`, `github.com` and `*.githubusercontent.com`, where kalka looks for and downloads new releases (spec §24); without it kalka only logs that the check failed. With the Jira plugin, HTTPS to your Jira site.
- A loopback TCP port for the local API and page (default 7717, `ui_port` in `config.yaml`). It listens on 127.0.0.1 only and needs a per-session token.
- Write access to the config folder (`%APPDATA%\kalka`, `~/.config/kalka`) and the blueprint folder.

No administrator rights, services, inbound ports or firewall exceptions are needed.
