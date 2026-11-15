package core

// Transfers between teams (spec 20.7): the offer is a pending document in
// the destination's replica, the decision a destination team owner's log
// operation, and for a move the source tombstone the actor signs once
// the destination log shows the accept.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/store"
	"github.com/golbinski/kalka/internal/syncproto"
	"github.com/golbinski/kalka/internal/teamlog"
)

// Notice kind of transfers: one waits for your team, yours was accepted
// or declined, a move finished or ended as a copy.
const NoticeTransfer = "transfer"

// Transfer states (TransferView.Status).
const (
	TransferPending   = "pending"
	TransferAccepted  = "accepted"
	TransferWithdrawn = "withdrawn"
)

func incomingDir(h *held) string { return filepath.Join(h.dir.Dir, ".kalka", "incoming") }

// TransferResult reports a copy or a move that was started.
type TransferResult struct {
	ID   string `json:"id"`   // the new document's kalka_id: the transfer's id
	Mode string `json:"mode"` // copy or move
	From string `json:"from"` // <team>/<kalka_id> of the source
	Team string `json:"team"` // the destination
	// Source is the source file; Path the pending file in the
	// destination's incoming folder.
	Source string `json:"source"`
	Path   string `json:"path"`
}

// Copy offers a copy of a team document to another team (spec 20.7).
func (d *Node) Copy(path, team string) (TransferResult, error) {
	return d.transfer(path, team, bp.ModeCopy, false)
}

// Move offers a team document to another team; the source is deleted with
// a `moved:` tombstone once the destination accepts. takeOver lets a team
// owner who is not the owner reassign the document to themselves first
// (spec 7.4); without it that is refused, as on the CLI.
func (d *Node) Move(path, team string, takeOver bool) (TransferResult, error) {
	return d.transfer(path, team, bp.ModeMove, takeOver)
}

func (d *Node) transfer(path, to, mode string, takeOver bool) (TransferResult, error) {
	var res TransferResult
	f, err := d.Store.Load(d.resolve(path))
	if err != nil {
		return res, err
	}
	src, err := d.heldAt(f.Path)
	if err != nil {
		if _, inTeam := d.teamDirAt(f.Path); !inTeam {
			return res, &Error{Kind: KindInPersonal, Msg: fmt.Sprintf("%s lies in Personal: publish it to a team instead (`%s publish`)", d.rel(f.Path), name.Binary)}
		}
		return res, &Error{Kind: KindNotMember, Msg: err.Error()}
	}
	id := f.Doc.KalkaID()
	if id == "" || f.Doc.Team() != src.dir.Name || f.Doc.HasErrors() || f.Doc.Deleted() {
		return res, &Error{Kind: KindNotShared, Msg: fmt.Sprintf("%s is not a document of %s", d.rel(f.Path), src.dir.Name)}
	}
	dt, err := d.findTeam(to)
	if err != nil {
		return res, &Error{Kind: KindNotMember, Msg: fmt.Sprintf("%v: only a member of both teams can copy or move a document between them", err)}
	}
	if dt.Team == src.dir.Team {
		return res, fmt.Errorf("%s already belongs to %s", d.rel(f.Path), dt.Name)
	}
	dst, err := d.heldTeam(dt.Team)
	if err != nil || !dst.isMember(d.self.Email, d.pub) {
		return res, &Error{Kind: KindNotMember, Msg: fmt.Sprintf("you are not a member of %s: only a member of both teams can copy or move a document between them", dt.Name)}
	}
	ch := merge.Releases(entriesOf(f.Doc), id, src.team())
	if ch.Disputed {
		return res, &Error{Kind: KindDisputed, Msg: "ownership of this document is disputed; a team owner settles it by reassigning it first"}
	}
	head, ok := ch.Head()
	if !ok || head.Release.Deleted {
		return res, &Error{Kind: KindNotShared, Msg: fmt.Sprintf("%s has no release in %s", d.rel(f.Path), src.dir.Name)}
	}
	releaser, _ := nextReleaser(ch, f.Doc.Owner())
	isOwner := bp.SameEmail(releaser, d.self.Email)
	me, _ := memberOf(src.st, d.self.Email)
	if !isOwner && !me.Owner {
		return res, &Error{Kind: KindNotOwner, Msg: fmt.Sprintf("only %s, the owner, or a team owner of %s can copy or move this document", releaser, src.dir.Name)}
	}
	if p := d.transferPending(src, f.Doc); p != "" {
		return res, &Error{Kind: KindTransferPending, Msg: p}
	}
	if mode == bp.ModeMove && !isOwner {
		if !takeOver {
			return res, &Error{Kind: KindNotOwner, Msg: fmt.Sprintf("only the owner moves a document; reassign it to yourself first (`%s reassign %s %s`)", name.Binary, d.rel(f.Path), d.self.Email)}
		}
		r, err := d.reassign(f.Path, d.self.Email, "taken over to move it to "+dt.Name)
		if err != nil {
			return res, err
		}
		if r.Version == 0 {
			return res, fmt.Errorf("reassigned %s to you; move it once the earlier releases arrive", d.rel(r.Path))
		}
		if f, err = d.Store.Load(r.Path); err != nil {
			return res, err
		}
		head, _ = merge.Releases(entriesOf(f.Doc), id, src.team()).Head()
	}
	if f.Doc.PlanHash() != head.Release.Plan {
		return res, fmt.Errorf("%s differs from its version %d; only released plans travel: release or undo the edits first", d.rel(f.Path), head.Release.Version)
	}

	unlock, ok := d.Store.Lock(id, syncproto.LockWait)
	if !ok {
		return res, errors.New("this document is busy in a sync; try again")
	}
	defer unlock()
	cur, err := d.Store.Load(f.Path)
	if err != nil || !bytes.Equal(cur.Raw, f.Raw) {
		return res, errors.New("the file changed while the transfer was prepared; try again")
	}
	offer, entry, err := d.buildOffer(src, dst, f.Doc, mode)
	if err != nil {
		return res, err
	}
	newID := offer.KalkaID()
	if err := os.MkdirAll(incomingDir(dst), 0o755); err != nil {
		return res, err
	}
	pending := filepath.Join(incomingDir(dst), newID+".md")
	if err := fsutil.WriteAtomic(pending, offer.Bytes(), nil); err != nil {
		return res, err
	}
	if mode == bp.ModeMove {
		// The source records the start: its members see the banner.
		se := *entry
		se.Sign(id, d.signer())
		doc, err := bp.Parse(f.Raw)
		if err == nil {
			err = doc.AddEntry(&se)
		}
		if err == nil {
			err = d.Store.Update(f, doc)
		}
		if err != nil {
			_ = os.Remove(pending)
			return res, err
		}
		d.pushTeam(src.dir.Team)
	}
	d.pushTeam(dst.dir.Team)
	d.bus.Publish(BusEvent{Type: "teams"})
	d.logf("offered %s to %s as a %s (%s)", f.Path, dst.dir.Name, mode, newID)
	return TransferResult{ID: newID, Mode: mode, From: src.dir.Name + "/" + id, Team: dst.dir.Name, Source: f.Path, Path: pending}, nil
}

