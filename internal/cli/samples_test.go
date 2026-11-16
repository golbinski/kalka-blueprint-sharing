package cli

import (
	"bytes"
	"encoding/json"
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
		"hold-shelf-expiry.md":               nil,
		"renewal-reminders.md":               {"hint-no-critical-sections", "hint-no-operations", "hint-no-verification-plan"},
		"interlibrary-requests.md":           nil,
		"import-pipeline-trace.md":           nil,
		"lending-desk-circulation-domain.md": nil,
		"returns-desk/book-drop-sorting.md":  {"hint-no-critical-sections", "hint-no-domain-reference"},
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
		var rs []struct {
			OK       bool `json:"ok"`
			Problems []struct {
				Severity, Code, Message string
			} `json:"problems"`
			Traces []struct {
				Valid bool `json:"valid"`
			} `json:"traces"`
		}
		if err := json.Unmarshal(out.Bytes(), &rs); err != nil || len(rs) != 1 {
			t.Fatalf("%s: %v\n%s", rel, err, out.String())
		}
		r := rs[0]
		var got []string
		for _, p := range r.Problems {
			switch {
			case p.Severity != "info":
				t.Errorf("%s: %s %s: %s", rel, p.Severity, p.Code, p.Message)
			case strings.HasPrefix(p.Code, "hint-"):
				got = append(got, p.Code)
			}
		}
		slices.Sort(got)
		if !r.OK || !slices.Equal(got, hints) {
			t.Errorf("%s: ok %v, hints %v, want %v", rel, r.OK, got, hints)
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
