package core

// Ownership tests (spec 7): handoff, reassignment, displaced edits and domain
// documents (spec 7, 6.12).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/teamlog"
)

// noticesOfKind keeps the notices of one kind.
func noticesOfKind(ns []Notice, kind string) []Notice {
	var out []Notice
	for _, n := range ns {
		if n.Kind == kind {
			out = append(out, n)
		}
	}
	return out
}

// head is the highest valid release n holds of id.
func (n *node) head(id string) merge.Link {
	n.w.t.Helper()
	_, doc := n.teamFile(id)
	if doc == nil {
		n.w.t.Fatalf("%s does not hold %s", n.email, id)
	}
	h, err := n.d.heldTeam(n.team())
	if err != nil {
		n.w.t.Fatal(err)
	}
	l, ok := merge.Releases(entriesOf(doc), id, h.team()).Head()
	if !ok {
		n.w.t.Fatalf("%s holds no release of %s", n.email, id)
	}
	return l
}

// Immediate handoff (spec 7.2): with the recipient offline and the owner
// paused, the owner hands off; the release names the recipient; the
// recipient's first round delivers it with a notice and no question; the
// recipient hands it back.
func TestImmediateHandoff(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	depot.write("lockers.md", lockers)
	waitIndexed(t, depot, 1)
	id := depot.share("lockers.md", courier)

	courier.stop()
	depot.d.SetPaused(true)
	r, err := depot.d.Handoff(depot.path("lockers.md"), courier.email)
	if err != nil || r.Version != 2 {
		t.Fatalf("handoff: %+v %v", r, err)
	}
	if l := depot.head(id); l.Release.Owner != courier.email || l.Entry.Author != depot.email || l.Release.Version != 2 {
		t.Fatalf("the release does not name the recipient: %+v", l.Release)
	}
	if _, err := depot.d.Release(depot.path("lockers.md"), "", nil); KindOf(err) != KindNotOwner {
		t.Fatalf("the former owner released again: %v", err)
	}
	depot.d.SetPaused(false)

	courier.start()
	waitKeyOnline(t, courier)
	// Pausing took the depot offline; resuming reconnects in the background.
	waitKeyOnline(t, depot)
	heard := noticesOf(t, courier)
	depot.replicate(courier)
	got := noticesOfKind(heard(), NoticeHandoff)
	if len(got) != 1 || !strings.Contains(got[0].Message, depot.email) {
		t.Fatalf("recipient's notices: %+v", got)
	}
	if _, doc := courier.teamFile(id); doc.Owner() != courier.email {
		t.Fatalf("owner on the courier: %s", doc.Owner())
	}

	// And back.
	depotHeard := noticesOf(t, depot)
	if _, err := courier.d.Handoff(must(courier.teamFile(id)), depot.email); err != nil {
		t.Fatal(err)
	}
	courier.replicate(depot)
	if got := noticesOfKind(depotHeard(), NoticeHandoff); len(got) != 1 {
		t.Fatalf("handed back: %+v", got)
	}
	if l := depot.head(id); l.Release.Owner != depot.email || l.Release.Version != 3 {
		t.Fatalf("after handing back: %+v", l.Release)
	}

	// Refused: a non-member, a Personal file.
	if _, err := depot.d.Handoff(must(depot.teamFile(id)), "stranger@parcels.example"); KindOf(err) != KindNotMember {
		t.Fatalf("handoff to a non-member: %v", err)
	}
	depot.write("personal/draft.md", "# Draft\n\nNot yet.\n")
	waitStored(t, depot, depot.path("personal/draft.md"), "Not yet")
	if _, err := depot.d.Handoff(depot.path("personal/draft.md"), courier.email); KindOf(err) != KindInPersonal {
		t.Fatalf("handoff of a Personal file: %v", err)
	}
}

// Reassignment without consent (spec 7.3): a team owner reassigns a
// present member's document while that member is offline; the previous
// and the new owner each get one notice; a plain member's attempt fails
// with not-team-owner.
func TestReassignmentWithoutConsent(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example", "hub@parcels.example")
	depot, courier, hub := ns[0], ns[1], ns[2]
	courier.write("lockers.md", lockers)
	waitIndexed(t, courier, 1)
	courier.publish("lockers.md")
	id := mustID(t, courier, "lockers.md")
	courier.replicate(depot)
	depot.replicate(hub)

	if _, err := hub.d.Reassign(must(hub.teamFile(id)), hub.email); KindOf(err) != KindNotTeamOwner {
		t.Fatalf("a plain member reassigned: %v", err)
	}

	courier.stop()
	r, err := depot.d.Reassign(must(depot.teamFile(id)), hub.email)
	if err != nil || r.From != courier.email || r.After != 1 || r.Version != 0 {
		t.Fatalf("reassign: %+v %v", r, err)
	}
	if v, _ := depot.d.Document(context.Background(), must(depot.teamFile(id))); v.Team.Pending != hub.email {
		t.Fatalf("not shown as pending: %+v", v.Team)
	}
	historySays(t, depot, "", teamlog.ReassignDocument, `reassigned "`, "from "+courier.email+" to "+hub.email+" from version 2")
	hubHeard := noticesOf(t, hub)
	depot.replicate(hub)
	waitOwner(t, hub, id, hub.email)
	if l := hub.head(id); l.Release.Version != 2 || l.Entry.Author != hub.email {
		t.Fatalf("hub's release: %+v", l.Release)
	}
	if got := noticesOfKind(hubHeard(), NoticeReassigned); len(got) != 1 || !strings.Contains(got[0].Message, "to you") {
		t.Fatalf("new owner's notices: %+v", got)
	}

	courier.start()
	waitKeyOnline(t, courier)
	courierHeard := noticesOf(t, courier)
	hub.replicate(courier)
	waitOwner(t, courier, id, hub.email)
	got := noticesOfKind(courierHeard(), NoticeReassigned)
	if len(got) != 1 || !strings.Contains(got[0].Message, hub.email) {
		t.Fatalf("previous owner's notices: %+v", got)
	}
	// One notice each, not one per round.
	hub.replicate(courier)
	depot.replicate(courier)
	if got := noticesOfKind(append(courierHeard(), hubHeard()...), NoticeReassigned); len(got) != 0 {
		t.Fatalf("repeated notices: %+v", got)
	}
}

