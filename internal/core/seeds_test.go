package core

import (
	"strings"
	"testing"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

const seedParent = "# Redelivery scheduling\n\n## Scope\nThe redelivery module.\n"

// R4 acceptance: budding from a reviewer's copy gives a seed with its
// keys; the parent gains one signed budded entry with its plan unchanged;
// the backlink and the seed survive a sync both ways.
func TestBudFromReviewerAndSync(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.node("alice@corp.com"), w.node("bob@corp.com")
	alice.write("plan.md", seedParent)
	alice.mustSync("plan.md", bob, IntentSync)
	planBefore := bob.doc("plan.md").Plan()
	dev := bob.comment("plan.md", "scope", "The courier app needs the new rule too.")

	res, err := bob.d.Bud(BudRequest{Path: "plan.md", From: dev.ID, Title: "Courier app rescheduling"})
	if err != nil {
		t.Fatal(err)
	}
	seed := bob.doc(res.Seed)
	parent := bob.doc("plan.md")
	if !seed.IsSeed() || seed.Owner() != "bob@corp.com" || seed.Parent() != parent.KalkaID() || seed.Origin() != dev.ID {
		t.Fatalf("seed:\n%s", seed.Bytes())
	}
	if parent.Plan() != planBefore || len(parent.Items()) != 2 || parent.Items()[1].Entry.Kind != bp.KindBudded {
		t.Fatalf("parent:\n%s", parent.Bytes())
	}

	// The backlink reaches the owner, and the seed can be sent to her.
	bob.mustSync("plan.md", alice, IntentSync)
	equalFiles(t, "plan.md", alice, bob)
	bob.mustSync(res.Seed, alice, IntentSync)
	seeds := alice.d.Seeds()
	if len(seeds) != 1 || seeds[0].Owner != "bob@corp.com" || seeds[0].ParentRel != "plan.md" || seeds[0].Mine || seeds[0].Active == "" {
		t.Fatalf("alice's pre-backlog %+v", seeds)
	}
	// And back: alice comments, bob receives; the backlink stays one.
	alice.comment("plan.md", "scope", "Agreed, later.")
	alice.mustSync("plan.md", bob, IntentSync)
	n := 0
	for _, it := range bob.doc("plan.md").Items() {
		if it.Entry.Kind == bp.KindBudded {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d budded entries", n)
	}
}

// If the parent cannot be written, the seed exists and Backlink adds the
// entry later; nothing makes a second seed.
func TestBacklinkRetry(t *testing.T) {
	w := newWorld(t)
	alice := w.node("alice@corp.com")
	alice.write("plan.md", seedParent)
	// Bud, then drop the backlink from the parent, as if the second
	// write had failed.
	res, err := alice.d.Bud(BudRequest{Path: "plan.md", Title: "Locker pickup", Problem: "Recipients ask for lockers."})
	if err != nil {
		t.Fatal(err)
	}
	withID := alice.doc("plan.md")
	stripped := strings.SplitN(string(withID.Bytes()), "\n## Review log", 2)[0] + "\n"
	alice.write("plan.md", stripped)
	if len(alice.doc("plan.md").Items()) != 0 {
		t.Fatal("not stripped")
	}
	e, err := alice.d.Backlink(res.Seed)
	if err != nil || e == nil {
		t.Fatalf("backlink: %v", err)
	}
	if e2, err := alice.d.Backlink(res.Seed); err != nil || e2 != nil {
		t.Fatalf("second backlink: %v %v", e2, err)
	}
	if len(alice.d.Seeds()) != 1 {
		t.Fatal("a second seed appeared")
	}
}

// Promotion by the owner only; a seed can be handed off like any
// blueprint, and the new owner can then promote it.
func TestPromoteAndHandoffSeed(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.node("alice@corp.com"), w.node("bob@corp.com")
	alice.write("plan.md", seedParent)
	res, err := alice.d.Bud(BudRequest{Path: "plan.md", Title: "Locker pickup", Problem: "Recipients ask for lockers."})
	if err != nil {
		t.Fatal(err)
	}
	alice.mustSync(res.Seed, bob, IntentSync)
	if err := bob.d.Promote(res.Seed, "draft"); err == nil || !strings.Contains(err.Error(), "only the owner") {
		t.Fatalf("reviewer promoted: %v", err)
	}
	alice.mustSync(res.Seed, bob, IntentHandoff)
	if err := bob.d.Promote(res.Seed, ""); err != nil {
		t.Fatal(err)
	}
	if d := bob.doc(res.Seed); d.IsSeed() || d.Status() != "draft" {
		t.Fatalf("status %q", d.Status())
	}
	bob.mustSync(res.Seed, alice, IntentSync)
	if alice.doc(res.Seed).Status() != "draft" || len(alice.d.Seeds()) != 0 {
		t.Fatal("promotion did not reach alice")
	}
}
