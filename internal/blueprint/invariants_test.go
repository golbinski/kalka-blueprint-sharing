package blueprint

import (
	"strings"
	"testing"
)

const lendingTrace = "```trace v1\nlanes: [business, code]\nnodes:\n" +
	"  - id: B1\n    lane: business\n    text: \"A member renews a loan\"\n    guards: [I1]\n" +
	"  - id: C1\n    lane: code\n    text: \"renew() counts the renewals\"\n    realizes: [B1]\n    guards: [\"loans#I3\"]\n```\n"

func TestInvariantBlocks(t *testing.T) {
	inv := func(body string) string { return "```invariants v1\n" + body + "```\n" }
	for _, c := range []struct {
		name, body string
		n          int
		rules      string
	}{
		{"guarded and verified", inv("invariants:\n  - id: I1\n    statement: \"A loan is renewed at most twice\"\n    verified_by: [\"gh:test/renew_test.go#L10\"]\n") + "\n" + lendingTrace, 1, ""},
		{"unguarded and unverified", inv("invariants:\n  - id: I1\n    statement: \"a\"\n    verified_by: [\"x\"]\n  - id: I2\n    statement: \"b\"\n") + "\n" + lendingTrace, 2, "I030 I031"},
		{"no trace, no I030", inv("invariants:\n  - id: I1\n    statement: \"a\"\n"), 1, "I031"},
		{"removed needs nothing", inv("invariants:\n  - id: I1\n    statement: \"a\"\n    verified_by: [\"x\"]\n  - id: I2\n    statement: \"b\"\n    status: removed\n") + "\n" + lendingTrace, 2, ""},
		{"guard on nothing", inv("invariants:\n  - id: I2\n    statement: \"a\"\n    verified_by: [\"x\"]\n") + "\n" + lendingTrace, 1, "I020 I030"},
		{"id twice across blocks", inv("invariants:\n  - id: I1\n    statement: \"a\"\n    verified_by: [\"x\"]\n") + "\n" + inv("invariants:\n  - id: I1\n    statement: \"b\"\n"), 1, "I011"},
		{"bad id", inv("invariants:\n  - id: \"1 a\"\n    statement: \"a\"\n"), 0, "I010"},
		{"no statement", inv("invariants:\n  - id: I1\n"), 0, "I012"},
		{"bad status", inv("invariants:\n  - id: I1\n    statement: \"a\"\n    status: added\n"), 0, "I013"},
		{"bad affects", inv("affects: [\"I3\"]\ninvariants:\n  - id: I1\n    statement: \"a\"\n"), 0, "I004"},
		{"affects alone", inv("affects: [\"loans#I3\"]\n"), 0, ""},
		{"affected but not guarded", inv("affects: [\"loans#I4\"]\n") + "\n" + lendingTrace, 0, "I020 I030"},
		{"unknown key", inv("invariants:\n  - id: I1\n    statement: \"a\"\n    verifed_by: [\"x\"]\n"), 1, "I007 I031"},
		{"clash with a trace step", inv("invariants:\n  - id: B1\n    statement: \"a\"\n    verified_by: [\"x\"]\n") + "\n" + strings.Replace(lendingTrace, "guards: [I1]", "guards: [B1]", 1), 1, "I016"},
	} {
		d, err := Parse([]byte("# Renewals\n\n## Plan\n\n" + c.body))
		if err != nil {
			t.Fatal(err)
		}
		if got := rulesOf(d, "I"); got != c.rules || len(d.Invariants()) != c.n {
			t.Errorf("%s: rules %q, %d invariants; want %q, %d", c.name, got, len(d.Invariants()), c.rules, c.n)
			for _, p := range d.Problems() {
				t.Log(p.Rule, p.Message)
			}
		}
	}
}

func TestInvariantAnchorsAndGuards(t *testing.T) {
	d, _ := Parse([]byte("# Renewals\n\n## Plan\n\n```invariants v1\ninvariants:\n  - id: I1\n    statement: \"a\"\n```\n\n" + lendingTrace))
	if !d.HasAnchor("plan#I1") || !d.HasAnchor("plan#B1") || d.HasAnchor("plan#I9") || d.HasAnchor("other#I1") {
		t.Error("anchors")
	}
	if g := d.GuardedBy("I1"); len(g) != 1 || g[0].Node != "B1" {
		t.Errorf("guarded by %v", g)
	}
	refs := d.DomainRefs()
	if len(refs) != 1 || refs[0].Text != "loans#I3" {
		t.Errorf("domain refs %v", refs)
	}
	// A bad guard is a trace error.
	bad, _ := Parse([]byte("# R\n\n" + strings.Replace(lendingTrace, "guards: [I1]", "guards: [\"no such thing\"]", 1)))
	if got := rulesOf(bad, "T"); got != "T034" {
		t.Errorf("bad guard: %q", got)
	}
}

func TestDomainInvariantStatus(t *testing.T) {
	d, _ := Parse([]byte(loansDomain + "```invariants v1\naffects: [\"loans#I1\"]\ninvariants:\n  - id: I1\n    statement: \"a\"\n    status: new\n    verified_by: [\"x\"]\n```\n"))
	if got := rulesOf(d, "I"); got != "I008 I008" || d.Invariants()[0].Status != "" {
		t.Errorf("domain status: %q", got)
	}
}

func TestInvariantsSatisfyVerificationHint(t *testing.T) {
	d, _ := Parse([]byte("---\nkalka_reference: kalka-blueprint@v1\n---\n# R\n\n```invariants v1\ninvariants:\n  - id: I1\n    statement: \"a\"\n```\n"))
	for _, h := range Hints(d) {
		if h.Rule == "H003" {
			t.Error("an invariants block is a verification plan")
		}
	}
}
