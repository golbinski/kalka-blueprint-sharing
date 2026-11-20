package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golbinski/kalka/internal/doctor"
	"github.com/golbinski/kalka/internal/rules"
)

var updateCheck = flag.Bool("update-check", false, "rewrite testdata/check/check.golden.json")

var corpus = filepath.Join("..", "..", "testdata", "check", "corpus")

// The --json output of the corpus is the golden file, byte for byte, and
// the same on every run (spec 6.17).
func TestCheckGolden(t *testing.T) {
	golden := filepath.Join("..", "..", "testdata", "check", "check.golden.json")
	args := []string{"check", "--json", "--domain", corpus, corpus}
	code, out, errs := runCLI(args...)
	if code != checkErrors {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if _, again, _ := runCLI(args...); again != out {
		t.Fatal("two runs differ")
	}
	if *updateCheck {
		os.WriteFile(golden, []byte(out), 0o644)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run with -update-check to create)", err)
	}
	if string(want) != out {
		t.Fatalf("output differs from %s; run with -update-check and review the diff\n%s", golden, out)
	}
	var res checkOutput
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Rules != "v1" {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestCheckExitCodesByFile(t *testing.T) {
	f := func(name string) string { return filepath.Join(corpus, name) }
	cases := []struct {
		args []string
		want int
	}{
		{[]string{f("clean.md")}, checkClean},
		{[]string{f("hints.md"), "--domain", corpus}, checkClean}, // information only
		{[]string{f("missing-domain.md"), "--domain", corpus}, checkClean},
		{[]string{f("errors.md")}, checkErrors},
		{[]string{f("warnings.md")}, checkWarnings},
		{[]string{f("warnings.md"), "--strict"}, checkErrors},
		{[]string{f("clean.md"), f("warnings.md")}, checkWarnings},
		{[]string{f("clean.md"), "--rules", "v1"}, checkClean},
		{[]string{filepath.Join(t.TempDir(), "missing.md")}, checkErrors}, // B000
		{[]string{}, checkUsage},
		{[]string{f("clean.md"), "--rules", "v2"}, checkUsage},
		{[]string{f("clean.md"), "--rules", "latest"}, checkUsage},
		{[]string{f("clean.md"), "--bogus"}, checkUsage},
		{[]string{"--explain", "X999"}, checkUsage},
	}
	for _, c := range cases {
		code, out, errs := runCLI(append([]string{"check"}, c.args...)...)
		if code != c.want {
			t.Errorf("check %v: exit %d, want %d\n%s%s", c.args, code, c.want, out, errs)
		}
	}
	if _, _, errs := runCLI("check", f("clean.md"), "--rules", "v2"); !strings.Contains(errs, "needs a newer kalka") {
		t.Errorf("v2: %q", errs)
	}
}

// Without --domain and with no folder set up, the domain rules say they
// could not check.
func TestCheckWithoutDomain(t *testing.T) {
	_, out, _ := runCLI("check", "--verbose", filepath.Join(corpus, "hints.md"))
	if !strings.Contains(out, "[R001]") || !strings.Contains(out, "no document of domain lending/loans") {
		t.Fatalf("%s", out)
	}
}

// --explain answers for every rule, in text and JSON.
func TestCheckExplainEveryRule(t *testing.T) {
	for _, r := range rules.All() {
		code, out, errs := runCLI("check", "--explain", r.ID)
		if code != checkClean || !strings.HasPrefix(out, r.ID+" ("+string(r.Severity)+"): "+r.Title) || !strings.Contains(out, "Wrong:") {
			t.Errorf("%s: %d %q %q", r.ID, code, out, errs)
		}
	}
	code, out, _ := runCLI("check", "--explain", "t040", "--json")
	var r rules.Rule
	if err := json.Unmarshal([]byte(out), &r); code != checkClean || err != nil || r.ID != "T040" || r.Why == "" {
		t.Fatalf("json: %d %v %s", code, err, out)
	}
}

// A folder is searched for *.md, without snapshots or dot-directories.
func TestCheckFolders(t *testing.T) {
	_, out, _ := runCLI("check", "--json", "--domain", corpus, corpus)
	var res checkOutput
	json.Unmarshal([]byte(out), &res)
	var names []string
	for _, f := range res.Files {
		names = append(names, filepath.Base(f.File))
	}
	if got := strings.Join(names, " "); got != "clean.md errors.md hints.md lending-loans-domain.md missing-domain.md typed.md warnings.md" {
		t.Fatalf("files %s", got)
	}
}

// kalka invariants prints the matrix and exits on the invariant
// diagnostics as check does (spec 12.4).
func TestInvariantsCommand(t *testing.T) {
	lending := filepath.Join("..", "..", "samples", "blueprints", "lending")
	code, out, _ := runCLI("invariants", "--json", "--domain", filepath.Join(lending, "loans-domain.md"), filepath.Join(lending, "online-renewals.md"))
	var res invariantsOutput
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != checkWarnings {
		t.Fatalf("%d %v %s", code, err, out)
	}
	var rows []string
	for _, r := range res.Invariants {
		rows = append(rows, r.Ref+"/"+strings.Join(r.Gaps, ","))
	}
	if got := strings.Join(rows, " "); got != "N1/ N2/unguarded N3/unverified loans#I2/ loans#I3/" || res.Domain != "lending/loans@v1" {
		t.Errorf("rows %s, domain %q", got, res.Domain)
	}
	// Without the domain version the references cannot resolve: R001.
	code, out, _ = runCLI("invariants", filepath.Join(lending, "online-renewals.md"))
	if code != checkWarnings || !strings.Contains(out, "[R001]") || !strings.Contains(out, "not found") {
		t.Errorf("without domain: %d\n%s", code, out)
	}
	if code, _, _ := runCLI("invariants"); code != checkUsage {
		t.Errorf("no file: %d", code)
	}
}

// kalka doctor prints each check with its fix and exits 0, 2 or 1 (spec
// 12.5); the probes are fakes, so nothing real is read.
func TestDoctorCommand(t *testing.T) {
	old := doctorProbes
	defer func() { doctorProbes = old }()
	probes := doctor.Probes{
		Config: func() (doctor.Config, error) { return doctor.Config{Email: "ida@library.example", Folder: "/f"}, nil },
		Daemon: func(context.Context) doctor.DaemonState { return doctor.DaemonState{} },
		Folder: func(string) error { return nil },
	}
	doctorProbes = func(string) doctor.Probes { return probes }
	code, out, _ := runCLI("doctor")
	if code != 2 || !strings.Contains(out, "warn  daemon") || !strings.Contains(out, "fix: run `kalka`") {
		t.Fatalf("%d\n%s", code, out)
	}
	probes.Folder = func(string) error { return errors.New("not writable") }
	code, out, _ = runCLI("doctor", "--json")
	var r doctor.Report
	if err := json.Unmarshal([]byte(out), &r); err != nil || code != 1 || r.Summary.Fail != 1 {
		t.Fatalf("%d %v\n%s", code, err, out)
	}
	probes.Daemon = func(context.Context) doctor.DaemonState { return doctor.DaemonState{Running: true} }
	probes.Folder = func(string) error { return nil }
	if code, _, _ := runCLI("doctor"); code != 0 {
		t.Fatalf("healthy: %d", code)
	}
	if code, _, _ := runCLI("doctor", "extra"); code != checkUsage {
		t.Fatalf("usage: %d", code)
	}
}

// kalka_issue and kalka_archived are checked without plugins (spec 6.17).
func TestCheckIssueRules(t *testing.T) {
	dir := t.TempDir()
	write := func(name, fm string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("---\n"+fm+"\n---\n# Redelivery\n\n## Plan\n\nA second attempt.\n"), 0o644)
		return p
	}
	cases := []struct {
		fm   string
		want int
		rule string
	}{
		{"kalka_issue: jira:DEV-123", checkClean, ""},
		{"kalka_issue: DEV-123", checkWarnings, "F020"},
		{"kalka_issue: tracker:DEV-123", checkClean, "F021"},
		{"kalka_archived: yes", checkWarnings, "F022"},
	}
	for i, c := range cases {
		p := write(fmt.Sprintf("b%d.md", i), c.fm)
		code, out, errs := runCLI("check", "--json", p)
		if code != c.want {
			t.Errorf("%s: exit %d, want %d (%s)", c.fm, code, c.want, errs)
		}
		if c.rule != "" && !strings.Contains(out, `"rule": "`+c.rule+`"`) && !strings.Contains(out, `"rule":"`+c.rule+`"`) {
			t.Errorf("%s: no %s in %s", c.fm, c.rule, out)
		}
		if _, again, _ := runCLI("check", "--json", p); again != out {
			t.Errorf("%s: two runs differ", c.fm)
		}
	}
}
