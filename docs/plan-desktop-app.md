# Plan: kalka as a background desktop app (Wails)

Triage and implementation plan for the feature request "turn kalka into a background desktop app (Wails)". Written 2026-11-14 against `specification.md` v0.4 and the code on `main` (0daece9). Nothing here is implemented. No package was downloaded for this plan: the Wails findings come from release notes, the documentation and the source at tag `v3.1.0`, and nothing was compiled or run.

`§n` points into `specification.md`. `IP §n` points into `implementation-plan.md`. Code is cited as `path:line`.

Contents: 1 summary, 2 triage, 3 Wails v2 against v3, 4 decisions, 5 milestones, 6 order and size, 7 risks and how to reduce them, 8 questions for the product owner.

---

## 1. Summary

The request is smaller than it reads. kalka already is a background daemon with a thin browser view. What is new is a window, a tray, a lock, a local socket and a second build. About 7 weeks of work, most of it in two places: the desktop shell itself (D6) and packaging for three systems (D7).

**Step 0 finding: nothing has to move.** All networking and all sync logic are in Go today.

- The front end has no WebRTC, PeerJS or WebSocket code. A search of `web/src` for `RTCPeerConnection`, `RTCDataChannel`, `PeerJS`, `new Peer` and `WebSocket` finds nothing. Its only network calls are `fetch` to the daemon (`web/src/api.ts:340`, `:478`) and one `EventSource('/api/events')` (`api.ts:519`). Its dependencies are `markdown-it`, `mermaid`, `react` and `react-dom` (`web/package.json`).
- WebRTC is pion in `internal/transport` (`endpoint.go`, `conn.go`, `ice.go`, `presence.go`). Signaling is `internal/peerjs/client.go`. The protocol is `internal/syncproto`. The loops (two signaling registrations, join retries, replication rounds, presence probes, folder watcher) start in `Daemon.Run` (`internal/daemon/daemon.go:229-273`) and do not depend on any connected page.
- The scenario tests already run daemons with the API switched off (`Options.APIAddr = "-"`, `daemon.go:63-65`), so "sync with no window" is the normal test setup.

Three things in the page are more than a pure view. None of them is sync, and none blocks the request:

