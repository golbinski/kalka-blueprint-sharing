package daemon

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/store"
)

// Roles of the local user for one blueprint (spec 11.1).
const (
	RoleOwner    = "owner"
	RoleReviewer = "reviewer"
	RoleLocal    = "local" // never sent: no kalka_id yet
)

// DocView is the parsed document the UI renders (implementation plan A1):
// the UI never computes slugs, revs or derived states itself.
type DocView struct {
	Path        string       `json:"path"`
	Rel         string       `json:"rel"`
	KalkaID     string       `json:"kalka_id,omitempty"`
	Title       string       `json:"title"`
	Owner       string       `json:"owner,omitempty"`
	Role        string       `json:"role"`
	Self        string       `json:"self"`
	Rev         string       `json:"rev"`
	Frontmatter string       `json:"frontmatter,omitempty"`
	Sections    []bp.Section `json:"sections"`
	Headings    []bp.Heading `json:"headings"`
	Entries     []EntryView  `json:"entries"`
	// LogLead is unparsed text between the log heading and the first entry.
	LogLead     string       `json:"log_lead,omitempty"`
	Problems    []bp.Problem `json:"problems"`
	OwnerEdited bool         `json:"owner_edited,omitempty"`
	Duplicate   bool         `json:"duplicate,omitempty"`
}

// EntryView is one review log entry with its derived states (spec 6.7).
type EntryView struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Author    string `json:"author"`
	Timestamp string `json:"timestamp"`
	Anchor    string `json:"anchor"`
	Section   string `json:"section,omitempty"`
	Node      string `json:"node,omitempty"`
	Rev       string `json:"rev"`
	ReplyTo   string `json:"reply_to,omitempty"`
	// Body is the entry text without its "re:" line, as Markdown.
	Body     string `json:"body"`
	Outdated bool   `json:"outdated,omitempty"`
	Orphaned bool   `json:"orphaned,omitempty"`
	Valid    bool   `json:"valid"`
	// Handoff is set for handoff entries: "verified" when it is a link of
	// the ownership chain with a good signature, otherwise "unverified".
	Handoff       string `json:"handoff,omitempty"`
	HandoffTarget string `json:"handoff_target,omitempty"`
	HandoffNote   string `json:"handoff_note,omitempty"`
	// Unparsed is text in the log after this entry that is not an entry.
	Unparsed string `json:"unparsed,omitempty"`
}

// resolve turns a path from the UI (relative to the folder, or absolute)
// into a file inside the folder.
func (d *Daemon) resolve(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(d.Store.Dir(), filepath.FromSlash(p))
}

// Document builds the view of one blueprint.
func (d *Daemon) Document(path string) (*DocView, error) {
	f, err := d.Store.Load(d.resolve(path))
	if err != nil {
		return nil, err
	}
	doc := f.Doc
	v := &DocView{
		Path: f.Path, KalkaID: doc.KalkaID(), Title: doc.Title(), Owner: doc.Owner(), Self: d.self.Email,
		Rev: doc.Rev(), Sections: doc.Sections(), Headings: doc.Headings(), Problems: doc.Problems(),
		Entries: []EntryView{},
	}
	if v.Headings == nil {
		v.Headings = []bp.Heading{}
	}
	if v.Problems == nil {
		v.Problems = []bp.Problem{}
	}
	if v.Title == "" {
		v.Title = filepath.Base(f.Path)
	}
	v.Rel, _ = filepath.Rel(d.Store.Dir(), f.Path)
	v.Rel = filepath.ToSlash(v.Rel)
	if fm := doc.Frontmatter(); fm != nil {
		v.Frontmatter = strings.ReplaceAll(fm.Inner(), "\r\n", "\n")
	}
	switch {
	case v.KalkaID == "":
		v.Role = RoleLocal
	case bp.SameEmail(v.Owner, d.self.Email):
		v.Role = RoleOwner
	default:
		v.Role = RoleReviewer
	}
	if info, ok := d.Store.Info(f.Path); ok {
		v.OwnerEdited = info.OwnerEdited
	}
	if v.KalkaID != "" {
		var dup *store.ErrDuplicate
		if _, err := d.Store.Find(v.KalkaID); errors.As(err, &dup) {
			v.Duplicate = true
		}
	}
	v.LogLead = strings.TrimSpace(bp.Normalize(strings.Join(doc.Lead(), "")))

	rev := doc.Rev()
	for _, it := range doc.Items() {
		e := it.Entry
		if e == nil {
			continue
		}
		ev := EntryView{
			ID: e.ID, Kind: e.Kind, Author: e.Author, Timestamp: e.Timestamp, Anchor: e.Anchor,
			Section: e.Section(), Node: e.Node(), Rev: e.Rev, ReplyTo: e.ReplyTo(), Body: displayBody(e),
			Outdated: e.Rev != rev, Valid: e.Valid(),
			Unparsed: strings.TrimSpace(bp.Normalize(strings.Join(it.Trailing, ""))),
		}
		ev.Orphaned = ev.Section != "" && !doc.HasSection(ev.Section)
		v.Entries = append(v.Entries, ev)
	}
	d.handoffStates(v.KalkaID, doc, v.Entries)
	return v, nil
}

