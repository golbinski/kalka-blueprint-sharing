package core

// Ownership of team documents (spec 7): handoff is a release naming the
// recipient, reassignment is a team owner's log operation, the new owner's
// kalka releases at once, and nothing is silently overwritten.

import (
	"context"
	"fmt"
	"github.com/golbinski/kalka/internal/addr"
	"os"
	"path/filepath"
	"strings"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/teamlog"
)

// Notice kinds of ownership.
const (
	NoticeHandoff    = "handoff"    // a document was handed off to you
	NoticeReassigned = "reassigned" // a document of yours, or to you, was reassigned
	NoticeDisplaced  = "displaced"  // replication replaced unreleased edits; a copy is kept
)

// Handoff makes another member the owner (spec 7.2): owner: is set to the
// recipient and the next version is released naming them. It needs no
// network and no consent.
func (d *Node) Handoff(path, email string) (PublishResult, error) {
	email = addr.Canonical(strings.TrimSpace(email))
	f, err := d.Store.Load(d.resolve(path))
	if err != nil {
		return PublishResult{}, err
	}
	if bp.SameEmail(email, d.self.Email) {
		return PublishResult{}, fmt.Errorf("you already own %s", d.rel(f.Path))
	}
	if h, err := d.heldAt(f.Path); err == nil {
		if _, ok := memberOf(h.st, email); !ok {
			return PublishResult{}, &Error{Kind: KindNotMember, Msg: fmt.Sprintf("%s is not a member of %s", email, h.dir.Name)}
		}
	}
	r, err := d.release(f.Path, relOpts{mode: modeHandoff, owner: email, note: "handed off to " + email})
	if err == nil {
		d.logf("handed %s off to %s", r.Path, email)
	}
	return r, err
}

// ReassignResult reports a reassignment.
type ReassignResult struct {
	Path  string `json:"path"`
	Team  string `json:"team"`
	From  string `json:"from"`
	Owner string `json:"owner"`
	After int64  `json:"after"`
	// Version is the release the new owner made at once, when that is
	// the local user (0 otherwise, or while the releases up to the cut-off
	// have not arrived).
	Version int64 `json:"version,omitempty"`
	// Warning is set when no other member could be reached first: the
	// reassignment was signed from a view that may be out of date.
	Warning string `json:"warning,omitempty"`
}

// Reassign makes email the owner of a team document from the next version
// on, without the current owner's consent (spec 7.3): a signed
// reassign-document naming the highest valid release held here.
func (d *Node) Reassign(path, email string) (ReassignResult, error) {
	warn := ""
	if h, err := d.heldAt(d.resolve(path)); err == nil {
		warn = d.catchUp(h.dir.Team)
	}
	r, err := d.reassign(path, email, "")
	r.Warning = warn
	return r, err
}

// CatchUpTimeout bounds the round run before signing an operation that
// voids what the signer has not seen; 0 skips the round.
var CatchUpTimeout = 20 * time.Second

// catchUp runs a replication round of team before the local user signs a
// reassignment, a removal or an owner's demotion (spec 19.5): those void
// releases and operations the signer has not seen, so the view should be
// current. It returns a warning when other members exist and none was
// reached.
func (d *Node) catchUp(team string) string {
	h, err := d.heldTeam(team)
	if err != nil || len(h.st.Members) < 2 || CatchUpTimeout <= 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(d.work, CatchUpTimeout)
	defer cancel()
	st, err := d.ReplicateNow(ctx, team)
	if err == nil && len(st.Reached) > 0 {
		return ""
	}
	return "no other member could be reached first: this was signed from what this computer holds, which may be out of date"
}

