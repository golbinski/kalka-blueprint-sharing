// Package autostart starts kalka when the user logs in (implementation
// plan Q3): per user, never as a system service, so the daemon can reach
// the user's desktop, profile and browser.
//
//   - Windows: a value under HKCU\...\CurrentVersion\Run that starts the
//     console-less kalkaw.exe, and the AppUserModelID registration that
//     lets kalka show notification-centre toasts.
//   - macOS: a LaunchAgent in ~/Library/LaunchAgents, loaded with launchctl.
//   - Linux: an XDG autostart entry in ~/.config/autostart.
//
// The entry runs `kalka daemon --background`: no browser at start,
// prompts as native notifications or popups, output to <config>/kalka.log.
// A second start finds the single-daemon lock held and exits 0 (spec 4.1).
package autostart

import (
	"errors"
	"os"
	"path/filepath"
)

// Args are the arguments autostart and Spawn pass to the binary.
var Args = []string{"daemon", "--background"}

// ArgString is Args as one command-line string; no argument needs quoting.
const ArgString = "daemon --background"

// Status describes the autostart entry.
type Status struct {
	Enabled bool   `json:"enabled"`
	Where   string `json:"where"`             // file or registry path
	Command string `json:"command,omitempty"` // what runs at logon
	// Stale is set when the entry runs a program that no longer exists.
	Stale bool `json:"stale,omitempty"`
}

// ErrNoBackgroundBinary means kalkaw.exe is missing next to kalka.exe.
var ErrNoBackgroundBinary = errors.New("kalkaw.exe is not next to kalka.exe; install both from the release archive")

// Executable returns the absolute path of the running binary, with
// symlinks resolved, so the entry survives a changed working directory.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Abs(exe)
}

// Enable writes the entry for exe and, where the platform does that,
// starts it now. It replaces an existing entry.
func Enable(exe string) (Status, error) { return enable(exe) }

// Disable removes the entry. Removing a missing entry is not an error.
func Disable() error { return disable() }

// Get reports the entry.
func Get() (Status, error) { return get() }

// StartsOnEnable reports whether Enable also starts kalka now (launchd
// does, on RunAtLoad); elsewhere the caller uses Start.
var StartsOnEnable = startsOnEnable

// Start launches the background daemon detached from this process, as the
// logon entry would.
func Start(exe string) error { return start(exe) }

// Spawn runs exe with Args detached from this process, with no console
// and no terminal: how the CLI starts a missing daemon (spec 12).
func Spawn(exe string) error { return spawn(exe) }
