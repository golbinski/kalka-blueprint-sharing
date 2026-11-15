package core

// Required tests of T4: transfers between teams (spec 20.7, D6).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/syncproto"
)

var w0 = time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC)

func sleep(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

// xfer is two teams: parcel-ops (ewa owns it, marek is a member) and
// library-desk (jan owns it; ewa and olga are members). Olga and Jan are
// not in parcel-ops.
type xfer struct {
	ewa, marek, jan, olga *node
	ops, lib              string // team hashes
}

func twoTeams(t *testing.T) *xfer {
	t.Helper()
	fastJoins(t)
	w := newWorld(t)
	x := &xfer{ewa: w.node("ewa@parcels.example"), marek: w.node("marek@parcels.example"),
		jan: w.node("jan@library.example"), olga: w.node("olga@library.example")}
	for _, n := range x.all() {
		waitKeyOnline(t, n)
	}
	pv, err := x.ewa.d.CreateTeam("parcel-ops")
	if err != nil {
		t.Fatal(err)
	}
	lv, err := x.jan.d.CreateTeam("library-desk")
	if err != nil {
		t.Fatal(err)
	}
	x.ops, x.lib = pv.Team, lv.Team
	joinTeam(t, x.ewa, x.marek, x.ops)
	joinTeam(t, x.jan, x.ewa, x.lib)
	joinTeam(t, x.jan, x.olga, x.lib)
	roundIn(t, x.jan, x.ewa, x.lib) // Ewa learns of Olga
	return x
}

func (x *xfer) all() []*node { return []*node{x.ewa, x.marek, x.jan, x.olga} }

// lockersIn publishes the lockers plan to parcel-ops with a comment by
// Marek, and returns its kalka_id and Ewa's path.
func (x *xfer) lockersIn(t *testing.T) (string, string) {
	t.Helper()
	x.ewa.write("lockers.md", lockers)
	waitStored(t, x.ewa, x.ewa.path("lockers.md"), "loading bay")
	r, err := x.ewa.d.Publish(x.ewa.path("lockers.md"), "parcel-ops", "")
	if err != nil {
		t.Fatal(err)
	}
	f, _ := x.ewa.d.Store.Load(r.Path)
	id := f.Doc.KalkaID()
	roundIn(t, x.ewa, x.marek, x.ops)
	mp, _ := x.marek.teamFile(id)
	if _, err := x.marek.d.AddEntry(EntryRequest{Path: mp, Kind: bp.KindComment, Anchor: "plan", Body: "Number the lockers."}); err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.marek, x.ewa, x.ops)
	return id, r.Path
}

