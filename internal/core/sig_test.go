package core

import (
	"context"
	"testing"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// Entries written through the UI are signed; the receiving member checks
// them against the key the team log binds (spec 6.5, 20.4). An entry typed
// by hand stays unsigned in the writer's file and does not travel.
func TestUIEntriesSigned(t *testing.T) {
	_, ns := teamOfN(t, "desk@library.example", "reader@library.example")
	desk, reader := ns[0], ns[1]
	desk.write("plan.md", "# Locker pickup\n\n## Plan\nAdd a pickup code.\n")
	waitIndexed(t, desk, 1)
	id := desk.share("plan.md", reader)
	name := reader.rel(id)
	if _, err := reader.d.AddEntry(EntryRequest{Path: name, Kind: "comment", Anchor: "plan", Body: "Codes expire after 48 hours."}); err != nil {
		t.Fatal(err)
	}
	d := reader.doc(name)
	e, err := bp.NewEntry(bp.KindComment, reader.email, reader.w.now(), "plan", d.Rev(), "Typed by hand.")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	reader.write(name, string(d.Bytes()))
	waitStored(t, reader, reader.path(name), "Typed by hand.")
	reader.replicate(desk)

	sigs := func(n *node) map[string]string {
		v, err := n.d.Document(context.Background(), n.rel(id))
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, e := range v.Entries {
			got[e.Body] = e.Sig
		}
		return got
	}
	if got := sigs(desk); got["Codes expire after 48 hours."] != "verified" || got["Typed by hand."] != "" {
		t.Fatalf("desk's sig states %v", got)
	}
	if got := sigs(reader); got["Typed by hand."] != "unsigned" {
		t.Fatalf("reader's sig states %v", got)
	}
}
