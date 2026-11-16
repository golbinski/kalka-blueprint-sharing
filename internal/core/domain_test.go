package core

// Team domain documents (spec 6.12), with three
// daemons over the in-process rendezvous.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

const lockersDomain = `---
kind: domain
team: parcels
domain: lockers
owner: %s
---
# Parcel lockers

## Glossary

- **Locker bank**: the lockers at one depot.
`

func (n *node) writeDomain(file string) string {
	n.w.t.Helper()
	// Domain documents are written in the team's folder, where they are
	// released in place.
	if ts := n.d.teamDirs(); len(ts) == 1 {
		file = filepath.Join(filepath.Base(ts[0].Dir), file)
	}
	n.write(file, strings.Replace(lockersDomain, "%s", n.email, 1))
	waitStored(n.w.t, n, n.path(file), "Locker bank")
	return n.path(file)
}

func (n *node) release(path, note string, inc ...string) PublishResult {
	n.w.t.Helper()
	r, err := n.d.Release(path, note, inc)
	if err != nil {
		n.w.t.Fatalf("%s releasing %s: %v", n.email, path, err)
	}
	return r
}

// editFile replaces text in a file outside kalka, as an editor would.
func editFile(t *testing.T, n *node, path, from, to string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), from) {
		t.Fatalf("%s does not contain %q", path, from)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), from, to, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	waitStored(t, n, path, to)
}

func refusedWith(res SessionResult, want string) bool {
	for _, r := range res.Refused {
		if strings.Contains(r.Reason, want) {
			return true
		}
	}
	return false
}

