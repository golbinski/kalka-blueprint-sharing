package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/golbinski/kalka/internal/desktop"
	"github.com/golbinski/kalka/internal/name"
)

// installer builds the per-user Windows installer (spec 23.1) from the
// staged, signed executables, when makensis is available.
func (r *release) installer(stage, goarch string) error {
	out := fmt.Sprintf("kalka_%s_windows_%s_setup.exe", r.version, goarch)
	mk, err := exec.LookPath("makensis")
	if err != nil {
		fmt.Fprintf(r.log, "skip %s: makensis not found (install NSIS: brew install makensis, apt install nsis, or nsis.sourceforge.io)\n", out)
		return nil
	}
	dir := filepath.Join(r.tmp, "nsis-"+goarch)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	script, err := nsisScript(nsisVars{Version: r.version, Arch: goarch, AppID: name.AppID,
		Src: stage, Out: filepath.Join(dir, out), WebView2: desktop.WebView2Download})
	if err != nil {
		return err
	}
	nsi := filepath.Join(dir, "kalka.nsi")
	if err := os.WriteFile(nsi, script, 0o644); err != nil {
		return err
	}
	cmd := exec.Command(mk, "-V2", "-INPUTCHARSET", "UTF8", nsi)
	cmd.Stdout, cmd.Stderr = r.log, r.log
	fmt.Fprintf(r.log, "build %s\n", out)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("makensis: %w", err)
	}
	if err := r.sign.windows(filepath.Join(dir, out), out, r.log); err != nil {
		return err
	}
	return r.addFile(filepath.Join(dir, out), out)
}

//go:embed kalka.nsi.tmpl
var nsisTemplate string

type nsisVars struct{ Version, Arch, AppID, Src, Out, WebView2 string }

func nsisScript(v nsisVars) ([]byte, error) {
	t, err := template.New("nsi").Delims("[[", "]]").Parse(nsisTemplate)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	err = t.Execute(&b, v)
	return b.Bytes(), err
}

// macApp builds Kalka.app (spec 23.1): one executable that is the shell
// without arguments and the CLI with them, with the gh plugin beside it.
func (r *release) macApp(stage, goarch string) error {
	out := fmt.Sprintf("kalka_%s_darwin_%s_app.zip", r.version, goarch)
	tags, cgo, why := r.desktopBuild("darwin", goarch)
	if why != "" {
		fmt.Fprintf(r.log, "skip %s: %s\n", out, why)
		return nil
	}
	dir := filepath.Join(r.tmp, "app-"+goarch)
	app := filepath.Join(dir, "Kalka.app")
	contents := filepath.Join(app, "Contents")
	macos := filepath.Join(contents, "MacOS")
	if err := os.MkdirAll(filepath.Join(contents, "Resources"), 0o755); err != nil {
		return err
	}
	if err := r.gobuildCgo("darwin", goarch, build{pkg: "./cmd/kalka-desktop", bin: "Kalka.app/Contents/MacOS/kalka", tags: tags},
		filepath.Join(macos, "kalka"), cgo); err != nil {
		return err
	}
	plugin := filepath.Join(macos, "kalka-plugin-gh")
	if err := copyFile(filepath.Join(stage, "kalka-plugin-gh"), plugin, 0o755); err != nil {
		return err
	}
	icon := "kalka"
	if err := macIcon(filepath.Join(r.tmp, "iconset-"+goarch), filepath.Join(contents, "Resources", "kalka.icns")); err != nil {
		fmt.Fprintf(r.log, "%s: no icon (%v)\n", out, err)
		icon = ""
	}
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), infoPlist(r.version, icon), 0o644); err != nil {
		return err
	}
	label := "Kalka.app (" + goarch + ")"
	if err := r.sign.mac(app, label, []string{plugin}, r.log); err != nil {
		return err
	}
	if r.sign.macNotary != "" {
		sub := filepath.Join(dir, "submit.zip")
		if err := zipDir(app, "Kalka.app", sub, r); err != nil {
			return err
		}
		if err := r.sign.notarize(sub, app, label, r.log); err != nil {
			return err
		}
		os.Remove(sub)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte(macReadme(r.version)), 0o644); err != nil {
		return err
	}
	files, err := collect(dir, strings.TrimSuffix(out, ".zip"))
	if err != nil {
		return err
	}
	return r.archive(out, files)
}