func (n *node) held(t *testing.T, team string) *held {
	t.Helper()
	h, err := n.d.heldTeam(team)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// fileIn is the live file of id in a team's folder on n, or "".
func (n *node) fileIn(t *testing.T, team, id string) (string, *bp.Document) {
	t.Helper()
	f, err := n.d.Store.FindIn(n.held(t, team).dir.Dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if f == nil {
		return "", nil
	}
	return f.Path, f.Doc
}

func (n *node) pendingRaw(t *testing.T, team, id string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(incomingDir(n.held(t, team)), id+".md"))
	if err != nil {
		return nil
	}
	return raw
}

func (n *node) stubbed(t *testing.T, team, id string) bool {
	_, _, ok := n.d.pendingStub(n.held(t, team), id)
	return ok
}

// offerTo applies a pending document to n directly, as a session would.
func offerTo(t *testing.T, n *node, team, from string, raw []byte) (bool, *Refusal) {
	t.Helper()
	doc, err := bp.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return n.d.applyPending(n.held(t, team), from, syncproto.TeamDoc{Team: team, KalkaID: doc.KalkaID(), Content: string(raw), Pending: true})
}

// Copy accepted (required): a new id, copied-from, the source unchanged,
// the accepting owner is the owner and can hand it off; the history is
// attested by the actor, then by both; a member outside the source team
// holds it.
func TestCopyAccepted(t *testing.T) {
	x := twoTeams(t)
	src, path := x.lockersIn(t)
	before, _ := os.ReadFile(path)

	r, err := x.ewa.d.Copy(path, "library-desk")
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == src || r.Mode != bp.ModeCopy || r.From != "parcel-ops/"+src {
		t.Fatalf("result %+v", r)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("a copy changed the source")
	}
	if _, err := x.ewa.d.Copy(path, "library-desk"); KindOf(err) != KindTransferPending {
		t.Fatalf("a second copy while one is pending: %v", err)
	}
	roundIn(t, x.ewa, x.jan, x.lib)
	ts := x.jan.d.Transfers()
	if len(ts) != 1 || ts[0].ID != r.ID || ts[0].Actor != x.ewa.email || !ts[0].CanDecide || ts[0].Entries < 2 {
		t.Fatalf("jan's transfers %+v", ts)
	}
	if ts := x.olga.d.Transfers(); len(ts) != 0 {
		t.Fatalf("a plain member sees %+v", ts)
	}
	if _, err := x.olga.d.AcceptTransfer(r.ID, ""); KindOf(err) == "" {
		t.Fatalf("olga does not hold it, or is not an owner: %v", err)
	}

	v, err := x.jan.d.AcceptTransfer(r.ID, "")
	if err != nil || v.Status != TransferAccepted || v.Owner != x.jan.email {
		t.Fatalf("accept %+v %v", v, err)
	}
	jp, jd := x.jan.fileIn(t, x.lib, r.ID)
	if jd == nil {
		t.Fatal("jan did not install the document")
	}
	if jd.Owner() != x.jan.email || jd.CopiedFrom() != "parcel-ops/"+src || jd.Team() != "library-desk" {
		t.Fatalf("frontmatter: owner %s, copied-from %s, team %s", jd.Owner(), jd.CopiedFrom(), jd.Team())
	}
	if l := x.jan.headIn(t, x.lib, r.ID); l.Release.Version != 1 || l.Entry.Author != x.jan.email {
		t.Fatalf("version 1: %+v", l.Release)
	}
	if x.jan.pendingRaw(t, x.lib, r.ID) != nil {
		t.Fatal("the pending file is still there")
	}
	dv, err := x.jan.d.Document(context.Background(), jp)
	if err != nil {
		t.Fatal(err)
	}
	if dv.Transfer == nil || dv.Transfer.Actor != x.ewa.email || dv.Transfer.By != x.jan.email || dv.Transfer.From != "parcel-ops/"+src {
		t.Fatalf("attestation %+v", dv.Transfer)
	}
	attested := 0
	for _, e := range dv.Entries {
		if e.TeamState == "attested" {
			attested++
			if e.Sig != "attested" {
				t.Fatalf("attested entry shown as %s", e.Sig)
			}
		}
	}
	if attested < 2 {
		t.Fatalf("%d attested entries: %+v", attested, dv.Entries)
	}

	// Olga, never in parcel-ops, gets it with its history; Jan hands it
	// off to her.
	roundIn(t, x.jan, x.olga, x.lib)
	_, od := x.olga.fileIn(t, x.lib, r.ID)
	if od == nil || !strings.Contains(string(od.Bytes()), "Number the lockers.") {
		t.Fatal("olga lacks the document or its history")
	}
	if _, err := x.jan.d.Handoff(jp, x.olga.email); err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.jan, x.olga, x.lib)
	if _, od := x.olga.fileIn(t, x.lib, r.ID); od.Owner() != x.olga.email {
		t.Fatalf("after the handoff: %s", od.Owner())
	}
	// The source in parcel-ops is untouched on Marek's side.
	if _, md := x.marek.fileIn(t, x.ops, src); md == nil || md.Deleted() {
		t.Fatal("the source is gone")
	}
}

func (n *node) headIn(t *testing.T, team, id string) merge.Link {
	t.Helper()
	_, doc := n.fileIn(t, team, id)
	if doc == nil {
		t.Fatalf("%s does not hold %s", n.email, id)
	}
	h := n.held(t, team)
	l, ok := merge.Releases(entriesOf(doc), id, h.team()).Head()
	if !ok {
		t.Fatalf("%s holds no release of %s", n.email, id)
	}
	return l
}

