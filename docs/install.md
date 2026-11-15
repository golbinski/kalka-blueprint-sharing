# Installing kalka

kalka is a desktop app that lives in the menu bar (macOS) or the notification area (Windows, Linux) and keeps syncing with your team in the background. It runs **per user, started at login**, not as a system service: a service runs in a separate session and could not show you anything.

Every release has, per system:

| System | Desktop app | Command line and headless only |
|---|---|---|
| Windows | `kalka_<v>_windows_<arch>_setup.exe` (installer) | `kalka_<v>_windows_<arch>.zip` |
| macOS | `kalka_<v>_darwin_<arch>_app.zip` (`Kalka.app`) | `kalka_<v>_darwin_<arch>.tar.gz` |
| Linux | `kalka_<v>_linux_<arch>_desktop.tar.gz` | `kalka_<v>_linux_<arch>.tar.gz` |

`SHA256SUMS` lists the checksums. The archives are reproducible from the tagged commit with `make release VERSION=x.y.z` (signatures and the installer aside).

Where IT policy blocks the desktop app, autostart entries or the webview, see [headless.md](headless.md): every feature except the tray and the window works without them.

## Windows

1. Run `kalka_<v>_windows_amd64_setup.exe` (`arm64` on ARM machines). It needs no administrator rights. It installs into `%LOCALAPPDATA%\Programs\kalka`, adds that folder to your `PATH`, and adds *kalka* to the Start menu.
2. Leave *Start kalka* ticked on the last page. The kalka window opens on the setup form: your email, the rendezvous your team uses, and your blueprint folder.
3. From then on kalka starts at each login, without a window, with its icon in the notification area. Closing the window hides it; the icon, the Start menu, or `kalka` in a terminal brings it back. *Quit kalka* in the icon's menu stops syncing.

The window needs the Microsoft Edge WebView2 runtime, which Windows 10 and 11 normally have. If it is missing, the installer offers its download page, and kalka offers to use the browser instead.

Unsigned builds trigger SmartScreen on first start: *More info*, then *Run anyway*.

**Uninstall:** *Settings > Apps > kalka*, or `uninstall.exe` in the install folder. It stops kalka and removes the login entry, the files and the `PATH` entry. Your settings, keys and team logs stay in `%APPDATA%\kalka`, your blueprints in their folder.

**By hand, from the zip:** unzip to a folder of your own, add it to `PATH`, and start `kalka-desktop.exe`. Keep `kalka.exe`, `kalka-desktop.exe` and `kalka-plugin-gh.exe` together: `kalka.exe` is the command line, `kalka-desktop.exe` the app (and, with arguments, the command line without a console window).

## macOS

1. Unzip `kalka_<v>_darwin_arm64_app.zip` (Apple Silicon) or `…_amd64_app.zip` (Intel) and move `Kalka.app` to `/Applications` or `~/Applications`.
2. Open it. An unsigned build is refused the first time: right-click `Kalka.app`, *Open*, then *Open* again (or run `xattr -dr com.apple.quarantine Kalka.app`). The window shows the setup form.
3. kalka then lives in the menu bar and starts at each login (a LaunchAgent in `~/Library/LaunchAgents`). It has no Dock icon.
4. For the command line, link the app's program onto your `PATH`; with arguments it is the CLI, and `kalka-plugin-gh` is found beside it:

   ```sh
   ln -s /Applications/Kalka.app/Contents/MacOS/kalka ~/bin/kalka
   ```

Unsigned builds cannot post to the notification centre: prompts that need an answer show as dialogs, other notices only in the window.

**Uninstall:** *Quit kalka* from the menu bar, `kalka autostart off`, then delete `Kalka.app` and `~/.config/kalka`.

## Linux

1. Install the libraries listed in `PACKAGES.txt` (GTK 4 and WebKitGTK 6.0; Ubuntu 24.04, Debian 13, Fedora 39 or newer).
2. Copy `kalka`, `kalka-desktop` and `kalka-plugin-gh` to `~/.local/bin`, `kalka.desktop` to `~/.local/share/applications`, and `kalka.png` to `~/.local/share/icons/hicolor/256x256/apps`.
3. Start *kalka* from the applications menu. The window shows the setup form; kalka then starts at each login (`~/.config/autostart/kalka.desktop`).

The tray icon uses StatusNotifierItem. Stock GNOME shows it only with an extension such as *AppIndicator and KStatusNotifierItem Support*; without one, closing the window minimises it, and starting kalka again shows it.

**Uninstall:** *Quit kalka* from its menu, `kalka autostart off`, then delete the files above and `~/.config/kalka`.

## How prompts reach you

A teammate's sync that needs your answer (a handoff, a new blueprint, the first-sync code) shows as a notification while the window is hidden; clicking it opens the window on the prompt. Answer within 5 minutes; after that the teammate gets "no answer" and can send again. The other notices (new comments on your documents, someone joined with your invite, your join completed, a newer domain version) can be turned off in *Settings*.

## Notes

- One kalka runs per user. A second start of the app shows the first one's window. `kalka status` in a terminal shows whether it runs and whether the window is attached; `kalka stop` stops it once running rounds finish.
- `kalka autostart status` shows what runs at login, and warns if that program has been moved. *Settings > Start at login* or `kalka autostart on|off` changes it.
- kalka writes to `kalka.log` in its config folder (`%APPDATA%\kalka`, `~/.config/kalka`). One older file is kept as `kalka.log.1` once it passes 5 MB.
- Plugins are found in `<config>/plugins`, then beside kalka, then on `PATH`. `kalka-plugin-gh` needs the GitHub CLI (`gh`), logged in.
