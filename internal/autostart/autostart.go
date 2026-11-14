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
// The entry starts the desktop shell with --hidden where it is installed
// (spec 22.2), else `kalka daemon --background`: no browser at start,
// prompts as native notifications or popups, output to <config>/kalka.log.
// A second start finds the single-daemon lock held and exits 0 (spec 4.1).
package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/golbinski/kalka/internal/name"
)

// Args are the arguments Spawn, and autostart without a shell, pass to
// the binary.
var Args = []string{"daemon", "--background"}

// ArgString is Args as one command-line string; no argument needs quoting.
const ArgString = "daemon --background"

// HiddenArg starts the desktop shell with no window (spec 22.2).
const HiddenArg = "--hidden"

// DesktopBuild is set by the desktop shell's build: the running binary is
// itself the shell (macOS, where one executable is shell and CLI).
var DesktopBuild bool

// Target is what the entry runs at logon.
type Target struct {
	Exe  string
	Args []string
}

// String is the command line as shown to the user.
func (t Target) String() string { return strings.Join(append([]string{t.Exe}, t.Args...), " ") }

// TargetFor picks what to start at logon for the binary exe: the shell
// with --hidden when exe is the shell or the shell is installed next to
// it, else the headless daemon (on Windows the console-less kalkaw.exe).
func TargetFor(exe string) (Target, error) {
	if DesktopBuild {
		return Target{exe, []string{HiddenArg}}, nil
	}
	d := filepath.Join(filepath.Dir(exe), name.DesktopBinary+filepath.Ext(exe))
	if st, err := os.Stat(d); err == nil && !st.IsDir() {
		return Target{d, []string{HiddenArg}}, nil
	}
	return headless(exe)
}

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

// Enable writes the entry for the binary exe (TargetFor) and, where the
// platform does that, starts it now. It replaces an existing entry.
func Enable(exe string) (Status, error) {
	t, err := TargetFor(exe)
	if err != nil {
		return Status{}, err
	}
	return enable(t)
}

// Disable removes the entry. Removing a missing entry is not an error.
func Disable() error { return disable() }

// Get reports the entry.
func Get() (Status, error) { return get() }

// StartsOnEnable reports whether Enable also starts kalka now (launchd
// does, on RunAtLoad); elsewhere the caller uses Start.
var StartsOnEnable = startsOnEnable

// Start launches what the logon entry runs, detached from this process.
func Start(exe string) error {
	t, err := TargetFor(exe)
	if err != nil {
		return err
	}
	return start(t)
}

// Spawn runs exe with Args detached from this process, with no console
// and no terminal: how the CLI starts a missing daemon (spec 12).
func Spawn(exe string) error { return spawn(exe) }
