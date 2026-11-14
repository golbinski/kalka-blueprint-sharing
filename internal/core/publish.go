package core

// Publishing, releasing and deleting team documents (spec 20.1, 20.4,
// 20.6), and what the daemon does when a team log changes (19.5).

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/store"
	"github.com/golbinski/kalka/internal/syncproto"
)

// nextReleaser is who may release the next version of a published
// document, and that version: the owner the head names, or the new owner
// of an effective reassignment whose cut-off is the head (spec 19.5).
func nextReleaser(ch merge.ReleaseChain, t merge.Team, id, firstOwner string) (string, int64) {
	head, ok := ch.Head()
	var v int64
	owner := firstOwner
	if ok {
		v, owner = head.Release.Version, head.Release.Owner
	}
	if o, after, ok := t.Override(id); ok && after == v {
		owner = o
	}
	if r, ok := t.(merge.DomainRule); ok {
		if o, after, isDomain := r.DomainOwner(); isDomain && v+1 > after {
			owner = o // "" when no domain owner is assigned
		}
	}
	return owner, v + 1
}

// PublishResult reports a publish or a publish update.
type PublishResult struct {
	Path    string `json:"path"`
	Team    string `json:"team"`
	Version int64  `json:"version"`
	// LocalOnly counts entries that stay in this file: unsigned, or signed
	// by someone the team log does not know (spec 20.4).
	LocalOnly int `json:"local_only"`
}

// Publish publishes a document to a team, or releases the next version of
// one already published (spec 20.1): `team:` and a release, by the owner.
func (d *Node) Publish(path, teamName, note string) (PublishResult, error) {
	return d.release(path, teamName, note, nil)
}

// Release releases the next version of a domain document (spec 6.12): the
// assigned domain owner raises `version:` and lists the comments the
// version incorporates. It is Publish for any other published document.
func (d *Node) Release(path, note string, incorporates []string) (PublishResult, error) {
	return d.release(path, "", note, incorporates)
}

func (d *Node) release(path, teamName, note string, incorporates []string) (PublishResult, error) {
	var res PublishResult
	for attempt := 0; ; attempt++ {
		f, err := d.Store.Load(d.resolve(path))
		if err != nil {
			return res, err
		}
		if f.Doc.HasErrors() {
			return res, fmt.Errorf("%s has format errors; run `%s check %s`", f.Path, name.Binary, f.Path)
		}
		if f.Doc.Deleted() {
			return res, errors.New("a deleted document cannot be published again")
		}
		if f.Doc.KalkaID() == "" {
			if f, err = d.prepare(f); err != nil {
				return res, err
			}
		}
		doc, err := bp.Parse(f.Raw)
		if err != nil {
			return res, err
		}
		if cur := doc.Team(); cur != "" {
			if teamName != "" && teamName != cur {
				return res, fmt.Errorf("this document is published to %s; a document belongs to one team", cur)
			}
			teamName = cur
		}
		if teamName == "" {
			t, err := d.findTeam("")
			if err != nil {
				return res, err
			}
			teamName = t.Name
		}
		h, err := d.heldByName(teamName)
		if err != nil {
			return res, err
		}
		id := doc.KalkaID()
		if dup, err := d.Store.Find(id); err != nil {
			return res, err
		} else if dup != nil && dup.Path != f.Path {
			return res, &store.ErrDuplicate{KalkaID: id, Paths: []string{f.Path, dup.Path}}
		}
		if doc.IsDomain() && doc.DomainName() == "" {
			return res, errors.New("a domain document needs `domain:` with a name of lower-case letters, digits and dashes")
		}
		doc.SetFrontmatter(bp.KeyTeam, teamName)
		team := h.teamFor(doc)
		ch := merge.Releases(entriesOf(doc), id, team)
		releaser, v := nextReleaser(ch, team, id, doc.Owner())
		if ch.Disputed {
			return res, errors.New("ownership of this document is disputed; a team owner settles it by reassigning the document")
		}
		if releaser == "" && doc.IsDomain() {
			return res, fmt.Errorf("no domain owner is assigned for %s; a team owner assigns one with `%s team assign-domain %s <email>`", doc.DomainName(), name.Binary, doc.DomainName())
		}
		if !bp.SameEmail(releaser, d.self.Email) {
			if doc.IsDomain() {
				return res, fmt.Errorf("only %s, the domain owner of %s, can release it", releaser, doc.DomainName())
			}
			return res, fmt.Errorf("only %s can publish this document", releaser)
		}
		for _, x := range incorporates {
			if e := entryByID(doc, x); e == nil || e.Kind == bp.KindRelease {
				return res, fmt.Errorf("%s is not a comment of this document", x)
			}
		}
		// The released plan names its owner: the local user, also after a
		// reassignment (spec 19.5).
		if !bp.SameEmail(doc.Owner(), d.self.Email) {
			doc.SetFrontmatter(name.FrontmatterOwner, strings.ToLower(d.self.Email))
		}
		if head, ok := ch.Head(); ok && head.Release.Plan == doc.PlanHash() {
			return res, fmt.Errorf("version %d is already the published version of this plan; edit it first", head.Release.Version)
		}
		if doc.IsDomain() {
			doc.SetFrontmatter(bp.KeyVersion, strconv.FormatInt(v, 10))
		}
		e, err := bp.NewRelease(doc, strings.ToLower(d.self.Email), d.now(), v, false, incorporates, note, d.signer())
		if err != nil {
			return res, err
		}
		if err := doc.AddEntry(e); err != nil {
			return res, err
		}
		err = d.Store.Update(f, doc)
		if errors.Is(err, fsutil.ErrChanged) && attempt < 3 {
			continue
		}
		if err != nil {
			return res, err
		}
		res = PublishResult{Path: f.Path, Team: teamName, Version: v}
		if doc.IsDomain() {
			if _, err := d.Store.SaveSnapshot(f.Path, v, doc); err != nil {
				d.logf("snapshot of %s v%d: %v", f.Path, v, err)
			}
		}
		for _, en := range entriesOf(doc) {
			if en.Valid() && !merge.VerifiedBy(team, id, en) {
				res.LocalOnly++
			}
		}
		d.logf("published %s to %s as version %d", f.Path, teamName, v)
		d.pushTeam(h.dir.Team)
		return res, nil
	}
}