// buildOffer is the pending document (spec 20.7 step 2 and 3): a fresh
// kalka_id, the destination's team, the source named, the local user as
// owner for now, the released plan, every entry the source team trusts
// byte for byte, and the signed transfer entry, which is returned too.
func (d *Node) buildOffer(src, dst *held, doc *bp.Document, mode string) (*bp.Document, *bp.Entry, error) {
	id := doc.KalkaID()
	nd, err := doc.Reparse()
	if err != nil {
		return nil, nil, err
	}
	newID := newUUID()
	nd.SetFrontmatter(name.FrontmatterID, newID)
	nd.SetFrontmatter(name.FrontmatterOwner, strings.ToLower(d.self.Email))
	nd.SetFrontmatter(bp.KeyTeam, dst.dir.Name)
	nd.DeleteFrontmatter(bp.KeyCopiedFrom)
	nd.DeleteFrontmatter(bp.KeyMovedFrom)
	key := bp.KeyCopiedFrom
	if mode == bp.ModeMove {
		key = bp.KeyMovedFrom
	}
	nd.SetFrontmatter(key, src.dir.Name+"/"+id)
	trust := merge.Trusted(src.team(), id, entriesOf(doc))
	var hist []*bp.Entry
	var items []*bp.Item
	for _, e := range entriesOf(doc) {
		if e.Valid() && trust(e) {
			hist = append(hist, e)
			items = append(items, &bp.Item{Entry: e})
		}
	}
	offer, err := bp.Compose(nd, nd.EOL(), nil, items)
	if err != nil {
		return nil, nil, err
	}
	t := bp.Transfer{Mode: mode, FromTeam: src.dir.Name, FromID: id, Source: src.dir.Team, ToTeam: dst.dir.Name, To: dst.dir.Team,
		Plan: offer.PlanHash(), History: bp.HistoryDigest(hist), Entries: bp.EntryIDs(hist)}
	e, err := bp.NewTransfer(offer, strings.ToLower(d.self.Email), d.now(), t, d.signer())
	if err != nil {
		return nil, nil, err
	}
	if err := offer.AddEntry(e); err != nil {
		return nil, nil, err
	}
	offer, err = offer.Reparse()
	return offer, e, err
}

// transferPending says why a transfer of doc cannot start because another
// is under way, or "": a move whose source entry is open, or an offer of
// it still waiting in a team held here.
func (d *Node) transferPending(src *held, doc *bp.Document) string {
	if mv := openMove(src, doc); mv != nil {
		t, _ := mv.Transfer()
		return fmt.Sprintf("a move of this document to %s is pending; withdraw it first", t.ToTeam)
	}
	from := src.dir.Name + "/" + doc.KalkaID()
	for _, t := range d.teamDirs() {
		h, err := d.heldTeam(t.Team)
		if err != nil {
			continue
		}
		for _, p := range d.pendingDocs(h) {
			if p.err == nil && p.offer.Transfer.From() == from && !p.decided(h) {
				return fmt.Sprintf("a %s of this document to %s is pending (%s)", p.offer.Transfer.Mode, h.dir.Name, p.id)
			}
		}
	}
	return ""
}

