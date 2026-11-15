// Package urlscheme registers the kalka:// URL scheme for the current
// user (spec 23.1a), never with administrator rights:
//
//   - Windows: HKCU\Software\Classes\kalka with "URL Protocol" and
//     shell\open\command = "…\kalka-desktop.exe" "%1". The installer
//     writes it and the uninstaller removes it; the shell re-checks it at
//     start.
//   - macOS: CFBundleURLTypes in Kalka.app's Info.plist (PlistURLTypes).
//     The system registers it when the bundle is first opened; nothing is
//     done at run time.
//   - Linux: kalka.desktop with MimeType=x-scheme-handler/kalka in
//     $XDG_DATA_HOME/applications, made the default with xdg-mime. The
//     shell writes it at start, since there is no installer.
//
// Everything a registration touches is a field of Target, so tests run
// against a fake registry, a temporary data directory and a recording
// runner. Tests never register a real handler.
package urlscheme

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/golbinski/kalka/internal/name"
)

// Scheme is the URL scheme.
const Scheme = "kalka"

// SkipEnv, set to 1, makes the shell leave the registration alone: the
// smoke test starts a built shell, which must not register itself.
const SkipEnv = "KALKA_NO_URL_SCHEME"

// ClassesKey is the scheme's key under HKEY_CURRENT_USER.
const ClassesKey = `Software\Classes\` + Scheme

// DesktopFile is the Linux desktop entry's file name.
const DesktopFile = name.Product + ".desktop"

// ErrNotExist is a registry key or value that is not there.
var ErrNotExist = errors.New("not registered")

// Registry is the current user's registry hive (HKCU), by key path.
type Registry interface {
	// String reads a string value; "" names the key's default value.
	String(key, value string) (string, error)
	SetString(key, value, data string) error
	// DeleteTree removes a key and everything under it; a missing key is
	// not an error.
	DeleteTree(key string) error
}

// Runner runs a program and waits for it.
type Runner func(program string, args ...string) error

// Target is one system's registration of the scheme for Exe.
type Target struct {
	OS  string // runtime.GOOS
	Exe string // the shell: kalka-desktop(.exe), or Kalka.app's executable

	Registry Registry // Windows
	DataHome string   // Linux: $XDG_DATA_HOME
	Run      Runner   // Linux: runs xdg-mime
}

// State is what Status found.
type State struct {
	Registered bool   `json:"registered"`
	Command    string `json:"command,omitempty"` // what a link starts
	Detail     string `json:"detail,omitempty"`
}

// System is the real registration for exe on this system.
func System(exe string) Target {
	t := Target{OS: runtime.GOOS, Exe: exe, Registry: systemRegistry(), Run: func(p string, args ...string) error {
		out, err := exec.Command(p, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %v: %s", p, err, strings.TrimSpace(string(out)))
		}
		return nil
	}}
	t.DataHome = os.Getenv("XDG_DATA_HOME")
	if t.DataHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			t.DataHome = filepath.Join(home, ".local", "share")
		}
	}
	return t
}

// Command is the Windows open command for exe.
func Command(exe string) string { return `"` + exe + `" "%1"` }

// Register writes the registration, unless it is already as wanted.
func Register(t Target) error {
	switch t.OS {
	case "windows":
		if t.Registry == nil {
			return errors.New("no registry")
		}
		if st, err := Status(t); err == nil && st.Registered && st.Command == Command(t.Exe) {
			return nil
		}
		for _, v := range []struct{ key, value, data string }{
			{ClassesKey, "", "URL:" + name.Product + " join link"},
			{ClassesKey, "URL Protocol", ""},
			{ClassesKey + `\DefaultIcon`, "", `"` + t.Exe + `",0`},
			{ClassesKey + `\shell\open\command`, "", Command(t.Exe)},
		} {
			if err := t.Registry.SetString(v.key, v.value, v.data); err != nil {
				return fmt.Errorf("register the %s:// scheme: %w", Scheme, err)
			}
		}
		return nil
	case "linux", "freebsd", "openbsd", "netbsd":
		if t.DataHome == "" || t.Run == nil {
			return errors.New("no data directory to register the scheme in")
		}
		dir := filepath.Join(t.DataHome, "applications")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, DesktopFile), []byte(DesktopEntry(t.Exe)), 0o644); err != nil {
			return err
		}
		return t.Run("xdg-mime", "default", DesktopFile, "x-scheme-handler/"+Scheme)
	}
	return nil // macOS: Info.plist
}

// Unregister removes the registration. Linux keeps xdg-mime's default
// line, which names a file that is gone and so does nothing.
func Unregister(t Target) error {
	switch t.OS {
	case "windows":
		if t.Registry == nil {
			return errors.New("no registry")
		}
		return t.Registry.DeleteTree(ClassesKey)
	case "linux", "freebsd", "openbsd", "netbsd":
		err := os.Remove(filepath.Join(t.DataHome, "applications", DesktopFile))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}

// Status reads the registration.
func Status(t Target) (State, error) {
	switch t.OS {
	case "windows":
		if t.Registry == nil {
			return State{}, errors.New("no registry")
		}
		if _, err := t.Registry.String(ClassesKey, "URL Protocol"); err != nil {
			if errors.Is(err, ErrNotExist) {
				return State{Detail: "the " + Scheme + ":// scheme is not registered"}, nil
			}
			return State{}, err
		}
		cmd, err := t.Registry.String(ClassesKey+`\shell\open\command`, "")
		if errors.Is(err, ErrNotExist) {
			return State{Detail: "the " + Scheme + ":// scheme has no open command"}, nil
		} else if err != nil {
			return State{}, err
		}
		return State{Registered: true, Command: cmd}, nil
	case "linux", "freebsd", "openbsd", "netbsd":
		p := filepath.Join(t.DataHome, "applications", DesktopFile)
		raw, err := os.ReadFile(p)
		if errors.Is(err, os.ErrNotExist) {
			return State{Detail: p + " is missing"}, nil
		} else if err != nil {
			return State{}, err
		}
		s := string(raw)
		if !strings.Contains(s, "x-scheme-handler/"+Scheme+";") {
			return State{Detail: p + " does not name the scheme"}, nil
		}
		cmd := ""
		for _, l := range strings.Split(s, "\n") {
			if strings.HasPrefix(l, "Exec=") {
				cmd = strings.TrimPrefix(l, "Exec=")
			}
		}
		return State{Registered: true, Command: cmd}, nil
	}
	return State{Registered: true, Detail: "registered by the app bundle's Info.plist"}, nil
}

// DesktopEntry is the Linux desktop entry starting exe, with the scheme.
// A link arrives as the single argument (%u).
func DesktopEntry(exe string) string {
	return `[Desktop Entry]
Type=Application
Name=` + name.Product + `
Comment=Peer-to-peer blueprint review
Exec=` + execQuote(exe) + ` %u
Icon=` + name.Product + `
Terminal=false
Categories=Development;Office;
MimeType=x-scheme-handler/` + Scheme + `;
StartupWMClass=` + name.Product + `
`
}

// execQuote quotes a path for a desktop entry's Exec key, when needed.
func execQuote(p string) string {
	if !strings.ContainsAny(p, " \t\"'\\$`;&|<>()*?#~=%") {
		return p
	}
	r := strings.NewReplacer(`\`, `\\\\`, `"`, `\\"`, "`", "\\\\`", `$`, `\\$`, `%`, `%%`)
	return `"` + r.Replace(p) + `"`
}

// PlistURLTypes is the Info.plist fragment that registers the scheme
// with Kalka.app (macOS).
const PlistURLTypes = `	<key>CFBundleURLTypes</key>
	<array>
		<dict>
			<key>CFBundleURLName</key>
			<string>` + name.AppID + `.join</string>
			<key>CFBundleURLSchemes</key>
			<array>
				<string>` + Scheme + `</string>
			</array>
		</dict>
	</array>
`
