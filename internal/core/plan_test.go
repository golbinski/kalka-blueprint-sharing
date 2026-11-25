package core

import (
	"context"
	"strings"
	"testing"
)

const (
	planEpic = "---\nkalka_id: 8d1c0f2a-4b7e-4c39-9a51-2e6f0b3d7c11\nkalka_owner: alice@library.example\nkalka_issue: jira:DEV-100\n---\n# Holds\n\n```goals v2\ngoal G1: The queue head learns that a copy waits\n```\n"
	planLeaf = "---\nkalka_id: 6a4c2e18-7b3d-4f91-8c05-d2e9a1b7f430\nkalka_owner: alice@library.example\nkalka_parent: 8d1c0f2a-4b7e-4c39-9a51-2e6f0b3d7c11\n---\n# Hold record\n\n```goals v2\ncriterion AC1: The queue head is notified once\n  realizes parent#G1\n  given a returned copy\n  when it is checked in\n  then one email goes out\n```\n"
)

// The document view shows the plan path, the children and the goal tree
// (spec 6.18).
func TestPlanView(t *testing.T) {
	w := newWorld(t)
	alice := w.node("alice@library.example")
	alice.write("epic.md", planEpic)
	alice.write("leaf.md", planLeaf)
	waitIndexed(t, alice, 2)
	ctx := context.Background()

	leaf, err := alice.d.Document(ctx, "leaf.md")
	if err != nil {
		t.Fatal(err)
	}
	if p := leaf.Plan; p == nil || len(p.Path) != 1 || p.Path[0].Title != "Holds" || p.Path[0].Issue != "jira:DEV-100" || p.Path[0].Rel != "epic.md" {
		t.Fatalf("leaf plan %+v", leaf.Plan)
	}
	if leaf.Approval != nil {
		t.Fatalf("a Personal document has an approval state: %+v", leaf.Approval)
	}
	epic, _ := alice.d.Document(ctx, "epic.md")
	if p := epic.Plan; p == nil || len(p.Children) != 1 || len(p.Goals) != 1 || p.Goals[0].State != "claimed" || p.Goals[0].Claimants[0].State != "" || epic.IssueKey != "DEV-100" {
		t.Fatalf("epic plan %+v issue %q", epic.Plan, epic.IssueKey)
	}

	// The agent context names the epic's goal.
	c, err := alice.d.AgentContext("leaf.md")
	if err != nil || len(c.Plan) != 2 || !strings.Contains(c.Markdown(), "goal G1: The queue head learns") {
		t.Fatalf("context %v %+v", err, c)
	}
	// A document outside the plan graph has no plan view.
	alice.write("lone.md", "# Lone\n")
	waitIndexed(t, alice, 3)
	if v, _ := alice.d.Document(ctx, "lone.md"); v.Plan != nil {
		t.Fatalf("lone plan %+v", v.Plan)
	}
}

// The plan graph stays in a scope (spec 14): a Personal draft under a team
// epic finds its parent, and does not claim the team epic's goals.
func TestPlanGraphScope(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	courier := ns[1]
	own := func(s string) string {
		return strings.ReplaceAll(s, "alice@library.example", "courier@parcels.example")
	}
	courier.write("epic.md", own(planEpic))
	waitIndexed(t, courier, 1)
	courier.share("epic.md")
	courier.write("draft.md", own(planLeaf))
	waitIndexed(t, courier, 2)
	ctx := context.Background()

	draft, err := courier.d.Document(ctx, courier.path("draft.md"))
	if err != nil {
		t.Fatal(err)
	}
	if p := draft.Plan; p == nil || len(p.Path) != 1 || p.Path[0].Title != "Holds" {
		t.Fatalf("draft plan %+v", draft.Plan)
	}
	epic, err := courier.d.Document(ctx, courier.path("epic.md"))
	if err != nil {
		t.Fatal(err)
	}
	if p := epic.Plan; p == nil || len(p.Children) != 0 || p.Goals[0].State != "unclaimed" {
		t.Fatalf("team epic plan %+v", epic.Plan)
	}
}

// The app's graph is kalka graph's, with where each card is held; the
// graph route finds a document by kalka_id in its scope (spec 11.1).
func TestGraphView(t *testing.T) {
	w := newWorld(t)
	alice := w.node("alice@library.example")
	alice.write("epic.md", planEpic)
	alice.write("leaf.md", planLeaf)
	waitIndexed(t, alice, 2)
	g, rel, err := alice.d.Graph("leaf.md")
	if err != nil || rel != "leaf.md" {
		t.Fatalf("%v %q", err, rel)
	}
	var cards []string
	for _, n := range g.Nodes {
		if n.Card == "" {
			cards = append(cards, n.Rel+" "+n.Scope)
		}
	}
	if strings.Join(cards, ", ") != "epic.md personal, leaf.md personal" {
		t.Errorf("cards %q", cards)
	}
	// The leaf has no trace, so nothing covers its criterion.
	if len(g.Open) != 1 || g.Open[0].Kind != "uncovered" {
		t.Errorf("open %+v", g.Open)
	}
	sc, _ := alice.d.ResolveScope(ScopePersonal)
	if r, err := alice.d.Locate(sc, "6A4C2E18-7b3d-4f91-8c05-d2e9a1b7f430"); err != nil || r != "leaf.md" {
		t.Errorf("locate %q %v", r, err)
	}
	if _, err := alice.d.Locate(sc, "00000000-0000-4000-8000-000000000000"); err == nil {
		t.Error("located a document that is not here")
	}
}
