package core

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUnread: what was held at the first start is read; a new document
// from a teammate is new until opened; comments by others count until the
// document is opened, one's own never do; the marks survive a restart of
// the list (read.json).
func TestUnread(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	if u := depot.d.Unread(); len(u) != 0 {
		t.Fatalf("nothing held, unread %+v", u)
	}
	if _, err := os.Stat(filepath.Join(depot.d.ConfigDir(), ReadFile)); err != nil {
		t.Fatalf("first look makes read.json: %v", err)
	}
	depot.write("lockers.md", lockers)
	waitIndexed(t, depot, 1)
	depot.publish("lockers.md")
	id := mustID(t, depot, "lockers.md")
	depot.replicate(courier)
	name := courier.rel(id)
	waitFor(t, "the copy indexed", func() bool { _, ok := courier.d.Store.Info(courier.d.resolve(name)); return ok })

	u := courier.d.Unread()
	if len(u) != 1 || !u[0].Never || u[0].Mine || u[0].Comments != 0 {
		t.Fatalf("a new document from a teammate: %+v", u)
	}
	if u := depot.d.Unread(); len(u) != 0 {
		t.Fatalf("own document, nothing new: %+v", u)
	}
	if err := courier.d.MarkRead(name); err != nil {
		t.Fatal(err)
	}
	if u := courier.d.Unread(); len(u) != 0 {
		t.Fatalf("opened: %+v", u)
	}

	courier.comment(name, "plan", "Missing a weekend slot.")
	courier.comment(name, "plan", "And a key for the night shift.")
	courier.replicate(depot)
	dname := depot.rel(id)
	waitFor(t, "comments indexed", func() bool { in, _ := depot.d.Store.Info(depot.d.resolve(dname)); return in.Entries >= 3 })
	u = depot.d.Unread()
	if len(u) != 1 || !u[0].Mine || u[0].Never || u[0].Comments != 2 || len(u[0].Authors) != 1 || u[0].Authors[0] != courier.email {
		t.Fatalf("two comments on the owner's document: %+v", u)
	}
	if n := depot.d.UnreadCounts()[depot.d.resolve(dname)]; n != 2 {
		t.Fatalf("count %d", n)
	}
	if u := courier.d.Unread(); len(u) != 0 {
		t.Fatalf("own comments are not new: %+v", u)
	}
	if err := depot.d.MarkRead(dname); err != nil {
		t.Fatal(err)
	}
	if u := depot.d.Unread(); len(u) != 0 {
		t.Fatalf("read: %+v", u)
	}
}
