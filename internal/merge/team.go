package merge

import (
	"crypto/ed25519"
	"fmt"
	"sort"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/name"
)

// Team is what the merge needs from a team's replayed log (spec 19, 20.4).
type Team interface {
	// Term resolves a term reference (spec 6.5) to the email and key the
	// log bound by that operation, also for members since removed, so
	// their entries still verify (20.4). A term of another team is not
	// found.
	Term(ref string) (email string, key ed25519.PublicKey, ok bool)
	// Anchors returns the document's effective reassignments and accepted
	// transfer (cut-off 0), ordered by cut-off, one per cut-off (19.5,
	// 19.6, docs/teamlog.md rule 8).
	Anchors(kalkaID string) []Anchor
	// Accepted returns the effective accept-transfer of a document that
	// came by a transfer (20.7).
	Accepted(kalkaID string) (Acceptance, bool)
}

// Anchor is an effective reassignment: Owner is the only valid signer of
// version After+1, and the chain restarts there (20.4).
type Anchor struct {
	Owner string
	After int64
}

// VerifiedBy reports whether e carries a signature made under a term of
// this team that bound its author: one key is checked, never every key
// the author ever had.
func VerifiedBy(team Team, kalkaID string, e *bp.Entry) bool {
	if e.Sig == "" || e.Term == "" {
		return false
	}
	email, key, ok := team.Term(e.Term)
	return ok && bp.SameEmail(email, e.Author) && e.Verify(kalkaID, key) == bp.SigOK
}

// ReleaseChain is the sequence of valid releases of a published document.
type ReleaseChain struct {
	// Releases holds the valid releases in version order. Below an anchor
	// it can have gaps: a gap or a dispute ends the chain only up to the
	// next anchor.
	Releases []Link
	// Void lists release entries that do not count, with the reason.
	Void map[string]string
	// Disputed is set when, above the last anchor, one version has two
	// valid releases with different content (spec 7). A team owner ends it
	// with a reassignment.
	Disputed bool
	// Pending is set when the last anchor's owner has not released yet:
	// the document is "reassigned to Owner, pending".
	Pending *Anchor
}

// Link is one valid release.
type Link struct {
	Entry   *bp.Entry
	Release bp.Release
}

// Head returns the highest valid release, if any.
func (c ReleaseChain) Head() (Link, bool) {
	if len(c.Releases) == 0 {
		return Link{}, false
	}
	return c.Releases[len(c.Releases)-1], true
}

// VersionOf returns the version whose release names plan hash h.
func (c ReleaseChain) VersionOf(h string) (int64, bool) {
	for i := len(c.Releases) - 1; i >= 0; i-- {
		if c.Releases[i].Release.Plan == h {
			return c.Releases[i].Release.Version, true
		}
	}
	return 0, false
}

// NextSigner is who may sign the next release: the head's owner, or the
// last anchor's when it is pending; "" when nobody may (a dispute above
// the last anchor, a tombstone, no release yet).
func (c ReleaseChain) NextSigner() string {
	if c.Pending != nil {
		return c.Pending.Owner
	}
	head, ok := c.Head()
	if !ok || head.Release.Deleted || c.Disputed {
		return ""
	}
	return head.Release.Owner
}

