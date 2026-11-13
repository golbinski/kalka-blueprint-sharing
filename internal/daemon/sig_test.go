package daemon

import (
	"context"
	"testing"
)

// Entries written through the UI are signed; the receiving peer, which
// has pinned the author's key, shows them as verified (spec 6.5).
func TestUIEntriesSigned(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.node("alice@corp.com"), w.node("bob@corp.com")
	alice.write("plan.md", "# Locker pickup\n\n## Plan\nAdd a pickup code.\n")
	alice.mustSync("plan.md", bob, IntentSync)
	if _, err := bob.d.AddEntry(EntryRequest{Path: "plan.md", Kind: "comment", Anchor: "plan", Body: "Codes expire after 48 hours."}); err != nil {
		t.Fatal(err)
	}
	bob.comment("plan.md", "plan", "Typed by hand.")
	bob.mustSync("plan.md", alice, IntentSync)

	v, err := alice.d.Document(context.Background(), "plan.md")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range v.Entries {
		got[e.Body] = e.Sig
	}
	if got["Codes expire after 48 hours."] != "verified" || got["Typed by hand."] != "unsigned" {
		t.Fatalf("sig states %v", got)
	}
}