// openMove is the source's transfer entry of a move that has not ended:
// signed under the source's id by a key of the source team, and with no
// transfer-withdrawn entry answering it.
func openMove(src *held, doc *bp.Document) *bp.Entry {
	id := doc.KalkaID()
	ended := map[string]bool{}
	for _, e := range entriesOf(doc) {
		if w := e.Withdraws(); w != "" && merge.VerifiedBy(src.team(), id, e) {
			ended[w] = true
		}
	}
	var open *bp.Entry
	for _, e := range entriesOf(doc) {
		if t, ok := e.Transfer(); ok && t.Mode == bp.ModeMove && t.FromID == id && !ended[e.ID] && e.Valid() && merge.VerifiedBy(src.team(), id, e) {
			open = e
		}
	}
	return open
}

// pendingDoc is a file in a team's incoming folder, checked.
type pendingDoc struct {
	id    string
	path  string
	raw   []byte
	doc   *bp.Document
	offer *merge.Offer
	err   error // why it does not count
}

func (p pendingDoc) digest() string { return p.offer.Offer.Digest() }

// decision is the effective decision on the offer, if any.
func (p pendingDoc) decision(h *held) (teamlog.Decision, bool) {
	dec, ok := h.st.Transfers[p.digest()]
	return dec, ok && dec.Doc == p.id
}

func (p pendingDoc) decided(h *held) bool {
	_, ok := p.decision(h)
	return ok || p.offer.Withdrawn
}

// checkPending checks a pending document against h (spec 20.7): the
// offer, and that its actor is a member.
func checkPending(h *held, doc *bp.Document) (*merge.Offer, error) {
	o, err := merge.CheckOffer(doc, h.team(), h.dir.Team)
	if err != nil {
		return nil, err
	}
	if _, ok := memberOf(h.st, o.Actor); !ok {
		return nil, fmt.Errorf("the transfer was signed by %s, who is not a member", o.Actor)
	}
	return o, nil
}

// pendingDocs lists the team's incoming folder, by id.
func (d *Node) pendingDocs(h *held) []pendingDoc {
	paths, _ := filepath.Glob(filepath.Join(incomingDir(h), "*.md"))
	sort.Strings(paths)
	var out []pendingDoc
	for _, p := range paths {
		pd, ok := readPending(h, p)
		if ok {
			out = append(out, pd)
		}
	}
	return out
}

func readPending(h *held, path string) (pendingDoc, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return pendingDoc{}, false
	}
	doc, err := bp.Parse(raw)
	if err != nil || doc.KalkaID() == "" {
		return pendingDoc{}, false
	}
	p := pendingDoc{id: doc.KalkaID(), path: path, raw: raw, doc: doc}
	p.offer, p.err = checkPending(h, doc)
	return p, true
}

// pendingStub is a tombstone of an offer that was declined or withdrawn:
// the frontmatter, the transfer entry and any withdrawal.
func (d *Node) pendingStub(h *held, id string) (*bp.Document, []byte, bool) {
	raw, err := os.ReadFile(filepath.Join(tombDir(h), id+".md"))
	if err != nil {
		return nil, nil, false
	}
	doc, err := bp.Parse(raw)
	if err != nil {
		return nil, nil, false
	}
	if _, _, err := merge.CheckStub(doc, h.team()); err != nil {
		return nil, nil, false
	}
	return doc, raw, true
}

// pendingItem is the inventory line of a pending document or its stub.
func pendingItem(doc *bp.Document, stub bool) syncproto.InvItem {
	var ids []string
	for _, e := range entriesOf(doc) {
		if e.Valid() {
			ids = append(ids, e.ID)
		}
	}
	sort.Strings(ids)
	return syncproto.InvItem{ID: doc.KalkaID(), Plan: doc.PlanHash(), Entries: idsDigest(ids), Pending: true, Deleted: stub}
}

