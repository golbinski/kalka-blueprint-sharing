package core

// Approval (spec 6.18): the owner's release of the approved version, the
// one end of a plan's review. kalka records it as a signed approved entry
// and computes the state from the entries that count; it never tracks
// implementation progress.

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golbinski/kalka/internal/addr"
	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/name"
)

// ApproveResult reports an approval. Changed is false when nothing was
// appended: the latest release was approved already and there were no
// edits to release.
type ApproveResult struct {
	Path    string `json:"path"`
	Team    string `json:"team,omitempty"`
	Version int64  `json:"version"`
	// Released: unreleased edits were released first as Version.
	Released bool   `json:"released"`
	Changed  bool   `json:"changed"`
	Entry    string `json:"entry,omitempty"`
	State    string `json:"state"`
}

// approvalAt is the approval state of the team document doc at path, over
// the entries that count in its team: the releases of its chain, and the
// other entries signed under a term of the team, not brought by a
// transfer (spec 20.4, 20.7). It is the same computation kalka check runs
// over the file (bp.ApprovalOf); only the void entries are dropped first.
// Empty for a document with no approval state.
func (d *Node) approvalAt(path string, doc *bp.Document) bp.Approval {
	if !doc.HasApproval() {
		return bp.Approval{}
	}
	h, err := d.heldAt(path)
	if err != nil || h.dir.Name != doc.Team() {
		return bp.Approval{}
	}
	id, team := doc.KalkaID(), h.team()
	all := entriesOf(doc)
	ch := merge.Releases(all, id, team)
	att := merge.Attested(team, id, all)
	var releases, kept []*bp.Entry
	for _, l := range ch.Releases {
		releases = append(releases, l.Entry)
	}
	void := map[string]string{}
	for _, e := range all {
		switch {
		case e.Kind == bp.KindRelease:
		case att.Has(e):
			if e.Kind == bp.KindApproved {
				void[e.ID] = "an approval in the team it came from: history only"
			}
		case !merge.VerifiedBy(team, id, e):
			if e.Kind == bp.KindApproved {
				void[e.ID] = "no valid signature by a key the team bound to " + e.Author
			}
		default:
			kept = append(kept, e)
		}
	}
	a := bp.ApprovalOf(releases, kept)
	if len(void) > 0 {
		if a.Ignored == nil {
			a.Ignored = map[string]string{}
		}
		for k, v := range void {
			a.Ignored[k] = v
		}
	}
	return a
}

// approvalOf is approvalAt for a document of the plan graph found by its
// kalka_id, for the graph and the agent context.
func (d *Node) approvalOf(doc *bp.Document) bp.Approval {
	if !doc.HasApproval() {
		return bp.Approval{}
	}
	for _, in := range d.Store.List() {
		if in.KalkaID != "" && strings.EqualFold(in.KalkaID, doc.KalkaID()) && !in.Duplicate && in.Domain == "" && in.Team == doc.Team() {
			return d.approvalAt(in.Path, doc)
		}
	}
	return bp.Approval{}
}

// Approve is the owner's release of the approved version (spec 6.18):
// with unreleased edits it releases them first, with note and
// incorporates, then appends a signed approved entry citing the latest
// release. Without edits it approves the latest release, unless the
// state is already approved, when nothing is appended. Only the owner
// approves; a team owner takes the document over first (TakeOverApprove).
func (d *Node) Approve(path, note string, incorporates []string) (ApproveResult, error) {
	var res ApproveResult
	f, err := d.Store.Load(d.resolve(path))
	if err != nil {
		return res, err
	}
	doc := f.Doc
	res.Path = f.Path
	if _, ok := d.teamDirAt(f.Path); !ok {
		return res, &Error{Kind: KindInPersonal, Msg: fmt.Sprintf("%s lies in Personal: publish it to a team first (`%s publish`); only team documents are approved", d.rel(f.Path), name.Binary)}
	}
	switch {
	case doc.IsSeed():
		return res, &Error{Kind: KindNoApproval, Msg: "a seed is not approved: turn it into a blueprint first (`" + name.Binary + " promote`)"}
	case doc.IsDomain():
		return res, &Error{Kind: KindNoApproval, Msg: "a domain document is not approved: its released versions are what blueprints pin"}
	case doc.Deleted():
		return res, &Error{Kind: KindNoApproval, Msg: "a deleted document is not approved"}
	}
	ts := d.teamDocState(f.Path, doc)
	if ts == nil || !ts.Held || doc.KalkaID() == "" {
		return res, &Error{Kind: KindNotShared, Msg: fmt.Sprintf("%s is not a document of this team: publish it first", d.rel(f.Path))}
	}
	res.Team = ts.Team
	if ts.Disputed {
		return res, &Error{Kind: KindDisputed, Msg: "ownership of this document is disputed; a team owner settles it by reassigning the document"}
	}
	if !bp.SameEmail(ts.Releaser, d.self.Email) {
		return res, &Error{Kind: KindNotOwner, Msg: fmt.Sprintf("only %s, the owner, can approve this document; a team owner can reassign it first", ts.Releaser)}
	}
	if ts.Pending != "" {
		return res, errors.New("this document was reassigned to you and your release has not been made yet: it follows when the earlier releases arrive; approve then")
	}
	approvalNote := note
	if ts.Unrel {
		r, err := d.Release(f.Path, note, incorporates)
		if err != nil {
			return res, err
		}
		res.Path, res.Released, approvalNote = r.Path, true, ""
	} else if len(incorporates) > 0 {
		return res, errors.New("there are no unreleased edits: --incorporates names the deviations a release takes up")
	}
	return d.appendApproval(res, approvalNote)
}

