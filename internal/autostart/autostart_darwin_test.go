package autostart

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchAgent(t *testing.T) {
	home := t.TempDir()
	homeDir = func() (string, error) { return home, nil }
	var calls []string
	launchctl = func(args ...string) error { calls = append(calls, strings.Join(args, " ")); return nil }
	t.Cleanup(func() { homeDir = os.UserHomeDir })

	exe := filepath.Join(home, "bin & <tools>", "kalka")
	os.MkdirAll(filepath.Dir(exe), 0o755)
	os.WriteFile(exe, []byte("x"), 0o755)

	s, err := Enable(exe)
	if err != nil || !s.Enabled || s.Stale || s.Command != exe+" --background" {
		t.Fatalf("enable: %+v %v", s, err)
	}
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "bootout gui/") || !strings.HasPrefix(calls[1], "bootstrap gui/") || !strings.HasSuffix(calls[1], s.Where) {
		t.Fatalf("launchctl calls %q", calls)
	}
	// The plist is valid for plutil, with the path escaped.
	if _, err := exec.LookPath("plutil"); err == nil {
		if out, err := exec.Command("plutil", "-lint", s.Where).CombinedOutput(); err != nil {
			t.Fatalf("plutil: %s", out)
		}
		out, _ := exec.Command("plutil", "-extract", "ProgramArguments.0", "raw", s.Where).Output()
		if strings.TrimSpace(string(out)) != exe {
			t.Fatalf("program %q", out)
		}
	}

	os.Remove(exe)
	if s, _ := Get(); !s.Stale {
		t.Fatalf("not stale: %+v", s)
	}
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
	if s, _ := Get(); s.Enabled {
		t.Fatalf("still enabled: %+v", s)
	}
	if err := Disable(); err != nil {
		t.Fatalf("second disable: %v", err)
	}
}
