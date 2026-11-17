// Package pickfolder shows the system's own folder dialog, for the setup
// form's "Choose…" (spec 5). It uses what each system ships: AppleScript
// on macOS, PowerShell on Windows, zenity or kdialog on Linux. A canceled
// dialog is "" and no error.
package pickfolder

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// ErrUnavailable means this system has no folder dialog kalka can show.
var ErrUnavailable = errors.New("no folder dialog is available here; type the path")

// Prompt is the dialog's title.
const Prompt = "Choose the blueprint folder"

// Pick shows the dialog, starting at start when it exists, and returns the
// chosen folder.
func Pick(ctx context.Context, start string) (string, error) {
	name, args, err := command(start)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	hide(cmd)
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return "", nil // canceled
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(strings.TrimSpace(string(out)), `/\`) + trailing(string(out)), nil
}

// trailing keeps a root such as "/" or `C:\` whole.
func trailing(out string) string {
	t := strings.TrimSpace(out)
	if t == "/" || (len(t) == 3 && t[1] == ':') {
		return t[len(t)-1:]
	}
	return ""
}

func command(start string) (string, []string, error) {
	switch runtime.GOOS {
	case "darwin":
		script := `POSIX path of (choose folder with prompt "` + Prompt + `"`
		if start != "" {
			script += ` default location (POSIX file "` + appleQuote(start) + `")`
		}
		return "osascript", []string{"-e", script + ")"}, nil
	case "windows":
		ps := `Add-Type -AssemblyName System.Windows.Forms;` +
			`$d = New-Object System.Windows.Forms.FolderBrowserDialog;` +
			`$d.Description = '` + Prompt + `';` +
			`$d.ShowNewFolderButton = $true;` +
			`$d.SelectedPath = '` + strings.ReplaceAll(start, "'", "''") + `';` +
			`if ($d.ShowDialog() -eq 'OK') { [Console]::Out.Write($d.SelectedPath) } else { exit 1 }`
		return "powershell", []string{"-NoProfile", "-STA", "-NonInteractive", "-Command", ps}, nil
	default:
		if p, err := exec.LookPath("zenity"); err == nil {
			args := []string{"--file-selection", "--directory", "--title=" + Prompt}
			if start != "" {
				args = append(args, "--filename="+strings.TrimRight(start, "/")+"/")
			}
			return p, args, nil
		}
		if p, err := exec.LookPath("kdialog"); err == nil {
			return p, []string{"--title", Prompt, "--getexistingdirectory", start}, nil
		}
		return "", nil, ErrUnavailable
	}
}

// appleQuote escapes a path for an AppleScript string.
func appleQuote(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