| What | Where | Effect |
|---|---|---|
| A sync started from the UI or CLI lives as long as its HTTP request | `internal/daemon/api.go:253-263` (`d.Sync(ctx, …)` with the request's context) | Destroying the page in the middle of a manual `send` cancels that one sync. Background replication is not affected. Kept as it is (D-note in section 4.8). |
| Browser notifications | `web/src/notify.ts`, permission banner in `web/src/App.tsx:32`, `:285` | Works only in a browser tab. The desktop shell replaces it with native notifications. |
| "Is a page open?" decides whether the daemon shows a popup | `internal/daemon/popups.go:40` (`bus.Clients() > 0`) | With a hidden window the page stays connected, so this test must become "is a window visible". |

Six findings shape the plan.

1. **Wails v3 is the choice, and it is one week past its first stable release.** v3.0.0 came out on 2026-10-26 and v3.1.0 on 2026-11-01. It has a tray, a close hook, hidden start, single instance, autostart and notifications built in. Wails v2 has no tray at all. Section 3 has the comparison and the sources.
2. **The page must be loaded from the loopback HTTP port, not through Wails' asset server.** On Windows the Wails asset server hands a response to WebView2 only when it is complete. The UI's live event stream and the sync progress stream never complete. Windows is the primary platform, so this decides the design (T6, D2).
3. **So the socket and the TCP port both exist.** The CLI moves to a Unix socket with user-only permissions. The window (and an optional browser tab) keep `127.0.0.1` with the token, `Host` and `Origin` checks that already exist. The request allows exactly this.
4. **"Single binary per platform" cannot hold for the desktop.** The desktop build needs cgo and system webview libraries on macOS and Linux. A binary linked against WebKitGTK does not start on a machine without it, and the CLI must work there. The plan is two binaries from one source tree: `kalka` (pure Go, static, as today) and `kalka-desktop` (T14, D9).
5. **"The CLI works on local files for read-only commands" is not what the spec says and should not become it.** `kalka comment` and `kalka seed` write files without a daemon today, safely, by compare-and-swap (§14). That must stay, because agents depend on it. What must never run without the daemon is anything that writes the team log or the config directory (T5, D4).
6. **There is no lock and no `kalka daemon` command today.** A second start fails only because the TCP port is taken (`cmd/kalka/net.go:166-176`). Two daemons with different `ui_port` values can share one config directory and one folder (T4).

There are no blockers. Seven points need a product-owner answer (section 8). The largest uncertainty is not design: nothing kalka-specific has ever run on a real Windows desktop (IP §6h), and the repository has no remote, so CI has never run.

---

## 2. Triage

Severity: **conflict** = the request contradicts the spec or itself, someone must choose; **decision** = workable, but open; **note** = worth knowing, no choice needed.

State: **done**, **partly**, **new**.

### 2.1 Overview

| # | Requirement | State | Severity |
|---|---|---|---|
| T1 | Step 0: networking and sync in Go, front end a pure view | done | note |
| T2 | One core and three thin shells | partly | decision |
| T3 | `kalka daemon` as a command | partly (bare `kalka` is the daemon) | note |
| T4 | Exactly one daemon, enforced with a lock | new | decision |
| T5 | CLI starts a daemon, or works on local files for read-only commands | partly | conflict |
| T6 | Socket or pipe instead of TCP, while the SPA must still reach the webview | new | conflict |
| T7 | Token and `Origin` on a kept TCP port, loopback only | done, with three gaps | note |
| T8 | Tray icon with status, pause, quit | new | decision |
| T9 | Start at login, on by default | partly | decision |
| T10 | Keep running when closed, one-time notice | new | note |
| T11 | Notifications: handoff, comments, invite join, newer domain version | partly | conflict (one trigger) |
| T12 | Start minimized under autostart | partly | note |
| T13 | Wails v2 or v3 | — | decision (section 3) |
| T14 | Single binary per platform, CLI works without the desktop shell | — | conflict |
| T15 | No bundled Node.js or Chromium | done | note |
| T16 | WebView2 missing: a clear message | new | note |
| T17 | Per-user install, no admin rights | done for archives | note |
| T18 | Code signing as a configuration step | new | decision |
| T19 | Restricted environments: headless `kalka daemon` | partly | note |
| T20 | Build per platform: cgo, cross-compiling, CI | new | decision |
| T21 | "One daemon per user per machine" against `KALKA_CONFIG_DIR` | — | note |
| T22 | Tray status: "last sync" and "pending items" are not defined | new | decision |
| T23 | Pause and resume | new | decision |
| T24 | Smaller conflicts with the spec | — | note |

### 2.2 Architecture and the daemon

**T1. Step 0 (note).** See section 1. The work the request reserves for "move networking into Go" is zero.

**T2. Core and shells (partly done).** The lower packages are already a core with no UI code: `blueprint`, `merge`, `teamlog`, `identity`, `peerid`, `store`, `syncproto`, `transport`, `peerjs`, `reference`, `plugin`. Two places mix layers:

- `internal/daemon` holds the orchestration (sync, teams, join, publish, replication, domains, seeds, document model: about 5,000 lines) together with the HTTP API (`api.go`, `teams_api.go`), the gate and static files (`web.go`), the client (`client.go`) and the popup logic (`popups.go`). It imports `net/http` and `internal/ui`.
- `cmd/kalka` is `package main` with about 2,000 lines of command code. A second executable cannot reuse it.

The split is mostly moving files, but the tests of `internal/daemon` (about 3,300 lines) reach into unexported fields (`manualReplication`, `wrapStore`, `wrapChan`, `daemon.go:80-84`, `:128-133`), so it is not free. See D1.

**T3. `kalka daemon` (partly done).** The command does not exist. The daemon is what `kalka` does with no arguments or with only flags (`cmd/kalka/main.go:45-46`, `cmdDaemon` at `cmd/kalka/net.go:42`). `--no-open` suppresses the browser and popups, `--background` is the logon mode with a log file (`net.go:45-46`, IP §6h). The headless path the request asks for exists under another name.

**T4. Single daemon (new, decision).** There is no lock anywhere in the code. What exists:

- A second start fails on `net.Listen` with "is kalka already running?" (`net.go:171-173`, `daemon.go:240-242`). This holds only when both use the same port. A changed `ui_port`, or `APIAddr: "-"`, gives two daemons on the same config directory, the same `peers.yaml`, `invites.yaml`, `joins.yaml` and the same team logs. The YAML lists are guarded by an in-process mutex only (`internal/daemon/teams.go:37-85`).
- `daemon.json` is removed on a clean exit only (`api.go:40-48`). After a crash it stays, `daemon.Connect` returns a client (`client.go:30-43`), and every call then reports "not running" (`client.go:62-64`). That is harmless today and wrong once the CLI starts daemons by itself.
- The macOS LaunchAgent has no `KeepAlive` precisely because a second, hand-started kalka would make launchd restart in a loop (`internal/autostart/autostart_darwin.go:40-41`). A lock that makes the second start exit cleanly removes that reason.

**T5. CLI with and without a daemon (conflict).** The request says: "If no daemon is running, the CLI either starts one (default) or, for read-only commands, works on the local files directly."

- *Starting one* is new. Today a network command prints "kalka is not running" and fails (`net.go:342-351`, `client.go:20`).
- *"Read-only commands work on local files"* does not match the code or §12. The file commands are `check`, `comment`, `seed`, `seeds`, `reference` and `version`. Two of them write: `kalka comment` (`cmd/kalka/main.go:318-407`) and `kalka seed` (`cmd/kalka/seed.go:43`) append to blueprints with no daemon involved, whether one is running or not. They are safe because §14 is not a single-writer model. Every writer, the daemon included, re-reads the file and replaces it atomically only if it is unchanged (`fsutil.WriteAtomic(path, out, raw)`, `main.go:393`), and the daemon's watcher picks the change up. An editor saving the same file is one more such writer.
- If the request were taken literally, `kalka comment` would need a daemon. Agents and factory skills run it in sandboxes and CI, where starting a background process with network access is unwanted or forbidden. That would break "the CLI stays a first-class interface".
- The real single-writer state is elsewhere: the team log, `invites.yaml`, `joins.yaml`, `peers.yaml`, release and tombstone writes. Those must go through the one daemon. They already do (§12 marks them "yes").

So the rule is by what a command touches, not by read against write. See D4.

**T21. Per user or per config directory (note).** `KALKA_CONFIG_DIR` (`internal/config/config.go:19-20`) gives a second identity on one machine. Tests use it, and so do the two-daemon checks by hand (IP §6r, §6s). "Exactly one daemon per user per machine" therefore becomes "exactly one daemon per config directory", which is the same thing for a normal user.

### 2.3 Local API

**T6. Socket against webview (conflict).** The request prefers a named pipe or Unix socket over TCP, and it wants the existing SPA in a Wails window. A webview cannot load a page from a Unix socket. There are three ways to get the SPA and its API into the window:

| Way | How | Verdict |
|---|---|---|
| Wails bindings | Replace `fetch` with generated Go bindings and Wails events | Rejected. The SPA would exist in two variants (browser and desktop), or the browser UI is dropped. About 35 API functions in `web/src/api.ts` would be rewritten. |
| Wails asset server | Give Wails the daemon's `http.Handler`; the page is served on `http://wails.localhost` (Windows) or a custom scheme | Rejected. On Windows "the WebView2 response writer accumulates the entire body in memory and only hands it over in `Finish`" (Wails `streams-internals.md` at v3.1.0). `GET /api/events` (server-sent events, `internal/daemon/web.go:200-228`) and `POST /api/sync` (NDJSON progress and prompts, `api.go:236-291`) never finish, so the page would get no live updates and no sync progress on the primary platform. WebView2's own tracker says the same of intercepted requests (WebView2Feedback #3519). The `Host` check (`web.go:52-58`) and the Mermaid frame's CSP (`web.go:36-40`) are also written for a loopback origin. |
| Loopback HTTP | The window opens `http://127.0.0.1:<port>/?token=…`, exactly as `kalka open` makes a browser do | Chosen. The SPA does not change. Real network requests stream on every platform. The gate, CSP and cookie work as they do today. |

The cost is that a TCP port stays open on loopback. The request covers this case ("if a localhost HTTP port is kept, e.g. for the SPA"). See D2.

**T7. Is the TCP hardening already done? (note)**

| Requirement | State | Evidence |
|---|---|---|
| Loopback only | Done | `net.go:170`, `daemon.go:236` |
| `Host` check against DNS rebinding | Done | `web.go:52-58`, `:85-88` |
| Per-session random token | Done | 32 random bytes per start (`net.go:68-70`); bearer for the CLI, `HttpOnly` `SameSite=Strict` cookie for the page (`web.go:94-121`) |
| `Origin` check | Done for state-changing requests with a cookie | `web.go:113-117` |
| Tests | Done | `TestAPIGuards` (`internal/daemon/api_test.go:17`), `TestUIServingAndTokenCookie` (`web_test.go:32`) |

Three gaps:

1. The token is on disk, in `daemon.json` (`api.go:35-38`), with owner-only permissions. With a socket the CLI no longer needs it, so it can stay in memory.
2. A `GET` with the cookie is served without looking at `Origin` (`web.go:113`). `SameSite=Strict` covers this in practice. A `Sec-Fetch-Site` check costs three lines.
3. A bearer token is accepted over TCP (`web.go:110`). Once the CLI uses the socket, nothing needs it.

### 2.4 Desktop features

**T8. Tray (new).** Nothing exists. Wails v3 has it (section 3). On Linux a tray is not guaranteed: GNOME shows one only with an extension, and there is no way to ask whether it is shown. Hiding the window on close with no tray would leave no way back, so the shell needs a rule for that case (D7).

**T9. Start at login (partly done).** `kalka autostart on|off|status` exists for all three systems with no admin rights: the `HKCU` Run key on Windows (`internal/autostart/autostart_windows.go`), a LaunchAgent on macOS (`autostart_darwin.go`), an XDG entry on Linux (`autostart_other.go`). Tests exist for macOS and Linux (`autostart_darwin_test.go`, `autostart_other_test.go`), none for Windows. Missing: a setting in the UI, "on by default after first run", and an entry that starts the desktop shell hidden. Today it starts the headless daemon with `--background`.

"On by default" needs care in one respect: a user who turns it off must not get it back at the next start. The shell enables it once and remembers that it did.

**T10. Keep running when closed (new).** Not applicable to a browser tab. Wails v3 has a cancellable close hook.

**T11. Notifications (partly done, one conflict).**

| Trigger in the request | State today |
|---|---|
| Incoming handoff | Done as a prompt. `syncproto.PromptHandoff` goes through the hub to `popups.go`, which shows a Windows toast or a native dialog (`internal/notify`, `cmd/kalka/net.go:116-122`). It needs an answer in the UI within 5 minutes (`syncproto.go:29`). First-contact verification and "new blueprint" prompts use the same path. |
| New comments on documents you own | New. Nothing compares a document before and after a merge for the user. |
| A pending invite join | **Conflict.** Nothing is pending for anyone to approve: the invite is the approval (§19.7, §18.2 decision 5, §18.1 question 5). The owner's side only writes a log line (`internal/daemon/join.go:413`). See D8 for the reading this plan takes. |
| A newer version of a domain your blueprints reference | Partly. The UI computes "newer version available" when a document is opened (`internal/daemon/domains.go:142`). Nothing fires when the new version arrives. |

`internal/notify` works without a window and stays the presenter for the headless daemon. Its Windows toast opens the page by protocol activation of an `http://` URL (`internal/notify/toast.go`), which starts a browser. That is wrong for the desktop shell, where a click must show the window.

**T12. Start minimized (partly done).** `--background` already means "started at logon, show nothing" (`net.go:82-84`). For the shell it becomes a hidden window.

**T22. Tray status is undefined (decision).** "Connected peers, last sync, pending items" have no single source today. `Status` (`daemon.go:897-912`) has only the rendezvous state. Presence is per peer (`daemon.go:136-139`). `TeamStatus.LastRound` is per team (`internal/daemon/replicate.go:743-750`). One-to-one syncs record no time. "Pending items" could mean prompts, joins or unsent changes. See D7.

**T23. Pause and resume (new, decision).** Nothing exists. The request does not say what pauses: all traffic, or only background replication. See D6.

### 2.5 Build and distribution

**T14. One binary or two (conflict).** The request wants a single binary per platform, a CLI that works where the desktop shell is not installed, and a Wails window. §15 says "Go, no CGO, single static binary per platform".

| Platform | What a Wails v3 desktop build needs | Consequence for one combined binary |
|---|---|---|
| Windows | No cgo. Cross-compiles from any host with plain Go. WebView2 is loaded at run time. | Possible. But a program is either a console program or a GUI program, which is why the archive already holds `kalka.exe` and `kalkaw.exe` (`tools/release/main.go:87-93`). |
| macOS | cgo (AppKit, WebKit). The frameworks are on every Mac. Notifications and a clean tray need a signed `.app` bundle. | Possible to run, but the CLI would live inside an app bundle and need a Mac to build. |
| Linux | cgo, linked against GTK4 and WebKitGTK 6.0. | Not possible. On a server or in a container without these libraries the dynamic loader refuses to start the program at all, `kalka check` included. |

So: two binaries per platform from one module. `kalka` stays what it is, pure Go, static, cross-compiled for six targets from one machine. `kalka-desktop` is new. On Windows that is the same count as today, because `kalka-desktop.exe` replaces `kalkaw.exe`. See D9.

**T15. No bundled Node or Chromium (done).** Node is a build-time tool for the SPA only (`Makefile`, IP §6d). The window uses the system webview. The Wails project templates and its Taskfile assume npm for the front end, but kalka does not use Wails to build or serve the front end, so they are not needed.

**T16. WebView2 missing (new).** Wails v3.1.0 does not tell the user. It logs "error getting WebView2 version" and gives up on the window (`v3/pkg/application/webview_window_windows.go:2575-2581` at tag `v3.1.0`). The check must be kalka's own, before the Wails application is created. kalka has a good fallback that most apps lack: the same UI in a browser.

**T17. Per-user install (done for archives).** Unzip anywhere, no admin rights, autostart under `HKCU` (`docs/install.md`). The desktop adds a Start-menu shortcut and, on macOS, an app bundle. Both are per user.

**T18. Signing (new).** Nothing is signed (`docs/install.md`, `tools/release/main.go:201`). An unsigned GUI program that registers itself for autostart is what SmartScreen and endpoint protection are built to distrust, so this matters more for the desktop than it did for the CLI. Certificates cost money and need an owner. See Q5.

**T19. Restricted environments (partly done).** `kalka --no-open` and `kalka --background` are this path. It gets a name (`kalka daemon`), a page in the docs, and a guarantee that every milestone keeps it working.

**T20. Building the desktop (new, decision).**

- Today one command on one machine builds all six targets reproducibly with `CGO_ENABLED=0` (`tools/release/main.go:100`, IP §6h).
- The Windows desktop build keeps that property. The macOS build needs a Mac. The Linux build needs Linux with the GTK4 and WebKitGTK 6.0 development packages, or Docker.
- CI exists as a file and has never run, because the repository has no remote (`.github/workflows/ci.yml`, IP §6h). Until it runs, every Linux and Windows desktop build is made by hand, and nothing tests them automatically.
- cgo builds are harder to make reproducible than the current ones. The reproducibility promise (`docs/install.md`) stays for `kalka` and is not made for `kalka-desktop`.
- Nothing has run on a real Windows desktop yet: not the toast, not the message box, not `kalkaw.exe` (IP §6h lists what to check). The desktop app multiplies that debt.

### 2.6 Other

**T24. Smaller conflicts with the spec (note).**

- §4 calls the UI "Web UI (localhost)" and the daemon "serves the web UI". §11 says "served from the binary". All three are rewritten in D0. The browser UI stays as an option, so no section is deleted.
- §13 says the CLI reads the token from a file. With D2 it does not.
- §5 says none of the files in the config directory "is ever asked for or edited by the user", and that setup asks for exactly three fields. New settings (keep running, notifications, pause) get defaults and are never part of setup. "Brainless config" holds.
- "Quit stops the daemon" and "closing every window must never stop sync" are consistent only if Quit is the one deliberate way to stop. With "keep running when closed" switched off, closing the window is a quit. The setting's label must say so.
- The request says "update the spec, and only then implement". Code cites spec section numbers, so new sections are appended as §22 and §23 and nothing is renumbered.
- Financial examples: the request adds none. New fixtures use library lending or parcel logistics.

---

## 3. Wails v2 against v3

### 3.1 Sources

Read on 2026-11-14. `wails.io` and `v3.wails.io` refused the fetching tool (HTTP 403), so the documentation was read from the repository at tag `v3.1.0`, where the site's source lives under `docs/mpress/content/`.

| # | Source | Date |
|---|---|---|
| S1 | Releases list, `https://github.com/wailsapp/wails/releases` | v3.0.0 2026-10-26; v3.1.0 and v2.17.1 2026-11-01; nightlies `v3.2.0-nightly.2026110x` |
| S2 | v3.0.0 release notes, `https://github.com/wailsapp/wails/releases/tag/v3.0.0` | 2026-10-26 |
| S3 | v3.1.0 release notes, `https://github.com/wailsapp/wails/releases/tag/v3.1.0` | 2026-11-01 |
| S4 | "Wails v3.0.0 Released", `docs/mpress/content/blog/wails-v3-released.md` (site: `https://wails.io/blog/wails-v3-released/`) | 2026-10-26 |
| S5 | "Wails v3.1.0: More Go, fewer build hurdles", `…/blog/wails-v3-1-released.md` | 2026-11-01 |
| S6 | Status and compatibility, `…/status.md` | at v3.1.0 |
| S7 | System tray, `…/features/menus/systray.md` | at v3.1.0 |
| S8 | Lifecycle and window options, `…/concepts/lifecycle.md`, `…/features/windows/options.md` | at v3.1.0 |
| S9 | Single instance, `…/guides/single-instance.md` | at v3.1.0 |
| S10 | Autostart, `…/features/autostart/basics.md` | at v3.1.0 |
| S11 | Notifications, `…/features/notifications/overview.md` | at v3.1.0 |
| S12 | Cross-platform builds, `…/guides/build/cross-platform.md`; Linux, `…/guides/build/linux.md`; Windows, `…/guides/build/windows.md`; installation, `…/getting-started/installation.md` | at v3.1.0 |
| S13 | Signing, `…/guides/build/signing.md` | at v3.1.0 |
| S14 | Stream transport internals, `…/guides/advanced/streams-internals.md` | at v3.1.0 |
| S15 | Source, `v3/pkg/application/webview_window_windows.go` | at v3.1.0 |
| S16 | WebView2Feedback issue 3519, "Stream HTTP responses to WebView2", `https://github.com/MicrosoftEdge/WebView2Feedback/issues/3519` | search result, not opened |
| S17 | Wails issue 4990, "[v2] Add System Tray support", closed as not planned, `https://github.com/wailsapp/wails/issues/4990`; v2 options reference, `https://wails.io/docs/v2.13.0/reference/options/` | search results, not opened |
| S18 | `fyne.io/systray`, `https://pkg.go.dev/fyne.io/systray` | v1.12.2 shown, 2026-06-09 (the page says a newer one exists) |
| S19 | Wails pull request 6284 (Linux tray dropped by GNOME's AppIndicator extension), in v3.1.0 | 2026-11 |

All `…/` paths are under `https://github.com/wailsapp/wails/blob/v3.1.0/docs/mpress/content/`.

The v2 column below rests on S17 and on general knowledge of the v2 documentation. It was not re-read line by line, because the outcome does not depend on its details: v2 has no tray.

### 3.2 Release status

- **v3** is stable for Windows, macOS and Linux desktops since v3.0.0 on 2026-10-26, after alphas from January 2023 and a beta from 2026-08-02 (S2, S4, S6). Mobile is experimental and outside the promise.
- **Cadence.** A minor release every week, nightly pre-releases in between, patch releases for hotfixes. "Minor version numbers advance with time, not with features" (S3). v3.1.0, one week after v3.0.0, carried about 100 fixes (S5). `go get …@latest` never picks a nightly.
- **v2** "isn't going away" and "continues to receive critical updates"; new projects are told to use v3 (S4). The latest is v2.17.1 (S1).
- **Known open issue in v3.0.0.** Security advisory GHSA-2p5f-2gx8-mh8m is "only partly fixed": on Linux, same-origin child frames can still reach the native message handler, and on all platforms bound-method calls from child frames are not restricted by the child-frame guard (S2). kalka's only frame is the Mermaid frame, which is sandboxed to an opaque origin (`web.go:27-40`), and kalka binds no Go methods into the page at all (D2). The exposure is small, and D0 checks it.

### 3.3 Features

| Feature | Wails v2 | Wails v3 (3.1.0) |
|---|---|---|
| System tray | None. A request was closed as not planned (S17). Apps add a separate tray library and its second event loop. | Built in on all three (S7). Menus, light and dark icons, macOS template icons, tooltips (3.1.0), show and hide. Linux uses StatusNotifierItem over D-Bus: KDE and XFCE show it, GNOME only with the AppIndicator extension, and there is no `SystemTraySupported()` (S7). 3.1.0 fixed two Linux tray bugs (S3, S19). |
| Hide on close | `HideWindowOnClose`, fixed at start-up (S17), or `OnBeforeClose`. | `RegisterHook(events.Common.WindowClosing, …)` with `window.Hide()` and `e.Cancel()`; decided per close, so a setting can change it at run time (S8). `ApplicationShouldTerminateAfterLastWindowClosed: false` on macOS, `DisableQuitOnLastWindowClosed` on Windows and Linux (S8). |
| Start minimized | `StartHidden` (S17). | `Hidden: true` on the window (S8). |
| Single instance | `SingleInstanceLock` (S17). | `SingleInstanceOptions` with a callback in the first instance. Named mutex on Windows and macOS, D-Bus name on Linux (S9). kalka does not need it: the daemon lock decides (D3). |
| Start at login | None. | `app.Autostart`: `HKCU` Run key, `SMAppService` for a bundled app on macOS 13 and later or a LaunchAgent otherwise, XDG autostart on Linux, with arguments such as `--hidden` (S10). The same mechanisms as `internal/autostart`. |
| Notifications | None. | Built in (S11). Windows: toasts with actions. macOS: needs user authorization and a packaged, signed app. Linux: `org.freedesktop.Notifications`, needs a running notification daemon. |
| Several windows | One window only. | Yes. Not needed now. |

### 3.4 Toolchain, WebView2, Linux

| Topic | Wails v2 | Wails v3 (3.1.0) |
|---|---|---|
| Go | 1.21 or later | 1.26 or later; 1.26.10 or 1.27.3 recommended for a `crypto/x509` fix (S2). kalka is on 1.27.4. |
| cgo | Windows: none. macOS, Linux: required. | Windows: none, "works from any host OS with no additional setup". macOS, Linux: required (S12). 3.1.0 adds experimental cgo-free back ends for macOS and Linux through purego, `CGO_ENABLED=0 go build -tags purego,production`; cgo remains the default (S5). |
| Cross-compiling | Windows from anywhere; macOS and Linux natively | Windows from anywhere. macOS natively, or a Docker image with Zig and the macOS SDK. Linux natively with gcc or clang, or Docker (S12). |
| Linux webview | GTK3 with WebKit2GTK 4.0, or 4.1 with a build tag | GTK4 with WebKitGTK 6.0 by default. GTK3 with WebKit2GTK 4.1 behind `-tags gtk3`, "removed in v3.1" according to S6 and S12, although S5 still names GTK3 for the purego back end. Supported base: Ubuntu 24.04. Ubuntu 22.04, Debian 12 and RHEL 9 do not ship WebKitGTK 6.0 (S12). |
| Linux quirk | — | With NVIDIA's proprietary driver WebKitGTK can show a blank window. Wails sets `WEBKIT_DISABLE_DMABUF_RENDERER=1` when it sees the driver (S12). |
| WebView2 runtime | A build flag chooses what happens when it is missing: download, embed the bootstrapper, open the browser, or fail | The NSIS installer carries the bootstrapper (S12). The run-time itself only logs an error (S15). A zip install gets nothing. |
| WebView2 process crash | — | 3.0.0 "recovers from WebView2 process failures instead of leaving you with a blank window" (S4). |
| Build tool | `wails build`, opaque | A Taskfile, or the experimental `wails.yaml` builder; plain `go build` with tags also works (S4, S5). |
| Signing | Documented, manual | Built in: Authenticode for EXE and MSIX from any host with an exported PFX, macOS signing and notarization from any host with exported credentials, DEB and RPM signatures (S13). |

### 3.5 Recommendation

**Use Wails v3, pinned to an exact weekly release (3.1.x or whatever is newest when D6 starts), and move the pin only on purpose.**

- v2 lacks the feature the request is about. With v2 the plan would add a tray library on day one, and its event loop next to Wails' is a known source of trouble.
- v3 covers the tray, the close hook that can change at run time, hidden start, autostart with arguments and notifications in one library. The Windows target needs no cgo, so the primary platform keeps the simple build.
- v2 is in maintenance. Starting on it means a port later, and the v2-to-v3 migration is "a real port rather than a version bump" (S4).

What speaks against v3 is its age: three weeks as a stable release when this was written, a weekly minor cadence, an experimental build tool, and Linux tray bugs fixed as recently as 2026-11-01. The plan limits that in four ways:

1. kalka uses a small part of Wails: one window on a URL, one tray, the close hook, native dialogs and notifications. No bindings, no asset server, no Wails events, no Wails build tool, no updater.
2. All Wails calls sit in one package behind a narrow interface (D6), with the logic (what the tray shows, what closing means) outside it and unit-tested.
3. The exact version is pinned in `go.mod`. A bump is its own commit with the manual checklist run again.
4. D0 has a time-boxed spike on a real Windows machine before any other code is written.

**Fallback tray library: `fyne.io/systray`** (S18), the maintained fork of `getlantern/systray`. It has `RunWithExternalLoop` for running beside another toolkit's event loop and names webview hosts as the use case. It talks StatusNotifierItem over D-Bus on Linux with no GTK dependency. Its README asks for cgo. It is used only if the Wails tray fails the D0 spike on a platform, and only on that platform, behind the same interface.

---

## 4. Decisions and recommendations

### D1. Layout: `internal/core`, `internal/daemon`, `internal/cli`, `internal/desktop`

| Package | Holds | Must not import |
|---|---|---|
| lower packages (unchanged) | format, merge, team log, identity, store, protocol, transport | — |
| `internal/core` (new, from `internal/daemon`) | The running node: `daemon.go`, `replicate.go`, `teams.go`, `join.go`, `publish.go`, `domains.go`, `seeds.go`, `refs.go`, `model.go`, `prompts.go`, the event bus | `net/http`, `internal/ui`, `internal/notify`, Wails |
| `internal/daemon` (what is left) | The process around the core: lock, listeners, HTTP API and gate, static UI, client, settings, notification presenters, shutdown | Wails |
| `internal/cli` (new, from `cmd/kalka`) | Every command, as `Run(args, stdin, stdout, stderr, now) int` | Wails |
| `internal/desktop` (new, build tag `desktop`) | Window, tray, close policy, native notifications, WebView2 check | — |
| `cmd/kalka` | `main` calling `cli.Run` | — |
| `cmd/kalka-desktop` (new, build tag `desktop`) | `main`: no arguments or `--hidden` starts the shell, anything else goes to `cli.Run` | — |

A test walks the import graph and fails when a package breaks its "must not import" column. That is how "no UI code in the core" stays true.

The build tag keeps `go build ./...`, `go vet ./...` and `go test ./...` free of Wails, cgo and GTK headers. `make test` and the existing CI job do not change.

### D2. IPC: a Unix socket for the CLI, loopback HTTP for the window

**The same HTTP API on two listeners.**

| | Socket | Loopback TCP |
|---|---|---|
| Who uses it | CLI, the desktop shell's tray and control code | The window's page, and a browser tab after `kalka open` |
| Address | `<config>/kalka.sock` | `127.0.0.1:<ui_port>`, default 7717 |
| Who may connect | The file's owner: mode 0600 in a 0700 directory; on Windows the owner-only ACL that `fsutil.WritePrivate` already applies to the key file (`internal/fsutil/private_windows.go`) | Anyone on the machine can connect; only the page gets answers |
| Authentication | File permissions. No token. | Token cookie on every `/api/` request; `Host` check; `Origin` on state-changing requests; a `Sec-Fetch-Site` header, when present, must be `same-origin` or `none` |
| Bearer token | — | No longer accepted |
| Can be switched off | No | Yes: `kalka daemon --no-ui` opens no TCP port at all |

- **Windows.** The recommendation is the same Unix socket there. Windows 10 (1803 and later) and 11 support `AF_UNIX`, and Go's `net` package listens on it. One transport on three systems, and no new dependency. The request names a named pipe for Windows. A pipe needs either `github.com/Microsoft/go-winio` or a few hundred lines of overlapped I/O. The D0 spike tests the socket on a real Windows machine under the usual endpoint protection. If it fails, the pipe goes behind the same `Listen` and `Dial` functions, and nothing else changes.
- **Path length.** A socket path is limited to about 104 bytes on macOS and 108 on Linux. When `<config>/kalka.sock` is longer (deep `KALKA_CONFIG_DIR`, test temp directories), the daemon uses a short directory of its own under the system temp directory, checks that it owns it and that its mode is 0700, and writes the real path into `daemon.json`.
- **The token leaves the disk.** `daemon.json` shrinks to `{pid, socket, addr}`. `kalka open` asks the daemon for a fresh UI URL over the socket (`GET /api/ui-url`). `Endpoint.Token` and the bearer branch go.
- **The window.** The shell asks for the same URL and loads it. The page then runs exactly as in a browser: cookie, CSP, event stream, Mermaid frame. No Go method is bound into the page and the Wails runtime script is not used.
- **The fixed port stays.** The page keeps a few preferences in `localStorage` (`web/src/Review.tsx:17-32`, `App.tsx:32`), which is per origin, port included. A random port per start would forget them at every start.
- **`kalka open` stays.** It is the UI for machines with no desktop shell and the fallback when WebView2 is missing.
- **External links.** The page opens links with `target="_blank"` (`web/src/refs.tsx:49`, `markdown.ts:67`). A webview has no tabs. The page calls `POST /api/open-external` instead when it runs in the shell, the daemon accepts only `http` and `https`, and the shell opens the system browser. The window itself never navigates away from the loopback origin.

### D3. Daemon placement: in-process, with attach as the second branch

The shell's window and tray are clients of the HTTP API in every case: the page by design (D2), the tray because it reads one status summary and one event stream. The only open point is which process calls `core.Run`.

**The shell takes the lock. If it gets it, it runs the daemon in its own process. If not, it attaches to the daemon that holds it.**

| Start | Lock is free | Lock is held |
|---|---|---|
| `kalka daemon` | Runs headless | Prints "already running (pid N)" and exits 0 |
| `kalka-desktop` | Runs the daemon in-process, shows tray and window | If the holder has a shell: asks it to show its window (`POST /api/ui/show`) and exits 0. If the holder is headless: attaches, with its own tray and window |
| `kalka-desktop --hidden` | The same, window hidden | Exits 0 without showing anything |
| `kalka` (no arguments) | Starts the daemon detached, then opens the UI | Opens the UI: the shell's window if a shell is attached, else the browser |

Why not always attached (the shell always starts `kalka daemon` as a child): two processes for every user, a supervision problem (the daemon dies, the shell must notice and restart it), and two versions that can drift apart after a partial update. Why not in-process only: the CLI starts a headless daemon when none runs (D4), and `kalka daemon` is the supported path in restricted environments, so the shell will meet a foreign daemon and must do something sensible. Attaching costs little, because the shell's code only ever talks to the API.

Wails' own single-instance feature is not used. The daemon lock already answers "is there one", and one mechanism is easier to reason about than two.

### D4. Which commands need the daemon

| Class | Commands | Daemon running | No daemon |
|---|---|---|---|
| A. File commands | `check`, `comment`, `seed`, `seeds`, `reference`, `version`, `setup`, `autostart`, `rendezvous`, `help` | Not used | Not used. They never start one. |
| B. Daemon commands | `send`, `handoff`, `peers add`, `peers forget`, `verify`, `team create`, `team invite`, `team remove`, `team add-owner`, `team remove-owner`, `team assign-domain`, `join`, `publish`, `release`, `delete`, `open`, `pause`, `resume` | Over the socket | Start one detached (`kalka daemon --background`), wait for the socket, then run. `--no-start`, or `KALKA_NO_START=1`, fails instead with exit 1 and `"kind": "not-running"`. |
| C. Views | `status`, `peers`, `team members`, `team log`, `team invites`, `domains` | Over the socket, with live data | Read the local files in-process, start nothing, and say so: presence and online state are reported as unknown, JSON carries `"daemon": false`. |
| D. Control | `daemon`, `stop` | `daemon`: exits 0, see D3. `stop`: asks it to shut down and waits. | `daemon`: runs. `stop`: exits 0, nothing to do. |

- Class A is today's behaviour and the answer to T5. `comment` and `seed` write under §14's compare-and-swap, with or without a daemon.
- Class B is everything that writes the team log, the config directory, a release or a tombstone, or needs the network. One writer: the daemon.
- Class C exists for agents. `kalka domains --json` in a sandbox should not register on a rendezvous server as a side effect. It needs a read-only constructor for the core that opens the store and the logs and starts no loop.
- A CLI-started daemon is always the headless one, also where the desktop shell is installed. An agent's command must not make a window or a tray icon appear. A shell started later attaches (D3).
- Setup: a class B command with no config fails with "kalka is not set up" as today. It does not start a daemon that would open a setup form.

### D5. The lock

- **File:** `<config>/daemon.lock`, taken before anything else, also before the setup form is served.
- **Unix:** `flock(LOCK_EX|LOCK_NB)` through `golang.org/x/sys/unix`, already a dependency.
- **Windows:** `LockFileEx` with `LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY` through `golang.org/x/sys/windows`.
- Both are released by the kernel when the process ends, however it ends. There is no stale lock and no PID to check. The PID in the file is for messages only.
- **Order at start:** lock, remove a leftover socket file (safe only because the lock is held), listen on the socket, listen on TCP, write `daemon.json`. At exit the reverse.
- **A client that finds the lock held but no socket yet** retries for a few seconds: the daemon is starting.
- A named mutex on Windows was considered and not chosen: a lock file follows `KALKA_CONFIG_DIR`, so the same test runs on all three systems.
- Not covered: two config directories that name the same blueprint folder. That is a deliberate setup, used in tests. A second lock inside the folder would have to work on network shares, where file locks are unreliable. The spec states it as a known limit.
- With the lock in place the macOS LaunchAgent can get `KeepAlive` on crash (`SuccessfulExit: false`), because a second start now exits 0 instead of failing.

### D6. Pause and resume

**Pause means offline.** The daemon closes both rendezvous registrations and stops the replication loop, the join loop and the presence probes. Peers see this kalka exactly as they see a closed laptop, so there is no new wire message and no new case in anyone's code. The folder watcher keeps running and local work continues: comments, seeds, `publish` and `release` write locally and are pushed after resume.

- While paused, a class B command that needs the network (`send`, `handoff`, `join`) fails with `"kind": "paused"`, and the UI offers *Resume*.
- **State:** `paused: true` in a new `<config>/state.yaml`, written only by the daemon. Changed through `POST /api/pause` and `POST /api/resume`, from the tray, the page, or `kalka pause` and `kalka resume`.
- **It survives a restart.** A pause that silently ends at the next login would surprise the person who paused. A pause that silently lasts would defeat the reason for this whole feature. So the tray icon has a distinct paused state, the page shows a banner, and a start in the paused state raises one notification.
- The alternative, pausing only background replication while one-to-one sync keeps working, needs a new `bye` reason so that peers do not count a refusal as an error, and it is harder to explain in one line. See Q4.

`state.yaml` also holds the other settings the daemon owns: `notifications`, `keep_running_on_close`, `close_notice_shown`, `autostart_initialized`. Autostart itself is not stored there; the truth is the system's entry, read with `autostart.Get()`. `config.yaml` keeps its three required fields and is not touched by any of this.

### D7. Tray, window and settings

**Tray menu.**

| Item | Source |
|---|---|
| Open kalka | Shows and focuses the window |
| Status line, for example "Online · 4 of 6 members reachable · last sync 14:02" or "Paused" or "Offline: rendezvous not reachable" | `GET /api/summary` |
| "2 waiting for you" (only when not zero), opens the window on them | the same |
| Pause sync / Resume sync | D6 |
| Start at login (check mark) | `autostart.Get()`, `Enable`, `Disable` |
| Quit kalka | Native confirmation: "kalka will stop syncing with your team until you start it again." Then `POST /api/shutdown`, which lets running rounds finish (`Daemon.Run` already waits, `daemon.go:270-272`). In attached mode this stops the headless daemon too, and the dialog says so. |

**`GET /api/summary`** defines the three numbers of T22:

- *Connected peers:* known peers and team members whose last presence probe says online, out of all known (`daemon.go:776-857`).
- *Last sync:* the latest of all teams' `LastRound` with at least one member reached, and the last completed one-to-one sync. The core records the second, which it does not do today.
- *Pending items:* prompts waiting for an answer, joins not completed (`Joins()`, `internal/daemon/join.go:50`), and teams with a push still queued (`replication.push`, `replicate.go:756`).

The tray refreshes on the bus events that already exist (`status`, `peers`, `teams`, `prompt`), at most once a second.

**Window.**

- Closing hides it when `keep_running_on_close` is on (the default). The first time, one native notification says that kalka keeps syncing and where the icon is, and `close_notice_shown` is set.
- With the setting off, closing the window quits the shell. An in-process daemon stops with it, without the confirmation dialog, because the user chose this meaning. A headless daemon the shell was attached to keeps running.
- `--hidden` starts with no window shown. The autostart entry passes it.
- **Linux without a tray.** Before hiding, the shell asks D-Bus whether a StatusNotifierWatcher is registered. If none is, closing minimizes instead of hiding, so the window stays reachable from the task bar. Starting `kalka-desktop` again always shows the window (D3), on every system, and the one-time notice says so.
- **Popups.** `popups.go:40` changes from "a page is connected" to "a window is visible or a browser page is connected". The shell reports visibility over the socket.

**Start at login.** At its first start the shell calls `autostart.Enable` once and sets `autostart_initialized`. After that only the user changes it, from the tray, the page's settings or `kalka autostart`. `internal/autostart` stays kalka's own package rather than Wails' `app.Autostart`: it has tests, it is used by the headless CLI too, and it already does the same three things. `Enable` learns to write the desktop binary with `--hidden` when called from the shell, and `kalka daemon --background` when called from the CLI on a machine without it.

**Settings in the page.** One new *Settings* view: start at login, keep running when closed, notifications, pause. It is served by `GET` and `PUT /api/settings`. In a browser tab the two window settings are hidden.

### D8. Notifications

The core computes them and publishes a `notice` event on the bus. Presenters show them. That keeps the rules testable with no desktop.

| Notice | Computed where | Rule |
|---|---|---|
| Handoff waiting | Exists: a `PromptHandoff` in the hub | Unchanged. Verification and "new blueprint" prompts keep their notifications too; all three need an answer within 5 minutes. |
| New comments on your document | Wherever a merge result is written: the one-to-one path (`daemon.go:494-549`, `:582-696`) and `applyReplica` (`replicate.go:598-680`). One helper compares the entry ids before and after. | The local user is the owner; the new entries are by someone else; kinds `comment`, `deviation` and custom kinds, not `release`, `handoff` or `budded`. One notice per document per session or round: "3 new comments on 'Parcel routing plan' from bob@corp.example". A first copy of a document raises none. |
| Someone joined with your invite | Owner's side, `redeem` after `ByeJoined` (`join.go:409-413`) | "carol@corp.example joined Parcel team." |
| Your join completed or failed | Joiner's side, `setJoin` when a join reaches a final state (`join.go:137-148`) | "You are now a member of Parcel team", or the reason it failed for good (expired, refused). Retries raise nothing. |
| Newer domain version | `applyReplica`, when a received release raises a domain document's version (`snapshotHead`, `replicate.go:701`) | Some document owned by the local user references `<team>/<domain>@v<M>` with M below the new version. One notice per domain and version, listing up to three documents. |

- **"A pending invite join"** is read as rows three and four. An approval step does not exist and is not added (§18.1 question 5 stays open with its default). See Q3.
- **Off switch.** One setting, `notifications`, default on. Prompts that need an answer are exempt from it only in the sense that the window still shows them; no OS notification is sent when the switch is off. Per-kind switches are left out until someone asks.
- **Presenters.** The desktop shell uses Wails' notifications; a click shows the window on the document. The headless daemon keeps `internal/notify` (toast or dialog, a click opens the browser). When a shell is attached to a headless daemon, the shell subscribes as presenter and the daemon's own are silent.
- **macOS without a signed bundle** cannot post to the notification centre (S11, IP §6h). Unsigned builds fall back to the existing AppleScript dialog for prompts and show the other notices only in the window.
- The page's own browser notifications (`web/src/notify.ts`) and its permission banner stay for the browser tab and are hidden in the shell, which the status response announces with `"shell": "desktop"`.

### D9. Build matrix and signing

**Two binaries, one module.**

| Target | `kalka` (CLI and headless daemon) | `kalka-desktop` | Built on |
|---|---|---|---|
| windows/amd64, windows/arm64 | Pure Go, as today | Pure Go (no cgo), `-H=windowsgui`, tags `desktop,production`. Replaces `kalkaw.exe`: with arguments it runs the CLI, so `kalka-desktop.exe daemon --background` is a console-less headless start. | Any host |
| darwin/arm64, darwin/amd64 | Pure Go, as today | cgo; packaged as `Kalka.app`, with `kalka` inside it as well | A Mac |
| linux/amd64 | Pure Go, as today | cgo, GTK4 and WebKitGTK 6.0; Ubuntu 24.04 as the base | Linux, or Docker |
| linux/arm64 | Pure Go, as today | Not built at first | — |

- **Build tool.** `tools/release` stays the one entry point and plain `go build` stays the compiler call. It gains a desktop step per target and skips, with a clear line of output, the desktop targets the host cannot build. Wails' Taskfile and its experimental `wails.yaml` builder are not adopted: kalka needs none of their front-end steps, and the release tool already produces reproducible archives.
- **Make targets:** `make build` (headless, unchanged), `make desktop` (the shell for the host), `make release VERSION=…` (everything the host can build).
- **Experimental cgo-free macOS and Linux back ends** (S5) would let one machine build all targets again. They are one week old. The plan does not depend on them; D7 tries them and records the result.
- **Windows packaging.** The zip as today, with `kalka.exe`, `kalka-desktop.exe` and `kalka-plugin-gh.exe`. At its first start `kalka-desktop.exe` creates a per-user Start-menu shortcut carrying kalka's AppUserModelID, which is what gives toasts their sender name and click target. A per-user installer (NSIS with `RequestExecutionLevel user`, into `%LOCALAPPDATA%\Programs\kalka`, with the WebView2 bootstrapper) is the next step after the zip works, not part of this plan's minimum. See Q6.
- **WebView2 check.** Before creating the Wails application the shell reads the runtime's version from the registry keys Microsoft documents for the Evergreen runtime. If it is missing, a native message box says so, names the download, and offers "Open kalka in the browser instead", which starts the daemon and runs the `kalka open` path. Sync works either way.
- **Linux packaging.** A tar archive with the binary, a `.desktop` file and an icon, and the list of packages to install. AppImage, deb and rpm are later work.

**Signing is configuration, and its absence is not an error.** `tools/release` reads these from the environment, or from a file outside the repository named by `KALKA_SIGNING`:

| Platform | Needed | Setting |
|---|---|---|
| Windows | An Authenticode code-signing certificate. Since 2023 certificate authorities issue them on hardware tokens or cloud HSMs only, so in practice a cloud signing service or a token on the build machine. EV or an established reputation is what quiets SmartScreen. A timestamp server. | `KALKA_WIN_SIGN_CMD`: a command template run on every `.exe`, for example `signtool`, `osslsigncode`, a cloud signer's CLI, or `wails3 tool sign`. A command, because the right tool depends on where the key lives. |
| macOS | Apple Developer Program membership; a Developer ID Application certificate; hardened runtime; notarization with `notarytool`; stapling | `KALKA_MAC_IDENTITY`, `KALKA_MAC_NOTARY_PROFILE` (or the App Store Connect API key variables) |
| Linux | Nothing for a tar archive. `SHA256SUMS` as today. | — |

With nothing set, the build prints "unsigned" per artifact and succeeds. Unsigned macOS apps are ad-hoc signed so they start on Apple Silicon. `docs/install.md` keeps the SmartScreen and quarantine instructions for unsigned builds.

### 4.8 Smaller decisions

- **A manual sync belongs to its request.** `POST /api/sync` keeps cancelling when its stream closes (`api.go:253`). Hiding the window does not close the stream, since the page keeps running. Quitting does. Detaching manual syncs from their request would need a new way to deliver their prompts and results, for a case that lasts seconds.
- **`kalka` with no arguments** changes from "be the daemon" to "make sure a daemon runs, then open the UI". The foreground daemon with terminal prompts (`terminalPrompts`, `net.go:249`) becomes `kalka daemon` run from a terminal. `--no-open` is dropped in favour of `kalka daemon`; `--background` stays as a flag of `kalka daemon`.
- **Log file.** The shell and `kalka daemon --background` write `<config>/kalka.log` as today (`net.go:155-164`).
- **Setup in the shell** is the existing setup form in the window. A native folder picker is not added.
- **No auto-updater.** Wails has one. Updates stay manual until signing exists.

---

## 5. Milestones

The CLI and the headless path work at the end of every milestone: `make test` passes with no desktop tag, and `kalka daemon` plus `kalka open` give the full product in a browser.

### D0. Spec v0.5 and three spikes

- **Spec:** §4 (rewritten: core, daemon, CLI, desktop shell, with the diagram), §5 (new files: `daemon.lock`, `kalka.sock`, `state.yaml`; `daemon.json` without a token), §11 (the UI is a window or a tab; new *Settings* view), §12 (the table by class from D4; `daemon`, `stop`, `status`, `pause`, `resume`), §13 (renamed "Local API": two listeners, D2), §15 (two binaries, the cgo rule, Wails v3 pinned), §16 (milestones D1 to D8 and their acceptance criteria), §18 (decisions from section 4; the answers to section 8), §21 (threat model: other local users, other local processes, web pages aimed at `localhost`). New, appended: **§22 Desktop app** (tray, window, settings, pause, notifications) and **§23 Platforms and distribution** (per-platform notes, WebView2, Linux tray, signing, restricted environments).
- **Spikes,** time-boxed to two days, in a scratch directory, not committed:
  1. Wails v3 on a real Windows 11 machine: a window on a loopback URL with an event stream and a streamed `fetch`; the tray; hide on close; hidden start; a toast and its click; the cgo-free cross-build from the Mac; whether the Wails bridge is reachable from a page loaded by URL.
  2. A Unix socket on the same Windows machine, with its ACL, under the machine's endpoint protection.
  3. The same window and tray on macOS, and on Ubuntu 24.04 GNOME with and without the AppIndicator extension.
- **Review** with the product owner (section 8) before D1.
- **Size:** 4 days. Needs a Windows machine (Q1).

### D1. Core and daemon split (no behaviour change)

- **Spec:** §4.
- **Packages:** `internal/core` (new), `internal/daemon` (shrinks), `internal/cli` (new), `cmd/kalka` (thin).
- **Format and wire:** none.
- **Tests:** all existing tests pass after the move. New: the import-boundary test of D1 in section 4.
- **Size:** 3 days.

### D2. The lock and `kalka daemon`

- **Spec:** §4, §5, §12.
- **Packages:** new `internal/lockfile`; `internal/daemon` (start order, `POST /api/shutdown`); `internal/cli` (`daemon`, `stop`; bare `kalka` as in 4.8); `internal/autostart` (`KeepAlive` on crash for macOS).
- **Tests (required area: the single-daemon lock):**
  - two hosts on one config directory in one test process: the second gets "held";
  - the built binary started twice: the second prints "already running" and exits 0, and the first is still serving;
  - the first killed with no clean-up: a third start takes the lock at once, ignores the stale `daemon.json` and removes the stale socket;
  - two different `ui_port` values on one config directory: still one daemon;
  - `kalka stop` lets a running replication round finish.
  All run on Linux, macOS and Windows.
- **Size:** 2 days.

### D3. Socket IPC and a hardened local API

- **Spec:** §13, §5, §21.
- **Packages:** new `internal/ipc` (`Listen`, `Dial`, path choice); `internal/daemon` (`gate` in `web.go`, `Client` in `client.go`, `daemon.json`, `/api/ui-url`, `--no-ui`); `internal/cli` (`open`).
- **Tests (required area: rejection without a token or with a foreign `Origin`):** `TestAPIGuards` extended into a table over both listeners:
  - TCP: no token, wrong token, bearer token (now refused), cookie with a foreign `Origin` on a write, cookie with `Sec-Fetch-Site: cross-site` on a read, wrong `Host`, a non-loopback `Host`: all 403. Cookie and own origin: 200.
  - Socket: works with no token; the socket file is 0600 in a 0700 directory; on Windows the ACL names only the current user.
  - `daemon.json` contains no token. `--no-ui` listens on no TCP port. A config directory with a 200-character path still gets a working socket.
- **Size:** 3 days.

### D4. The CLI with and without a daemon

- **Spec:** §12.
- **Packages:** `internal/cli`; `internal/core` (read-only constructor); `internal/autostart` (`Start` reused to spawn).
- **Tests (required area: CLI behaviour with and without a running daemon),** against the built binary and a local `kalka rendezvous`:
  - class A with no daemon: `comment` and `seed` succeed and no daemon is running afterwards;
  - class A with a daemon: the daemon's index shows the new entry;
  - class B with no daemon: one is started, the command succeeds, the daemon is still running;
  - class B with `--no-start`: exit 1, `"kind": "not-running"`, nothing started;
  - class B with no config: "not set up", nothing started;
  - two class B commands at the same moment with no daemon: exactly one daemon results;
  - class C with no daemon: output from files, `"daemon": false`, nothing started;
  - `daemon` twice, `stop` twice.
- **Size:** 3 days.

### D5. Sync controls, settings and notices (headless)

- **Spec:** §22, §12, §11.
- **Packages:** `internal/core` (pause, summary, last-sync time, notices); `internal/daemon` (`state.yaml`, `/api/summary`, `/api/settings`, `/api/pause`, `/api/resume`, presenters, `popups.go`); `internal/cli` (`status`, `pause`, `resume`); `web/` (Settings view, paused banner, hidden notification banner in the shell).
- **Format and wire:** none on the wire. New local file `state.yaml`. New bus event `notice`.
- **Tests (required area: sync continuing with no window open):**
  - two daemons replicate a published document with no API listener and no event subscriber; again after a page connected and disconnected; again while a subscriber reports "window hidden";
  - pause: both registrations close, a peer's probe says offline, no round runs, a local `publish` succeeds and reaches the peer after resume; pause survives a restart;
  - each notice of D8 from a scenario: comment on an owned document (and none for one's own, none for a first copy), join on both sides, domain version, handoff; none when `notifications` is off;
  - summary numbers against a known scenario.
- **Size:** 5 days.

### D6. The desktop shell

- **Spec:** §22, §23.
- **Packages:** new `internal/desktop` and `cmd/kalka-desktop` (build tag `desktop`); `internal/autostart` (target and `--hidden`); `internal/daemon` (`/api/ui/show`, `/api/open-external`, presenter hand-over); `web/` (external links).
- **Tests:**
  - Without Wails, in the normal test run: the tray model built from a summary; the close policy as a table (setting, tray present or not, first close or later); the start table of D3; the WebView2 version check against sample registry values; the external-link filter.
  - **Required area: autostart toggling per platform.** macOS and Linux tests exist and are extended for the new command line. New: a Windows test that writes and removes the value under a test-only registry key, run on Windows CI. On all three: toggling through `PUT /api/settings` with a fake autostart back end; "first run enables once, a later start does not re-enable".
  - With the `desktop` tag, a smoke test where the system has a display: the shell starts hidden, `kalka status` answers, `POST /api/ui/show` succeeds, `kalka stop` ends it.
  - A manual checklist per system, kept in `docs/desktop-testing.md`: tray items, close and reopen, the one-time notice, hidden start at login, second launch, quit with confirmation, each notification and its click, attach to a headless daemon, WebView2 removed.
- **Size:** 7 days.

### D7. Builds, packaging, signing, documentation

- **Spec:** §15, §23.
- **Packages:** `tools/release`, `Makefile`, `.github/workflows/ci.yml` (a desktop build job per system; the Linux job installs the GTK4 and WebKitGTK 6.0 packages), `docs/install.md` (rewritten around the desktop app), new `docs/headless.md` (restricted environments: `kalka daemon`, `--no-ui`, autostart without a tray, what IT needs to allow).
- **Tests:** the release tool's desktop step on the host; the PE subsystem check that exists for `kalkaw.exe` moved to `kalka-desktop.exe`; an unsigned build succeeds and says "unsigned"; a signing command that fails makes the build fail.
- **Also:** the try-out of the cgo-free back ends; the list of webview rendering differences found (section 7, R6), written into §23.
- **Size:** 5 days.

### D8. Windows pass and fixes

- A full run of `docs/desktop-testing.md` and of IP §6h's open list on a real Windows 10 and a real Windows 11 machine, one of them managed by corporate IT if possible. Then the same, shorter, on macOS and Ubuntu.
- **Size:** 3 days, the least certain number in this plan.

---

## 6. Order and size

```
D0 ─ D1 ─ D2 ─ D3 ─ D4 ─ D5 ─ D6 ─ D7 ─ D8
```

| Milestone | Days | Risk |
|---|---|---|
| D0 Spec and spikes | 4 | Low; decides D2's and D3's details |
| D1 Core and daemon split | 3 | Low; large diff, no behaviour |
| D2 Lock and `kalka daemon` | 2 | Low |
| D3 Socket IPC and API hardening | 3 | Medium (Windows socket) |
| D4 CLI with and without a daemon | 3 | Medium (process start on three systems) |
| D5 Sync controls, settings, notices | 5 | Low |
| D6 Desktop shell | 7 | High (new framework, three systems) |
| D7 Builds, packaging, signing, docs | 5 | Medium |
| D8 Windows pass | 3 | High (unknown findings) |
| **Total** | **35** | about 7 weeks |

- D1 to D5 contain no Wails and no cgo. After D5 the product has everything in the request except the window and the tray, and it runs headless and in a browser. That is a sensible place to stop and look.
- D4 can swap with D5. D6 needs D2, D3 and D5.
- If the Wails spike in D0 fails on Windows, D1 to D5 are unaffected, and D6 is re-planned with the fallback tray library.

---

## 7. Risks and how to reduce them

| # | Risk | Reduction |
|---|---|---|
| R1 | **Wails v3 is three weeks old as a stable release** and moves every week. Tray, close and notification behaviour may still change or break. | An exact version pin; Wails behind one small package; no bindings, asset server or build tool; the D0 spike before any commitment; `fyne.io/systray` as the tray fallback; the browser UI as the fallback for everything. |
| R2 | **Windows has never been tested on a real machine,** and it is the primary platform. Open from before: toasts, the message box, console flashes (IP §6h). New: WebView2, the socket, the tray, autostart of a GUI program, endpoint protection. | A Windows machine from D0 on (Q1). D8 is its own milestone. The Windows items are spiked first. |
| R3 | **The page cannot stream through Wails' asset server on Windows.** | Avoided by design (D2). The spike confirms that a loopback URL streams in WebView2, WKWebView and WebKitGTK. |
| R4 | **A Unix socket on Windows** may be blocked or slowed by security software, or behave differently under an old build. | Spike 2. A named pipe behind the same two functions as the prepared alternative. |
| R5 | **Linux tray and webview.** No tray on stock GNOME; WebKitGTK 6.0 missing on Ubuntu 22.04 and Debian 12; blank windows with NVIDIA drivers. | Minimize instead of hide when no tray host answers; second launch always shows the window; Ubuntu 24.04 as the stated base; the headless daemon and the browser everywhere else. Linux is lower priority in the request. |
| R6 | **Webview rendering differences.** Not yet known, since nothing was run. Expected places: the sandboxed Mermaid frame and its CSP in WebKit; clipboard access for the invite token (`web/src/Team.tsx:418`); fonts and scroll bars; `localStorage` and cookie persistence in each webview's profile; the missing web Notification API in WKWebView (already handled as "unsupported", `web/src/notify.ts:9-15`). | The manual checklist of D6 names each. Findings and work-arounds are written into §23 in D7, as the request asks. |
| R7 | **No CI.** Desktop builds for Linux and Windows are made by hand and tested by nobody. | Q2. Until then the release tool builds what the host can, and D8 is manual. |
| R8 | **Unsigned desktop binaries.** SmartScreen warnings, Gatekeeper refusing the app, no macOS notifications, endpoint protection distrusting an unsigned program that registers for autostart. | Signing wired as configuration in D7; Q5 for the certificates; unsigned fall-backs documented; the headless path for places that block it. |
| R9 | **A CLI that starts daemons** surprises agents and CI: a background process with network access appears. | Class A and C never start one; `--no-start` and `KALKA_NO_START`; a started daemon is headless; `kalka stop`; all of it in the reference agents read (§6.13) and in `--help`. |
| R10 | **A forgotten pause** takes a member out of replication for days. | Paused tray icon, banner in the page, a notification at each start while paused (D6). |
| R11 | **The core split (D1) disturbs 3,300 lines of tests.** | It comes first, alone, with no behaviour change, and is reviewed as a pure move. |
| R12 | **Notification noise** from a busy team. | One notice per document per round; nothing for one's own entries or first copies; one switch to turn all off. |

---

## 8. Questions for the product owner

Only the ones that change the plan. Each has the default the plan uses if there is no answer.

1. **A Windows test machine.** Is there a real Windows 10 or 11 desktop, ideally one managed by corporate IT, available from D0? *Default: yes, from D0. Without one, D0's spikes and D8 cannot be done, and the plan's Windows claims stay unverified.*
2. **A remote and CI.** May the repository get a remote, so that the existing workflow runs and builds the Linux and Windows desktop binaries? *Default: no; builds are made by hand on a Mac (Windows and macOS targets) and in Docker (Linux), and D8 is fully manual.*
3. **"A pending invite join".** The spec has no approval step for joins (§18.2 decision 5). The plan notifies an owner when someone joins with their invite, and a joiner when their join completes or fails. Is that what was meant, or should owners approve each join? *Default: the first. Manual approval is §18.1 question 5 and a larger change to §19.7.*
4. **What pause means.** Fully offline (the plan: no new wire message, peers see a closed laptop), or only background replication paused while one-to-one sync keeps working? *Default: fully offline, surviving a restart, with a visible paused state.*
5. **Signing.** Will there be an Authenticode certificate (a cloud signing service or a hardware token) and an Apple Developer membership, and who holds them? *Default: none for now; builds are unsigned, signing is wired but unconfigured, and macOS notifications work only in the window.*
6. **Two binaries, and how Windows users get them.** Is `kalka` plus `kalka-desktop` acceptable in place of "a single binary per platform" (T14)? And is a zip enough for Windows, or is a per-user installer with the WebView2 bootstrapper part of this work? *Default: two binaries; a zip now, the installer as the next step after D8.*
7. **A Unix socket on Windows instead of a named pipe.** The request names a pipe. A socket gives one transport on all three systems and no new dependency (D2). *Default: the socket if spike 2 passes, otherwise the pipe.*
