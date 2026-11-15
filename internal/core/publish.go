package core

// Publishing, releasing and deleting team documents (spec 20.1, 20.4,
// 20.6), and what the daemon does when a team log changes (19.5).

import (
	"bytes"
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
// document, and that version: the owner the head names, or the owner of
// the last anchor while it is pending; firstOwner before any release.
// The owner is "" when nobody may: a dispute, a gap, a tombstone.
func nextReleaser(ch merge.ReleaseChain, firstOwner string) (string, int64) {
	if ch.Pending != nil {
		return ch.Pending.Owner, ch.Pending.After + 1
	}
	head, ok := ch.Head()
	if !ok {
		if ch.Disputed {
			return "", 1
		}
		return firstOwner, 1
	}
	return ch.NextSigner(), head.Release.Version + 1
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
// A file in Personal moves into the team's folder; the result has the new
// path. A file in a team's folder belongs to that team (spec 14).
func (d *Node) Publish(path, teamName, note string) (PublishResult, error) {
	return d.release(path, relOpts{mode: modePublish, team: teamName, note: note})
}

// Release releases the next version of a team document (spec 20.4): the
// owner's plan as it stands, listing the comments it incorporates. A
// domain document's `version:` is raised.
func (d *Node) Release(path, note string, incorporates []string) (PublishResult, error) {
	return d.release(path, relOpts{mode: modeRelease, note: note, incorporates: incorporates})
}

// Release modes.
const (
	modePublish = iota // Personal into a team, first release
	modeRelease        // the next version of a team document
	modeHandoff        // the next version, naming another owner (7.2)
	modeAuto           // kalka's own release (reassigned to you, promotion)
)

type relOpts struct {
	mode         int
	team         string
	note         string
	incorporates []string
	owner        string // the owner the release names, for a handoff
}

func (d *Node) release(path string, o relOpts) (PublishResult, error) {
	teamName := o.team
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
		h, personal, err := d.publishTarget(f, doc, teamName)
		if err != nil {
			return res, err
		}
		if personal && o.mode == modeHandoff {
			return res, &Error{Kind: KindInPersonal, Msg: fmt.Sprintf("%s lies in Personal: publish it to a team before handing it off", d.rel(f.Path))}
		}
		if personal && o.mode != modePublish {
			return res, &Error{Kind: KindInPersonal, Msg: fmt.Sprintf("%s lies in Personal: publish it to a team first (`%s publish`)", d.rel(f.Path), name.Binary)}
		}
		teamName = h.dir.Name
		id := doc.KalkaID()
		unlock, ok := d.Store.Lock(id, syncproto.LockWait)
		if !ok {
			return res, errors.New("this document is busy in a sync; try again")
		}
		res, again, err := d.releaseLocked(f, doc, h, personal, teamName, id, o)
		unlock()
		if again && attempt < 3 {
			continue
		}
		return res, err
	}
}

// releaseLocked is one attempt of release, holding the document's lock.
// again is set when the file changed underneath and the caller retries.
func (d *Node) releaseLocked(f *store.File, doc *bp.Document, h *held, personal bool, teamName, id string, o relOpts) (res PublishResult, again bool, err error) {
	if cur, err := d.Store.Load(f.Path); err != nil || !bytes.Equal(cur.Raw, f.Raw) {
		return res, err == nil, err
	}
	if dup, err := d.Store.FindIn(h.dir.Dir, id); err != nil {
		return res, false, err
	} else if dup != nil && dup.Path != f.Path {
		return res, false, &store.ErrDuplicate{KalkaID: id, Paths: []string{f.Path, dup.Path}}
	}
	if doc.IsDomain() && doc.DomainName() == "" {
		return res, false, errors.New("a domain document needs `domain:` with a name of lower-case letters, digits and dashes")
	}
	if doc.IsDomain() {
		if other := d.domainHolder(h, doc.DomainName(), id); other != "" {
			return res, false, &Error{Kind: KindDomainExists, Msg: fmt.Sprintf("%s already holds domain %s; a team has one document per domain", other, doc.DomainName())}
		}
	}
	doc.SetFrontmatter(bp.KeyTeam, teamName)
	team := h.team()
	ch := merge.Releases(entriesOf(doc), id, team)
	head, released := ch.Head()
	if o.mode == modePublish && !personal && released {
		return res, false, fmt.Errorf("%s is already published as version %d; release a new version with `%s release`", d.rel(f.Path), head.Release.Version, name.Binary)
	}
	if o.mode == modeAuto && (ch.Pending == nil || !bp.SameEmail(ch.Pending.Owner, d.self.Email)) {
		return res, false, errTaken
	}
	releaser, v := nextReleaser(ch, doc.Owner())
	if ch.Disputed {
		return res, false, &Error{Kind: KindDisputed, Msg: "ownership of this document is disputed; a team owner settles it by reassigning the document"}
	}
	if releaser == "" {
		return res, false, errors.New("this document has no valid release to build on; a team owner settles it by reassigning the document")
	}
	if !bp.SameEmail(releaser, d.self.Email) {
		return res, false, &Error{Kind: KindNotOwner, Msg: fmt.Sprintf("only %s, the owner, can release this document; a team owner can reassign it first", releaser)}
	}
	for _, x := range o.incorporates {
		if e := entryByID(doc, x); e == nil || e.Kind == bp.KindRelease {
			return res, false, fmt.Errorf("%s is not a comment of this document", x)
		}
	}
	// The released plan names its owner: the local user, also after a
	// reassignment (spec 7.3), or the recipient of a handoff (7.2).
	owner := strings.ToLower(d.self.Email)
	if o.mode == modeHandoff {
		owner = strings.ToLower(o.owner)
	}
	if !bp.SameEmail(doc.Owner(), owner) {
		doc.SetFrontmatter(name.FrontmatterOwner, owner)
	}
	if released && head.Release.Plan == doc.PlanHash() && o.mode != modeAuto {
		if !personal {
			return res, false, fmt.Errorf("version %d is already the published version of this plan; edit it first", head.Release.Version)
		}
		// Released, but lying in Personal: it only needs to move.
		path, err := d.Store.Move(f, h.dir.Dir, doc)
		if err != nil {
			return res, errors.Is(err, fsutil.ErrChanged), err
		}
		d.logf("moved %s into %s", path, teamName)
		d.pushTeam(h.dir.Team)
		return PublishResult{Path: path, Team: teamName, Version: head.Release.Version}, false, nil
	}
	if doc.IsDomain() {
		doc.SetFrontmatter(bp.KeyVersion, strconv.FormatInt(v, 10))
	}
	e, err := bp.NewRelease(doc, strings.ToLower(d.self.Email), d.now(), v, false, o.incorporates, o.note, d.signer())
	if err != nil {
		return res, false, err
	}
	if err := doc.AddEntry(e); err != nil {
		return res, false, err
	}
	path := f.Path
	if personal {
		path, err = d.Store.Move(f, h.dir.Dir, doc)
	} else {
		err = d.Store.Update(f, doc)
	}
	if err != nil {
		return res, errors.Is(err, fsutil.ErrChanged), err
	}
	res = PublishResult{Path: path, Team: teamName, Version: v}
	if doc.IsDomain() {
		if _, err := d.Store.SaveSnapshot(path, v, doc); err != nil {
			d.logf("snapshot of %s v%d: %v", path, v, err)
		}
	}
	for _, en := range entriesOf(doc) {
		if en.Valid() && !merge.VerifiedBy(team, id, en) {
			res.LocalOnly++
		}
	}
	d.logf("published %s to %s as version %d", path, teamName, v)
	d.pushTeam(h.dir.Team)
	return res, false, nil
}

// errTaken: kalka's own release after a reassignment was already made.
var errTaken = errors.New("already released by the new owner")

// publishTarget is the team a publish goes to, and whether the file lies in
// Personal and so moves. A file in a team's folder belongs to that team;
// one in Personal goes to the team named, or to its `team:`, or to the
// only team held.
func (d *Node) publishTarget(f *store.File, doc *bp.Document, teamName string) (*held, bool, error) {
	if t, ok := d.teamDirAt(f.Path); ok {
		if teamName != "" && teamName != t.Name && teamName != t.Team {
			return nil, false, fmt.Errorf("%s lies in the folder of %s; a document belongs to one team", d.rel(f.Path), t.Name)
		}
		h, err := d.heldAt(f.Path)
		return h, false, err
	}
	if cur := doc.Team(); cur != "" {
		if teamName != "" && teamName != cur {
			return nil, false, fmt.Errorf("this document names team %s; publish it there, or remove its team: line first", cur)
		}
		teamName = cur
	}
	if teamName == "" {
		t, err := d.findTeam("")
		if err != nil {
			return nil, false, err
		}
		teamName = t.Name
	}
	t, err := d.findTeam(teamName)
	if err != nil {
		return nil, false, err
	}
	h, err := d.heldTeam(t.Team)
	if err != nil {
		return nil, false, err
	}
	if !h.isMember(d.self.Email, d.pub) {
		return nil, false, fmt.Errorf("you are not a member of %s: only a member publishes to a team", t.Name)
	}
	return h, true, nil
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
	h, err := d.heldAt(f.Path)
	if err != nil || doc.Team() != h.dir.Name {
		return fmt.Errorf("%s is not a document of a team; delete the file as usual", f.Path)
	}
	id := doc.KalkaID()
	unlock, ok := d.Store.Lock(id, syncproto.LockWait)
	if !ok {
		return errors.New("this document is busy in a sync; try again")
	}
	defer unlock()
	team := h.team()
	ch := merge.Releases(entriesOf(doc), id, team)
	if _, ok := ch.Head(); !ok {
		return errors.New("the document has no valid release; nothing to delete for the team")
	}
	releaser, v := nextReleaser(ch, doc.Owner())
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
	Pending   string `json:"reassigned_to,omitempty"` // reassigned, the new owner has not released yet
	OwnerGone bool   `json:"owner_not_member,omitempty"`
	LocalOnly int    `json:"local_only,omitempty"`
	Void      int    `json:"void_releases,omitempty"`
	// TeamOwner is set when the local user is a team owner: they may
	// reassign the document, or take it over to act on it (spec 7.4).
	TeamOwner bool `json:"team_owner,omitempty"`
	// Domain is set for a domain document (spec 6.12); DomainClash names
	// another published document of the team claiming the same domain.
	Domain      string `json:"domain,omitempty"`
	DomainClash string `json:"domain_clash,omitempty"`
}

func (d *Node) teamDocState(path string, doc *bp.Document) *TeamDocState {
	name := doc.Team()
	if name == "" {
		return nil
	}
	ts := &TeamDocState{Team: name}
	h, err := d.heldAt(path)
	if err != nil || h.dir.Name != name || doc.KalkaID() == "" {
		return ts // in Personal, or in another team's folder: not shared
	}
	ts.Held = true
	id := doc.KalkaID()
	team := h.team()
	ch := merge.Releases(entriesOf(doc), id, team)
	head, ok := ch.Head()
	ts.Unrel = true
	if ok {
		ts.Version = head.Release.Version
		ts.Unrel = head.Release.Plan != doc.PlanHash()
	}
	ts.Releaser, _ = nextReleaser(ch, doc.Owner())
	ts.Domain = doc.DomainName()
	if doc.IsDomain() {
		ts.DomainClash = d.domainHolder(h, ts.Domain, id)
	}
	ts.CanAct = bp.SameEmail(ts.Releaser, d.self.Email) && !ch.Disputed
	ts.Disputed, ts.Void = ch.Disputed, len(ch.Void)
	if ch.Pending != nil {
		ts.Pending = ch.Pending.Owner
	}
	if me, ok := memberOf(h.st, d.self.Email); ok && me.Owner {
		ts.TeamOwner = true
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
	if !merge.VerifiedBy(h.team(), id, e) {
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

// prepare gives a document its kalka_id and owner.
func (d *Node) prepare(f *store.File) (*store.File, error) {
	doc, err := bp.Parse(f.Raw)
	if err != nil {
		return nil, err
	}
	doc.SetFrontmatter(name.FrontmatterID, newUUID())
	if doc.Owner() == "" {
		doc.SetFrontmatter(name.FrontmatterOwner, d.self.Email)
	}
	if err := d.Store.Update(f, doc); err != nil {
		return nil, err
	}
	return d.Store.Load(f.Path)
}
