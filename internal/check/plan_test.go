package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

const (
	epicID  = "8d1c0f2a-4b7e-4c39-9a51-2e6f0b3d7c11"
	storyID = "3e9b7a60-1f24-4d8e-b0c5-7a2d91e4f803"
	leafID  = "6a4c2e18-7b3d-4f91-8c05-d2e9a1b7f430"
	otherID = "9f2b7d41-0c6e-4a85-b3d9-51e8c7a2f064"
)

// planDoc writes a document of the plan graph.
func planDoc(t *testing.T, dir, file, id, parent, extra, title, body string) {
	t.Helper()
	fm := "---\nkalka_id: " + id + "\n"
	if parent != "" {
		fm += "kalka_parent: " + parent + "\n"
	}
	fm += extra + "---\n# " + title + "\n\n"
	if err := os.WriteFile(filepath.Join(dir, file), []byte(fm+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func goals(body string) string { return "```goals v2\n" + body + "```\n\n" }

const epicGoals = "goal G1: The queue head learns that a copy waits\ngoal G2: A held copy is not lent to anyone else\n"

const criterion = "criterion AC1: The queue head is notified once\n  realizes parent#G1\n  given a copy with two reservations\n  when it is returned\n  then the queue head gets one email\n"

type planCase struct {
	dir string
	f   *Folder
}

func (c planCase) check(t *testing.T, file, prefixes string) (string, Result) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(c.dir, file))
	if err != nil {
		t.Fatal(err)
	}
	d, err := bp.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	r := Document(d, Options{Docs: c.f})
	return ruleList(r.Problems, prefixes), r
}

func plans(t *testing.T, write func(dir string)) planCase {
	dir := t.TempDir()
	write(dir)
	return planCase{dir, NewFolder(dir)}
}

func states(ns []GoalNode) string {
	var out []string
	for _, n := range ns {
		s := n.Kind + " " + n.ID
		if n.State != "" {
			s += "=" + n.State
		}
		if len(n.Claimants) > 0 {
			s += "[" + states(n.Claimants) + "]"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// Goal states are computed: unclaimed or claimed, through any depth
// (spec 6.18). Criteria carry no state, and kalka_status: closed is an
// ordinary label that changes nothing.
func TestGoalStates(t *testing.T) {
	tree := func(status string) string {
		c := plans(t, func(dir string) {
			planDoc(t, dir, "epic.md", epicID, "", status, "Holds", goals(epicGoals))
			planDoc(t, dir, "story.md", storyID, epicID, status, "Return creates a hold", goals("goal S1: A return creates a hold\n  realizes parent#G1\n"))
			planDoc(t, dir, "leaf.md", leafID, storyID, status, "Hold record", goals(strings.Replace(criterion, "parent#G1", "parent#S1", 1)))
		})
		raw, _ := os.ReadFile(filepath.Join(c.dir, "epic.md"))
		d, _ := bp.Parse(raw)
		return states(GoalTree(d, c.f))
	}
	want := "goal G1=claimed[goal S1=claimed[criterion AC1]] goal G2=unclaimed"
	for _, status := range []string{"", "kalka_status: closed\n"} {
		if got := tree(status); got != want {
			t.Errorf("status %q:\n got %s\nwant %s", status, got, want)
		}
	}
}

func TestPlanRules(t *testing.T) {
	epic := func(dir, extra, body string) { planDoc(t, dir, "epic.md", epicID, "", extra, "Holds", body) }
	leaf := func(dir, parent, extra, body string) {
		planDoc(t, dir, "leaf.md", leafID, parent, extra, "Hold record", body)
	}
	for _, c := range []struct {
		name, file string
		write      func(dir string)
		want       string
	}{
		{"open epic, unclaimed goal", "epic.md", func(d string) { epic(d, "", goals(epicGoals)); leaf(d, epicID, "", goals(criterion)) }, "P030"},
		{"closed is a label, unclaimed goal", "epic.md", func(d string) {
			epic(d, "kalka_status: closed\n", goals(epicGoals))
			leaf(d, epicID, "kalka_status: closed\n", goals(criterion))
		}, "P030"},
		{"closed is a label, claimed goal", "epic.md", func(d string) {
			epic(d, "kalka_status: closed\n", goals("goal G1: x\n"))
			leaf(d, epicID, "", goals(criterion))
		}, ""},
		{"adopted breakdown", "epic.md", func(d string) {
			epic(d, "kalka_breakdown: adopted\n", goals(epicGoals))
			leaf(d, epicID, "", goals(criterion))
		}, "P032"},
		{"parent not here", "leaf.md", func(d string) { leaf(d, otherID, "", goals(criterion)) }, "P010"},
		{"parent with no kalka_parent", "leaf.md", func(d string) {
			leaf(d, "", "", goals(strings.Replace(criterion, "parent#G1", "G1", 1)+"goal G1: x\n")+invs("affects parent#E1\n"))
		}, "P021"},
		{"no such goal on the parent", "leaf.md", func(d string) {
			epic(d, "", goals("goal G1: x\n"))
			leaf(d, epicID, "", goals(strings.Replace(criterion, "G1", "G9", 1)))
		}, "P020"},
		{"inherited invariant reused", "leaf.md", func(d string) {
			epic(d, "", goals("goal G1: x\n")+invs("invariant E1: A hold is never lent\n"))
			leaf(d, epicID, "", goals(criterion)+invs("invariant E1: Something looser\n  verified-by x\n"))
		}, "P023"},
		{"inherited invariant guarded", "leaf.md", func(d string) {
			epic(d, "", goals("goal G1: x\n")+invs("invariant E1: A hold is never lent\n"))
			leaf(d, epicID, "", goals(criterion)+invs("affects parent#E1\n")+"```trace v2\nlane business\nstep B1: Return\n  covers AC1\n  guards parent#E1\nlane code\nstep C1: x\n  realizes B1\n  ref gh:a.go\n```\n")
		}, ""},
		{"no such inherited invariant", "leaf.md", func(d string) {
			epic(d, "", goals("goal G1: x\n"))
			leaf(d, epicID, "", goals(criterion)+invs("affects parent#E7\n"))
		}, "I020"},
		{"cycle", "leaf.md", func(d string) {
			planDoc(t, d, "a.md", epicID, leafID, "", "A", "")
			leaf(d, epicID, "", "")
		}, "P012"},
	} {
		got, r := plans(t, c.write).check(t, c.file, "PI")
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
			for _, p := range r.Problems {
				t.Log(p.Rule, p.Message)
			}
		}
	}
	// A goal of the grandparent: the message names the level to realize.
	c := plans(t, func(d string) {
		planDoc(t, d, "epic.md", epicID, "", "", "Holds", goals("goal G1: x\n"))
		planDoc(t, d, "story.md", storyID, epicID, "", "Story", goals("goal S1: y\n  realizes parent#G1\n"))
		leaf(d, storyID, "", goals(criterion))
	})
	_, r := c.check(t, "leaf.md", "P")
	if len(r.Problems) == 0 || !strings.Contains(r.Problems[len(r.Problems)-1].Message, `a goal of "Holds", which is not the direct parent`) {
		t.Errorf("grandparent: %+v", r.Problems)
	}
	// Not looked up at all: P011, nothing else.
	raw, _ := os.ReadFile(filepath.Join(c.dir, "leaf.md"))
	d, _ := bp.Parse(raw)
	if got := ruleList(Document(d, Options{}).Problems, "P"); got != "P011" {
		t.Errorf("no docs: %q", got)
	}
}

// The goals block's own rules.
func TestGoalsBlock(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{criterion, ""},
		{"criterion AC1: x\n  realizes parent#G1\n  when a\n  given b\n  then c\n", "P002"},
		{"criterion AC1: x\n  realizes parent#G1\n  given a\n  when b\n", "P003"},
		{"goal G1: x\n  realizes G2\ngoal G2: y\n", "P004"},
		{"goal G1: x\ngoal G1: y\n", "P001"},
		{"criterion AC1: x\n  realizes G7\n  then y\n", "P020"},
		{"goal G1: x\ncriterion AC1: y\n  realizes G1\n  then z\n", ""},
	} {
		d, _ := bp.Parse([]byte("# Plan\n\n" + goals(c.body)))
		if got := ruleList(d.Problems(), "P"); got != c.want {
			t.Errorf("%q: %q, want %q", c.body, got, c.want)
		}
	}
	// Steps keep their keywords and order; covers on a missing criterion.
	d, _ := bp.Parse([]byte("# Plan\n\n" + goals(criterion+"  then the second member gets nothing\n") + "```trace v2\nlane a\nstep B1: x\n  covers AC9\nlane b\n```\n"))
	g := d.Goal("AC1")
	if g == nil || len(g.Steps) != 4 || g.Steps[3].Keyword != "then" || g.Steps[3].Text != "the second member gets nothing" {
		t.Errorf("criterion %+v", g)
	}
	if got := ruleList(d.Problems(), "P"); got != "P022" {
		t.Errorf("covers: %q", got)
	}
}

// kalka context: the plan path with each level's goals and invariants,
// and the domain path with its terms (spec 6.18).
func TestAgentContext(t *testing.T) {
	c := plans(t, func(d string) {
		planDoc(t, d, "epic.md", epicID, "", "kalka_issue: jira:DEV-100\n", "Holds", goals(epicGoals)+invs("invariant E1: A hold is never lent\n"))
		planDoc(t, d, "leaf.md", leafID, epicID, "kalka_domain: lending/library.reservations@v1\n", "Hold record", goals(criterion))
		planDoc(t, d, "sibling.md", storyID, epicID, "", "Sibling", goals("criterion AC7: secret\n  realizes parent#G2\n  then x\n"))
	})
	f := library(t, nil)
	raw, _ := os.ReadFile(filepath.Join(c.dir, "leaf.md"))
	d, _ := bp.Parse(raw)
	ctx := Context("leaf.md", d, Options{Docs: c.f, Domains: f.Tree()})
	if len(ctx.Plan) != 2 || ctx.Plan[0].Issue != "jira:DEV-100" || !ctx.Plan[1].Self || len(ctx.Plan[0].Invariants) != 1 {
		t.Fatalf("plan %+v", ctx.Plan)
	}
	if len(ctx.Domains) != 2 || ctx.Domains[0].Domain != "library" || ctx.Domains[1].Domain != "library.reservations" || len(ctx.Domains[0].Terms) != 3 {
		t.Fatalf("domains %+v", ctx.Domains)
	}
	md := ctx.Markdown()
	for _, want := range []string{"### 1. jira:DEV-100 Holds", "- goal G1: The queue head learns", "- invariant E1: A hold is never lent", "(this document)", "  - given a copy with two reservations", "### library.reservations@v1", "narrowed by library.reservations#R2"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "secret") {
		t.Error("a sibling is in the context")
	}
}

// A missing ancestor is P010 and its invariants are not checked against;
// with only the files checked, a parent not among them is P011, and goal
// coverage is not judged without the children.
func TestPlanNotHere(t *testing.T) {
	const rootID = "2c7e4a19-5d3b-4f80-a6e1-9b0d8c3f7a25"
	c := plans(t, func(d string) {
		planDoc(t, d, "epic.md", epicID, rootID, "", "Epic", goals("goal G1: x\n"))
		planDoc(t, d, "leaf.md", leafID, epicID, "", "Hold record", goals(criterion)+invs("affects parent#R1\n"))
	})
	got, r := c.check(t, "leaf.md", "PI")
	if got != "P010" || !strings.Contains(r.Problems[len(r.Problems)-1].Message, `"Epic", a plan node above`) {
		t.Errorf("grandparent missing: %q %+v", got, r.Problems)
	}

	dir := t.TempDir()
	planDoc(t, dir, "epic.md", epicID, "", "kalka_status: closed\n", "Holds", goals(epicGoals))
	planDoc(t, dir, "leaf.md", leafID, epicID, "", "Hold record", goals(criterion))
	only := func(files ...string) *Folder {
		f := NewFolder()
		for _, x := range files {
			f.Add(filepath.Join(dir, x))
		}
		return f
	}
	for _, x := range []struct {
		file  string
		given []string
		want  string
	}{
		{"epic.md", []string{"epic.md"}, "P011"},
		{"leaf.md", []string{"leaf.md"}, "P011"},
		{"epic.md", []string{"epic.md", "leaf.md"}, "P030"},
		{"leaf.md", []string{"leaf.md", "epic.md"}, ""},
	} {
		raw, _ := os.ReadFile(filepath.Join(dir, x.file))
		d, _ := bp.Parse(raw)
		if got := ruleList(Document(d, Options{Docs: only(x.given...)}).Problems, "P"); got != x.want {
			t.Errorf("%s given %v: %q, want %q", x.file, x.given, got, x.want)
		}
	}
}

// A criterion realizing two own goals claims both; one naming an own goal
// that does not exist is still in the tree, on its own (P020).
func TestOwnCriteria(t *testing.T) {
	d, _ := bp.Parse([]byte("# Plan\n\n" + goals("goal G1: x\ngoal G2: y\ncriterion AC1: z\n  realizes G1\n  realizes G2\n  then w\ncriterion AC2: v\n  realizes G9\n  then u\n")))
	if got := states(GoalTree(d, nil)); got != "goal G1=claimed[criterion AC1] goal G2=claimed[criterion AC1] criterion AC2" {
		t.Errorf("tree %s", got)
	}
	for _, p := range d.Problems() {
		if p.Rule == "P020" && p.Line != 11 {
			t.Errorf("P020 at line %d, want the realizes line 11", p.Line)
		}
	}
}

// A cycle above the document says where it is.
func TestCycleAbove(t *testing.T) {
	c := plans(t, func(d string) {
		planDoc(t, d, "epic.md", epicID, storyID, "", "Epic", "")
		planDoc(t, d, "story.md", storyID, epicID, "", "Story", "")
		planDoc(t, d, "leaf.md", leafID, storyID, "", "Leaf", "")
	})
	got, r := c.check(t, "leaf.md", "P")
	if got != "P012" || !strings.Contains(r.Problems[len(r.Problems)-1].Message, "above this document") {
		t.Errorf("%q %+v", got, r.Problems)
	}
}
