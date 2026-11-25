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
		"library/hold-blocks-lending.md":                      nil,
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
		// kalka tracks no delivery: no sample carries a status that once
		// meant it, and no goal or criterion has a state beyond claimed.
		raw, _ := os.ReadFile(p)
		if strings.Contains(string(raw), "kalka_status: closed") {
			t.Errorf("%s: kalka_status: closed is only a label now", rel)
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

// The story of the library samples in kalka graph and kalka context: its
// goal states are unclaimed or claimed, and its documents are team
// documents with an approval state as their files say: DEV-100 a draft,
// DEV-123 and DEV-124 approved.
func TestSampleStoryStates(t *testing.T) {
	root := filepath.Join("..", "..", "samples", "blueprints")
	story := filepath.Join(root, "library", "holds-on-return.md")
	for _, cmd := range []string{"graph", "context"} {
		var out, errb bytes.Buffer
		if code := Run([]string{cmd, "--json", story, "--domain", filepath.Join(root, "library")}, nil, &out, &errb); code != exitOK {
			t.Fatalf("%s: exit %d: %s", cmd, code, errb.String())
		}
		s := out.String()
		for _, gone := range []string{`"state": "delivered"`, `"state": "ready"`, `"state": "open"`, `"closed"`} {
			if strings.Contains(s, gone) {
				t.Errorf("kalka %s prints %s:\n%s", cmd, gone, s)
			}
		}
		if cmd == "graph" && (!strings.Contains(s, `"state": "unclaimed"`) || !strings.Contains(s, `"state": "claimed"`) || !strings.Contains(s, `"approval": {`)) {
			t.Errorf("kalka graph has no goal or approval states:\n%s", s)
		}
		if cmd == "context" && (!strings.Contains(s, `"state": "draft"`) || !strings.Contains(s, `"approved v1"`)) {
			t.Errorf("kalka context names no approval states:\n%s", s)
		}
	}
}