// reassign is Reassign; note is the note of the release the local user
// makes at once when they are the new owner.
func (d *Node) reassign(path, email, note string) (ReassignResult, error) {
	email = addr.Canonical(strings.TrimSpace(email))
	f, err := d.Store.Load(d.resolve(path))
	if err != nil {
		return ReassignResult{}, err
	}
	t, ok := d.teamDirAt(f.Path)
	if !ok {
		return ReassignResult{}, &Error{Kind: KindInPersonal, Msg: fmt.Sprintf("%s lies in Personal; only team documents are reassigned", d.rel(f.Path))}
	}
	id := f.Doc.KalkaID()
	d.teams.mu.Lock()
	st, err := d.ownerOf(t)
	if err != nil {
		d.teams.mu.Unlock()
		return ReassignResult{}, err
	}
	h := &held{dir: t, st: st}
	if id == "" || f.Doc.Team() != t.Name || f.Doc.HasErrors() {
		d.teams.mu.Unlock()
		return ReassignResult{}, &Error{Kind: KindNotShared, Msg: fmt.Sprintf("%s is not a document of %s", d.rel(f.Path), t.Name)}
	}
	if _, ok := memberOf(st, email); !ok {
		d.teams.mu.Unlock()
		return ReassignResult{}, &Error{Kind: KindNotMember, Msg: fmt.Sprintf("%s is not a member of %s", email, t.Name)}
	}
	ch := merge.Releases(entriesOf(f.Doc), id, h.team())
	head, released := ch.Head()
	if !released && !ch.Disputed {
		d.teams.mu.Unlock()
		return ReassignResult{}, &Error{Kind: KindNotShared, Msg: fmt.Sprintf("%s has no release yet: its owner publishes it first", d.rel(f.Path))}
	}
	from, _ := nextReleaser(ch, f.Doc.Owner())
	if from == "" {
		from = addr.Canonical(f.Doc.Owner())
	}
	if bp.SameEmail(from, email) && !ch.Disputed {
		d.teams.mu.Unlock()
		return ReassignResult{}, fmt.Errorf("%s already owns %s", email, d.rel(f.Path))
	}
	var after int64
	if released {
		after = head.Release.Version
	}
	op := d.teamSigner(t).Reassign(st.Heads, id, addr.Canonical(from), email, after, d.now())
	st, err = d.commit(t, []teamlog.Op{op})
	d.teams.mu.Unlock()
	if err != nil {
		return ReassignResult{}, err
	}
	res := ReassignResult{Path: f.Path, Team: t.Name, From: addr.Canonical(from), Owner: email, After: after}
	d.logf("reassigned %s from %s to %s after version %d", f.Path, from, email, after)
	if bp.SameEmail(email, d.self.Email) {
		h := &held{dir: t, st: st}
		if r, ok := d.takeReassigned(h, id, note); ok {
			res.Version = r.Version
		} else if f, err := d.Store.FindIn(t.Dir, id); err == nil && f != nil {
			// The background reaction to the log change may have made it.
			if head, ok := merge.Releases(entriesOf(f.Doc), id, h.team()).Head(); ok && head.Release.Version == after+1 && bp.SameEmail(head.Entry.Author, d.self.Email) {
				res.Version = head.Release.Version
			}
		}
	}
	return res, nil
}

// takeReassigned releases a document whose last anchor names the local
// user, once the releases up to its cut-off are held (spec 7.3). It
// releases the plan held, whatever it is: the new owner may publish any
// text.
func (d *Node) takeReassigned(h *held, id, note string) (PublishResult, bool) {
	f, err := d.Store.FindIn(h.dir.Dir, id)
	if err != nil || f == nil || f.Doc.Team() != h.dir.Name || f.Doc.HasErrors() {
		return PublishResult{}, false
	}
	ch := merge.Releases(entriesOf(f.Doc), id, h.team())
	p := ch.Pending
	if p == nil || !bp.SameEmail(p.Owner, d.self.Email) {
		return PublishResult{}, false
	}
	if head, ok := ch.Head(); p.After > 0 && (!ok || head.Release.Version < p.After) {
		return PublishResult{}, false // the releases up to the cut-off have not arrived yet
	}
	if note == "" {
		note = "reassigned to " + addr.Canonical(d.self.Email)
	}
	r, err := d.release(f.Path, relOpts{mode: modeAuto, note: note})
	if err != nil {
		if err != errTaken {
			d.logf("taking over %s: %v", f.Path, err)
		}
		return PublishResult{}, false
	}
	return r, true
}

// afterLogChange reacts to new operations in a team log (spec 7.3, 19.5):
// a document reassigned to the local user is released in their name, and
// the previous and new owners hear of it.
func (d *Node) afterLogChange(h *held) {
	for id, as := range h.st.Reassigned {
		if last := as[len(as)-1]; bp.SameEmail(last.Owner, d.self.Email) {
			d.takeReassigned(h, id, "")
		}
	}
	d.reassignNotices(h)
	d.afterTransfers(h)
}

// sawOps marks operations as already noticed.
func (d *Node) sawOps(ops []teamlog.Op) {
	d.notes.mu.Lock()
	defer d.notes.mu.Unlock()
	if d.notes.opsSeen == nil {
		d.notes.opsSeen = map[string]bool{}
	}
	for _, o := range ops {
		d.notes.opsSeen[o.Hash()] = true
	}
}

// seedNotices marks every reassignment held at start as seen, so only
// new ones make a notice.
func (d *Node) seedNotices() {
	for _, t := range d.teamDirs() {
		if _, ops, _, _, err := d.load(t); err == nil {
			d.sawOps(ops)
		}
	}
}

// reassignNotices raises one notice per new effective reassignment that
// names the local user as previous or new owner.
func (d *Node) reassignNotices(h *held) {
	for id, as := range h.st.Reassigned {
		for _, a := range as {
			if a.Transfer != "" {
				continue // an accept: transferNotices
			}
			d.notes.mu.Lock()
			if d.notes.opsSeen == nil {
				d.notes.opsSeen = map[string]bool{}
			}
			seen := d.notes.opsSeen[a.Op]
			d.notes.opsSeen[a.Op] = true
			d.notes.mu.Unlock()
			if seen {
				continue
			}
			toMe, fromMe := bp.SameEmail(a.Owner, d.self.Email), bp.SameEmail(a.From, d.self.Email)
			if !toMe && !fromMe {
				continue
			}
			title, path := id, ""
			if f, err := d.Store.FindIn(h.dir.Dir, id); err == nil && f != nil {
				title, path = f.Doc.Title(), f.Path
			}
			msg := fmt.Sprintf("“%s” was reassigned to you in %s", title, h.dir.Name)
			if !toMe {
				msg = fmt.Sprintf("“%s” was reassigned to %s in %s; your releases after version %d do not count", title, a.Owner, h.dir.Name, a.After)
			}
			d.notice(Notice{Kind: NoticeReassigned, Title: title, Path: path, Team: h.dir.Name, Message: msg})
		}
	}
}