// Move accepted (required): no tombstone before the accept, the moved:
// tombstone after it; the source team's members see it gone.
func TestMoveAccepted(t *testing.T) {
	x := twoTeams(t)
	src, path := x.lockersIn(t)
	r, err := x.ewa.d.Move(path, "library-desk", false)
	if err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.ewa, x.marek, x.ops)
	mp, _ := x.marek.fileIn(t, x.ops, src)
	mv, err := x.marek.d.Document(context.Background(), mp)
	if err != nil || mv.Transfer == nil || mv.Transfer.MovingTo != "library-desk" {
		t.Fatalf("marek sees no banner: %+v %v", mv.Transfer, err)
	}
	if x.ewa.stubbed(t, x.ops, src) || tombstoneOf(t, x.ewa, x.ops, src) != nil {
		t.Fatal("a tombstone before the accept")
	}
	roundIn(t, x.ewa, x.jan, x.lib)
	if _, err := x.jan.d.AcceptTransfer(r.ID, ""); err != nil {
		t.Fatal(err)
	}
	if tombstoneOf(t, x.ewa, x.ops, src) != nil {
		t.Fatal("the source was deleted before Ewa saw the accept")
	}
	heard := noticesOf(t, x.ewa)
	roundIn(t, x.jan, x.ewa, x.lib)
	waitFor(t, "the moved: tombstone", func() bool { return tombstoneOf(t, x.ewa, x.ops, src) != nil })
	stub := tombstoneOf(t, x.ewa, x.ops, src)
	if stub.MovedTo() != "library-desk/"+r.ID {
		t.Fatalf("moved-to %q", stub.MovedTo())
	}
	if !hasMoveRelease(stub, "library-desk/"+r.ID) {
		t.Fatalf("no moved: release in %s", stub.Bytes())
	}
	if got := noticesOfKind(heard(), NoticeTransfer); len(got) == 0 {
		t.Fatal("ewa heard nothing")
	}
	roundIn(t, x.ewa, x.marek, x.ops)
	if p, _ := x.marek.fileIn(t, x.ops, src); p != "" {
		t.Fatal("marek still holds the moved document")
	}
	if tombstoneOf(t, x.marek, x.ops, src) == nil {
		t.Fatal("marek has no tombstone")
	}
	// Ewa holds the destination copy, through which a link to the source
	// still resolves.
	roundIn(t, x.jan, x.ewa, x.lib)
	if f, err := x.ewa.d.Store.Resolve(src); err != nil || f == nil || f.Doc.KalkaID() != r.ID {
		t.Fatalf("the source id does not resolve to the moved document: %v %v", f, err)
	}
}

func tombstoneOf(t *testing.T, n *node, team, id string) *bp.Document {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(tombDir(n.held(t, team)), id+".md"))
	if err != nil {
		return nil
	}
	doc, err := bp.Parse(raw)
	if err != nil || !doc.Deleted() {
		return nil
	}
	return doc
}

