//go:build !windows

package update

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestApplyTarballRelaunches(t *testing.T) {
	dir := t.TempDir()
	for _, p := range programs {
		os.WriteFile(filepath.Join(dir, p), []byte("old"), 0o755)
	}
	files := map[string]string{}
	for _, p := range programs {
		files[p] = "new"
	}
	var ran []string
	orig := start
	start = func(args ...string) error { ran = args; return nil }
	defer func() { start = orig }()
	if err := Apply(Target{Tarball, dir}, tarball(t, files)); err != nil {
		t.Fatal(err)
	}
	// The new kalka starts once this process has ended.
	if len(ran) != 5 || ran[0] != "/bin/sh" || ran[3] != strconv.Itoa(os.Getpid()) || ran[4] != filepath.Join(dir, "kalka-desktop") {
		t.Fatalf("ran %q", ran)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "kalka")); string(b) != "new" {
		t.Errorf("kalka = %q", b)
	}
}

func TestApplyRefusesWhatItDoesNotKnow(t *testing.T) {
	orig := start
	start = func(...string) error { t.Fatal("started something"); return nil }
	defer func() { start = orig }()
	if err := Apply(Target{}, "x"); err == nil || !strings.Contains(err.Error(), "cannot update") {
		t.Fatalf("err = %v", err)
	}
}
