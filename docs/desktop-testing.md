# Desktop app: the manual test pass

The automated tests cover the shell's decisions without a display
(`internal/desktop`), the daemon's side (`internal/daemon`), the page's
models (`web/src/*.test.ts`) and one smoke run of the real shell (`go test
-tags desktop ./cmd/kalka-desktop`). This script is what only a person at
the machine can check: run it before a release. It is written as one
pass, in order, for Windows; on macOS and Linux run the same pass with the
items marked for them.

**What you need**

- **A**: the Windows machine under test, with the release built by
  `make release VERSION=x.y.z` (the installer and the zip).
- **B**: a second installation, as a teammate: another machine, or the same
  one with a second config directory and another `ui_port`:

  ```sh
  export KALKA_CONFIG_DIR=$HOME/kalka-b      # PowerShell: $env:KALKA_CONFIG_DIR = "$HOME\kalka-b"
  ```

- **A rendezvous both reach**: `kalka-rendezvous --listen :9000` from the
  release's `kalka-rendezvous_<v>_<os>_<arch>` archive on any host (users
  then set `http://<host>:9000`), or the public PeerJS cloud for a quick
  run.
- Test content: the blueprints in `samples/blueprints` (see
  `samples/README.md`), copied into the folder's `personal\`.

Write down anything that differs from what an item says, with the time and
the line from `kalka.log` (`%APPDATA%\kalka\kalka.log`) if there is one.

## 1. Install (A)

- [ ] Run `kalka_<v>_windows_amd64_setup.exe`. No elevation prompt. It
      installs into `%LOCALAPPDATA%\Programs\kalka` and adds *kalka* to the
      Start menu.
- [ ] Unsigned build: SmartScreen asks once; *More info*, *Run anyway*.
- [ ] A new terminal finds `kalka` (`kalka version` prints the version and
      the protocol numbers).
- [ ] `HKCU\Software\Classes\kalka\shell\open\command` names
      `"…\kalka-desktop.exe" "%1"`.
- [ ] Leave *Start kalka* ticked on the last page: the window opens on the
      setup form; the tray icon appears.

## 2. Setup and the first team (A)

- [ ] Fill in email, rendezvous and folder; *Save and start*. The window
      shows *Blueprints · Personal* (no team yet), and the folder holds
      `personal\`.
- [ ] `kalka status` says running, and that the desktop app is attached.
- [ ] Start kalka again from the Start menu: the same window comes forward,
      no second tray icon. Bare `kalka` in a terminal does the same.
- [ ] `kalka open` opens a browser tab with the same page; both stay live.
- [ ] Switcher (top left) → *Create team…* → `lending-desk`. The page
      switches to it; the switcher lists Personal, then `lending-desk`.
- [ ] Settings: *Start at login* is on (first start turns it on once);
      *Keep running when the window is closed* is shown in the window and
      absent in the browser tab. The version line is at the bottom.

## 3. Invite and join by link (A invites, B joins)

- [ ] A: *Team* → invite B's email → *Invite*. The box shows the
      `kalka://join/…` link; *Copy link* and *Copy token* both copy.
- [ ] Send the link to B through a chat tool or mail and click it there,
      with kalka running on B: B's window comes forward on the join screen,
      showing the team, the inviter, the invited email and the expiry.
      Nothing is joined until *Join*.
- [ ] Repeat with kalka not running on B: kalka starts on the join screen.
- [ ] Repeat on an installation with no configuration: the setup form is
      filled in with the email and the rendezvous from the invite; after
      the folder it goes on to the join screen.
- [ ] A link for another email, an expired one (`--expires 1m`, wait), and
      one for another rendezvous each show their line instead of *Join*.
- [ ] B: *Join*. While A is reached, B's switcher shows "joining
      lending-desk…"; then the team appears, and A gets a "joined" notice.
- [ ] B: *Join with link or token…* in the switcher, paste the token of a
      fresh invite: the same join screen.
- [ ] A: reload the page, then *Show link* on an open invite made earlier:
      the same box, and *Copy link* and *Copy token* copy it.
      `kalka team invites` prints that link; used invites show none.
- [ ] A: *Team* shows B with presence "online"; stop kalka on B and the
      presence turns to "offline" within a minute.

## 4. Publish, review, hand off (A and B)

- [ ] A: copy the sample folder in, in one go (Explorer, or
      `Copy-Item samples\blueprints <folder>\personal\samples -Recurse`).
      All six show in *Blueprints · Personal* within a second,
      `interlibrary-requests.md` as a local blueprint, each with *Publish to
      team…*.
- [ ] A: publish `lending-desk-circulation-domain.md` to `lending-desk`.
      *Domains* lists `circulation` v1.