// Releases computes the release chain over entries (spec 20.4, 19.5):
//
//   - version 1 is valid if signed by the owner it names (the publisher);
//   - version n+1 is valid if signed by the owner version n names;
//   - at an anchor with cut-off n, only its owner may sign n+1, and the
//     chain restarts there whatever stands below;
//   - every release needs a signature under a term the team bound to its
//     author;
//   - nothing above a tombstone is valid, and nothing above a gap or a
//     dispute until the next anchor.
func Releases(entries []*bp.Entry, kalkaID string, team Team) ReleaseChain {
	c := ReleaseChain{Void: map[string]string{}}
	byVersion := map[int64][]Link{}
	for _, e := range entries {
		r, ok := e.Release()
		if e.Kind != bp.KindRelease {
			continue
		}
		switch {
		case !e.Valid():
			c.Void[e.ID] = "the id does not match the content"
		case !ok:
			c.Void[e.ID] = "not a release body"
		case !VerifiedBy(team, kalkaID, e):
			c.Void[e.ID] = "no valid signature by a key the team bound to " + e.Author
		default:
			byVersion[r.Version] = append(byVersion[r.Version], Link{e, r})
		}
	}
	anchors := map[int64]Anchor{}
	var last *Anchor
	for _, a := range team.Anchors(kalkaID) {
		anchors[a.After] = a
	}
	// The chain visits only the versions something names: a release, or
	// the version after an anchor's cut-off. A version in between holds
	// nothing, so the chain breaks there, as it would visiting each one;
	// stepping one by one would let a single entry naming a huge version
	// hold every peer that receives it.
	steps := map[int64]bool{}
	for v := range byVersion {
		steps[v] = true
	}
	for after := range anchors {
		steps[after+1] = true
	}
	versions := make([]int64, 0, len(steps))
	for v := range steps {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	prevOwner := ""
	// broken says why the chain stands still until the next anchor.
	broken := ""
	deleted := false
	seen := int64(0) // the last version visited
	for _, v := range versions {
		if v > seen+1 && broken == "" {
			// An anchor at v-1, below, restarts the chain all the same.
			broken = fmt.Sprintf("version %d is missing or void", seen+1)
		}
		seen = v
		want, anchored := prevOwner, false
		if a, ok := anchors[v-1]; ok && !deleted {
			want, anchored, broken = a.Owner, true, ""
			c.Disputed = false
			last = &a
		}
		if deleted {
			broken = "the document was deleted at an earlier version"
		}
		var ok []Link
		for _, l := range byVersion[v] {
			switch {
			case broken != "":
				c.Void[l.Entry.ID] = broken
			case v == 1 && !anchored && !bp.SameEmail(l.Entry.Author, l.Release.Owner):
				c.Void[l.Entry.ID] = "version 1 must be released by the owner it names"
			case anchored && !bp.SameEmail(l.Entry.Author, want):
				c.Void[l.Entry.ID] = fmt.Sprintf("reassigned to %s from version %d", want, v)
			case !anchored && want != "" && !bp.SameEmail(l.Entry.Author, want):
				c.Void[l.Entry.ID] = fmt.Sprintf("only %s may release version %d", want, v)
			default:
				ok = append(ok, l)
			}
		}
		if broken != "" {
			continue
		}
		if len(ok) == 0 {
			broken = fmt.Sprintf("version %d is missing or void", v)
			continue
		}
		sort.Slice(ok, func(i, j int) bool { return ok[i].Entry.ID < ok[j].Entry.ID })
		// Two different releases of one version: neither counts, nor
		// anything above until an anchor. Picking one would let whoever
		// signed the second (any member, for version 1) win by searching
		// for a lower id. A team owner settles it by reassigning (19.5).
		split := false
		for _, l := range ok[1:] {
			split = split || l.Release.Plan != ok[0].Release.Plan || !bp.SameEmail(l.Release.Owner, ok[0].Release.Owner) || l.Release.Deleted != ok[0].Release.Deleted
		}
		if split {
			c.Disputed = true
			broken = fmt.Sprintf("ownership disputed: version %d has two different releases", v)
			for _, l := range ok {
				c.Void[l.Entry.ID] = broken
			}
			continue
		}
		c.Releases = append(c.Releases, ok[0])
		prevOwner = ok[0].Release.Owner
		deleted = ok[0].Release.Deleted
		if last != nil && v == last.After+1 {
			last = nil // the anchor's owner has released
		}
	}
	if last != nil && !deleted {
		c.Pending = last
	}
	return c
}

// Merge merges incoming bytes into local, the copy held of a team
// document; local is nil when none is held (spec 20.4).
func Merge(local *bp.Document, incoming []byte, team Team) Result {
	in, err := bp.Parse(incoming)
	if err != nil {
		return reject(Malformed, err.Error())
	}
	return Docs(local, in, team)
}

// Docs is Merge for a parsed document.
func Docs(local, in *bp.Document, team Team) Result {
	if msg := malformed(in); msg != "" {
		return reject(Malformed, msg)
	}
	id := in.KalkaID()
	all := allEntries(in)
	if local != nil {
		all = append(all, allEntries(local)...)
	}
	verified := Trusted(team, id, all)
	if local == nil {
		// A new document enters only with a valid release of its plan.
		ch := Releases(validEntries(in, verified), id, team)
		if _, ok := ch.VersionOf(in.PlanHash()); !ok {
			return reject(NotOwner, "the document has no valid release of its plan")
		}
		res := Docs(emptyLike(in), in, team)
		if res.Outcome != Rejected {
			res.Outcome = New
		}
		return res
	}
	if local.KalkaID() != id {
		return reject(Malformed, "kalka_id differs from the local file")
	}

	var res Result
	// Entries: everything valid from the local file; from the incoming one,
	// only entries signed by a key of the team (20.4). The verified copy of
	// an entry wins.
	dropped := 0
	keep := func(e *bp.Entry, fromLocal bool) bool {
		if fromLocal || verified(e) {
			return true
		}
		dropped++
		return false
	}
	union := unionWith(local, in, keep, verified)
	ch := Releases(union, id, team)

	lh, ih := local.PlanHash(), in.PlanHash()
	if lh != ih {
		if head, ok := ch.Head(); ok && head.Release.Plan == ih {
			base, released := ch.VersionOf(lh)
			switch {
			case released:
				res.PlanFromIncoming = head.Release.Version > base
			case releasedButVoid(union, ch, lh):
				// The local plan came from a release that no longer counts
				// (a discarded reassignment): ownership is disputed, and
				// the highest valid release is taken (19.5).
				res.PlanFromIncoming = true
				res.Notes = append(res.Notes, "the local plan's release is void; the highest valid release replaces it")
			default:
				// Unreleased local edits: kept unless a newer version
				// than the one they build on exists.
				res.PlanFromIncoming = head.Release.Version > baseVersion(local, ch)
			}
		}
	}
	plan, other := local, in
	if res.PlanFromIncoming {
		plan, other = in, local
	}
	head, hasHead := ch.Head()
	stub := hasHead && head.Release.Deleted && plan.PlanHash() == head.Release.Plan
	if stub {
		// A tombstone keeps its releases only (20.6).
		inner := keep
		keep = func(e *bp.Entry, fromLocal bool) bool { return e.Kind == bp.KindRelease && inner(e, fromLocal) }
		res.Notes = append(res.Notes, "the document was deleted for the team; only its releases are kept")
	}
	lead, items, notes := assembleWith(plan, other, local, keep, verified)
	res.Notes = append(res.Notes, notes...)
	if stub {
		lead = nil
		for _, it := range items {
			it.Trailing = nil
		}
	}
	if dropped > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf("%d incoming entries were not signed by a team member's key and stay with the sender", dropped))
	}
	if ch.Disputed {
		res.Notes = append(res.Notes, "ownership disputed: one version has two different releases")
	}
	doc, err := bp.Compose(plan, local.EOL(), lead, items)
	if err != nil {
		return reject(Malformed, "merged file: "+err.Error())
	}
	if doc.Plan() != plan.Plan() || countValid(doc) != len(items) {
		return reject(Malformed, bp.ErrOpenBlock.Error())
	}
	res.Doc = doc
	if bp.Normalize(string(doc.Bytes())) == bp.Normalize(string(local.Bytes())) {
		res.Outcome, res.Doc = Unchanged, local
	} else {
		res.Outcome = Updated
	}
	return res
}

