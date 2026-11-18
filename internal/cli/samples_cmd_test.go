package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/samples"
)

// kalka samples before setup asks for them; after setup it copies them
// into Personal, and a second run adds nothing (spec 12.1).
func TestSamplesCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KALKA_CONFIG_DIR", dir)
	code, out, _ := runCLI("samples")
	if code != 0 || !strings.Contains(out, "not set up yet") {
		t.Fatalf("%d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, samples.PendingFile)); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	if err := config.Save(&config.Config{Email: "ida@library.example", Rendezvous: config.PeerJSCloud, Folder: folder}); err != nil {
		t.Fatal(err)
	}
	code, out, _ = runCLI("samples")
	if code != 0 || !strings.Contains(out, "added") {
		t.Fatalf("%d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(folder, "personal", "samples", "lending", "loans-domain.md")); err != nil {
		t.Fatal(err)
	}
	if code, out, _ = runCLI("samples"); code != 0 || !strings.Contains(out, "already") {
		t.Fatalf("again: %d %s", code, out)
	}
	if code, _, _ = runCLI("samples", "extra"); code != exitUsage {
		t.Fatalf("usage: %d", code)
	}
}