// handoffNotice: a merge made the local user the owner by a release
// someone else signed (spec 7.2).
func (d *Node) handoffNotice(h *held, before, after *bp.Document, path string) {
	if after == nil || (before != nil && bp.SameEmail(before.Owner(), d.self.Email)) {
		return
	}
	ch := merge.Releases(entriesOf(after), after.KalkaID(), h.team())
	head, ok := ch.Head()
	if !ok || !bp.SameEmail(head.Release.Owner, d.self.Email) || bp.SameEmail(head.Entry.Author, d.self.Email) {
		return
	}
	d.notice(Notice{Kind: NoticeHandoff, Title: after.Title(), Path: path, Team: h.dir.Name,
		Message: fmt.Sprintf("%s handed “%s” off to you", head.Entry.Author, after.Title())})
}

// keepDisplaced writes the local file to the team's trash before a merge
// replaces a plan no release names (spec 7.5), and says so. It returns the
// copy's path, or "".
func (d *Node) keepDisplaced(h *held, live *bp.Document, merged *bp.Document, path string) string {
	lh := live.PlanHash()
	if merged == nil || merged.PlanHash() == lh {
		return ""
	}
	for _, e := range entriesOf(merged) {
		if r, ok := e.Release(); ok && r.Plan == lh {
			return "" // a released plan: nothing is lost
		}
	}
	if err := os.MkdirAll(trashDir(h), 0o755); err != nil {
		d.logf("displaced copy of %s: %v", path, err)
		return ""
	}
	base := strings.TrimSuffix(filepath.Base(path), ".md")
	dst := filepath.Join(trashDir(h), fmt.Sprintf("%s.displaced-%s.md", base, d.now().UTC().Format("20060102T150405Z")))
	if err := fsutil.WriteAtomic(dst, live.Bytes(), nil); err != nil {
		d.logf("displaced copy of %s: %v", path, err)
		return ""
	}
	d.notice(Notice{Kind: NoticeDisplaced, Title: live.Title(), Path: path, Team: h.dir.Name,
		Message: fmt.Sprintf("your unreleased edits of “%s” were replaced by a release; a copy is in %s", live.Title(), d.rel(dst))})
	return dst
}

// domainHolder names another published document of the team that claims
// domain, or "".
func (d *Node) domainHolder(h *held, domain, id string) string {
	for _, in := range d.Store.FindDomain(h.dir.Name, domain) {
		if !inDir(h.dir.Dir, in.Path) || strings.EqualFold(in.KalkaID, id) {
			continue
		}
		f, err := d.Store.Load(in.Path)
		if err == nil && f.Doc.IsDomain() && isTeamDoc(h, f.Path, f.Doc, in.Duplicate) == "" {
			return d.rel(f.Path)
		}
	}
	return ""
}

// TakeOverRelease is the UI's Release on someone else's document by a team
// owner (spec 7.4): a reassignment to the local user, whose release of the
// plan held follows at once. With no take-over needed it is Release.
func (d *Node) TakeOverRelease(path, note string, incorporates []string) (PublishResult, error) {
	need, err := d.needsTakeOver(path)
	if err != nil || !need {
		if err != nil {
			return PublishResult{}, err
		}
		return d.Release(path, note, incorporates)
	}
	r, err := d.reassign(path, d.self.Email, note)
	if err != nil {
		return PublishResult{}, err
	}
	if r.Version == 0 {
		return PublishResult{}, fmt.Errorf("reassigned %s to you; the release follows when the earlier releases arrive", d.rel(r.Path))
	}
	return PublishResult{Path: r.Path, Team: r.Team, Version: r.Version}, nil
}

// TakeOverDelete is the UI's Delete for the team on someone else's
// document by a team owner (spec 7.4): reassign to the local user, then
// delete.
func (d *Node) TakeOverDelete(path string) error {
	need, err := d.needsTakeOver(path)
	if err != nil {
		return err
	}
	if need {
		if _, err := d.Reassign(path, d.self.Email); err != nil {
			return err
		}
	}
	return d.Delete(path)
}

// needsTakeOver reports whether someone other than the local user may
// release path next.
func (d *Node) needsTakeOver(path string) (bool, error) {
	f, err := d.Store.Load(d.resolve(path))
	if err != nil {
		return false, err
	}
	h, err := d.heldAt(f.Path)
	if err != nil {
		return false, &Error{Kind: KindInPersonal, Msg: err.Error()}
	}
	ch := merge.Releases(entriesOf(f.Doc), f.Doc.KalkaID(), h.team())
	owner, _ := nextReleaser(ch, f.Doc.Owner())
	return !bp.SameEmail(owner, d.self.Email), nil
}
