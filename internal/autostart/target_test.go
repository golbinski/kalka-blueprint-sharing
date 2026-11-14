package autostart

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/golbinski/kalka/internal/name"
)

// TestTargetFor: the shell with --hidden where it is the running binary
// or installed next to it, else the headless daemon (spec 22.2).
func TestTargetFor(t *testing.T) {
	dir := t.TempDir()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	exe := filepath.Join(dir, name.Binary+ext)
	os.WriteFile(exe, []byte("x"), 0o755)
	os.WriteFile(filepath.Join(dir, name.BackgroundBinary+ext), []byte("x"), 0o755)

	got, err := TargetFor(exe)
	if err != nil || len(got.Args) != 2 || got.Args[0] != "daemon" {
		t.Fatalf("headless: %+v %v", got, err)
	}
	desk := filepath.Join(dir, name.DesktopBinary+ext)
	os.WriteFile(desk, []byte("x"), 0o755)
	if got, _ := TargetFor(exe); got.Exe != desk || len(got.Args) != 1 || got.Args[0] != HiddenArg {
		t.Fatalf("with the shell next to it: %+v", got)
	}
	os.Remove(desk)
	DesktopBuild = true
	t.Cleanup(func() { DesktopBuild = false })
	if got, _ := TargetFor(exe); got.Exe != exe || got.String() != exe+" --hidden" {
		t.Fatalf("desktop build: %+v", got)
	}
}
