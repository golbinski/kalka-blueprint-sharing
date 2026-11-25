package blueprint

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Approval (spec 6.18): the owner's signed release of an approved
// version, the one end of a plan's review. The state is computed from the
// review log, never written into frontmatter, and it says nothing about
// implementation progress.

// KindApproved is an approval of a released version (spec 6.5).
const KindApproved = "approved"

// Approval states. A document that has none (Personal, a seed, a domain
// document, a tombstone) has an empty state.
const (
	ApprovalDraft    = "draft"    // never approved
	ApprovalApproved = "approved" // the latest release's body is that of the approved version
	ApprovalChanged  = "changed"  // a later release changed the body: changed since approved vN
)

// Approved is an approved entry's body:
//
//	approved: v3 · plan:<64 hex>
//	An optional note.
type Approved struct {
	Version int64  `json:"version"`
	Plan    string `json:"plan"`
	Note    string `json:"note,omitempty"`
}

var approvedRe = regexp.MustCompile(`^approved: v([1-9][0-9]{0,17}) · plan:([0-9a-f]{64})$`)

// Body is the entry text of an approved entry.
func (a Approved) Body() string {
	b := fmt.Sprintf("approved: v%d · plan:%s", a.Version, a.Plan)
	if n := strings.TrimSpace(a.Note); n != "" {
		b += "\n" + n
	}
	return b
}

// Approved reads an approved entry; ok is false for another kind or a
// body that does not parse.
func (e *Entry) Approved() (Approved, bool) {
	if e == nil || e.Kind != KindApproved {
		return Approved{}, false
	}
	lines := strings.Split(Normalize(e.Body), "\n")
	m := approvedRe.FindStringSubmatch(strings.TrimSpace(lines[0]))
	if m == nil {
		return Approved{}, false
	}
	v, _ := strconv.ParseInt(m[1], 10, 64)
	return Approved{Version: v, Plan: m[2], Note: strings.TrimSpace(strings.Join(lines[1:], "\n"))}, true
}

// NewApproval builds the signed approved entry of d citing release
// version v with plan hash plan. The caller checks who may approve and
// adds the entry.
func NewApproval(d *Document, author string, at time.Time, v int64, plan, note string, sign Signer) (*Entry, error) {
	if d.KalkaID() == "" {
		return nil, errors.New("an approval needs a kalka_id")
	}
	if !sign.Ok() {
		return nil, errors.New("an approval must be signed")
	}
	e, err := NewEntry(KindApproved, author, at, "", d.Rev(), Approved{Version: v, Plan: plan, Note: note}.Body())
	if err != nil {
		return nil, err
	}
	e.Sign(d.KalkaID(), sign)
	return e, nil
}

// Approval is a document's computed approval state. Version is the
// approved version (for approved and changed); Entry the approved entry
// that applies. Ignored gives, per approved entry that does not count,
// the reason.
type Approval struct {
	State   string            `json:"state"`
	Version int64             `json:"version,omitempty"`
	Entry   string            `json:"entry,omitempty"`
	Ignored map[string]string `json:"ignored,omitempty"`
}

// Text is the state in words: draft, approved v3, changed since approved v3.
func (a Approval) Text() string {
	switch a.State {
	case ApprovalApproved:
		return fmt.Sprintf("approved v%d", a.Version)
	case ApprovalChanged:
		return fmt.Sprintf("changed since approved v%d", a.Version)
	}
	return a.State
}

// Short is the state without the reasons, as kalka graph and kalka
// context print it.
func (a Approval) Short() *Approval {
	if a.State == "" {
		return nil
	}
	return &Approval{State: a.State, Version: a.Version}
}

