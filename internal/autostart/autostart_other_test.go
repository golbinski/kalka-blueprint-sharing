//go:build !darwin && !windows

package autostart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopEntry(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	exe := filepath.Join(cfg, `my "bin" $HOME`, "kalka")
	os.MkdirAll(filepath.Dir(exe), 0o755)
	os.WriteFile(exe, []byte("x"), 0o755)

	s, err := Enable(exe)
	if err != nil || !s.Enabled || s.Stale || !strings.HasSuffix(s.Command, " --background") {
		t.Fatalf("enable: %+v %v", s, err)
	}
	if got, ok := unquoteFirst(s.Command); !ok || got != exe {
		t.Fatalf("round trip %q -> %q", exe, got)
	}
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
	if s, _ := Get(); s.Enabled {
		t.Fatal("still enabled")
	}
}
