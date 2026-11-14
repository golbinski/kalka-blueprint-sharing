package core

// Team domain documents (spec 6.12): their list, the version a blueprint
// references, comments a release incorporated, and local snapshots.

import (
	"context"
	"fmt"
	"sort"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/merge"
)

// DomainInfo is what the document view shows of a domain document.
type DomainInfo struct {
	Name string `json:"name"`
	Team string `json:"team"`
	// Version is the highest valid release; Versions the snapshots kept
	// here; Viewing the snapshot shown, 0 for the live file.
	Version  int64   `json:"version"`
	Versions []int64 `json:"versions"`
	Viewing  int64   `json:"viewing,omitempty"`
	// Owner is the assigned domain owner, "" when none is.
	Owner string `json:"owner,omitempty"`
}

// DomainRefView is a blueprint's `domain:` reference (spec 6.12).
type DomainRefView struct {
	Ref     string `json:"ref"`
	Team    string `json:"team"`
	Domain  string `json:"domain"`
	Version int64  `json:"version"`
	// Latest is the newest version held here; Rel the live domain
	// document. Held is false when no document of that domain is here.
	Latest    int64  `json:"latest,omitempty"`
	Rel       string `json:"rel,omitempty"`
	Held      bool   `json:"held"`
	Missing   bool   `json:"missing,omitempty"`   // the pinned version is not kept here
	Ambiguous bool   `json:"ambiguous,omitempty"` // two teams held here have that name
}

// DomainRow is one domain document in `kalka domains` and the Domains view.
type DomainRow struct {
	Team       string  `json:"team"`
	Domain     string  `json:"domain"`
	Title      string  `json:"title"`
	Path       string  `json:"path"`
	Rel        string  `json:"rel"`
	Version    int64   `json:"version"`
	Owner      string  `json:"owner,omitempty"` // assigned domain owner
	You        bool    `json:"you,omitempty"`   // you are the domain owner
	Unreleased bool    `json:"unreleased,omitempty"`
	Open       int     `json:"open"` // comments no release incorporated
	Versions   []int64 `json:"versions"`
}

// Domains lists the domain documents held here.
func (d *Node) Domains() []DomainRow {
	out := []DomainRow{}
	for _, in := range d.Store.List() {
		if in.Domain == "" || in.Duplicate {
			continue
		}
		f, err := d.Store.Load(in.Path)
		if err != nil {
			continue
		}
		r := DomainRow{Team: in.Team, Domain: in.Domain, Title: in.Title, Path: in.Path, Rel: d.rel(in.Path), Versions: d.Store.Snapshots(in.Path)}
		if r.Versions == nil {
			r.Versions = []int64{}
		}
		if h, err := d.heldByName(in.Team); err == nil {
			team := h.teamFor(f.Doc)
			ch := merge.Releases(entriesOf(f.Doc), f.Doc.KalkaID(), team)
			head, ok := ch.Head()
			r.Unreleased = !ok || head.Release.Plan != f.Doc.PlanHash()
			if ok {
				r.Version = head.Release.Version
			}
			r.Owner, _ = nextReleaser(ch, team, f.Doc.KalkaID(), f.Doc.Owner())
			r.You = r.Owner != "" && bp.SameEmail(r.Owner, d.self.Email)
			inc := incorporated(ch)
			for _, e := range entriesOf(f.Doc) {
				if e.Valid() && e.Kind != bp.KindRelease && e.ReplyTo() == "" && inc[e.ID] == 0 {
					r.Open++
				}
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Team != out[j].Team {
			return out[i].Team < out[j].Team
		}
		return out[i].Domain < out[j].Domain
	})
	return out
}

// incorporated maps entry ids to the version whose valid release
// incorporated them.
func incorporated(ch merge.ReleaseChain) map[string]int64 {
	out := map[string]int64{}
	for _, l := range ch.Releases {
		for _, id := range l.Release.Incorporates {
			if out[id] == 0 {
				out[id] = l.Release.Version
			}
		}
	}
	return out
}

// domainInfo describes a domain document for the view; viewing is the
// snapshot shown, 0 for the live file.
func (d *Node) domainInfo(live string, doc *bp.Document, viewing int64) (*DomainInfo, map[string]int64) {
	if !doc.IsDomain() {
		return nil, nil
	}
	di := &DomainInfo{Name: doc.DomainName(), Team: doc.Team(), Versions: d.Store.Snapshots(live), Viewing: viewing}
	if di.Versions == nil {
		di.Versions = []int64{}
	}
	h, err := d.heldByName(doc.Team())
	if err != nil {
		return di, nil
	}
	team := h.teamFor(doc)
	ch := merge.Releases(entriesOf(doc), doc.KalkaID(), team)
	if head, ok := ch.Head(); ok {
		di.Version = head.Release.Version
	}
	if a, ok := h.st.Domains[di.Name]; ok {
		di.Owner = a.Owner
	}
	return di, incorporated(ch)
}

// domainRef resolves a blueprint's `domain:` reference against the domain
// documents held here.
func (d *Node) domainRef(doc *bp.Document) *DomainRefView {
	ref, ok := doc.DomainRef()
	if !ok {
		return nil
	}
	v := &DomainRefView{Ref: ref.String(), Team: ref.Team, Domain: ref.Domain, Version: ref.Version}
	n := 0
	for _, t := range d.teamDirs() {
		if t.Name == ref.Team {
			n++
		}
	}
	v.Ambiguous = n > 1
	docs := d.Store.FindDomain(ref.Team, ref.Domain)
	if len(docs) == 0 {
		return v
	}
	in := docs[0]
	v.Held, v.Rel = true, d.rel(in.Path)
	f, err := d.Store.Load(in.Path)
	if err != nil {
		return v
	}
	v.Latest = f.Doc.DomainVersion()
	if h, err := d.heldByName(ref.Team); err == nil {
		ch := merge.Releases(entriesOf(f.Doc), f.Doc.KalkaID(), h.teamFor(f.Doc))
		if head, ok := ch.Head(); ok {
			v.Latest = head.Release.Version
		}
	}
	held := v.Latest == ref.Version
	for _, s := range d.Store.Snapshots(in.Path) {
		held = held || s == ref.Version
		v.Latest = max(v.Latest, s)
	}
	v.Missing = !held
	return v
}

// DocumentAt is Document for a kept version of a domain document.
func (d *Node) DocumentAt(ctx context.Context, path string, version int64) (*DocView, error) {
	if version <= 0 {
		return d.Document(ctx, path)
	}
	f, err := d.Store.Load(d.resolve(path))
	if err != nil {
		return nil, err
	}
	if !f.Doc.IsDomain() {
		return nil, fmt.Errorf("%s is not a domain document; only domain documents keep versions", f.Path)
	}
	snap, err := d.Store.Snapshot(f.Path, version)
	if err != nil {
		return nil, err
	}
	return d.docView(ctx, f.Path, snap, version)
}
