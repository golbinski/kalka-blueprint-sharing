package blueprint

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"
)

const parentDoc = "---\nkalka_id: 6f1c2b9e-4d7a-4c3e-9b1a-2e5f8c7d6a40\nowner: dana@example.org\n---\n# Redelivery scheduling\n\n## Scope\nThe redelivery module.\n"

func TestBud(t *testing.T) {
	k := testKey("bob")
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	p := mustParse(t, parentDoc)
	dev, err := Append(p, KindDeviation, "bob@example.org", at, "scope", "", "", "The courier app's reschedule endpoint also needs the new rule.", signerOf(k))
	if err != nil {
		t.Fatal(err)
	}
	p = mustParse(t, string(p.Bytes()))
	planBefore := p.Plan()

	sid := NewKalkaID()
	seed, e, err := Bud(p, SeedSpec{KalkaID: sid, Owner: "bob@example.org", Title: "Courier app rescheduling", At: at.Add(time.Hour), From: dev.ID}, signerOf(k))
	if err != nil {
		t.Fatal(err)
	}
	// The seed: status, one owner, parent, origin, created, a new id.
	if !seed.IsSeed() || seed.Owner() != "bob@example.org" || seed.Parent() != p.KalkaID() || seed.Origin() != dev.ID || seed.KalkaID() != sid || sid == p.KalkaID() {
		t.Fatalf("seed:\n%s", seed.Bytes())
	}
	if c, ok := seed.Created(); !ok || !c.Equal(at.Add(time.Hour)) {
		t.Fatal("created")
	}
	if !seed.HasSection("problem") || !seed.HasSection("why-it-was-split-off") || seed.Title() != "Courier app rescheduling" ||
		!strings.Contains(string(seed.Bytes()), "> The courier app's reschedule endpoint") {
		t.Fatalf("seed body:\n%s", seed.Bytes())
	}
	if len(seed.Problems()) != 0 && seed.HasErrors() {
		t.Fatalf("problems %+v", seed.Problems())
	}
	// The parent: exactly one signed budded entry, plan unchanged.
	again := mustParse(t, string(p.Bytes()))
	if again.Plan() != planBefore || len(again.Items()) != 2 {
		t.Fatalf("parent:\n%s", again.Bytes())
	}
	b := again.Items()[1].Entry
	id, title, ok := b.Budded()
	if !ok || id != sid || title != "Courier app rescheduling" || b.ReplyTo() != dev.ID || b.Anchor != "scope" {
		t.Fatalf("budded %q", b.Body)
	}
	if b.Verify(again.KalkaID(), k.Public().(ed25519.PublicKey)) != SigOK {
		t.Fatal("budded entry not signed")
	}
	// The retry adds nothing when the backlink is there...
	if e2, err := AddBacklink(again, seed, "bob@example.org", at, signerOf(k)); err != nil || e2 != nil {
		t.Fatalf("second backlink: %v %v", e2, err)
	}
	// ...and the same entry content when it is missing.
	fresh := mustParse(t, parentDoc)
	fresh.AddEntry(dev)
	fresh = mustParse(t, string(fresh.Bytes()))
	e3, err := AddBacklink(fresh, seed, "bob@example.org", at, signerOf(k))
	if err != nil || e3 == nil || e3.ReplyTo() != dev.ID || e3.Anchor != "scope" {
		t.Fatalf("retry: %v %+v", err, e3)
	}
	_ = e
}

func TestBudWithoutOrigin(t *testing.T) {
	p := mustParse(t, parentDoc)
	seed, e, err := Bud(p, SeedSpec{KalkaID: NewKalkaID(), Owner: "dana@example.org", Title: "Locker pickup", Problem: "Recipients want lockers.", At: time.Now()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if seed.Origin() != "" || e.Anchor != "" || e.ReplyTo() != "" {
		t.Fatalf("origin set: %+v", e)
	}
	if _, _, err := Bud(p, SeedSpec{KalkaID: NewKalkaID(), Owner: "dana@example.org", Title: "x", At: time.Now()}, nil); err == nil {
		t.Fatal("seed with no problem")
	}
	if _, _, err := Bud(mustParse(t, "# Local\n"), SeedSpec{KalkaID: NewKalkaID(), Owner: "a@b.c", Title: "x", Problem: "y", At: time.Now()}, nil); err == nil {
		t.Fatal("parent without kalka_id")
	}
}

func TestPromote(t *testing.T) {
	p := mustParse(t, parentDoc)
	seed, _, _ := Bud(p, SeedSpec{KalkaID: NewKalkaID(), Owner: "dana@example.org", Title: "Locker pickup", Problem: "p", At: time.Now()}, nil)
	if err := Promote(seed, "seed"); err == nil {
		t.Fatal("promoted to seed")
	}
	if err := Promote(seed, "draft"); err != nil || seed.IsSeed() || seed.Status() != "draft" {
		t.Fatalf("promote: %v %q", err, seed.Status())
	}
	if err := Promote(seed, "ready"); err == nil {
		t.Fatal("promoted a non-seed")
	}
}