func displayBody(e *bp.Entry) string {
	b := bp.Normalize(e.Body)
	if e.ReplyTo() != "" {
		_, b, _ = strings.Cut(b, "\n")
	}
	return strings.TrimLeft(b, "\n")
}

// handoffStates marks each handoff entry as a verified link of the
// ownership chain or not (spec 7.2). The first handoff's author is taken as
// the original owner; each later link must be signed by the owner the
// chain has reached.
func (d *Daemon) handoffStates(kalkaID string, doc *bp.Document, views []EntryView) {
	var hs []*bp.Entry
	for _, it := range doc.Items() {
		if it.Entry != nil && it.Entry.Kind == bp.KindHandoff {
			hs = append(hs, it.Entry)
		}
	}
	slices.SortStableFunc(hs, func(a, b *bp.Entry) int {
		if a.Less(b) {
			return -1
		}
		if b.Less(a) {
			return 1
		}
		return 0
	})
	state := map[string][3]string{}
	current := ""
	for _, e := range hs {
		target, ok := e.HandoffTarget()
		key, known := d.keys(e.Author)
		switch {
		case !ok:
			state[e.ID] = [3]string{"unverified", "", "the entry does not name a new owner"}
		case !known:
			state[e.ID] = [3]string{"unverified", target, fmt.Sprintf("no key pinned for %s: sync with them to check this handoff", e.Author)}
		case !e.VerifySig(kalkaID, key):
			state[e.ID] = [3]string{"unverified", target, "the signature does not match " + e.Author + "'s key"}
		case current != "" && !bp.SameEmail(e.Author, current):
			state[e.ID] = [3]string{"unverified", target, fmt.Sprintf("%s was not the owner at this point (%s was)", e.Author, current)}
		default:
			state[e.ID] = [3]string{"verified", target, ""}
			current = target
		}
	}
	for i := range views {
		if s, ok := state[views[i].ID]; ok {
			views[i].Handoff, views[i].HandoffTarget, views[i].HandoffNote = s[0], s[1], s[2]
		}
	}
}

// EntryRequest adds an entry from the UI.
type EntryRequest struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Anchor string `json:"anchor"`
	Re     string `json:"re,omitempty"`
	Quote  string `json:"quote,omitempty"`
	Body   string `json:"body"`
}

// AddEntry appends an entry by the local user, merging again if the file
// changes while it is written (spec 14).
func (d *Daemon) AddEntry(req EntryRequest) (*bp.Entry, error) {
	if strings.TrimSpace(req.Body) == "" {
		return nil, errors.New("the entry is empty")
	}
	if req.Kind == "" {
		req.Kind = bp.KindComment
	}
	path := d.resolve(req.Path)
	f, err := d.Store.Load(path)
	if err != nil {
		return nil, err
	}
	if id := f.Doc.KalkaID(); id != "" {
		unlock, ok := d.Store.Lock(id, 10*time.Second)
		if !ok {
			return nil, errors.New("the blueprint is busy in a sync; try again")
		}
		defer unlock()
	}
	for attempt := 0; ; attempt++ {
		if f, err = d.Store.Load(path); err != nil {
			return nil, err
		}
		doc, err := bp.Parse(f.Raw)
		if err != nil {
			return nil, err
		}
		e, err := bp.Append(doc, req.Kind, d.self.Email, d.now(), req.Anchor, req.Re, req.Quote, req.Body)
		if err != nil {
			return nil, err
		}
		err = d.Store.Update(f, doc)
		if errors.Is(err, fsutil.ErrChanged) && attempt < 3 {
			continue
		}
		if err != nil {
			return nil, err
		}
		d.logf("added %s %s to %s", e.Kind, e.ID, f.Path)
		return e, nil
	}
}
