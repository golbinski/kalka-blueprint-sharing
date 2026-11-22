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

// The document view shows the plan path, the children and the goal tree;
// the owner closes a leaf, and the goal it claims becomes ready (spec 6.18).
func TestPlanViewAndClose(t *testing.T) {
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
	epic, _ := alice.d.Document(ctx, "epic.md")
	if p := epic.Plan; p == nil || len(p.Children) != 1 || len(p.Goals) != 1 || p.Goals[0].State != "claimed" || epic.IssueKey != "DEV-100" {
		t.Fatalf("epic plan %+v issue %q", epic.Plan, epic.IssueKey)
	}

	if err := alice.d.Close("leaf.md", false); err != nil {
		t.Fatal(err)
	}
	if d := alice.doc("leaf.md"); !d.IsClosed() {
		t.Fatalf("status %q", d.Status())
	}
	epic, _ = alice.d.Document(ctx, "epic.md")
	if g := epic.Plan.Goals[0]; g.State != "ready" || g.Claimants[0].State != "delivered" {
		t.Fatalf("goal %+v", g)
	}
	if err := alice.d.Close("leaf.md", true); err != nil {
		t.Fatal(err)
	}
	if d := alice.doc("leaf.md"); d.IsClosed() {
		t.Fatal("not reopened")
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