- [ ] `hold-shelf-expiry.md`: *Publish to team…* → `lending-desk`. The file
      moves to the team folder at version 1. Its Mermaid diagram and trace
      render (one divergence: C3), its domain reference resolves to v1,
      and it reaches B within seconds (*Sync now* on *Team* forces a
      round).
- [ ] A: edit the domain document and release v2. B's copy of
      `hold-shelf-expiry.md` shows "newer domain version available (v2)".
- [ ] B: comment on a section. A, window hidden: one notification for the
      new comment; clicking it opens the document.
- [ ] A: edit the plan; the review strip offers *Release v2*. Release it; B
      gets v2.
- [ ] A: *Hand off…* to B. B's switcher badge and the tray read "1 waiting
      for you"; the tray item opens B's waiting list for `lending-desk`.
- [ ] B: *Open* the item. It clears, and the badge goes.
- [ ] A (team owner): *Reassign…* the document back to A from its review.
      B's copy says it was reassigned, and A releases on its own.
- [ ] A: hand another document to B, then on *Team* use *Reassign…* in B's
      row: it lists B's documents (the domain document labelled with its
      domain), and *Reassign* takes one back. The history reads
      "reassigned "<title>" from … to …".

## 5. Transfers between teams (A)

- [ ] A: create a second team `returns-desk` from the switcher; B joins it.
- [ ] A: on the published document, *Copy to team…* → `returns-desk`. B, as
      a member but not an owner, sees nothing waiting; A (owner of
      `returns-desk`) has a *Transfer* in its waiting list.
- [ ] A: *Accept*. The copy appears in `returns-desk` with the "attested"
      strip naming who offered and who accepted it; its old comments carry
      an "attested" badge.
      *Team → History* of `returns-desk` reads "accepted "<title>" from
      another team, owned by …"; a declined one later reads "declined …".
- [ ] A: *Move to team…* another document. Until it is decided the review
      shows "being moved to …; comments added now will not travel".
      *Withdraw* it from *Team → Transfers*, then move again and *Decline*
      from the waiting list. Each ends with a notice on the offering side.

## 6. Tray and settings

- [ ] The status line reads "Online · N of M members reachable" and changes
      within seconds when B starts or stops.
- [ ] Stop the rendezvous: "Offline: …" and the ring icon; start it again
      and it comes back on its own.
- [ ] *Pause sync*: two bars on the icon, "Paused" in the tray, a banner in
      the page. Edits and releases are kept; *Resume sync* sends them.
- [ ] Settings → *Blueprint reference: Save a copy*: the browser tab saves
      `BLUEPRINT-REFERENCE.md`. In the window, if saving does nothing, *Copy*
      puts it on the clipboard; note which worked.
- [ ] Notifications off: no OS notifications; notices still show in the
      page.
- [ ] *Quit kalka* asks first; *Cancel* keeps it; *Quit* stops it and
      `kalka status` says not running.

## 7. Close, login, headless

- [ ] Close the window: it hides, with one notification that kalka keeps
      syncing. Close it again later: no notification.
- [ ] Keep running off, close the window: the shell and its daemon stop.
- [ ] Log out and in: kalka runs with no window, the tray icon is there.
      `kalka autostart status` shows the entry running the shell with
      `--hidden`.
- [ ] `kalka stop`, then `kalka daemon` in a terminal, then start the app:
      it attaches. Quitting from the tray says it also stops the kalka
      started without a window, and the terminal daemon stops.
- [ ] `kalka-desktop.exe status` (the GUI build with arguments) prints
      nothing to a console window and returns at once.

## 8. Windows only

- [ ] Remove or block WebView2: starting the app names the download and
      offers *Open kalka in the browser instead*. The window is never blank.
- [ ] Notifications show "kalka" as the sender.
- [ ] Install again over a running kalka: it is stopped first, the files are
      replaced, the login entry still works.
- [ ] A `PATH` longer than 900 characters is left alone, with a message.
- [ ] Uninstall (*Settings → Apps*): kalka stops; the login entry, files,
      Start-menu entry, `PATH` entry and the `kalka` URL key are gone;
      `%APPDATA%\kalka` and the blueprint folder stay.

## On macOS and Linux

Run sections 2 to 7 the same way. Instead of section 1:

- macOS: `Kalka.app` from the zip opens after right-click → *Open*, has no
  Dock icon, and lives in the menu bar. Opening the installed bundle
  registers `kalka://`; never open a development build, which would stay
  registered. The `kalka` symlink to `Kalka.app/Contents/MacOS/kalka` works
  as the CLI.
- Linux: after the first start, `~/.local/share/applications/kalka.desktop`
  has `MimeType=x-scheme-handler/kalka;` and `xdg-mime query default
  x-scheme-handler/kalka` prints `kalka.desktop`. Without a tray host
  (stock GNOME), closing the window minimises it.