// stubOffer replaces a pending file by its stub (spec 20.7: decline,
// withdrawal); the full offer goes to the trash, from where an accept that
// wins after all restores it.
func (d *Node) stubOffer(h *held, p pendingDoc) error {
	plan, err := stubOf(p.doc)
	if err != nil {
		return err
	}
	items := []*bp.Item{{Entry: p.offer.Offer}}
	for _, e := range entriesOf(p.doc) {
		if e.Withdraws() == p.offer.Offer.ID && bp.SameEmail(e.Author, p.offer.Actor) {
			items = append(items, &bp.Item{Entry: e})
		}
	}
	stub, err := bp.Compose(plan, p.doc.EOL(), nil, items)
	if err != nil {
		return err
	}
	for _, dir := range []string{tombDir(h), trashDir(h)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := fsutil.WriteAtomic(filepath.Join(tombDir(h), p.id+".md"), stub.Bytes(), nil); err != nil {
		return err
	}
	if err := fsutil.WriteAtomic(offerTrash(h, p.id), p.raw, nil); err != nil {
		return err
	}
	if err := os.Remove(p.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	d.bus.Publish(BusEvent{Type: "teams"})
	return nil
}

func offerTrash(h *held, id string) string { return filepath.Join(trashDir(h), id+".offer.md") }

// restoreOffer undoes a stub when an accept counts after all (rule 8: an
// accept beats a decline or a withdrawal).
func (d *Node) restoreOffer(h *held, id string) {
	raw, err := os.ReadFile(offerTrash(h, id))
	if err != nil {
		_ = os.Remove(filepath.Join(tombDir(h), id+".md")) // the offer comes again from a peer
		return
	}
	if err := os.MkdirAll(incomingDir(h), 0o755); err != nil {
		return
	}
	if err := fsutil.WriteAtomic(filepath.Join(incomingDir(h), id+".md"), raw, nil); err != nil {
		return
	}
	_ = os.Remove(filepath.Join(tombDir(h), id+".md"))
	_ = os.Remove(offerTrash(h, id))
}

// applyPending merges a received pending document or stub (spec 20.7).
// Nothing that fails the checks is written; the refusal names the reason.
func (d *Node) applyPending(h *held, from string, off syncproto.TeamDoc) (bool, *Refusal) {
	refuse := func(title, reason string) (bool, *Refusal) {
		return false, &Refusal{KalkaID: off.KalkaID, Title: title, From: from, Reason: "transfer refused: " + reason, At: d.now().UTC()}
	}
	in, err := bp.Parse([]byte(off.Content))
	if err != nil {
		return refuse("", "damaged file: "+err.Error())
	}
	id := in.KalkaID()
	if id != strings.ToLower(off.KalkaID) || in.Team() != h.dir.Name {
		return refuse(in.Title(), "the file is not the pending document asked for")
	}
	unlock, ok := d.Store.Lock(id, syncproto.LockWait)
	if !ok {
		return refuse(in.Title(), "the document is busy in another sync; it comes again in the next round")
	}
	written, ref := d.applyPendingLocked(h, in, refuse)
	unlock()
	if written {
		d.afterTransfers(h)
	}
	return written, ref
}

func (d *Node) applyPendingLocked(h *held, in *bp.Document, refuse func(string, string) (bool, *Refusal)) (bool, *Refusal) {
	id := in.KalkaID()
	if f, _ := d.Store.FindIn(h.dir.Dir, id); f != nil {
		return false, nil // released here already
	}
	if _, _, stubbed := d.pendingStub(h, id); stubbed {
		return false, nil // final, unless an accept restores it
	}
	path := filepath.Join(incomingDir(h), id+".md")
	if offer, withdrawn, err := merge.CheckStub(in, h.team()); err == nil && in.Body() == "" && len(entriesOf(in)) <= 2 {
		// A withdrawal's stub: the withdrawal ends the offer here too,
		// unless an accept counts.
		if !withdrawn {
			return false, nil
		}
		if _, ok := h.st.Transfers[offer.Digest()]; ok {
			return false, nil
		}
		p, held := readPending(h, path)
		if held && p.err == nil && p.offer.Offer.ID == offer.ID {
			merged, _, err := merge.Pending(p.doc, withWithdrawal(p.doc, in), h.team(), h.dir.Team)
			if err == nil {
				p.doc, p.raw = merged, merged.Bytes()
				p.offer, _ = checkPending(h, merged)
				return d.stubOffer(h, p) == nil, nil
			}
		}
		if err := os.MkdirAll(tombDir(h), 0o755); err != nil {
			return refuse(in.Title(), err.Error())
		}
		return fsutil.WriteAtomic(filepath.Join(tombDir(h), id+".md"), in.Bytes(), nil) == nil, nil
	}
	o, err := checkPending(h, in)
	if err != nil {
		return refuse(in.Title(), err.Error())
	}
	var local *bp.Document
	if p, ok := readPending(h, path); ok && p.err == nil {
		local = p.doc
	}
	merged, _, err := merge.Pending(local, in, h.team(), h.dir.Team)
	if err != nil {
		return refuse(in.Title(), err.Error())
	}
	if local != nil && bytes.Equal(merged.Bytes(), local.Bytes()) {
		return false, nil
	}
	if err := os.MkdirAll(incomingDir(h), 0o755); err != nil {
		return refuse(in.Title(), err.Error())
	}
	if err := fsutil.WriteAtomic(path, merged.Bytes(), nil); err != nil {
		return refuse(in.Title(), err.Error())
	}
	d.bus.Publish(BusEvent{Type: "teams"})
	if me, ok := memberOf(h.st, d.self.Email); local == nil && ok && me.Owner && !bp.SameEmail(o.Actor, d.self.Email) {
		d.notice(Notice{Kind: NoticeTransfer, Title: in.Title(), Team: h.dir.Name,
			Message: fmt.Sprintf("%s offers “%s” from %s to %s: accept or decline it (`%s transfer accept %s`)", o.Actor, in.Title(), o.Transfer.FromTeam, h.dir.Name, name.Binary, id)})
	}
	return true, nil
}

// withWithdrawal is doc with the withdrawal entries of stub added.
func withWithdrawal(doc, stub *bp.Document) *bp.Document {
	out, err := doc.Reparse()
	if err != nil {
		return doc
	}
	have := map[string]bool{}
	for _, e := range entriesOf(doc) {
		have[e.ID] = true
	}
	for _, e := range entriesOf(stub) {
		if e.Kind == bp.KindTransferWithdrawn && !have[e.ID] {
			_ = out.AddEntry(e)
		}
	}
	if r, err := out.Reparse(); err == nil {
		return r
	}
	return out
}

// afterTransfers acts on the decisions in h's log (spec 20.7): an
// accepted offer whose version 1 the local user signs is installed and
// released; a declined or withdrawn one is replaced by its stub; a stub
// whose offer was accepted after all is restored. Then the moves the
// local user started are finished.
func (d *Node) afterTransfers(h *held) {
	for _, p := range d.pendingDocs(h) {
		if p.err != nil {
			continue
		}
		dec, ok := p.decision(h)
		switch {
		case ok && dec.Accepted:
			d.install(h, p.id)
		case ok, p.offer.Withdrawn:
			d.withLock(p.id, func() {
				if q, held := readPending(h, p.path); held && q.err == nil {
					if err := d.stubOffer(h, q); err != nil {
						d.logf("discarding the offer %s: %v", p.id, err)
					}
				}
			})
		}
	}
	stubs, _ := filepath.Glob(filepath.Join(tombDir(h), "*.md"))
	for _, s := range stubs {
		id := strings.TrimSuffix(filepath.Base(s), ".md")
		doc, _, ok := d.pendingStub(h, id)
		if !ok {
			continue
		}
		offer, _, _ := merge.CheckStub(doc, h.team())
		if dec, ok := h.st.Transfers[offer.Digest()]; ok && dec.Accepted && dec.Doc == id {
			d.withLock(id, func() { d.restoreOffer(h, id) })
			d.install(h, id)
		}
	}
	d.transferNotices(h)
	d.finishMoves()
}

func (d *Node) withLock(id string, f func()) {
	unlock, ok := d.Store.Lock(id, syncproto.LockWait)
	if !ok {
		return
	}
	defer unlock()
	f()
}

// install makes an accepted offer an ordinary team document, when the
// local user signs its version 1: owner: is rewritten, withdrawals are
// dropped, the file moves into the team's folder, and release 1 follows.
func (d *Node) install(h *held, id string) {
	d.installs.Lock()
	defer d.installs.Unlock()
	var path, from string
	d.withLock(id, func() {
		p, ok := readPending(h, filepath.Join(incomingDir(h), id+".md"))
		if !ok || p.err != nil {
			return
		}
		if f, _ := d.Store.FindIn(h.dir.Dir, id); f != nil {
			_ = os.Remove(p.path) // released already
			return
		}
		doc, err := p.doc.Reparse()
		if err != nil {
			return
		}
		doc.SetFrontmatter(name.FrontmatterOwner, strings.ToLower(d.self.Email))
		ch := merge.Releases(entriesOf(doc), id, h.team())
		if ch.Pending == nil || !bp.SameEmail(ch.Pending.Owner, d.self.Email) {
			return // version 1 is someone else's to sign
		}
		var items []*bp.Item
		for _, e := range entriesOf(doc) {
			if e.Kind != bp.KindTransferWithdrawn {
				items = append(items, &bp.Item{Entry: e})
			}
		}
		final, err := bp.Compose(doc, doc.EOL(), nil, items)
		if err != nil {
			d.logf("installing %s: %v", id, err)
			return
		}
		if err := os.MkdirAll(h.dir.Dir, 0o755); err != nil {
			return
		}
		path, err = d.Store.CreateIn(h.dir.Dir, final, store.SafeName(final.Title()))
		if err != nil {
			d.logf("installing %s: %v", id, err)
			return
		}
		_ = os.Remove(p.path)
		from = p.offer.Transfer.FromTeam
	})
	if path == "" {
		return
	}
	r, err := d.release(path, relOpts{mode: modeAuto, note: "accepted from " + from})
	if err != nil {
		d.logf("release 1 of %s: %v", path, err)
		return
	}
	d.logf("accepted %s from %s as version %d", r.Path, from, r.Version)
}

// dropPending removes a pending copy, or the stub of one an accept beat,
// once the released document is held.
func (d *Node) dropPending(h *held, id string) {
	_ = os.Remove(filepath.Join(incomingDir(h), id+".md"))
	if _, _, ok := d.pendingStub(h, id); ok {
		_ = os.Remove(filepath.Join(tombDir(h), id+".md"))
		_ = os.Remove(offerTrash(h, id))
	}
}

// transferNotices tells the actor of each decision on their offers, and a
// named owner other than the decider of their new document, once.
func (d *Node) transferNotices(h *held) {
	for digest, dec := range h.st.Transfers {
		if !d.once(dec.Op) {
			continue
		}
		actor, title, mode := d.offerActor(h, dec.Doc, digest)
		if bp.SameEmail(actor, d.self.Email) && !bp.SameEmail(dec.By, d.self.Email) {
			verb := "declined"
			if dec.Accepted {
				verb = "accepted"
			}
			d.notice(Notice{Kind: NoticeTransfer, Title: title, Team: h.dir.Name,
				Message: fmt.Sprintf("%s %s your %s of “%s” to %s", dec.By, verb, mode, title, h.dir.Name)})
		}
		if dec.Accepted && bp.SameEmail(dec.Owner, d.self.Email) && !bp.SameEmail(dec.By, d.self.Email) {
			d.notice(Notice{Kind: NoticeTransfer, Title: title, Team: h.dir.Name,
				Message: fmt.Sprintf("%s accepted “%s” into %s and made you its owner", dec.By, title, h.dir.Name)})
		}
	}
}

// offerActor finds who offered doc, from the pending file, its stub, or
// the released document.
func (d *Node) offerActor(h *held, id, digest string) (actor, title, mode string) {
	var docs []*bp.Document
	if p, ok := readPending(h, filepath.Join(incomingDir(h), id+".md")); ok {
		docs = append(docs, p.doc)
	}
	if s, _, ok := d.pendingStub(h, id); ok {
		docs = append(docs, s)
	}
	if f, _ := d.Store.FindIn(h.dir.Dir, id); f != nil {
		docs = append(docs, f.Doc)
	}
	for _, doc := range docs {
		for _, e := range entriesOf(doc) {
			if t, ok := e.Transfer(); ok && e.Digest() == digest {
				return e.Author, doc.Title(), t.Mode
			}
		}
	}
	return "", id, "transfer"
}

// once reports whether key is new, and marks it.
func (d *Node) once(key string) bool {
	d.notes.mu.Lock()
	defer d.notes.mu.Unlock()
	if d.notes.opsSeen == nil {
		d.notes.opsSeen = map[string]bool{}
	}
	if d.notes.opsSeen[key] {
		return false
	}
	d.notes.opsSeen[key] = true
	return true
}

// finishMoves ends the moves the local user started (spec 20.7), derived
// from files: a source document with an open move entry by the local
// user, and the destination log's decision on it. An accept deletes the
// source with a `moved:` tombstone, if the local user still releases it;
// otherwise the move ends as a copy. A decline ends it with "move
// declined".
func (d *Node) finishMoves() {
	for _, t := range d.teamDirs() {
		src, err := d.heldTeam(t.Team)
		if err != nil || !src.isMember(d.self.Email, d.pub) {
			continue
		}
		for _, in := range d.Store.List() {
			if in.KalkaID == "" || !inDir(src.dir.Dir, in.Path) {
				continue
			}
			f, err := d.Store.Load(in.Path)
			if err != nil {
				continue
			}
			mv := openMove(src, f.Doc)
			if mv == nil || !bp.SameEmail(mv.Author, d.self.Email) {
				continue
			}
			tr, _ := mv.Transfer()
			dst, err := d.heldTeam(tr.To)
			if err != nil {
				continue
			}
			dec, ok := dst.st.Transfers[mv.Digest()]
			if !ok {
				continue
			}
			d.endMove(src, dst, f.Path, mv, dec)
		}
	}
}

func (d *Node) endMove(src, dst *held, path string, mv *bp.Entry, dec teamlog.Decision) {
	d.withLock(mvSourceID(mv), func() {
		f, err := d.Store.Load(path)
		if err != nil || openMove(src, f.Doc) == nil {
			return
		}
		id, title := f.Doc.KalkaID(), f.Doc.Title()
		if !dec.Accepted {
			d.endMoveWith(src, f, mv, "move declined")
			d.notice(Notice{Kind: NoticeTransfer, Title: title, Path: f.Path, Team: src.dir.Name,
				Message: fmt.Sprintf("the move of “%s” to %s was declined; it stays in %s", title, dst.dir.Name, src.dir.Name)})
			return
		}
		ch := merge.Releases(entriesOf(f.Doc), id, src.team())
		releaser, _ := nextReleaser(ch, f.Doc.Owner())
		if !bp.SameEmail(releaser, d.self.Email) || ch.Disputed {
			d.endMoveWith(src, f, mv, "move ended as a copy")
			d.notice(Notice{Kind: NoticeTransfer, Title: title, Path: f.Path, Team: src.dir.Name,
				Message: fmt.Sprintf("“%s” was accepted into %s, but you no longer own it in %s: the move ended as a copy, and its owner there may delete it", title, dst.dir.Name, src.dir.Name)})
			return
		}
		if err := d.deleteLocked(src, f, ch, dst.dir.Name+"/"+dec.Doc); err != nil {
			d.logf("tombstone of the move of %s: %v", f.Path, err)
			return
		}
		d.notice(Notice{Kind: NoticeTransfer, Title: title, Team: src.dir.Name,
			Message: fmt.Sprintf("“%s” moved to %s", title, dst.dir.Name)})
		d.pushTeam(src.dir.Team)
	})
}

func mvSourceID(mv *bp.Entry) string {
	t, _ := mv.Transfer()
	return t.FromID
}

// endMoveWith appends the transfer-withdrawn entry that ends a move in the
// source document.
func (d *Node) endMoveWith(src *held, f *store.File, mv *bp.Entry, text string) {
	doc, err := bp.Parse(f.Raw)
	if err != nil {
		return
	}
	e, err := bp.NewWithdrawal(doc, strings.ToLower(d.self.Email), d.now(), mv.ID, text, d.signer())
	if err == nil {
		err = doc.AddEntry(e)
	}
	if err == nil {
		err = d.Store.Update(f, doc)
	}
	if err != nil {
		d.logf("ending the move of %s: %v", f.Path, err)
		return
	}
	d.pushTeam(src.dir.Team)
}

// findPending finds a pending transfer by id in the teams held.
func (d *Node) findPending(id string) (*held, pendingDoc, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, t := range d.teamDirs() {
		h, err := d.heldTeam(t.Team)
		if err != nil {
			continue
		}
		if p, ok := readPending(h, filepath.Join(incomingDir(h), id+".md")); ok {
			if p.err != nil {
				return nil, p, &Error{Kind: KindUnknownTransfer, Msg: fmt.Sprintf("the offer %s does not count: %v", id, p.err)}
			}
			return h, p, nil
		}
		if _, _, ok := d.pendingStub(h, id); ok {
			return h, pendingDoc{}, &Error{Kind: KindTransferDecided, Msg: fmt.Sprintf("the transfer %s was already declined or withdrawn", id)}
		}
		if f, _ := d.Store.FindIn(h.dir.Dir, id); f != nil {
			if _, ok := h.team().Accepted(id); ok {
				return h, pendingDoc{}, &Error{Kind: KindTransferDecided, Msg: fmt.Sprintf("the transfer %s was already accepted", id)}
			}
		}
	}
	return nil, pendingDoc{}, &Error{Kind: KindUnknownTransfer, Msg: fmt.Sprintf("no pending transfer %s here", id)}
}

// AcceptTransfer accepts a pending transfer into its team (spec 20.7):
// accept-transfer, signed by a team owner, naming owner (the local user
// when empty) as the owner of version 1. When that is the local user, the
// document is installed and released at once.
func (d *Node) AcceptTransfer(id, owner string) (TransferView, error) {
	h, p, err := d.findPending(id)
	if err != nil {
		return TransferView{}, err
	}
	owner = strings.ToLower(strings.TrimSpace(owner))
	if owner == "" {
		owner = strings.ToLower(d.self.Email)
	}
	d.teams.mu.Lock()
	st, err := d.ownerOf(h.dir)
	if err != nil {
		d.teams.mu.Unlock()
		return TransferView{}, err
	}
	h = &held{dir: h.dir, st: st}
	if p.decided(h) {
		d.teams.mu.Unlock()
		return TransferView{}, &Error{Kind: KindTransferDecided, Msg: fmt.Sprintf("the transfer %s was already decided or withdrawn", p.id)}
	}
	if _, ok := memberOf(st, owner); !ok {
		d.teams.mu.Unlock()
		return TransferView{}, &Error{Kind: KindNotMember, Msg: fmt.Sprintf("%s is not a member of %s", owner, h.dir.Name)}
	}
	if p.doc.IsDomain() {
		if other := d.domainHolder(h, p.doc.DomainName(), p.id); other != "" {
			d.teams.mu.Unlock()
			return TransferView{}, &Error{Kind: KindDomainExists, Msg: fmt.Sprintf("%s already holds domain %s; decline the transfer, or delete that document first", other, p.doc.DomainName())}
		}
	}
	op := d.teamSigner(h.dir).Accept(st.Heads, p.id, owner, p.digest(), d.now())
	st, err = d.commit(h.dir, []teamlog.Op{op})
	d.teams.mu.Unlock()
	if err != nil {
		return TransferView{}, err
	}
	h, _ = d.heldTeam(h.dir.Team)
	d.afterTransfers(h)
	d.logf("accepted the transfer %s into %s, owner %s", p.id, h.dir.Name, owner)
	v := d.transferView(h, p)
	v.Status, v.Owner = TransferAccepted, owner
	if f, _ := d.Store.FindIn(h.dir.Dir, p.id); f != nil {
		v.Path = d.rel(f.Path)
	}
	return v, nil
}

// DeclineTransfer declines a pending transfer (spec 20.7): every peer
// discards the offer; a move's source stays.
func (d *Node) DeclineTransfer(id string) error {
	h, p, err := d.findPending(id)
	if err != nil {
		return err
	}
	d.teams.mu.Lock()
	st, err := d.ownerOf(h.dir)
	if err != nil {
		d.teams.mu.Unlock()
		return err
	}
	h = &held{dir: h.dir, st: st}
	if p.decided(h) {
		d.teams.mu.Unlock()
		return &Error{Kind: KindTransferDecided, Msg: fmt.Sprintf("the transfer %s was already decided or withdrawn", p.id)}
	}
	op := d.teamSigner(h.dir).Decline(st.Heads, p.id, p.digest(), d.now())
	_, err = d.commit(h.dir, []teamlog.Op{op})
	d.teams.mu.Unlock()
	if err != nil {
		return err
	}
	h, _ = d.heldTeam(h.dir.Team)
	d.afterTransfers(h)
	d.logf("declined the transfer %s into %s", p.id, h.dir.Name)
	return nil
}

// WithdrawTransfer withdraws the local user's own pending transfer (spec
// 20.7): a signed transfer-withdrawn on the pending document, which then
// has the effect of a decline unless an accept counts. A move's source
// records it.
func (d *Node) WithdrawTransfer(id string) error {
	h, p, err := d.findPending(id)
	if err != nil {
		return err
	}
	if !bp.SameEmail(p.offer.Actor, d.self.Email) {
		return &Error{Kind: KindNotOwner, Msg: fmt.Sprintf("only %s, who offered it, can withdraw the transfer %s", p.offer.Actor, p.id)}
	}
	if p.decided(h) {
		return &Error{Kind: KindTransferDecided, Msg: fmt.Sprintf("the transfer %s was already decided", p.id)}
	}
	var werr error
	d.withLock(p.id, func() {
		q, ok := readPending(h, p.path)
		if !ok || q.err != nil {
			werr = errors.New("the pending document changed; try again")
			return
		}
		doc, err := q.doc.Reparse()
		if err != nil {
			werr = err
			return
		}
		e, err := bp.NewWithdrawal(doc, strings.ToLower(d.self.Email), d.now(), q.offer.Offer.ID, "withdrawn", d.signer())
		if err == nil {
			err = doc.AddEntry(e)
		}
		if err == nil {
			doc, err = doc.Reparse()
		}
		if err != nil {
			werr = err
			return
		}
		q.doc, q.raw = doc, doc.Bytes()
		if q.offer, err = checkPending(h, doc); err != nil {
			werr = err
			return
		}
		werr = d.stubOffer(h, q)
	})
	if werr != nil {
		return werr
	}
	if p.offer.Transfer.Mode == bp.ModeMove {
		if src, err := d.heldTeam(p.offer.Transfer.Source); err == nil {
			if f, _ := d.Store.FindIn(src.dir.Dir, p.offer.Transfer.FromID); f != nil {
				if mv := openMove(src, f.Doc); mv != nil && mv.ID == p.offer.Offer.ID {
					d.withLock(f.Doc.KalkaID(), func() {
						if f, err := d.Store.Load(f.Path); err == nil {
							d.endMoveWith(src, f, mv, "move withdrawn")
						}
					})
				}
			}
		}
	}
	d.pushTeam(h.dir.Team)
	d.logf("withdrew the transfer %s", p.id)
	return nil
}

// TransferView is one pending transfer in `kalka transfers` and the Team
// view (spec 20.7): shown to the team's owners, to decide, and to the
// actor, to watch or withdraw.
type TransferView struct {
	ID     string `json:"id"`
	Team   string `json:"team"`
	Mode   string `json:"mode"`
	From   string `json:"from"`
	Title  string `json:"title"`
	Actor  string `json:"actor"`
	At     string `json:"at"`
	Status string `json:"status"`
	// Mine: the local user offered it; CanDecide: the local user is a
	// team owner of the destination.
	Mine      bool   `json:"mine,omitempty"`
	CanDecide bool   `json:"can_decide,omitempty"`
	Owner     string `json:"owner,omitempty"` // accepted: the owner of version 1
	Path      string `json:"path,omitempty"`  // accepted and held: the document
	// Entries counts the attested entries the offer brings.
	Entries int `json:"entries"`

	teamHash string
}

func (d *Node) transferView(h *held, p pendingDoc) TransferView {
	v := TransferView{ID: p.id, Team: h.dir.Name, Title: p.doc.Title(), Status: TransferPending, teamHash: h.dir.Team}
	if p.offer != nil {
		v.Mode, v.From, v.Actor, v.At = p.offer.Transfer.Mode, p.offer.Transfer.From(), p.offer.Actor, p.offer.Offer.Timestamp
		v.Entries = len(p.offer.Entries)
		v.Mine = bp.SameEmail(p.offer.Actor, d.self.Email)
		if dec, ok := p.decision(h); ok && dec.Accepted {
			v.Status, v.Owner = TransferAccepted, dec.Owner
		} else if p.offer.Withdrawn {
			v.Status = TransferWithdrawn
		}
	}
	if me, ok := memberOf(h.st, d.self.Email); ok && me.Owner {
		v.CanDecide = v.Status == TransferPending
	}
	return v
}

// Transfers lists the pending transfers the local user may see (spec
// 20.7): every one of a team they own, and their own elsewhere.
func (d *Node) Transfers() []TransferView {
	out := []TransferView{}
	for _, t := range d.teamDirs() {
		h, err := d.heldTeam(t.Team)
		if err != nil || !h.isMember(d.self.Email, d.pub) {
			continue
		}
		me, _ := memberOf(h.st, d.self.Email)
		for _, p := range d.pendingDocs(h) {
			if p.err != nil {
				continue
			}
			v := d.transferView(h, p)
			if me.Owner || v.Mine {
				out = append(out, v)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At != out[j].At {
			return out[i].At < out[j].At
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// TransferInfo is what the document view shows of a transfer (spec 20.7):
// where an attested history came from and who vouches for it, or that the
// document is being moved away.
type TransferInfo struct {
	// From is the source (<team>/<kalka_id>) of a copy or a move held
	// here; Actor and By the two who attest its history.
	Mode  string `json:"mode,omitempty"`
	From  string `json:"from,omitempty"`
	Actor string `json:"actor,omitempty"`
	By    string `json:"by,omitempty"`
	// MovingTo names the destination of an open move of this document.
	MovingTo string `json:"moving_to,omitempty"`
}

func (d *Node) transferInfo(h *held, doc *bp.Document) *TransferInfo {
	if h == nil {
		return nil
	}
	var ti TransferInfo
	if a := merge.Attested(h.team(), doc.KalkaID(), entriesOf(doc)); a != nil {
		ti.Mode, ti.From, ti.Actor, ti.By = a.Transfer.Mode, a.Transfer.From(), a.Actor, a.By
	}
	if mv := openMove(h, doc); mv != nil {
		t, _ := mv.Transfer()
		ti.MovingTo = t.ToTeam
	}
	if ti == (TransferInfo{}) {
		return nil
	}
	return &ti
}
