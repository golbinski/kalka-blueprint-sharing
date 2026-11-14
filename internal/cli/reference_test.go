package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/reference"
)

func runCLI(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, nil, &out, &errb, time.Now)
	return code, out.String(), errb.String()
}

// `kalka reference export` (spec 6.13, R1 acceptance).
func TestReferenceExport(t *testing.T) {
	// To stdout: exactly the embedded bytes.
	code, out, _ := runCLI("reference", "export")
	if code != exitOK || out != string(reference.Bytes()) {
		t.Fatalf("stdout export: %d, %d bytes", code, len(out))
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "SKILL.md")
	if code, _, errs := runCLI("reference", "export", "-o", file); code != exitOK {
		t.Fatalf("-o: %d %s", code, errs)
	}
	if b, _ := os.ReadFile(file); !bytes.Equal(b, reference.Bytes()) {
		t.Fatal("-o wrote different bytes")
	}

	// No overwrite without --force; the file is left alone.
	os.WriteFile(file, []byte("my own notes\n"), 0o644)
	code, _, errs := runCLI("reference", "export", "-o", file)
	if b, _ := os.ReadFile(file); code != exitProblems || !strings.Contains(errs, "--force") || string(b) != "my own notes\n" {
		t.Fatalf("overwrite refused: %d %q %q", code, errs, b)
	}
	if code, _, _ := runCLI("reference", "export", "-o", file, "--force"); code != exitOK {
		t.Fatal("--force")
	}
	if b, _ := os.ReadFile(file); !bytes.Equal(b, reference.Bytes()) {
		t.Fatal("--force did not overwrite")
	}

	// A directory gets the reference's own file name.
	if code, out, _ := runCLI("reference", "export", "-o", dir, "--json"); code != exitOK || !strings.Contains(out, reference.FileName) {
		t.Fatalf("dir: %d %s", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, reference.FileName)); !bytes.Equal(b, reference.Bytes()) {
		t.Fatal("dir export")
	}

	// The exported file declares its version.
	if !strings.Contains(string(reference.Bytes()), "kalka_reference: "+reference.Version) {
		t.Fatal("version not declared")
	}
	if code, out, _ := runCLI("reference", "version"); code != exitOK || strings.TrimSpace(out) != reference.Version {
		t.Fatalf("version: %q", out)
	}
	if code, _, _ := runCLI("reference", "bogus"); code != exitUsage {
		t.Fatal("bogus subcommand")
	}
}

// Hints show in `kalka check --verbose` and JSON, and never fail it.
func TestCheckHints(t *testing.T) {
	f := filepath.Join(t.TempDir(), "plan.md")
	os.WriteFile(f, []byte("---\nreference: kalka-blueprint@v1\n---\n# Redelivery\n\n## Scope\nThe redelivery module.\n"), 0o644)

	code, out, _ := runCLI("check", f)
	if code != exitOK || strings.Contains(out, "hint-") {
		t.Fatalf("plain check: %d %q", code, out)
	}
	code, out, _ = runCLI("check", f, "--verbose")
	if code != exitOK || !strings.Contains(out, "[hint-no-verification-plan]") || strings.Contains(out, "hint-no-scope") {
		t.Fatalf("verbose: %d %q", code, out)
	}
	code, out, _ = runCLI("check", f, "--json")
	var res []struct {
		OK       bool `json:"ok"`
		Problems []struct {
			Code     string `json:"code"`
			Severity string `json:"severity"`
		} `json:"problems"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != exitOK || !res[0].OK {
		t.Fatalf("json: %d %v %s", code, err, out)
	}
	n := 0
	for _, p := range res[0].Problems {
		if strings.HasPrefix(p.Code, "hint-") {
			if p.Severity != "info" {
				t.Fatalf("hint severity %q", p.Severity)
			}
			n++
		}
	}
	if n != 4 {
		t.Fatalf("%d hints in %s", n, out)
	}
}