// A former owner's unreleased edits, replaced by the new owner's release,
// land in the trash with a notice (spec 7.5).
func TestDisplacedEdits(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	courier.write("lockers.md", lockers)
	waitIndexed(t, courier, 1)
	courier.publish("lockers.md")
	id := mustID(t, courier, "lockers.md")
	courier.replicate(depot)

	// The courier edits without releasing; the depot takes the document
	// over and releases its own plan.
	courier.edit("lockers.md", "by the loading bay", "by the loading bay, unreleased")
	if _, err := depot.d.Reassign(must(depot.teamFile(id)), depot.email); err != nil {
		t.Fatal(err)
	}
	editFile(t, depot, must(depot.teamFile(id)), "by the loading bay", "at the gate")
	depot.publish(depot.rel(id))
	heard := noticesOf(t, courier)
	depot.replicate(courier)
	waitOwner(t, courier, id, depot.email)
	if _, doc := courier.teamFile(id); !strings.Contains(doc.Body(), "at the gate") {
		t.Fatalf("courier kept its plan: %q", doc.Body())
	}
	got := noticesOfKind(heard(), NoticeDisplaced)
	if len(got) != 1 {
		t.Fatalf("notices: %+v", got)
	}
	copies, _ := filepath.Glob(filepath.Join(courier.d.Store.Dir(), "parcels", ".kalka", "trash", "lockers.displaced-*.md"))
	if len(copies) != 1 {
		t.Fatalf("displaced copies: %v", copies)
	}
	raw, _ := os.ReadFile(copies[0])
	if !strings.Contains(string(raw), "unreleased") {
		t.Fatalf("copy: %s", raw)
	}
	for _, in := range courier.d.Store.List() {
		if strings.Contains(in.Path, "displaced") {
			t.Fatalf("the copy is indexed: %s", in.Path)
		}
	}
}

// A domain document is released by its owner, handed off and reassigned
// like any document; a second document for the same domain is refused
// locally and flagged when published concurrently (spec 6.12).
func TestDomainOwnership(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	path := depot.writeDomain("parcels-lockers-domain.md")
	depot.release(path, "glossary")
	f, _ := depot.d.Store.Load(path)
	id := f.Doc.KalkaID()
	if _, err := depot.d.Handoff(path, courier.email); err != nil {
		t.Fatal(err)
	}
	depot.replicate(courier)
	cpath := must(courier.teamFile(id))
	editFile(t, courier, cpath, "the lockers at one depot.", "the lockers at one depot, numbered.")
	if r := courier.release(cpath, "numbering"); r.Version != 3 {
		t.Fatalf("courier's release: %+v", r)
	}
	courier.replicate(depot)
	// Removing courier would take the domain with the document.
	if prev, err := depot.d.RemovalPreview("", courier.email); err != nil || len(prev) != 1 || prev[0].KalkaID != id || prev[0].Domain == "" || prev[0].Domain != f.Doc.DomainName() {
		t.Fatalf("removal preview of the domain owner: %+v %v", prev, err)
	}
	if r, err := depot.d.Reassign(path, depot.email); err != nil || r.Version != 4 {
		t.Fatalf("depot takes it back: %+v %v", r, err)
	}
	if _, doc := depot.teamFile(id); doc.DomainVersion() != 4 || doc.Owner() != depot.email {
		t.Fatalf("after reassignment: v%d %s", doc.DomainVersion(), doc.Owner())
	}

	// A second document for the domain is refused here.
	depot.write("parcels/lockers-again.md", strings.Replace(lockersDomain, "%s", depot.email, 1))
	waitStored(t, depot, depot.path("parcels/lockers-again.md"), "Locker bank")
	if _, err := depot.d.Release(depot.path("parcels/lockers-again.md"), "", nil); KindOf(err) != KindDomainExists {
		t.Fatalf("second domain document: %v", err)
	}
}

// Two members publish a document for one domain before either sees the
// other's: both are kept and both are flagged.
func TestConcurrentDomainsFlagged(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	depot.release(depot.writeDomain("parcels-lockers-domain.md"), "")
	courier.release(courier.writeDomain("lockers-glossary.md"), "")
	depot.replicate(courier)
	for _, n := range []*node{depot, courier} {
		var clash int
		for _, in := range n.d.Store.FindDomain("parcels", "lockers") {
			v, err := n.d.Document(context.Background(), in.Path)
			if err != nil {
				t.Fatal(err)
			}
			if v.Team.DomainClash != "" {
				clash++
			}
		}
		if clash != 2 {
			t.Fatalf("%s flags %d of the two", n.email, clash)
		}
	}
	depot.write("locker-pickup.md", "---\nkalka_domain: parcels/lockers@v1\n---\n# Locker pickup\n")
	waitStored(t, depot, depot.path("locker-pickup.md"), "Locker pickup")
	if v, err := depot.d.Document(context.Background(), "locker-pickup.md"); err != nil || v.DomainRef == nil || !v.DomainRef.Ambiguous {
		t.Fatalf("reference: %+v %v", v.DomainRef, err)
	}
}