func zipDir(dir, prefix, dst string, r *release) error {
	files, err := collect(dir, prefix)
	if err != nil {
		return err
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeZip(f, files, r.mtime)
}

// macIcon renders the app icon into an .icns with iconutil (macOS only).
func macIcon(iconset, dst string) error {
	if _, err := exec.LookPath("iconutil"); err != nil {
		return fmt.Errorf("iconutil needs macOS")
	}
	iconset += ".iconset"
	if err := os.MkdirAll(iconset, 0o755); err != nil {
		return err
	}
	for _, s := range []int{16, 32, 128, 256, 512} {
		if err := os.WriteFile(filepath.Join(iconset, fmt.Sprintf("icon_%dx%d.png", s, s)), desktop.AppIcon(s), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(iconset, fmt.Sprintf("icon_%dx%d@2x.png", s, s)), desktop.AppIcon(2*s), 0o644); err != nil {
			return err
		}
	}
	out, err := exec.Command("iconutil", "-c", "icns", "-o", dst, iconset).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iconutil: %v: %s", err, out)
	}
	return nil
}

// infoPlist describes the bundle. LSUIElement: no Dock icon, the menu bar
// item is the way in (spec 22.1).
func infoPlist(version, icon string) []byte {
	iconKey := ""
	if icon != "" {
		iconKey = "\t<key>CFBundleIconFile</key>\n\t<string>" + icon + "</string>\n"
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>` + name.AppID + `</string>
	<key>CFBundleName</key>
	<string>` + name.Product + `</string>
	<key>CFBundleDisplayName</key>
	<string>` + name.Product + `</string>
	<key>CFBundleExecutable</key>
	<string>kalka</string>
` + iconKey + `	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>` + version + `</string>
	<key>CFBundleVersion</key>
	<string>` + version + `</string>
	<key>LSMinimumSystemVersion</key>
	<string>` + MacMinimum + `</string>
	<key>LSUIElement</key>
	<true/>
	<key>NSHighResolutionCapable</key>
	<true/>
</dict>
</plist>
`)
}

// linuxDesktop builds the Linux desktop archive (spec 23.1).
func (r *release) linuxDesktop(stage, goarch string) error {
	base := fmt.Sprintf("kalka_%s_linux_%s_desktop", r.version, goarch)
	tags, cgo, why := r.desktopBuild("linux", goarch)
	if why != "" {
		fmt.Fprintf(r.log, "skip %s.tar.gz: %s\n", base, why)
		return nil
	}
	dir := filepath.Join(r.tmp, base)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, b := range []string{"kalka", "kalka-plugin-gh"} {
		if err := copyFile(filepath.Join(stage, b), filepath.Join(dir, b), 0o755); err != nil {
			return err
		}
	}
	if err := r.gobuildCgo("linux", goarch, build{pkg: "./cmd/kalka-desktop", bin: "kalka-desktop", tags: tags}, filepath.Join(dir, "kalka-desktop"), cgo); err != nil {
		return err
	}
	for n, data := range map[string][]byte{
		"kalka.desktop": []byte(desktopEntry),
		"kalka.png":     desktop.AppIcon(256),
		"PACKAGES.txt":  []byte(linuxPackages),
		"README.txt":    []byte(linuxDesktopReadme(r.version)),
	} {
		if err := os.WriteFile(filepath.Join(dir, n), data, 0o644); err != nil {
			return err
		}
	}
	files, err := collect(dir, base)
	if err != nil {
		return err
	}
	return r.archive(base+".tar.gz", files)
}

const desktopEntry = `[Desktop Entry]
Type=Application
Name=kalka
Comment=Peer-to-peer blueprint review
Exec=kalka-desktop
Icon=kalka
Terminal=false
Categories=Development;Office;
StartupWMClass=kalka
`

const linuxPackages = `kalka-desktop needs GTK 4 and WebKitGTK 6.0 (Ubuntu 24.04, Debian 13,
Fedora 39 or newer). The tray uses StatusNotifierItem: stock GNOME shows
it only with an extension such as "AppIndicator and KStatusNotifierItem
Support"; without one, closing the window minimises it.

  Ubuntu, Debian:  sudo apt install libgtk-4-1 libwebkitgtk-6.0-4
  Fedora:          sudo dnf install gtk4 webkitgtk6.0
  Arch:            sudo pacman -S gtk4 webkitgtk-6.0

kalka itself (the CLI and headless daemon) needs none of these.
`

func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}

func readme(version, goos string) string {
	s := "kalka " + version + "\n\nPeer-to-peer review of blueprints. Full guide: docs/install.md in the source.\n\n"
	switch goos {
	case "windows":
		s += `The installer (kalka_` + version + `_windows_*_setup.exe) is the easy way:
it installs per user, with no administrator rights. This archive is the
same programs, for installing by hand:

  kalka.exe            the CLI and the headless daemon (console)
  kalka-desktop.exe    the desktop app; with arguments, the CLI with no
                       console window
  kalka-plugin-gh.exe  the GitHub reference plugin

Install by hand
  1. Unzip to a folder of your own, e.g. %LOCALAPPDATA%\Programs\kalka,
     and add it to your PATH. Keep the three .exe files together.
  2. Start kalka-desktop.exe. Its window opens the setup form; after that
     kalka starts at each logon, in the notification area.
     Without the desktop app: kalka autostart on --headless, then
     kalka open for the page (docs/headless.md).

Unsigned builds trigger SmartScreen on first start ("More info", then
"Run anyway").
Uninstall: kalka stop, kalka autostart off, then delete the folder and
%APPDATA%\kalka.
`
	case "darwin":
		s += `This is the command line and headless build. The desktop app is
Kalka.app (kalka_` + version + `_darwin_*_app.zip), which holds these
programs too.

Install
  1. Copy kalka and kalka-plugin-gh to a folder on your PATH, e.g. ~/bin.
  2. Unsigned binaries are quarantined:
       xattr -d com.apple.quarantine ~/bin/kalka ~/bin/kalka-plugin-gh
  3. Run kalka once: the browser opens the setup form.
  4. kalka autostart on    (a LaunchAgent; kalka starts at each login)

Uninstall: kalka autostart off, then delete the binaries and
~/.config/kalka.
`
	default:
		s += `This is the command line and headless build, for servers and machines
without a desktop. The desktop app is kalka_` + version + `_linux_*_desktop.tar.gz.

Install
  1. Copy kalka and kalka-plugin-gh to a folder on your PATH, e.g. ~/.local/bin.
  2. Run kalka once: the browser opens the setup form.
  3. kalka autostart on    (an entry in ~/.config/autostart)

Popups need zenity. Uninstall: kalka autostart off, then delete the
binaries and ~/.config/kalka.
`
	}
	return s
}

func macReadme(version string) string {
	return "kalka " + version + `

Install
  1. Move Kalka.app to /Applications or ~/Applications.
  2. Open it. Unsigned builds are refused the first time: right-click
     Kalka.app, Open, then Open again (or: xattr -dr com.apple.quarantine
     Kalka.app). The window shows the setup form; kalka then lives in the
     menu bar and starts at each login.
  3. For the command line, link the same program onto your PATH:
       ln -s /Applications/Kalka.app/Contents/MacOS/kalka ~/bin/kalka
     kalka with arguments is the CLI; kalka-plugin-gh is found beside it.

Uninstall: Quit kalka from the menu bar, kalka autostart off, then delete
Kalka.app and ~/.config/kalka.
`
}

func linuxDesktopReadme(version string) string {
	return "kalka " + version + ` (desktop)

See PACKAGES.txt for the system libraries the desktop app needs.

Install
  1. Copy kalka, kalka-desktop and kalka-plugin-gh to ~/.local/bin (on
     your PATH).
  2. Copy kalka.desktop to ~/.local/share/applications and kalka.png to
     ~/.local/share/icons/hicolor/256x256/apps.
  3. Start "kalka" from the applications menu (or kalka-desktop). The
     window shows the setup form; kalka then starts at each login.

Uninstall: Quit kalka from its tray menu, kalka autostart off, then delete
the files above and ~/.config/kalka.
`
}