// A domain release signed by a member who is not the document's owner is
// applied by no peer, and each says why (spec 6.12: its owner releases it
// like any owner).
func TestDomainReleaseByNonOwner(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example", "hub@parcels.example")
	depot, courier, hub := ns[0], ns[1], ns[2]
	path := depot.writeDomain("parcels-lockers-domain.md")

	if r := depot.release(path, "first glossary"); r.Version != 1 {
		t.Fatalf("version %d", r.Version)
	}
	f, _ := depot.d.Store.Load(path)
	if f.Doc.DomainVersion() != 1 {
		t.Fatalf("version: %q", f.Doc.Frontmatter().Inner())
	}
	if _, err := os.Stat(bp.SnapshotPath(path, 1)); err != nil {
		t.Fatalf("no snapshot of v1: %v", err)
	}
	// Snapshots are never listed, so never duplicates.
	for _, in := range depot.d.Store.List() {
		if bp.IsSnapshotName(in.Path) || in.Duplicate {
			t.Fatalf("listed: %+v", in)
		}
	}
	id := f.Doc.KalkaID()
	depot.replicate(courier)
	depot.replicate(hub)
	cpath, cdoc := courier.teamFile(id)
	if cdoc == nil || cdoc.DomainVersion() != 1 {
		t.Fatal("courier did not receive v1")
	}
	if _, err := os.Stat(bp.SnapshotPath(cpath, 1)); err != nil {
		t.Fatalf("courier kept no snapshot of the received v1: %v", err)
	}

	// The courier is not the owner: kalka refuses to release...
	editFile(t, courier, cpath, "the lockers at one depot.", "the lockers at one depot, forged.")
	if _, err := courier.d.Release(cpath, "", nil); err == nil || !strings.Contains(err.Error(), "only depot@parcels.example") {
		t.Fatalf("courier released: %v", err)
	}
	// ...so they sign a release of their own by hand.
	_, cdoc = courier.teamFile(id)
	forged, err := bp.Parse(cdoc.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	forged.SetFrontmatter(bp.KeyVersion, "2")
	e, err := bp.NewRelease(forged, courier.email, courier.w.now(), 2, false, nil, "", courier.d.signer())
	if err != nil {
		t.Fatal(err)
	}
	forged.AddEntry(e)
	os.WriteFile(cpath, forged.Bytes(), 0o644)
	waitStored(t, courier, cpath, "version: 2")

	// Each peer that takes from the courier says why it did not apply v2.
	// (The first session also gives the courier the valid v1 back, so the
	// forged file is put back before the next.)
	for _, n := range []*node{hub, depot} {
		os.WriteFile(cpath, forged.Bytes(), 0o644)
		waitStored(t, courier, cpath, "forged")
		res := n.replicate(courier)
		if !refusedWith(res, "only depot@parcels.example may release version 2") {
			t.Fatalf("%s: refusals %+v", n.email, res.Refused)
		}
		_, doc := n.teamFile(id)
		if doc.DomainVersion() != 1 || strings.Contains(string(doc.Bytes()), "forged") {
			t.Fatalf("%s applied the forged release", n.email)
		}
	}
	v, err := hub.d.Document(context.Background(), hub.d.rel(must(hub.teamFile(id))))
	if err != nil || v.Domain == nil || v.Domain.Version != 1 || v.Domain.Owner != depot.email || v.Team.Void != 1 {
		t.Fatalf("hub's view: %+v %+v %v", v.Domain, v.Team, err)
	}
}

func must(path string, _ *bp.Document) string { return path }

// A release reaches a member whose domain owner is offline through a
// relay; a reassignment with a cut-off voids the old owner's later
// release, and the new owner takes the document over.
func TestDomainRelayAndReassignment(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example", "hub@parcels.example")
	depot, courier, hub := ns[0], ns[1], ns[2]
	path := depot.writeDomain("parcels-lockers-domain.md")
	if _, err := depot.d.SetOwner("", courier.email, true); err != nil {
		t.Fatal(err)
	}
	depot.release(path, "")
	f, _ := depot.d.Store.Load(path)
	id := f.Doc.KalkaID()
	depot.replicate(courier)
	depot.replicate(hub)

	// v2 goes to the courier only; the depot goes offline; the hub gets
	// it from the courier.
	editFile(t, depot, path, "the lockers at one depot.", "the lockers at one depot, numbered.")
	depot.release(path, "numbering")
	depot.replicate(courier)
	depot.stop()
	if res := hub.replicate(courier); len(res.Received) != 1 {
		t.Fatalf("relay: %+v", res)
	}
	hpath, hdoc := hub.teamFile(id)
	if hdoc.DomainVersion() != 2 {
		t.Fatalf("hub at v%d", hdoc.DomainVersion())
	}
	if _, err := os.Stat(bp.SnapshotPath(hpath, 2)); err != nil {
		t.Fatal("no snapshot of the relayed v2")
	}

	// The depot, offline, releases v3. Meanwhile the courier, a team owner,
	// reassigns the document to the hub from version 3.
	depot.start()
	editFile(t, depot, path, "numbered.", "numbered by bay.")
	depot.release(path, "by bay")
	if r, err := courier.d.Reassign(must(courier.teamFile(id)), hub.email); err != nil || r.After != 2 || r.From != depot.email {
		t.Fatalf("reassign: %+v %v", r, err)
	}
	// The hub learns the assignment and takes the document over: owner line
	// and a release of v3.
	hub.replicate(courier)
	waitOwner(t, hub, id, hub.email)
	if _, hdoc = hub.teamFile(id); hdoc.DomainVersion() != 3 {
		t.Fatalf("hub did not release v3: v%d", hdoc.DomainVersion())
	}
	// The depot's v3 is void everywhere, and the depot gets the hub's.
	if res := hub.replicate(depot); !refusedWith(res, "reassigned to hub@parcels.example from version 3") {
		t.Fatalf("hub took the old owner's v3: %+v", res.Refused)
	}
	waitOwner(t, depot, id, hub.email)
	if _, d := depot.teamFile(id); strings.Contains(d.Plan(), "by bay") || d.DomainVersion() != 3 {
		t.Fatalf("depot kept its void v3: v%d %q", d.DomainVersion(), d.Plan())
	}
}

// Spec 6.12: comments a release incorporates are marked with its version;
// a blueprint's reference shows a newer version, and a version that is
// not kept here.
func TestDomainIncorporatedAndReferences(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	path := depot.writeDomain("parcels-lockers-domain.md")
	depot.release(path, "")
	f, _ := depot.d.Store.Load(path)
	id := f.Doc.KalkaID()
	depot.replicate(courier)

	cpath, _ := courier.teamFile(id)
	c, err := courier.d.AddEntry(EntryRequest{Path: cpath, Kind: bp.KindComment, Anchor: "glossary", Body: "Define a locker bank's size."})
	if err != nil {
		t.Fatal(err)
	}
	courier.replicate(depot)
	if rows := depot.d.Domains(); len(rows) != 1 || rows[0].Open != 1 || !rows[0].You {
		t.Fatalf("domains: %+v", rows)
	}
	if _, err := depot.d.Release(path, "", []string{"000000000000"}); err == nil {
		t.Fatal("released incorporating an unknown entry")
	}
	editFile(t, depot, path, "the lockers at one depot.", "the lockers at one depot, up to 40.")
	depot.release(path, "sizes", c.ID)
	v, err := depot.d.Document(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range v.Entries {
		if e.ID == c.ID && e.IncorporatedIn != 2 {
			t.Fatalf("comment incorporated in v%d", e.IncorporatedIn)
		}
	}
	if rows := depot.d.Domains(); rows[0].Open != 0 || rows[0].Version != 2 || len(rows[0].Versions) != 2 {
		t.Fatalf("domains after release: %+v", rows)
	}
	old, err := depot.d.DocumentAt(context.Background(), path, 1)
	if err != nil || old.Domain.Viewing != 1 || strings.Contains(old.Sections[len(old.Sections)-1].Markdown, "up to 40") {
		t.Fatalf("v1 view: %v", err)
	}

	// A blueprint written against v1 shows that v2 is held; one against v9
	// shows that version is not kept here.
	depot.write("locker-pickup.md", "---\ndomain: parcels/lockers@v1\n---\n# Locker pickup\n")
	waitStored(t, depot, depot.path("locker-pickup.md"), "Locker pickup")
	v, err = depot.d.Document(context.Background(), "locker-pickup.md")
	if err != nil || v.DomainRef == nil || !v.DomainRef.Held || v.DomainRef.Latest != 2 || v.DomainRef.Missing {
		t.Fatalf("reference: %+v %v", v.DomainRef, err)
	}
	depot.write("locker-pickup.md", "---\ndomain: parcels/lockers@v9\n---\n# Locker pickup\n")
	waitStored(t, depot, depot.path("locker-pickup.md"), "@v9")
	if v, _ = depot.d.Document(context.Background(), "locker-pickup.md"); !v.DomainRef.Missing {
		t.Fatalf("missing version not flagged: %+v", v.DomainRef)
	}
}