// ApprovalOf is the one computation of the approval state (spec 6.18).
// releases are the release entries that count, in any order; entries are
// the document's other entries that may count. Both are what the file
// says (FileApproval) or what the daemon kept after dropping void
// entries, so every peer, and kalka check, compute the same state from
// the same entries in any order of arrival.
//
// An approved entry counts if its id matches its content, it cites a
// counting release by version and plan hash, and its author is the owner
// that release names. The counting entry citing the highest version
// applies, never the latest by time. It holds while the latest counting
// release has the body hash (rev) of the approved release.
func ApprovalOf(releases, entries []*Entry) Approval {
	type rel struct {
		r   Release
		rev string
	}
	byVersion := map[int64]rel{}
	var head rel
	for _, e := range releases {
		r, ok := e.Release()
		if !ok {
			continue
		}
		byVersion[r.Version] = rel{r, e.Rev}
		if r.Version > head.r.Version {
			head = rel{r, e.Rev}
		}
	}
	a := Approval{State: ApprovalDraft}
	if head.r.Deleted {
		return Approval{}
	}
	var best *Entry
	var bestV int64
	for _, e := range entries {
		if e == nil || e.Kind != KindApproved {
			continue
		}
		why := ""
		x, ok := e.Approved()
		r, held := byVersion[x.Version]
		switch {
		case !e.Valid():
			why = "the id does not match the content"
		case !ok:
			why = "not an approval body"
		case !held:
			why = fmt.Sprintf("version %d is not a release that counts", x.Version)
		case r.r.Plan != x.Plan:
			why = fmt.Sprintf("the plan it cites is not the plan of version %d", x.Version)
		case r.r.Deleted:
			why = "it cites a deletion"
		case !SameEmail(e.Author, r.r.Owner):
			why = fmt.Sprintf("only %s, the owner version %d names, approves it", r.r.Owner, x.Version)
		}
		if why != "" {
			if a.Ignored == nil {
				a.Ignored = map[string]string{}
			}
			a.Ignored[e.ID] = why
			continue
		}
		if best == nil || x.Version > bestV || (x.Version == bestV && e.ID < best.ID) {
			best, bestV = e, x.Version
		}
	}
	if best == nil {
		return a
	}
	a.Version, a.Entry = bestV, best.ID
	if head.rev == byVersion[bestV].rev {
		a.State = ApprovalApproved
	} else {
		a.State = ApprovalChanged
	}
	return a
}

// HasApproval reports whether a document can have an approval state as
// the file says: a team document (kalka_id and kalka_team) that is not a
// seed, a domain document or a tombstone (spec 6.18).
func (d *Document) HasApproval() bool {
	return d.KalkaID() != "" && d.Team() != "" && !d.IsSeed() && !d.IsDomain() && !d.Deleted()
}

// FileApproval is the approval state as the file says, for the commands
// that read files with no team log (check, context, graph): entries a
// transfer brought from another team are history, a version with two
// different releases counts for nothing, and nothing above a deletion
// counts. Signatures are not checked (spec 18.2, decision 3). The state is
// empty for a document that has none (HasApproval).
func (d *Document) FileApproval() Approval {
	if !d.HasApproval() {
		return Approval{}
	}
	history := TransferHistory(d)
	var mine []*Entry
	byVersion := map[int64][]*Entry{}
	for _, it := range d.Items() {
		e := it.Entry
		if e == nil || history[e.ID] {
			continue
		}
		if e.Kind != KindRelease {
			mine = append(mine, e)
			continue
		}
		if r, ok := e.Release(); ok && e.Valid() {
			byVersion[r.Version] = append(byVersion[r.Version], e)
		}
	}
	versions := make([]int64, 0, len(byVersion))
	for v := range byVersion {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	var releases []*Entry
	for _, v := range versions {
		es := byVersion[v]
		first, _ := es[0].Release()
		same := true
		for _, e := range es[1:] {
			r, _ := e.Release()
			same = same && r.Plan == first.Plan && SameEmail(r.Owner, first.Owner) && r.Deleted == first.Deleted
		}
		if !same {
			continue
		}
		releases = append(releases, es[0])
		if first.Deleted {
			break
		}
	}
	return ApprovalOf(releases, mine)
}

// TransferHistory is the set of entry ids a transfer into this document
// brought from another team (spec 20.7): the entries listed by a transfer
// entry whose source is another document. They are history, and take no
// part in this document's releases or approval. The transfer entry of a
// move recorded in its own source lists the source's own entries, which
// still count there.
func TransferHistory(d *Document) map[string]bool {
	out := map[string]bool{}
	for _, it := range d.Items() {
		t, ok := it.Entry.transferOf()
		if !ok || strings.EqualFold(t.FromID, d.KalkaID()) {
			continue
		}
		for _, id := range t.Entries {
			out[id] = true
		}
	}
	return out
}

func (e *Entry) transferOf() (Transfer, bool) {
	if e == nil {
		return Transfer{}, false
	}
	return e.Transfer()
}
