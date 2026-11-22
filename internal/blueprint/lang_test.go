package blueprint

import (
	"strings"
	"testing"
)

// The lexical rules are shared by every block (spec 6.10, kalka language v2).
func TestLanguageLexis(t *testing.T) {
	g := func(body string) string { return "# Plan\n\n```goals v2\n" + body + "```\n" }
	for _, c := range []struct{ name, src, want string }{
		{"gherkin scenario", g("Scenario: a member returns a copy\n"), "S010"},
		{"gherkin step", g("criterion AC1: x\n  Given a copy\n"), "S010"},
		{"and", g("criterion AC1: x\n  given a\n  and b\n  then c\n"), "S010"},
		{"slash comment", g("// later\ngoal G1: x\n"), "S011"},
		{"own keyword", g("goal G1: x\n  x-owner ewa\n"), ""},
		{"capital keyword", g("Goal G1: x\n"), "S002"},
		{"id too long", g("goal " + strings.Repeat("G", 33) + ": x\n"), "S030"},
		{"criterion on the attribute level", g("goal G1: x\n  criterion AC1: y\n"), "S008"},
	} {
		d, _ := Parse([]byte(c.src))
		if got := rulesOf(d, "S"); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
			for _, p := range d.Problems() {
				t.Log(p.Rule, p.Message)
			}
		}
	}
	// Fixes say the v2 form to write.
	d, _ := Parse([]byte(g("Goal G1: x\n")))
	if p := d.Problems()[0]; p.Fix == nil || p.Fix.Text != "goal G1: x" {
		t.Errorf("fix %+v", p)
	}
	// One fix per line, carrying every fix of it.
	d, _ = Parse([]byte(g("Goal: G1: x\n")))
	var fixes []string
	for _, p := range d.Problems() {
		if p.Fix != nil {
			fixes = append(fixes, p.Rule+"="+p.Fix.Text)
		}
	}
	if strings.Join(fixes, " ") != "S003=goal G1: x" {
		t.Errorf("fixes %v", fixes)
	}
	// and repeats the last step keyword, not any attribute.
	d, _ = Parse([]byte(g("criterion AC1: x\n  given a\n  realizes G1\n  And b\n")))
	for _, p := range d.Problems() {
		if p.Rule == "S010" && (p.Fix == nil || p.Fix.Text != "  given b") {
			t.Errorf("and fix %+v", p.Fix)
		}
	}
	// A labelled reference with no label.
	d, _ = Parse([]byte("# Plan\n\n```trace v2\nlane a\nstep B1: x\n  next B2:\nstep B2: y\nlane b\n```\n"))
	if got := rulesOf(d, "S"); got != "S007" {
		t.Errorf("next B2: %q", got)
	}
	d, _ = Parse([]byte(g("criterion AC1: x\n  Given a copy\n")))
	for _, p := range d.Problems() {
		if p.Rule == "S010" && !strings.Contains(p.Message, "given") {
			t.Errorf("drift message %q", p.Message)
		}
	}
}
