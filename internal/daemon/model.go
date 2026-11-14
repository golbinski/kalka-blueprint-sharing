package daemon

import (
	"context"
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
	LogLead  string       `json:"log_lead,omitempty"`
	Problems []bp.Problem `json:"problems"`
	// Hints are the soft suggestions of the blueprint reference (spec
	// 6.13), kept apart from Problems: they never block a sync.
	Hints       []bp.Problem `json:"hints"`
	OwnerEdited bool         `json:"owner_edited,omitempty"`
	Duplicate   bool         `json:"duplicate,omitempty"`
	// Traces are the body's trace blocks; Refs the references in them, as
	// the reader's plugins resolved them.
	Traces []TraceView        `json:"traces"`
	Refs   map[string]RefView `json:"refs"`
	// Seed is set for a document with status: seed (spec 6.11).
	Seed *SeedInfo `json:"seed,omitempty"`
	// Team is set for a document published to a team (spec 20).
	Team *TeamDocState `json:"team,omitempty"`
	// Domain is set for a domain document, DomainRef for a document whose
	// `domain:` names a domain version (spec 6.12).
	Domain    *DomainInfo    `json:"domain,omitempty"`
	DomainRef *DomainRefView `json:"domain_ref,omitempty"`
}

// SeedInfo describes a seed in the document view.
type SeedInfo struct {
	Parent      string `json:"parent,omitempty"`
	ParentRel   string `json:"parent_rel,omitempty"`
	ParentTitle string `json:"parent_title,omitempty"`
	// BacklinkMissing: the parent is held and has no budded entry for
	// this seed (a failed second write); the UI offers to add it.
	BacklinkMissing bool `json:"backlink_missing,omitempty"`
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
	// Sig is "unsigned", "invalid", "unknown" (no key held for the author
	// yet) or "verified" (spec 6.5).
	Sig string `json:"sig"`
	// Handoff is set for handoff entries: "verified" when it is a link of
	// the ownership chain with a good signature, otherwise "unverified".
	Handoff       string `json:"handoff,omitempty"`
	HandoffTarget string `json:"handoff_target,omitempty"`
	HandoffNote   string `json:"handoff_note,omitempty"`
	// BuddedSeed is the seed a budded entry names, and its path if held.
	BuddedSeed  string `json:"budded_seed,omitempty"`
	BuddedTitle string `json:"budded_title,omitempty"`
	BuddedRel   string `json:"budded_rel,omitempty"`
	// Unparsed is text in the log after this entry that is not an entry.
	Unparsed string `json:"unparsed,omitempty"`
	// TeamState, for a published document: member, removed-member or
	// local-only (spec 11.1, 20.4).
	TeamState string `json:"team_state,omitempty"`
	// IncorporatedIn is the domain version whose release incorporated this
	// comment (spec 6.12), 0 if none.
	IncorporatedIn int64 `json:"incorporated_in,omitempty"`
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
func (d *Daemon) Document(ctx context.Context, path string) (*DocView, error) {
	f, err := d.Store.Load(d.resolve(path))
	if err != nil {
		return nil, err
	}
	return d.docView(ctx, f.Path, f.Doc, 0)
}

// docView builds the view of doc, held at path; snapshot is the version
// shown when doc is a kept domain version, 0 for the live file.
func (d *Daemon) docView(_ context.Context, path string, doc *bp.Document, snapshot int64) (*DocView, error) {
	v := &DocView{
		Path: path, KalkaID: doc.KalkaID(), Title: doc.Title(), Owner: doc.Owner(), Self: d.self.Email,
		Rev: doc.Rev(), Sections: doc.Sections(), Headings: doc.Headings(), Problems: doc.Problems(),
		Hints: bp.Hints(doc), Entries: []EntryView{},
	}
	if v.Headings == nil {
		v.Headings = []bp.Heading{}
	}
	if v.Problems == nil {
		v.Problems = []bp.Problem{}
	}
	if v.Hints == nil {
		v.Hints = []bp.Problem{}
	}
	if v.Title == "" {
		v.Title = filepath.Base(path)
	}
	v.Rel, _ = filepath.Rel(d.Store.Dir(), path)
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
	if info, ok := d.Store.Info(path); ok {
		v.OwnerEdited = info.OwnerEdited
	}
	if v.KalkaID != "" {
		var dup *store.ErrDuplicate
		if _, err := d.Store.Find(v.KalkaID); errors.As(err, &dup) {
			v.Duplicate = true
		}
	}
	rel := v.Rel
	v.Traces, v.Refs = d.refViews(doc, func() { d.bus.Publish(BusEvent{Type: "refs", Data: map[string]string{"path": rel}}) })
	v.LogLead = strings.TrimSpace(bp.Normalize(strings.Join(doc.Lead(), "")))

	if snapshot == 0 {
		v.Team = d.teamDocState(doc)
	}
	var incorp map[string]int64
	v.Domain, incorp = d.domainInfo(path, doc, snapshot)
	if snapshot > 0 {
		v.Role = RoleReviewer // a kept version is read only
		v.Domain, _ = d.domainInfo(path, mustLive(d, path, doc), snapshot)
	}
	v.DomainRef = d.domainRef(doc)
	var th *held
	if v.Team != nil && v.Team.Held {
		th, _ = d.heldByName(v.Team.Team)
	}
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
		ev.Orphaned = e.Anchor != "" && !doc.HasAnchor(e.Anchor)
		ev.IncorporatedIn = incorp[e.ID]
		ev.Sig = d.sigState(v.KalkaID, e)
		if th != nil {
			ev.TeamState = d.entryTeamState(th, v.KalkaID, e)
			if ev.TeamState != "local-only" {
				ev.Sig = "verified" // by a key the team log binds (spec 8.2)
			}
		}
		if id, title, ok := e.Budded(); ok {
			ev.BuddedSeed, ev.BuddedTitle = id, title
			if sf, err := d.Store.Find(id); err == nil && sf != nil {
				ev.BuddedRel = d.rel(sf.Path)
			}
		}
		v.Entries = append(v.Entries, ev)
	}
	d.handoffStates(v.KalkaID, doc, v.Entries)
	if doc.IsSeed() {
		si := &SeedInfo{Parent: doc.Parent()}
		if si.Parent != "" {
			if pf, err := d.Store.Find(si.Parent); err == nil && pf != nil {
				si.ParentRel, si.ParentTitle = d.rel(pf.Path), pf.Doc.Title()
				si.BacklinkMissing = true
				for _, it := range pf.Doc.Items() {
					if id, _, ok := it.Entry.Budded(); ok && id == v.KalkaID {
						si.BacklinkMissing = false
					}
				}
			}
		}
		v.Seed = si
	}
	return v, nil
}

// sigState checks an entry's signature against the key held for its
// author. Team keys (spec 20.4) take over once the team log is held (R7).
func (d *Daemon) sigState(kalkaID string, e *bp.Entry) string {
	if e.Sig == "" {
		return "unsigned"
	}
	key, ok := d.keys(e.Author)
	if !ok {
		return "unknown"
	}
	if e.Verify(kalkaID, key) == bp.SigBad {
		return "invalid"
	}
	return "verified"
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
		e, err := bp.Append(doc, req.Kind, d.self.Email, d.now(), req.Anchor, req.Re, req.Quote, req.Body, d.signer())
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

// mustLive is the live file at path, or doc when it cannot be read.
func mustLive(d *Daemon, path string, doc *bp.Document) *bp.Document {
	if f, err := d.Store.Load(path); err == nil {
		return f.Doc
	}
	return doc
}
