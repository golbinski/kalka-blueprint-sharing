# Desktop app: manual test checklist

The automated tests cover the shell's decisions without a display
(`internal/desktop`), the daemon's side (`internal/daemon`, `d6_test.go`)
and one smoke run of the real shell (`go test -tags desktop
./cmd/kalka-desktop`). This list is what only a person at the machine can
check. Run it on each system before a release; D8 runs it on Windows.

Use a scratch config directory, and mark start at login as already set up,
unless the point of the run is the first-start behaviour:

```sh
export KALKA_CONFIG_DIR=$HOME/kalka-test
printf 'autostart_initialized: true\n' > $KALKA_CONFIG_DIR/state.yaml   # after setup
```

Two machines (or two config directories with different `ui_port`s) and one
rendezvous are needed for the notification items.

## Start and the window

- [ ] First start with no config: the window opens on the setup form; after
      saving, it shows the blueprint list. The tray icon appears.
- [ ] `kalka status` says running, and "window: the desktop app is attached".
- [ ] Start the shell again (shortcut, or `kalka-desktop`): the existing
      window comes forward; no second tray icon.
- [ ] Bare `kalka` in a terminal shows the window, not a browser tab.
- [ ] `kalka open` still opens a browser tab; both stay live.
- [ ] A link to another site in a blueprint opens in the system browser; the
      window stays on kalka.
- [ ] The page in the window shows no "turn on desktop notifications"
      banner; the Settings view shows "Keep running when the window is
      closed". In a browser tab that setting is absent.

## Tray

- [ ] The status line reads "Online · N of M peers reachable · last sync
      HH:MM" and changes within a second or two of a sync.
- [ ] Stop the rendezvous: the line becomes "Offline: …" and the icon a ring.
- [ ] Pause sync: the icon shows two bars, the line says "Paused", the page
      shows its banner. Resume sync restores all three.
- [ ] With a prompt or a pending join, "N waiting for you" appears and opens
      the window (on the Team page for a join).
- [ ] Start at login: the check mark follows `kalka autostart status`;
      toggling it changes the system entry (and the page's Settings view).
- [ ] Quit kalka asks "kalka will stop syncing with your team until you
      start it again."; Cancel keeps it running; Quit stops it within the
      drain time, and `kalka status` says not running.

## Close and reopen

- [ ] Close the window: it hides, and one notification says kalka keeps
      syncing. Close it again later: no notification.
- [ ] Turn "Keep running when the window is closed" off, close the window:
      the shell and its daemon stop, without a question.
- [ ] Linux without a tray host (stock GNOME): closing minimises instead,
      and starting the shell again shows the window.

## Start at login

- [ ] First start of a fresh install turns start at login on (once); turning
      it off and restarting leaves it off.
- [ ] Log out and in: kalka runs with no window; the tray icon is there.
- [ ] The entry runs the shell with `--hidden` (`kalka autostart status`).

## Attach to a headless daemon

- [ ] `kalka daemon` in a terminal, then start the shell: it attaches (tray
      and window), and the terminal shows no popup for prompts while the
      shell runs.
- [ ] Close the window with keep running off: the shell quits, the terminal
      daemon keeps running.
- [ ] Quit from the tray: the question says it also stops the kalka started
      without a window, and the terminal daemon stops.
- [ ] `kalka stop` while the shell runs: the shell quits too.

## Notifications (window hidden)

- [ ] A handoff from the other machine: a notification; clicking it shows
      the window with the prompt.
- [ ] New comments on a document you own: one notification per document;
      clicking opens the document.
- [ ] Someone joins with your invite; your own join completes or fails.
- [ ] A newer domain version than one your document references.
- [ ] Notifications off in Settings: no OS notifications; prompts still
      show in the window.
- [ ] With the window visible, notices show as toasts in the page instead.
- [ ] macOS, unsigned build outside an app bundle: no notification centre;
      a prompt shows as a dialog with "Open kalka", other notices only in
      the window.

## Packages

- [ ] Windows installer: installs without an elevation prompt into
      `%LOCALAPPDATA%\Programs\kalka`; a new terminal finds `kalka`; the
      Start-menu entry starts the app; *Start kalka* on the last page works.
- [ ] Installing again over a running kalka stops it first and replaces
      the files; the login entry still works.
- [ ] Uninstall (Settings > Apps): kalka stops, the login entry, files,
      Start-menu entry and PATH entry are gone; `%APPDATA%\kalka` stays.
- [ ] A PATH longer than 900 characters is left alone, with a message.
- [ ] macOS: `Kalka.app` from the zip opens after right-click > Open, has
      no Dock icon, shows its icon in Finder; the `kalka` symlink works as
      the CLI and finds `kalka-plugin-gh`.
- [ ] Linux: the `.desktop` entry appears in the applications menu with
      its icon and starts the app.

## Windows only

- [ ] Remove or block WebView2: starting the shell shows a message naming
      the download and offers "Open kalka in the browser instead", which
      starts the daemon and opens a tab. The window is never blank.
- [ ] Notifications show "kalka" as the sender (the Start-menu shortcut's
      AppUserModelID).
- [ ] `kalka-desktop.exe status` (the GUI build with arguments) prints
      nothing to a console window and returns at once; use `kalka.exe` in a
      terminal.
