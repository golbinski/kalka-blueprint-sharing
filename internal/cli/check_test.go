package cli

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if got := strings.Join(names, " "); got != "clean.md errors.md hints.md lending-loans-domain.md missing-domain.md warnings.md" {
		t.Fatalf("files %s", got)
	}
}