func hasMoveRelease(doc *bp.Document, moved string) bool {
	for _, e := range entriesOf(doc) {
		if r, ok := e.Release(); ok && r.Deleted && r.Moved == moved {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if ok() {
			return
		}
		sleep(50)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// Decline of a copy (required): every destination peer discards it, and a
// lagging peer cannot bring it back. Decline of a move: the source keeps
// the document and ends the move, with no tombstone.
func TestDeclines(t *testing.T) {
	x := twoTeams(t)
	src, path := x.lockersIn(t)
	c, err := x.ewa.d.Copy(path, "library-desk")
	if err != nil {
		t.Fatal(err)
	}
	stale := x.ewa.pendingRaw(t, x.lib, c.ID)
	roundIn(t, x.ewa, x.olga, x.lib)
	roundIn(t, x.olga, x.jan, x.lib)
	if err := x.jan.d.DeclineTransfer(c.ID); err != nil {
		t.Fatal(err)
	}
	if x.jan.pendingRaw(t, x.lib, c.ID) != nil || !x.jan.stubbed(t, x.lib, c.ID) {
		t.Fatal("jan kept the declined offer")
	}
	roundIn(t, x.jan, x.olga, x.lib)
	roundIn(t, x.olga, x.ewa, x.lib)
	waitFor(t, "every peer stubs it", func() bool {
		for _, n := range []*node{x.ewa, x.olga} {
			if n.pendingRaw(t, x.lib, c.ID) != nil || !n.stubbed(t, x.lib, c.ID) {
				return false
			}
		}
		return true
	})
	// A lagging peer offers the old pending document: refused, the stub
	// stays.
	if ok, _ := offerTo(t, x.olga, x.lib, x.ewa.email, stale); ok || x.olga.pendingRaw(t, x.lib, c.ID) != nil {
		t.Fatal("a stale offer came back")
	}
	if _, err := x.jan.d.AcceptTransfer(c.ID, ""); KindOf(err) != KindTransferDecided {
		t.Fatalf("accept after decline: %v", err)
	}

	// A declined move.
	m, err := x.ewa.d.Move(path, "library-desk", false)
	if err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.ewa, x.jan, x.lib)
	if err := x.jan.d.DeclineTransfer(m.ID); err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.jan, x.ewa, x.lib)
	waitFor(t, "the move ends", func() bool {
		_, doc := x.ewa.fileIn(t, x.ops, src)
		return doc != nil && strings.Contains(string(doc.Bytes()), "move declined")
	})
	if tombstoneOf(t, x.ewa, x.ops, src) != nil {
		t.Fatal("a declined move deleted the source")
	}
	sp, _ := x.ewa.fileIn(t, x.ops, src)
	if v, _ := x.ewa.d.Document(context.Background(), sp); v.Transfer != nil && v.Transfer.MovingTo != "" {
		t.Fatalf("still being moved: %+v", v.Transfer)
	}
	// And a new transfer may start.
	if _, err := x.ewa.d.Copy(sp, "library-desk"); err != nil {
		t.Fatal(err)
	}
}

// Countersigned history (required): a pending document with one attested
// entry altered, removed or added is refused; and one whose transfer is
// signed by a key the destination does not bind is refused by every
// receiving peer, with the reason. A non-member actor is refused at once.
func TestOfferChecks(t *testing.T) {
	x := twoTeams(t)
	src, path := x.lockersIn(t)
	c, err := x.ewa.d.Copy(path, "library-desk")
	if err != nil {
		t.Fatal(err)
	}
	good := string(x.ewa.pendingRaw(t, x.lib, c.ID))
	doc, _ := bp.Parse([]byte(good))
	var comment *bp.Entry
	for _, e := range entriesOf(doc) {
		if e.Kind == bp.KindComment {
			comment = e
		}
	}
	if comment == nil {
		t.Fatal("the offer carries no comment")
	}
	altered := strings.Replace(good, "Number the lockers.", "Paint the lockers.", 1)
	removed := strings.Replace(good, comment.Header()+"\n\n"+comment.Body+"\n", "", 1)
	if removed == good {
		removed = strings.Replace(good, comment.Header(), "", 1)
	}
	added := good + "\n### comment · olek@parcels.example · 2026-11-12T09:30:00Z · § · rev:" + doc.Rev() + " · id:" + fakeID(t, doc) + "\n\nSneaked in.\n"
	for name, raw := range map[string]string{"altered": altered, "removed": removed, "added": added} {
		for _, n := range []*node{x.jan, x.olga} {
			ok, ref := offerTo(t, n, x.lib, x.ewa.email, []byte(raw))
			if ok || ref == nil || !strings.Contains(ref.Reason, "transfer refused") {
				t.Fatalf("%s offer on %s: %v %+v", name, n.email, ok, ref)
			}
		}
	}

	// Signed by Marek, whose key library-desk does not bind.
	forged := forgeOffer(t, x, src, path)
	for _, n := range []*node{x.jan, x.olga} {
		ok, ref := offerTo(t, n, x.lib, x.marek.email, forged)
		if ok || ref == nil || !strings.Contains(ref.Reason, "not signed by a key this team binds") {
			t.Fatalf("forged offer on %s: %v %+v", n.email, ok, ref)
		}
	}
	// Marek cannot start one: he is not in library-desk.
	mp, _ := x.marek.fileIn(t, x.ops, src)
	if _, err := x.marek.d.Copy(mp, "library-desk"); KindOf(err) != KindNotMember {
		t.Fatalf("a non-member's copy: %v", err)
	}
	// The intact offer is taken.
	if ok, ref := offerTo(t, x.jan, x.lib, x.ewa.email, []byte(good)); !ok || ref != nil {
		t.Fatalf("intact offer: %v %+v", ok, ref)
	}
}

// fakeID is a well-formed id for an entry text that was never hashed.
func fakeID(t *testing.T, doc *bp.Document) string {
	e, err := bp.NewEntry(bp.KindComment, "olek@parcels.example", w0, "", doc.Rev(), "Sneaked in.")
	if err != nil {
		t.Fatal(err)
	}
	return e.ID
}

// forgeOffer builds Ewa's offer again with the transfer entry signed by
// Marek.
func forgeOffer(t *testing.T, x *xfer, src, path string) []byte {
	t.Helper()
	f, err := x.ewa.d.Store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ops, lib := x.ewa.held(t, x.ops), x.ewa.held(t, x.lib)
	offer, _, err := x.ewa.d.buildOffer(ops, lib, f.Doc, bp.ModeCopy)
	if err != nil {
		t.Fatal(err)
	}
	var items []*bp.Item
	var tr bp.Transfer
	for _, e := range entriesOf(offer) {
		if t2, ok := e.Transfer(); ok && e.Author == x.ewa.email {
			tr = t2
			continue
		}
		items = append(items, &bp.Item{Entry: e})
	}
	offer.SetFrontmatter("owner", x.marek.email)
	plain, err := bp.Compose(offer, offer.EOL(), nil, items)
	if err != nil {
		t.Fatal(err)
	}
	tr.Plan = plain.PlanHash()
	e, err := bp.NewTransfer(plain, x.marek.email, w0, tr, x.marek.d.signer())
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	return plain.Bytes()
}

// The actor goes offline right after the click; an owner reached only
// through a third member still gets the offer.
func TestOfferRelayed(t *testing.T) {
	x := twoTeams(t)
	_, path := x.lockersIn(t)
	c, err := x.ewa.d.Copy(path, "library-desk")
	if err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.ewa, x.olga, x.lib)
	x.ewa.stop()
	heard := noticesOf(t, x.jan)
	roundIn(t, x.olga, x.jan, x.lib)
	if got := noticesOfKind(heard(), NoticeTransfer); len(got) != 1 || !strings.Contains(got[0].Message, x.ewa.email) {
		t.Fatalf("jan's notices: %+v", got)
	}
	if _, err := x.jan.d.AcceptTransfer(c.ID, ""); err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.jan, x.olga, x.lib)
	if p, _ := x.olga.fileIn(t, x.lib, c.ID); p == "" || x.olga.pendingRaw(t, x.lib, c.ID) != nil {
		t.Fatal("olga has no released copy, or keeps the pending one")
	}
}

