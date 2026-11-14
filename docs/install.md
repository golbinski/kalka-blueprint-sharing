# Installing kalka

Each release archive holds `kalka` and `kalka-plugin-gh`. The Windows archive also holds `kalkaw.exe`, which is the same program without a console window. `SHA256SUMS` lists the archives' checksums, and the archives are reproducible from the tagged commit with `make release VERSION=x.y.z`.

kalka runs **per user, started at logon**, not as a system service. A service runs in a separate session and could not show you anything or open your browser. While it runs in the background, a teammate's sync that needs your answer reaches you as:

| | How you hear about it | Clicking it |
|---|---|---|
| Windows, after `kalka autostart on` | A notification in the notification centre, kept until dismissed | Opens kalka in the browser |
| Windows, no autostart | A message box | OK opens kalka |
| macOS | A dialog, "Open kalka" / "Later" | Opens kalka |
| Linux with zenity | A dialog | Opens kalka |
| Anywhere, with a kalka tab open | A browser notification if the tab is in the background | Shows the tab |

The answer itself (accept, compare the code) is always given in the kalka page, within 5 minutes. After that the teammate gets "no answer" and can send again.

## Windows

1. Unzip to a folder of your own, for example `%LOCALAPPDATA%\Programs\kalka`. Keep the three `.exe` files together.
2. Run `kalka.exe` once. The browser opens the setup form: your email, the rendezvous your team uses, and your blueprint folder.
3. In a terminal in that folder, run `kalka autostart on`. This:
   - adds `"…\kalkaw.exe" daemon --background` under `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`;
   - registers kalka's notification identity under `HKCU\Software\Classes\AppUserModelId\com.github.golbinski.kalka`;
   - starts kalka now, in the background.

   Nothing needs administrator rights.
4. `kalka open` opens the page at any time.

The binaries are not code-signed, so SmartScreen may warn on the first start ("More info", then "Run anyway").

To uninstall, run `kalka autostart off`, then delete the folder and `%APPDATA%\kalka`.

## macOS

1. Copy `kalka` and `kalka-plugin-gh` to a folder on your PATH, for example `~/bin`.
2. The binaries are not notarized. If macOS refuses to open them, run `xattr -d com.apple.quarantine ~/bin/kalka ~/bin/kalka-plugin-gh`.
3. Run `kalka` once and finish setup in the browser.
4. Run `kalka autostart on`. It writes `~/Library/LaunchAgents/com.github.golbinski.kalka.plist` and loads it, which starts kalka now.

To uninstall, run `kalka autostart off`, then delete the binaries and `~/.config/kalka`.

## Linux

1. Copy both binaries to a folder on your PATH, for example `~/.local/bin`.
2. Run `kalka` once and finish setup in the browser.
3. Run `kalka autostart on`. It writes `~/.config/autostart/kalka.desktop` and starts kalka now.

Popups need `zenity`. Without it, kalka opens the browser instead, at most once a minute.

## Notes

- `kalka autostart status` shows what runs at logon, and warns if that program has been moved or deleted. After moving the binaries, run `kalka autostart on` again from the new place.
- In the background, kalka writes to `kalka.log` in its config folder. One older file is kept as `kalka.log.1` once the log passes 5 MB.
- One kalka runs per user. `kalka` starts it in the background when it is not running, then opens its page. A second `kalka daemon` prints "already running (pid N)" and exits. `kalka stop` stops it once running rounds finish.
- `kalka daemon` runs it in a terminal instead, where prompts can also be answered with y or n. `--no-popups` keeps prompts in the page and the terminal only.
- Plugins are found in `<config>/plugins` first, then on PATH. `kalka-plugin-gh` needs the GitHub CLI (`gh`), logged in.
