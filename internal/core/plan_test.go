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

// Closing a team document releases it, so it is refused, as archiving
// is, to anyone but the owner and over edits not released (spec 6.18,
// 10.5); a domain document is not closed at all.
func TestCloseRefused(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	courier.write("redelivery.md", issueDoc)
	waitIndexed(t, courier, 1)
	id := courier.share("redelivery.md", depot)
	path := courier.path("redelivery.md")

	if err := depot.d.Close(func() string { f, _ := depot.teamFile(id); return f }(), false); KindOf(err) != KindNotOwner {
		t.Fatalf("a member closed: %v", err)
	}
	orig := courier.read("redelivery.md")
	courier.write("redelivery.md", strings.Replace(orig, "the next day", "the same evening", 1))
	if err := courier.d.Close(path, false); err == nil || !strings.Contains(err.Error(), "not released") {
		t.Fatalf("closed over unreleased edits: %v", err)
	}
	courier.write("redelivery.md", orig)
	if err := courier.d.Close(path, false); err != nil {
		t.Fatal(err)
	}
	if d := courier.doc("redelivery.md"); !d.IsClosed() {
		t.Fatalf("status %q", d.Status())
	}
	if ts := courier.d.teamDocState(path, courier.doc("redelivery.md")); ts == nil || ts.Unrel {
		t.Fatalf("the close was not released: %+v", ts)
	}

	courier.write("lockers-domain.md", "---\nkalka_kind: domain\nkalka_domain: lockers\nkalka_team: parcels\nkalka_version: 1\n---\n# Lockers\n")
	if err := courier.d.Close("lockers-domain.md", false); err == nil || !strings.Contains(err.Error(), "domain") {
		t.Fatalf("closed a domain document: %v", err)
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
