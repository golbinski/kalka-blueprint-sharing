# Installing kalka

kalka is a desktop app that lives in the menu bar (macOS) or the notification area (Windows, Linux) and keeps syncing with your team in the background. It runs **per user, started at login**, not as a system service: a service runs in a separate session and could not show you anything.

## You were sent an invite

A teammate sent you a message with a `kalka://join/…` link and an invite code.

1. Install kalka for your system (below) and start it. Leave the window open.
2. Click the link in the message again. If nothing happens (the link is not clickable, or your system does not pass it on), choose *Join with an invite…* in the switcher at the top left of the kalka window, and paste the link or the code.
3. kalka fills in your email and your team's server from the invite. Choose where to keep your blueprints (the suggested folder is fine) and click *Save and join*.
4. You are in as soon as your teammate's kalka is running. Until then the window shows what it is waiting for, and kalka keeps trying in the background, also with the window closed.

## Downloads

Every release has, per system:

| System | Desktop app | Command line and headless only |
|---|---|---|
| Windows | `kalka_<v>_windows_<arch>_setup.exe` (installer) | `kalka_<v>_windows_<arch>.zip` |
| macOS | `kalka_<v>_darwin_<arch>_app.zip` (`Kalka.app`) | `kalka_<v>_darwin_<arch>.tar.gz` |
| Linux | `kalka_<v>_linux_<arch>_desktop.tar.gz` | `kalka_<v>_linux_<arch>.tar.gz` |

The release also has `kalka-rendezvous_<v>_<os>_<arch>.tar.gz` (`.zip` on Windows): the rendezvous server, for the one host your team runs it on, not for users ([rendezvous.md](rendezvous.md)).

`SHA256SUMS` lists the checksums, `SHA256SUMS.minisig` signs them with the kalka release key, and every file has a build provenance attestation from the workflow that built it. [SECURITY.md](../SECURITY.md) shows how to check a download with `minisign` and `gh attestation verify`. The archives are reproducible from the tagged commit with `make release VERSION=x.y.z` (signatures and the installer aside).

Where IT policy blocks the desktop app, autostart entries or the webview, see [headless.md](headless.md): every feature except the tray and the window works without them.

## Windows

1. Run `kalka_<v>_windows_amd64_setup.exe` (`arm64` on ARM machines). It needs no administrator rights. It installs into `%LOCALAPPDATA%\Programs\kalka`, adds that folder to your `PATH`, and adds *kalka* to the Start menu.
2. Leave *Start kalka* ticked on the last page. The kalka window opens on the setup form: your email, your team's kalka server (the *rendezvous*, or the public demo server to try kalka), and your blueprint folder.
3. From then on kalka starts at each login, without a window, with its icon in the notification area. Closing the window hides it; the icon, the Start menu, or `kalka` in a terminal brings it back. *Quit kalka* in the icon's menu stops syncing.

The window needs the Microsoft Edge WebView2 runtime, which Windows 10 and 11 normally have. If it is missing, the installer offers its download page, and kalka offers to use the browser instead.

Unsigned builds trigger SmartScreen on first start: *More info*, then *Run anyway*.

**Uninstall:** *Settings > Apps > kalka*, or `uninstall.exe` in the install folder. It stops kalka and removes the login entry, the files and the `PATH` entry. Your settings, keys and team logs stay in `%APPDATA%\kalka`, your blueprints in their folder.

**By hand, from the zip:** unzip to a folder of your own, add it to `PATH`, and start `kalka-desktop.exe`. Keep `kalka.exe`, `kalka-desktop.exe`, `kalka-plugin-gh.exe` and `kalka-plugin-jira.exe` together: `kalka.exe` is the command line, `kalka-desktop.exe` the app (and, with arguments, the command line without a console window).

## macOS

1. Unzip `kalka_<v>_darwin_arm64_app.zip` (Apple Silicon) or `…_amd64_app.zip` (Intel) and move `Kalka.app` to `/Applications` or `~/Applications`.
2. Open it. An unsigned build is refused the first time: right-click `Kalka.app`, *Open*, then *Open* again (or run `xattr -dr com.apple.quarantine Kalka.app`). The window shows the setup form.
3. kalka then lives in the menu bar and starts at each login (a LaunchAgent in `~/Library/LaunchAgents`). It has no Dock icon.
4. For the command line, link the app's program onto your `PATH`; with arguments it is the CLI, and the plugins (`kalka-plugin-gh`, `kalka-plugin-jira`) are found beside it:

   ```sh
   ln -s /Applications/Kalka.app/Contents/MacOS/kalka ~/bin/kalka
   ```

Unsigned builds cannot post to the notification centre: prompts that need an answer show as dialogs, other notices only in the window.

**Uninstall:** *Quit kalka* from the menu bar, `kalka autostart off`, then delete `Kalka.app` and `~/.config/kalka`.

## Linux

1. Install the libraries listed in `PACKAGES.txt` (GTK 4 and WebKitGTK 6.0; Ubuntu 24.04, Debian 13, Fedora 39 or newer).
2. Copy `kalka`, `kalka-desktop`, `kalka-plugin-gh` and `kalka-plugin-jira` to `~/.local/bin`, `kalka.desktop` to `~/.local/share/applications`, and `kalka.png` to `~/.local/share/icons/hicolor/256x256/apps`.
3. Start *kalka* from the applications menu. The window shows the setup form; kalka then starts at each login (`~/.config/autostart/kalka.desktop`).

The tray icon uses StatusNotifierItem. Stock GNOME shows it only with an extension such as *AppIndicator and KStatusNotifierItem Support*; without one, closing the window minimises it, and starting kalka again shows it.

**Uninstall:** *Quit kalka* from its menu, `kalka autostart off`, then delete the files above and `~/.config/kalka`.

## Updates

The desktop app looks for a new release on GitHub shortly after it starts and every six hours, and *Settings > Updates* (or `kalka update --check`) looks now. A release is compatible when it has the same major version (before 1.0, the same minor version); with *Install updates automatically* on, a compatible release is downloaded and checked against `SHA256SUMS`, whose signature by the release key is checked first, and the window and the tray offer *Restart to update*. Nothing restarts until you click. With the setting off, or for a new major version, they show that an update is available, with its release notes, and *Download and restart* downloads it then.

kalka replaces itself only where it recognises the install: the Windows installer's folder, `Kalka.app` in a folder you can write to, or the Linux programs side by side in a writable folder. Elsewhere (a headless install, a package manager) it only tells you about the release. `kalka update` does the same from a terminal. Builds without a release version (`dev`) never check. The Linux arm64 desktop archive is not published yet; there the release is announced only.

## How notices reach you

While the window is hidden, notices show as notifications: new comments on your documents, someone joined with your invite, your join completed, a newer domain version, an update ready to install. Clicking one opens the window on its subject. They can be turned off in *Settings*, or with `kalka daemon --no-notifications`.

## Notes

- One kalka runs per user. A second start of the app shows the first one's window. `kalka status` in a terminal shows whether it runs and whether the window is attached; `kalka stop` stops it once running rounds finish.
- `kalka autostart status` shows what runs at login, and warns if that program has been moved. *Settings > Start at login* or `kalka autostart on|off` changes it.
- kalka writes to `kalka.log` in its config folder (`%APPDATA%\kalka`, `~/.config/kalka`). One older file is kept as `kalka.log.1` once it passes 5 MB.
- Plugins are found in `<config>/plugins`, then beside kalka, then on `PATH`. `kalka-plugin-gh` needs the GitHub CLI (`gh`), logged in; `kalka-plugin-jira` needs `kalka-plugin-jira login` ([jira.md](jira.md)).
