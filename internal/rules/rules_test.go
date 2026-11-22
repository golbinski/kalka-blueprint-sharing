package rules

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite docs/rules.md and the rule list in BLUEPRINT-REFERENCE.md")

// emitted are the rule ids that code outside this package names in a
// string literal: the checker's diagnostics.
func emitted(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	re := regexp.MustCompile(`"([FBSTGIPDRH][0-9]{3})"`)
	root := filepath.Join("..", "..")
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == "rules" || d.Name() == "ui" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
				out[m[1]] = append(out[m[1]], p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// Every rule the code emits is registered, and every registered rule is
// emitted somewhere (spec 6.17).
func TestRegistryMatchesCode(t *testing.T) {
	em := emitted(t)
	for id, where := range em {
		if _, ok := Lookup(id); !ok {
			t.Errorf("%s is emitted (%s) but not registered", id, where[0])
		}
	}
	for _, r := range All() {
		if len(em[r.ID]) == 0 {
			t.Errorf("%s is registered but nothing emits it", r.ID)
		}
	}
}

// Every rule explains itself: --explain prints a title, a meaning, a
// reason and a wrong and a right example.
func TestEveryRuleExplains(t *testing.T) {
	for _, r := range All() {
		if r.Title == "" || r.Meaning == "" || r.Why == "" || r.Wrong == "" || r.Right == "" {
			t.Errorf("%s is missing a title, meaning, reason or example", r.ID)
		}
		if r.Severity != Error && r.Severity != Warning && r.Severity != Info {
			t.Errorf("%s: severity %q", r.ID, r.Severity)
		}
		if r.Blocks && r.Severity != Error {
			t.Errorf("%s blocks but is not an error", r.ID)
		}
		x := Explain(r)
		for _, part := range []string{r.ID, r.Title, "Why:", "Wrong:", "Right:"} {
			if !strings.Contains(x, part) {
				t.Errorf("%s: explanation lacks %q", r.ID, part)
			}
		}
	}
}

// RulesDoc is docs/rules.md, generated from the registry.
func RulesDoc() string {
	return "# kalka rules\n\n" +
		"<!-- Generated from internal/rules; run `go test ./internal/rules -update`. -->\n\n" +
		"Every diagnostic of `kalka check` and of the app has one of these ids (specification 6.17). " +
		"`kalka check --explain <id>` prints what a rule means, why it exists, and a wrong and a right example.\n\n" +
		Markdown("##")
}

const (
	refStart = "<!-- rules -->\n"
	refEnd   = "<!-- /rules -->\n"
)

// docs/rules.md and the reference's rule list are what the registry
// generates; -update rewrites them.
func TestGeneratedLists(t *testing.T) {
	root := filepath.Join("..", "..")
	doc := filepath.Join(root, "docs", "rules.md")
	ref := filepath.Join(root, "internal", "reference", "BLUEPRINT-REFERENCE.md")
	raw, _ := os.ReadFile(ref)
	s := string(raw)
	i, j := strings.Index(s, refStart), strings.Index(s, refEnd)
	if i < 0 || j < i {
		t.Fatalf("BLUEPRINT-REFERENCE.md has no %q … %q block", strings.TrimSpace(refStart), strings.TrimSpace(refEnd))
	}
	wantRef := s[:i+len(refStart)] + "\n" + Markdown("####") + s[j:]
	if *update {
		os.WriteFile(doc, []byte(RulesDoc()), 0o644)
		os.WriteFile(ref, []byte(wantRef), 0o644)
		return
	}
	if got, _ := os.ReadFile(doc); string(got) != RulesDoc() {
		t.Error("docs/rules.md is out of date; run go test ./internal/rules -update")
	}
	if s != wantRef {
		t.Error("the rule list in BLUEPRINT-REFERENCE.md is out of date; run go test ./internal/rules -update")
	}
}
