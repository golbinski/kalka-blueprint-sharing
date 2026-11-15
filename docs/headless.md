# kalka without the desktop app

The desktop app is a window and a tray icon around the kalka daemon. Everything else (sync, teams, replication, prompts, notices) is the daemon's, and it runs fine on its own:

- on servers, containers and machines without a desktop;
- where IT policy blocks autostart entries, tray apps or the webview;
- for agents and scripts, which only need the command line.

Use the headless archive for your system (`kalka_<v>_<os>_<arch>.tar.gz`, or the Windows `.zip`). It holds `kalka` and `kalka-plugin-gh`, plus `kalka-desktop.exe` on Windows.

## Running the daemon

| How | Command | Notes |
|---|---|---|
| In a terminal | `kalka daemon` | Prompts are asked in the terminal too (answer y or n). Ctrl+C stops it after running rounds. |
| In the background, now | `kalka` | Starts `kalka daemon --background` detached if none runs, then opens the page in the browser. |
| At login, no window | `kalka autostart on --headless` | macOS: a LaunchAgent; Linux: `~/.config/autostart`; Windows: the Run key, starting the console-less `kalka-desktop.exe daemon --background`. |
| From anything else | `kalka daemon --background` | A login script, a scheduler, a container entry point. Output goes to `kalka.log` in the config folder. |

A second start finds the running daemon and exits with status 0, so starting it from several places is safe. `kalka stop` stops it; `kalka status` shows its state.

The command line works with or without a running daemon: reading commands (`status`, `peers`, `team members`, `domains`, `check`, `comment`, `seed`) read the files, and commands that need the network start the daemon in the background unless told not to (`--no-start`, or `KALKA_NO_START=1`), which agents in sandboxes should use.

## The page

`kalka open` opens the web UI in the browser; `kalka open --print` prints its address with the session token, for opening it elsewhere on the same machine. It is the same UI the desktop window shows.

Options for the daemon:

- `--no-popups`: no native dialogs or notifications; prompts wait in the page and the terminal.
- `--no-ui`: no web UI at all (page paths answer 404); the local API stays up for the CLI. Implies `--no-popups`.

Without a page open, prompts reach you as notification-centre toasts on Windows (registered by `kalka autostart on`), dialogs on macOS, and `zenity` dialogs on Linux; otherwise kalka opens the browser at most once a minute.

## What IT needs to allow

- Outgoing WebSocket (`wss://` or `ws://`) to the team's rendezvous server.
- WebRTC to peers: UDP, or TURN over TCP/TLS on 443 when UDP is blocked (`kalka rendezvous` hands out short-lived TURN credentials; other rendezvous servers need their own TURN).
- A loopback TCP port for the local API and page (default 7717, `ui_port` in `config.yaml`). It listens on 127.0.0.1 only and needs a per-session token.
- Write access to the config folder (`%APPDATA%\kalka`, `~/.config/kalka`) and the blueprint folder.

No administrator rights, services, inbound ports or firewall exceptions are needed.