// appendApproval appends the approved entry citing the latest release of
// the document at res.Path, unless the state is already approved.
func (d *Node) appendApproval(res ApproveResult, note string) (ApproveResult, error) {
	for attempt := 0; ; attempt++ {
		f, err := d.Store.Load(res.Path)
		if err != nil {
			return res, err
		}
		h, err := d.heldAt(f.Path)
		if err != nil {
			return res, err
		}
		doc, err := bp.Parse(f.Raw)
		if err != nil {
			return res, err
		}
		id := doc.KalkaID()
		head, ok := merge.Releases(entriesOf(doc), id, h.team()).Head()
		switch {
		case !ok:
			return res, errors.New("this document has no released version to approve")
		case head.Release.Plan != doc.PlanHash():
			return res, errors.New("the plan changed while it was approved; approve again")
		case !bp.SameEmail(head.Release.Owner, d.self.Email):
			return res, &Error{Kind: KindNotOwner, Msg: fmt.Sprintf("version %d names %s as its owner, who approves it", head.Release.Version, head.Release.Owner)}
		}
		res.Version = head.Release.Version
		if cur := d.approvalAt(f.Path, doc); cur.State == bp.ApprovalApproved && !res.Released {
			res.State = cur.Text()
			res.Version = cur.Version
			return res, nil
		}
		unlock, locked := d.Store.Lock(id, 10*time.Second)
		if !locked {
			return res, errors.New("this document is busy in a sync; try again")
		}
		e, err := bp.NewApproval(doc, addr.Canonical(d.self.Email), d.now(), head.Release.Version, head.Release.Plan, note, d.signerIn(h))
		if err == nil {
			err = doc.AddEntry(e)
		}
		if err == nil {
			err = d.Store.Update(f, doc)
		}
		unlock()
		if errors.Is(err, fsutil.ErrChanged) && attempt < 3 {
			continue
		}
		if err != nil {
			if res.Released {
				return res, fmt.Errorf("version %d was released, but the approval could not be written (%v); approve again to approve it", res.Version, err)
			}
			return res, err
		}
		res.Changed, res.Entry = true, e.ID
		res.State = d.approvalAt(f.Path, doc).Text()
		d.logf("approved %s v%d", f.Path, head.Release.Version)
		d.pushTeam(h.dir.Team)
		return res, nil
	}
}

// TakeOverApprove is Approve by a team owner on someone else's document:
// a reassignment to the actor, whose kalka releases at once, then the
// approval (spec 7.4).
func (d *Node) TakeOverApprove(path, note string, incorporates []string) (ApproveResult, error) {
	need, err := d.needsTakeOver(path)
	if err != nil {
		return ApproveResult{}, err
	}
	if !need {
		return d.Approve(path, note, incorporates)
	}
	r, err := d.reassign(path, d.self.Email, note)
	if err != nil {
		return ApproveResult{}, err
	}
	if r.Version == 0 {
		return ApproveResult{}, fmt.Errorf("reassigned %s to you; the release follows when the earlier releases arrive, then approve it", d.rel(r.Path))
	}
	res, err := d.Approve(r.Path, "", nil)
	res.Released = true
	return res, err
}