// Delete deletes a published document for the team (spec 20.6): a final
// release whose plan is a stub, kept in the team's tombstones; the last
// full copy goes to the team's trash.
func (d *Node) Delete(path string) error {
	f, err := d.Store.Load(d.resolve(path))
	if err != nil {
		return err
	}
	doc := f.Doc
	if doc.Team() == "" {
		return fmt.Errorf("%s is not published to a team; delete the file as usual", f.Path)
	}
	h, err := d.heldByName(doc.Team())
	if err != nil {
		return err
	}
	id := doc.KalkaID()
	unlock, ok := d.Store.Lock(id, syncproto.LockWait)
	if !ok {
		return errors.New("this document is busy in a sync; try again")
	}
	defer unlock()
	team := h.teamFor(doc)
	ch := merge.Releases(entriesOf(doc), id, team)
	if _, ok := ch.Head(); !ok {
		return errors.New("the document has no valid release; nothing to delete for the team")
	}
	releaser, v := nextReleaser(ch, team, id, doc.Owner())
	if !bp.SameEmail(releaser, d.self.Email) {
		return fmt.Errorf("only %s can delete this document for the team", releaser)
	}
	stub, err := stubOf(doc)
	if err != nil {
		return err
	}
	e, err := bp.NewRelease(stub, strings.ToLower(d.self.Email), d.now(), v, true, nil, "", d.signer())
	if err != nil {
		return err
	}
	var items []*bp.Item
	for _, l := range ch.Releases {
		items = append(items, &bp.Item{Entry: l.Entry})
	}
	items = append(items, &bp.Item{Entry: e})
	final, err := bp.Compose(stub, doc.EOL(), nil, items)
	if err != nil {
		return err
	}
	if err := d.writeTombstone(h, final, f); err != nil {
		return err
	}
	d.logf("deleted %s for team %s", f.Path, h.dir.Name)
	d.pushTeam(h.dir.Team)
	return nil
}

// stubOf is a tombstone's plan: the frontmatter with deleted: true, no
// body (spec 20.6).
func stubOf(doc *bp.Document) (*bp.Document, error) {
	inner := ""
	if fm := doc.Frontmatter(); fm != nil {
		inner = strings.ReplaceAll(fm.Inner(), "\r\n", "\n")
	}
	stub, err := bp.Parse([]byte("---\n" + strings.TrimRight(inner, "\n") + "\n---\n"))
	if err != nil {
		return nil, err
	}
	stub.SetFrontmatter(bp.KeyDeleted, "true")
	return stub.Reparse()
}

// releaseOwn makes a release after one of kalka's own changes to a
// published document (spec 20.4: promotion, reassignment), when the local
// user may release it. It does nothing for unpublished documents.
func (d *Node) releaseOwn(path, note string) {
	f, err := d.Store.Load(path)
	if err != nil || f.Doc.Team() == "" {
		return
	}
	if _, err := d.Publish(path, "", note); err != nil {
		d.logf("release after %s: %v", note, err)
	}
}