// Two owners accept concurrently: one version 1 everywhere. Accept
// against decline, and accept against withdrawal: the accept wins.
func TestConcurrentDecisions(t *testing.T) {
	x := twoTeams(t)
	if _, err := x.jan.d.SetOwner("library-desk", x.olga.email, true); err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.jan, x.olga, x.lib)
	roundIn(t, x.jan, x.ewa, x.lib)
	_, path := x.lockersIn(t)
	offer := func() string {
		c, err := x.ewa.d.Copy(path, "library-desk")
		if err != nil {
			t.Fatal(err)
		}
		roundIn(t, x.ewa, x.jan, x.lib)
		roundIn(t, x.ewa, x.olga, x.lib)
		return c.ID
	}
	settle := func() {
		for i := 0; i < 2; i++ {
			roundIn(t, x.jan, x.olga, x.lib)
			roundIn(t, x.olga, x.ewa, x.lib)
			roundIn(t, x.ewa, x.jan, x.lib)
		}
	}
	oneV1 := func(id string) string {
		t.Helper()
		var owner string
		for _, n := range []*node{x.jan, x.olga, x.ewa} {
			l := n.headIn(t, x.lib, id)
			if l.Release.Version != 1 {
				t.Fatalf("%s: head v%d", n.email, l.Release.Version)
			}
			if owner == "" {
				owner = l.Release.Owner
			}
			if l.Release.Owner != owner {
				t.Fatalf("two versions 1: %s and %s", owner, l.Release.Owner)
			}
			if n.pendingRaw(t, x.lib, id) != nil || n.stubbed(t, x.lib, id) {
				t.Fatalf("%s keeps the offer", n.email)
			}
		}
		return owner
	}

	// Two accepts.
	id := offer()
	if _, err := x.jan.d.AcceptTransfer(id, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := x.olga.d.AcceptTransfer(id, ""); err != nil {
		t.Fatal(err)
	}
	settle()
	oneV1(id)

	// Accept against decline.
	id = offer()
	if err := x.jan.d.DeclineTransfer(id); err != nil {
		t.Fatal(err)
	}
	if _, err := x.olga.d.AcceptTransfer(id, ""); err != nil {
		t.Fatal(err)
	}
	settle()
	if owner := oneV1(id); owner != x.olga.email {
		t.Fatalf("owner %s", owner)
	}

	// Accept against withdrawal.
	id = offer()
	if err := x.ewa.d.WithdrawTransfer(id); err != nil {
		t.Fatal(err)
	}
	if _, err := x.jan.d.AcceptTransfer(id, x.olga.email); err != nil {
		t.Fatal(err)
	}
	settle()
	if owner := oneV1(id); owner != x.olga.email {
		t.Fatalf("owner %s", owner)
	}
}

