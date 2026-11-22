package core

import (
	"strings"
	"testing"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

const seedParent = "# Redelivery scheduling\n\n## Scope\nThe redelivery module.\n"

// Budding from a reviewer's copy gives a seed with its
// keys; the parent gains one signed budded entry with its plan unchanged;
// the backlink and the seed survive replication both ways.
func TestBudFromReviewerAndSync(t *testing.T) {
	_, ns := teamOfN(t, "alice@parcels.example", "bob@parcels.example")
	alice, bob := ns[0], ns[1]
	alice.write("plan.md", seedParent)
	waitIndexed(t, alice, 1)
	pid := alice.share("plan.md", bob)
	pname := bob.rel(pid)
	planBefore := bob.doc(pname).Plan()
	dev := bob.comment(pname, "scope", "The courier app needs the new rule too.")

	res, err := bob.d.Bud(BudRequest{Path: pname, From: dev.ID, Title: "Courier app rescheduling"})
	if err != nil {
		t.Fatal(err)
	}
	seed := bob.doc(res.Seed)
	parent := bob.doc(pname)
	if !seed.IsSeed() || seed.Owner() != bob.email || seed.OriginDoc() != parent.KalkaID() || func() string { _, e, _ := seed.Origin(); return e }() != dev.ID {
		t.Fatalf("seed:\n%s", seed.Bytes())
	}
	if its := parent.Items(); parent.Plan() != planBefore || its[len(its)-1].Entry.Kind != bp.KindBudded {
		t.Fatalf("parent:\n%s", parent.Bytes())
	}

	// The backlink reaches the owner, and so does the seed: budded from a
	// team document, it is published to the team with its first version.
	bob.replicate(alice)
	seeds := alice.d.Seeds()
	if len(seeds) != 1 || seeds[0].Owner != bob.email || seeds[0].ParentRel != alice.rel(pid) || seeds[0].Mine || seeds[0].Active == "" {
		t.Fatalf("alice's pre-backlog %+v", seeds)
	}
	budded := 0
	for _, it := range alice.doc(alice.rel(pid)).Items() {
		if it.Entry.Kind == bp.KindBudded {
			budded++
		}
	}
	if budded != 1 {
		t.Fatalf("alice holds %d budded entries", budded)
	}
	// And back: alice comments, bob receives; the backlink stays one.
	alice.comment(alice.rel(pid), "scope", "Agreed, later.")
	alice.replicate(bob)
	n := 0
	for _, it := range bob.doc(pname).Items() {
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

// Promotion by the owner only.
func TestPromoteSeed(t *testing.T) {
	_, ns := teamOfN(t, "alice@parcels.example", "bob@parcels.example")
	alice, bob := ns[0], ns[1]
	alice.write("plan.md", seedParent)
	res, err := alice.d.Bud(BudRequest{Path: "plan.md", Title: "Locker pickup", Problem: "Recipients ask for lockers."})
	if err != nil {
		t.Fatal(err)
	}
	waitIndexed(t, alice, 2)
	id := alice.share(res.Seed, bob)
	if err := bob.d.Promote(bob.rel(id), "draft"); err == nil || !strings.Contains(err.Error(), "only the owner") {
		t.Fatalf("reviewer promoted: %v", err)
	}
	name := alice.rel(id)
	if err := alice.d.Promote(name, ""); err != nil {
		t.Fatal(err)
	}
	if d := alice.doc(name); d.IsSeed() || d.Status() != "draft" {
		t.Fatalf("status %q", d.Status())
	}
}

// In Personal a seed is yours whatever kalka_owner says, also one never
// sent, with no kalka_id and no owner (spec 7.1).
func TestPromotePersonalSeed(t *testing.T) {
	_, ns := teamOfN(t, "alice@parcels.example", "bob@parcels.example")
	alice := ns[0]
	alice.write("idea.md", "---\nkalka_status: seed\n---\n\n# Lockers by the door\n")
	waitStored(t, alice, alice.path("idea.md"), "Lockers by the door")
	var row *SeedView
	for _, s := range alice.d.Seeds() {
		if s.Rel == "idea.md" {
			row = &s
		}
	}
	if row == nil || !row.Mine {
		t.Fatalf("seed row %+v", row)
	}
	if err := alice.d.Promote("idea.md", ""); err != nil {
		t.Fatal(err)
	}
	if d := alice.doc("idea.md"); d.IsSeed() || d.Status() != "draft" {
		t.Fatalf("status %q", d.Status())
	}
}