// afterLogChange reacts to new operations in a team log (spec 19.5): a
// document reassigned to the local user gets its owner line and a release.
func (d *Node) afterLogChange(h *held) {
	for dom, a := range h.st.Domains {
		if !bp.SameEmail(a.Owner, d.self.Email) {
			continue
		}
		for _, in := range d.Store.FindDomain(h.dir.Name, dom) {
			f, err := d.Store.Load(in.Path)
			if err != nil || bp.SameEmail(f.Doc.Owner(), d.self.Email) || f.Doc.HasErrors() {
				continue
			}
			team := h.teamFor(f.Doc)
			ch := merge.Releases(entriesOf(f.Doc), f.Doc.KalkaID(), team)
			head, ok := ch.Head()
			if !ok || head.Release.Version < a.After || head.Release.Plan != f.Doc.PlanHash() {
				continue // the releases up to the cut-off have not arrived yet
			}
			if _, err := d.Publish(f.Path, h.dir.Name, "domain assigned to "+d.self.Email); err != nil {
				d.logf("taking over domain %s: %v", dom, err)
			}
		}
	}
	for id, a := range h.st.Reassigned {
		if !bp.SameEmail(a.Owner, d.self.Email) {
			continue
		}
		f, err := d.Store.Find(id)
		if err != nil || f == nil || f.Doc.Team() != h.dir.Name || bp.SameEmail(f.Doc.Owner(), d.self.Email) {
			continue
		}
		if f.Doc.IsDomain() {
			continue // a domain follows assign-domain
		}
		ch := merge.Releases(entriesOf(f.Doc), id, h.teamFor(f.Doc))
		if r, _ := nextReleaser(ch, h.teamFor(f.Doc), id, f.Doc.Owner()); !bp.SameEmail(r, d.self.Email) {
			continue // the releases up to the cut-off have not arrived yet
		}
		if _, err := d.Publish(f.Path, h.dir.Name, "reassigned to "+d.self.Email); err != nil {
			d.logf("taking over %s: %v", f.Path, err)
		}
	}
}

// memberOnline starts a round with each team the peer belongs to (spec
// 20.3: when a member comes online).
func (d *Node) memberOnline(email string) {
	for _, t := range d.teamDirs() {
		if st, _, _, _, err := d.load(t); err == nil {
			if _, ok := memberOf(st, email); ok {
				d.kickTeam(t.Team)
			}
		}
	}
}

// TeamDocState is what the document view shows about a published
// document (spec 11.1, 19.5).
type TeamDocState struct {
	Team      string `json:"team"`
	Held      bool   `json:"held"` // the team log is held and you are a member
	Version   int64  `json:"version"`
	Releaser  string `json:"releaser,omitempty"` // who may release next
	CanAct    bool   `json:"can_act,omitempty"`  // the local user may publish an update or delete
	Unrel     bool   `json:"unreleased,omitempty"`
	Disputed  bool   `json:"disputed,omitempty"`
	Pending   string `json:"reassigned_to,omitempty"` // reassigned, owner line not yet updated
	OwnerGone bool   `json:"owner_not_member,omitempty"`
	LocalOnly int    `json:"local_only,omitempty"`
	Void      int    `json:"void_releases,omitempty"`
	// Domain is set for a domain document (spec 6.12); Releaser is then the
	// assigned domain owner, "" when none is assigned.
	Domain string `json:"domain,omitempty"`
}

func (d *Node) teamDocState(doc *bp.Document) *TeamDocState {
	name := doc.Team()
	if name == "" {
		return nil
	}
	ts := &TeamDocState{Team: name}
	h, err := d.heldByName(name)
	if err != nil || doc.KalkaID() == "" {
		return ts
	}
	ts.Held = true
	id := doc.KalkaID()
	team := h.teamFor(doc)
	ch := merge.Releases(entriesOf(doc), id, team)
	head, ok := ch.Head()
	ts.Unrel = true
	if ok {
		ts.Version = head.Release.Version
		ts.Unrel = head.Release.Plan != doc.PlanHash()
	}
	ts.Releaser, _ = nextReleaser(ch, team, id, doc.Owner())
	ts.Domain = doc.DomainName()
	ts.CanAct = bp.SameEmail(ts.Releaser, d.self.Email) && !ch.Disputed
	ts.Disputed, ts.Void = ch.Disputed, len(ch.Void)
	if a, ok := h.st.Reassigned[id]; ok && !doc.IsDomain() && !bp.SameEmail(a.Owner, doc.Owner()) {
		ts.Pending = a.Owner
	}
	if doc.IsDomain() && ts.Releaser != "" && !bp.SameEmail(ts.Releaser, doc.Owner()) {
		ts.Pending = ts.Releaser
	}
	if _, ok := memberOf(h.st, ts.Releaser); !ok && ts.Pending == "" && ts.Releaser != "" {
		ts.OwnerGone = true
	}
	for _, e := range entriesOf(doc) {
		if e.Valid() && !merge.VerifiedBy(team, id, e) {
			ts.LocalOnly++
		}
	}
	return ts
}

// entryTeamState marks an entry of a published document: signed by a
// current member, by a removed member, or local only (spec 11.1).
func (d *Node) entryTeamState(h *held, id string, e *bp.Entry) string {
	if !merge.VerifiedBy(h.teamFor(), id, e) {
		return "local-only"
	}
	if _, ok := memberOf(h.st, e.Author); !ok {
		return "removed-member"
	}
	return "member"
}

func entryByID(doc *bp.Document, id string) *bp.Entry {
	for _, e := range entriesOf(doc) {
		if e.ID == id && e.Valid() {
			return e
		}
	}
	return nil
}
