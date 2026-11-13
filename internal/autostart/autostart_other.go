//go:build !darwin && !windows

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/name"
)

// Overridable in tests.
var configHome = func() (string, error) {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".config"), nil
}

func desktopPath() (string, error) {
	c, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(c, "autostart", name.Product+".desktop"), nil
}

// quoteExec quotes an argument for a desktop entry's Exec key (Desktop
// Entry Specification, "The Exec key").
func quoteExec(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`)
	return `"` + r.Replace(s) + `"`
}

func entry(exe string) []byte {
	return []byte(fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Comment=Peer-to-peer blueprint review
Exec=%s %s
Terminal=false
NoDisplay=true
X-GNOME-Autostart-enabled=true
`, name.Product, quoteExec(exe), BackgroundFlag))
}

func enable(exe string) (Status, error) {
	p, err := desktopPath()
	if err != nil {
		return Status{}, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return Status{}, err
	}
	if err := fsutil.WriteAtomic(p, entry(exe), nil); err != nil {
		return Status{}, err
	}
	return get()
}

func disable() error {
	p, err := desktopPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func get() (Status, error) {
	p, err := desktopPath()
	if err != nil {
		return Status{}, err
	}
	s := Status{Where: p}
	raw, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.Enabled = true
	for _, line := range strings.Split(string(raw), "\n") {
		if cmd, ok := strings.CutPrefix(line, "Exec="); ok {
			s.Command = cmd
			if prog, ok := unquoteFirst(cmd); ok {
				_, statErr := os.Stat(prog)
				s.Stale = statErr != nil
			}
		}
	}
	return s, nil
}

// unquoteFirst returns the first quoted argument of an Exec value.
func unquoteFirst(cmd string) (string, bool) {
	if !strings.HasPrefix(cmd, `"`) {
		return "", false
	}
	var b strings.Builder
	for i := 1; i < len(cmd); i++ {
		switch c := cmd[i]; c {
		case '\\':
			if i+1 < len(cmd) {
				i++
				b.WriteByte(cmd[i])
			}
		case '"':
			return b.String(), true
		default:
			b.WriteByte(c)
		}
	}
	return "", false
}
