package blueprint

import (
	"strings"
	"testing"
)

const lendingTrace = "```trace v2\nlane business\nstep B1: A member renews a loan\n  guards I1\n\n" +
	"lane code\nstep C1: renew() counts the renewals\n  realizes B1\n  guards loans#I3\n```\n"

func TestInvariantBlocks(t *testing.T) {
	inv := func(body string) string { return "```invariants v2\n" + body + "```\n" }
	for _, c := range []struct {
		name, body string
		n          int
		rules      string
	}{
		{"guarded and verified", inv("invariant I1: A loan is renewed at most twice\n  verified-by gh:test/renew_test.go#L10\n") + "\n" + lendingTrace, 1, ""},
		{"unguarded and unverified", inv("invariant I1: a\n  verified-by x\ninvariant I2: b\n") + "\n" + lendingTrace, 2, "I030 I031"},
		{"no trace, no I030", inv("invariant I1: a\n"), 1, "I031"},
		{"removed needs nothing", inv("invariant I1: a\n  verified-by x\ninvariant I2: b\n  status removed\n") + "\n" + lendingTrace, 2, ""},
		{"guard on nothing", inv("invariant I2: a\n  verified-by x\n") + "\n" + lendingTrace, 1, "I020 I030"},
		{"id twice across blocks", inv("invariant I1: a\n  verified-by x\n") + "\n" + inv("invariant I1: b\n"), 1, "I011"},
		{"own id in affects", inv("affects I3\ninvariant I1: a\n  verified-by x\n"), 1, "I017"},
		{"affects alone", inv("affects loans#I3\n"), 0, ""},
		{"affects the parent", inv("affects parent#E1\n"), 0, ""},
		{"affected but not guarded", inv("affects loans#I4\n") + "\n" + lendingTrace, 0, "I020 I030"},
		{"clash with a trace step", inv("invariant B1: a\n  verified-by x\n") + "\n" + strings.Replace(lendingTrace, "guards I1", "guards B1", 1), 1, "I016"},
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
	// The lexical rules of the language report the rest.
	for _, c := range []struct{ name, body, rules string }{
		{"bad id", inv("invariant 1 a: a\n"), "S030"},
		{"no statement", inv("invariant I1:\n"), "S005"},
		{"bad status", inv("invariant I1: a\n  status added\n"), "S022"},
		{"yaml key", inv("invariant I1: a\n  verified_by x\n"), "S020"},
		{"colon", inv("invariant I1: a\n  why: x\n"), "S003"},
		{"affects with spaces", inv("affects a#I1 b#I2\n"), "S009"},
	} {
		d, _ := Parse([]byte("# Renewals\n\n## Plan\n\n" + c.body))
		if got := rulesOf(d, "S"); got != c.rules {
			t.Errorf("%s: rules %q, want %q", c.name, got, c.rules)
		}
	}
	// The parts of an invariant.
	d, _ := Parse([]byte("# R\n\n## Plan\n\n" + inv("invariant R2: A loan cannot be renewed: ever\n  refines loan\n  why the queue waits\n  verified-by gh:a#L1\n  verified-by Reviewer: try it\n  status changed\n")))
	if v := d.Invariants()[0]; v.ID != "R2" || v.Statement != "A loan cannot be renewed: ever" || v.Refines[0] != "loan" || v.Why != "the queue waits" ||
		strings.Join(v.VerifiedBy, "|") != "gh:a#L1|Reviewer: try it" || v.Status != "changed" || v.Section != "plan" {
		t.Errorf("invariant %+v", v)
	}
}

func TestInvariantAnchorsAndGuards(t *testing.T) {
	d, _ := Parse([]byte("# Renewals\n\n## Plan\n\n```invariants v2\ninvariant I1: a\n```\n\n" + lendingTrace))
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
	// Dotted domains and the parent plan node.
	d, _ = Parse([]byte("# R\n\n## Plan\n\n" + strings.Replace(lendingTrace, "guards loans#I3", "guards library.reservations#R1\n  guards parent#E1", 1)))
	if refs := d.DomainRefs(); len(refs) != 1 || refs[0].Ref.Domain != "library.reservations" {
		t.Errorf("dotted domain refs %v", refs)
	}
	if refs := d.ParentRefs(); len(refs) != 1 || refs[0].Text != "parent#E1" {
		t.Errorf("parent refs %v", refs)
	}
	// A bad guard is a lexical error.
	bad, _ := Parse([]byte("# R\n\n" + strings.Replace(lendingTrace, "guards I1", "guards no such thing", 1)))
	if got := rulesOf(bad, "S"); got != "S009" {
		t.Errorf("bad guard: %q", got)
	}
}

func TestDomainInvariantStatus(t *testing.T) {
	d, _ := Parse([]byte(loansDomain + "```invariants v2\naffects loans#I1\ninvariant I1: a\n  status new\n  verified-by x\n```\n"))
	if got := rulesOf(d, "I"); got != "I008 I008" || d.Invariants()[0].Status != "" {
		t.Errorf("domain status: %q", got)
	}
}

func TestInvariantsSatisfyVerificationHint(t *testing.T) {
	d, _ := Parse([]byte("---\nkalka_reference: kalka-blueprint@v2\n---\n# R\n\n```invariants v2\ninvariant I1: a\n```\n"))
	for _, h := range Hints(d) {
		if h.Rule == "H003" {
			t.Error("an invariants block is a verification plan")
		}
	}
}
