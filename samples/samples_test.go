package samples

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeed(t *testing.T) {
	folder := t.TempDir()
	names := Names()
	if len(names) < 5 {
		t.Fatalf("only %v", names)
	}
	written, err := Seed(folder)
	if err != nil || len(written) != len(names) {
		t.Fatalf("%v %v", written, err)
	}
	b, err := os.ReadFile(filepath.Join(folder, "personal", "samples", "lending", "loans-domain.md"))
	if err != nil || !strings.Contains(string(b), "kalka_kind: domain") {
		t.Fatalf("%v %.40q", err, b)
	}
	// A team sample is a draft in Personal: no team, no review log.
	b, err = os.ReadFile(filepath.Join(folder, "personal", "samples", "library", "return-creates-hold.md"))
	if err != nil || strings.Contains(string(b), "kalka_team") || strings.Contains(string(b), "## Review log") || !strings.Contains(string(b), "kalka_issue: jira:DEV-123\n") || !strings.HasSuffix(string(b), "`C4`.\n") {
		t.Fatalf("team sample in Personal: %v\n%s", err, b)
	}
	// A sample the user changed stays as they left it.
	mine := filepath.Join(folder, "personal", "samples", "relay-indicator.md")
	os.WriteFile(mine, []byte("# mine\n"), 0o644)
	if written, err := Seed(folder); err != nil || len(written) != 0 {
		t.Fatalf("again: %v %v", written, err)
	}
	if b, _ := os.ReadFile(mine); string(b) != "# mine\n" {
		t.Fatalf("overwritten: %q", b)
	}
	if _, err := Seed(""); err == nil {
		t.Fatal("no folder")
	}
}

func TestSeedIfAsked(t *testing.T) {
	cfg, folder := t.TempDir(), t.TempDir()
	if w, err := SeedIfAsked(cfg, folder); err != nil || w != nil {
		t.Fatalf("not asked: %v %v", w, err)
	}
	if err := Ask(cfg); err != nil {
		t.Fatal(err)
	}
	if w, err := SeedIfAsked(cfg, folder); err != nil || len(w) == 0 {
		t.Fatalf("asked: %v %v", w, err)
	}
	if _, err := os.Stat(filepath.Join(cfg, PendingFile)); !os.IsNotExist(err) {
		t.Fatalf("request kept: %v", err)
	}
}
