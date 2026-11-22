package cli

import (
	"bytes"
	"encoding/json"
	bp "github.com/golbinski/kalka/internal/blueprint"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestSamples keeps the sample folder (samples/blueprints, used for manual
// testing) as samples/README.md describes it: each file passes `kalka
// check` with no error and only the warnings it is there to show, every
// trace block is valid, and each shows the hints it says. Each folder's
// plans are checked against its domain document (--domain).
func TestSamples(t *testing.T) {
	root := filepath.Join("..", "..", "samples", "blueprints")
	want := map[string][]string{ // hints, then the warnings shown on purpose
		"kalka-core-sync-domain.md":                           nil,
		"push-during-round.md":                                nil,
		"retry-first-release.md":                              {"H003", "H004", "H005"},
		"relay-indicator.md":                                  nil,
		"seed-push-latency.md":                                nil,
		"join-flow-trace.md":                                  nil,
		"rendering-checks.md":                                 nil,
		"packaging/packaging-release-domain.md":               nil,
		"packaging/windows-installer.md":                      {"H004"},
		"packaging/windows-app-icon.md":                       nil,
		"lending/loans-domain.md":                             nil,
		"lending/online-renewals.md":                          {"I030", "I031"},
		"lending/hold-notices.md":                             {"G020", "G020"},
		"lending/overdue-reminders.md":                        nil,
		"library/team-lending-library-domain.md":              nil,
		"library/team-lending-library.reservations-domain.md": nil,
		"library/holds-on-return.md":                          {"P030"},
		"library/return-creates-hold.md":                      nil,
	}
	// The domain document each folder's plans are checked against.
	domains := map[string]string{
		"lending":   "lending/loans-domain.md",
		"packaging": "packaging/packaging-release-domain.md",
		".":         "kalka-core-sync-domain.md",
		"library":   "library",
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
		args := []string{"check", "--json", p}
		args = append(args, "--domain", filepath.Join(root, filepath.FromSlash(domains[path.Dir(rel)])))
		code := Run(args, nil, &out, &errb)
		if code != checkClean && code != checkWarnings {
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
			case p.Severity == bp.Error:
				t.Errorf("%s: %s %s: %s", rel, p.Severity, p.Rule, p.Message)
			case p.Severity == bp.Warning, strings.HasPrefix(p.Rule, "H"):
				got = append(got, p.Rule)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, hints) {
			t.Errorf("%s: hints and warnings %v, want %v", rel, got, hints)
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