// A move whose actor lost the source document ends as a copy, with a
// notice.
func TestMoveEndsAsCopy(t *testing.T) {
	x := twoTeams(t)
	src, path := x.lockersIn(t)
	m, err := x.ewa.d.Move(path, "library-desk", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x.ewa.d.Reassign(path, x.marek.email); err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.ewa, x.marek, x.ops)
	roundIn(t, x.marek, x.ewa, x.ops) // Marek's release
	roundIn(t, x.ewa, x.jan, x.lib)
	if _, err := x.jan.d.AcceptTransfer(m.ID, ""); err != nil {
		t.Fatal(err)
	}
	heard := noticesOf(t, x.ewa)
	roundIn(t, x.jan, x.ewa, x.lib)
	waitFor(t, "the move ends as a copy", func() bool {
		_, doc := x.ewa.fileIn(t, x.ops, src)
		return doc != nil && strings.Contains(string(doc.Bytes()), "move ended as a copy")
	})
	if tombstoneOf(t, x.ewa, x.ops, src) != nil {
		t.Fatal("the source was deleted")
	}
	got := noticesOfKind(heard(), NoticeTransfer)
	found := false
	for _, n := range got {
		found = found || strings.Contains(n.Message, "ended as a copy")
	}
	if !found {
		t.Fatalf("notices %+v", got)
	}
	if p, _ := x.jan.fileIn(t, x.lib, m.ID); p == "" {
		t.Fatal("the destination has no copy")
	}
}

// A move to a team and back works: a new id each way, and the second
// source tombstone in library-desk.
func TestMoveThereAndBack(t *testing.T) {
	x := twoTeams(t)
	src, path := x.lockersIn(t)
	there, err := x.ewa.d.Move(path, "library-desk", false)
	if err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.ewa, x.jan, x.lib)
	if _, err := x.jan.d.AcceptTransfer(there.ID, x.ewa.email); err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.jan, x.ewa, x.lib)
	waitFor(t, "ewa installs and the source goes", func() bool {
		p, _ := x.ewa.fileIn(t, x.lib, there.ID)
		return p != "" && tombstoneOf(t, x.ewa, x.ops, src) != nil
	})
	lp, _ := x.ewa.fileIn(t, x.lib, there.ID)
	back, err := x.ewa.d.Move(lp, "parcel-ops", false)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID == src || back.ID == there.ID {
		t.Fatalf("ids: %s %s %s", src, there.ID, back.ID)
	}
	v, err := x.ewa.d.AcceptTransfer(back.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the second tombstone", func() bool { return tombstoneOf(t, x.ewa, x.lib, there.ID) != nil })
	p, doc := x.ewa.fileIn(t, x.ops, back.ID)
	if p == "" || doc.MovedFrom() != "library-desk/"+there.ID || v.Status != TransferAccepted {
		t.Fatalf("back home: %q %+v", p, v)
	}
	// Its history: the comment made in parcel-ops, attested twice over.
	if !strings.Contains(string(doc.Bytes()), "Number the lockers.") {
		t.Fatal("the history did not come back")
	}
	dv, _ := x.ewa.d.Document(context.Background(), p)
	n := 0
	for _, e := range dv.Entries {
		if e.TeamState == "attested" {
			n++
		}
	}
	if n == 0 {
		t.Fatalf("nothing attested: %+v", dv.Entries)
	}
}

// A seed's parent resolves to a moved document.
func TestSeedParentAfterMove(t *testing.T) {
	x := twoTeams(t)
	src, path := x.lockersIn(t)
	b, err := x.ewa.d.Bud(BudRequest{Path: path, Title: "Locker numbering", Problem: "Couriers cannot find lockers."})
	if err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(x.ewa.d.Store.Dir(), b.Seed)
	f, _ := x.ewa.d.Store.Load(path)
	m, err := x.ewa.d.Move(f.Path, "library-desk", false)
	if err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.ewa, x.jan, x.lib)
	if _, err := x.jan.d.AcceptTransfer(m.ID, ""); err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.jan, x.ewa, x.lib)
	waitFor(t, "the move", func() bool { return tombstoneOf(t, x.ewa, x.ops, src) != nil })
	v, err := x.ewa.d.Document(context.Background(), seed)
	if err != nil {
		t.Fatal(err)
	}
	if v.Seed == nil || v.Seed.Parent != src || v.Seed.ParentTitle == "" || !strings.HasPrefix(v.Seed.ParentRel, "library-desk") {
		t.Fatalf("seed %+v", v.Seed)
	}
}