// releasedButVoid reports whether some release entry names plan hash h
// but the chain does not count it.
func releasedButVoid(union []*bp.Entry, ch ReleaseChain, h string) bool {
	for _, e := range union {
		if r, ok := e.Release(); ok && r.Plan == h {
			if _, void := ch.Void[e.ID]; void {
				return true
			}
		}
	}
	return false
}

// baseVersion is the version an unreleased local plan builds on: the
// highest valid release the local file already held.
func baseVersion(local *bp.Document, ch ReleaseChain) int64 {
	mine := map[string]bool{}
	for _, it := range local.Items() {
		mine[it.Entry.ID] = true
	}
	var v int64
	for _, l := range ch.Releases {
		if mine[l.Entry.ID] {
			v = l.Release.Version
		}
	}
	return v
}

func validEntries(d *bp.Document, ok func(*bp.Entry) bool) []*bp.Entry {
	var out []*bp.Entry
	for _, it := range d.Items() {
		if it.Entry.Valid() && ok(it.Entry) {
			out = append(out, it.Entry)
		}
	}
	return out
}

// emptyLike is a local stand-in with no plan and no entries, so a new
// document passes through the same filters as an update.
func emptyLike(in *bp.Document) *bp.Document {
	d, _ := bp.Parse([]byte("---\n" + name.FrontmatterID + ": " + in.KalkaID() + "\n" + name.FrontmatterOwner + ": " + in.Owner() + "\n" + bp.KeyTeam + ": " + in.Team() + "\n---\n"))
	return d
}

// Replica is what a member sends of a published document (spec 20.4): the
// plan as held, and only the entries signed by a key the team bound to
// their author. Unsigned and outside entries, and unparsed log content,
// stay in the local file.
func Replica(d *bp.Document, team Team) (*bp.Document, error) {
	id := d.KalkaID()
	verified := Trusted(team, id, allEntries(d))
	keep := func(e *bp.Entry, _ bool) bool { return verified(e) }
	src, err := d.Reparse() // items are edited below
	if err != nil {
		return nil, err
	}
	_, items, _ := assembleWith(src, emptyLike(src), emptyLike(src), keep, verified)
	for _, it := range items {
		it.Trailing = nil
	}
	return bp.Compose(src, src.EOL(), nil, items)
}
