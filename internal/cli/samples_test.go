package cli

import (
	"bytes"
	"encoding/json"
	bp "github.com/golbinski/kalka/internal/blueprint"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestSamples keeps the sample folder (samples/blueprints, used for manual
// testing) valid: each file passes `kalka check` with no error or
// warning, every trace block is valid, and each shows the hints
// samples/README.md says it does.
func TestSamples(t *testing.T) {
	root := filepath.Join("..", "..", "samples", "blueprints")
	want := map[string][]string{
		"kalka-core-sync-domain.md":      nil,
		"push-during-round.md":           nil,
		"retry-first-release.md":         {"H003", "H004", "H005"},
		"relay-indicator.md":             nil,
		"join-flow-trace.md":             nil,
		"packaging/windows-installer.md": {"H001", "H004"},
		"packaging/windows-app-icon.md":  {"H001"},
	}
	var seen []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		seen = append(seen, rel)
		hints, ok := want[rel]
		if !ok {
			t.Errorf("%s: a sample not listed here or in samples/README.md", rel)
			return nil
		}
		var out, errb bytes.Buffer
		if code := Run([]string{"check", "--json", p}, nil, &out, &errb); code != 0 {
			t.Errorf("%s: check exit %d: %s", rel, code, errb.String())
			return nil
		}
		var res checkOutput
		if err := json.Unmarshal(out.Bytes(), &res); err != nil || len(res.Files) != 1 {
			t.Fatalf("%s: %v\n%s", rel, err, out.String())
		}
		r := res.Files[0]
		var got []string
		for _, p := range r.Diagnostics {
			switch {
			case p.Severity != bp.Info:
				t.Errorf("%s: %s %s: %s", rel, p.Severity, p.Rule, p.Message)
			case strings.HasPrefix(p.Rule, "H"):
				got = append(got, p.Rule)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, hints) {
			t.Errorf("%s: hints %v, want %v", rel, got, hints)
		}
		for i, tr := range r.Traces {
			if !tr.Valid {
				t.Errorf("%s: trace block %d is invalid", rel, i)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != len(want) {
		t.Errorf("samples %v, want %d files", seen, len(want))
	}
}
