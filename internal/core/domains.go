package core

// Team domain documents (spec 6.12): their list, the version a blueprint
// references, comments a release incorporated, and local snapshots.

import (
	"context"
	"fmt"
	"sort"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/check"
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
	// Owner releases the next version, "" when nobody may (a dispute).
	Owner string `json:"owner,omitempty"`
	// Parent is the parent version the node pins (kalka_parent), and
	// ParentLatest the newest version of the parent held here: newer than
	// the pin is "parent newer" (spec 6.12). ParentRel is the parent's
	// live document.
	Parent       string `json:"parent,omitempty"`
	ParentLatest int64  `json:"parent_latest,omitempty"`
	ParentRel    string `json:"parent_rel,omitempty"`
	// Children are the subdomains held here that name this node's domain
	// as their parent.
	Children []string `json:"children,omitempty"`
}

// DomainRefView is a blueprint's `kalka_domain:` reference (spec 6.12).
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
	Ambiguous bool   `json:"ambiguous,omitempty"` // two teams held here have that name, or two documents claim the domain
}

// DomainRow is one domain document in `kalka domains` and the Domains view.
type DomainRow struct {
	Team       string  `json:"team"`
	Domain     string  `json:"domain"`
	Title      string  `json:"title"`
	Path       string  `json:"path"`
	Rel        string  `json:"rel"`
	Version    int64   `json:"version"`
	Owner      string  `json:"owner,omitempty"` // who releases the next version
	You        bool    `json:"you,omitempty"`   // you are the owner
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
		if h, err := d.heldAt(in.Path); err == nil && h.dir.Name == in.Team {
			team := h.team()
			ch := merge.Releases(entriesOf(f.Doc), f.Doc.KalkaID(), team)
			head, ok := ch.Head()
			r.Unreleased = !ok || head.Release.Plan != f.Doc.PlanHash()
			if ok {
				r.Version = head.Release.Version
			}
			r.Owner, _ = nextReleaser(ch, f.Doc.Owner())
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
	sd := storeDomains{d}
	if up, ok := doc.DomainParent(); ok {
		di.Parent = up.String()
		di.ParentLatest = sd.Latest(doc.Team(), up.Domain)
		if docs := d.Store.FindDomain(doc.Team(), up.Domain); len(docs) > 0 {
			di.ParentRel = d.rel(docs[0].Path)
		}
	}
	for _, c := range sd.Children(doc.Team(), doc.DomainName()) {
		di.Children = append(di.Children, c.DomainName())
	}
	h, err := d.heldAt(live)
	if err != nil || h.dir.Name != doc.Team() {
		return di, nil
	}
	team := h.team()
	ch := merge.Releases(entriesOf(doc), doc.KalkaID(), team)
	if head, ok := ch.Head(); ok {
		di.Version = head.Release.Version
	}
	di.Owner, _ = nextReleaser(ch, doc.Owner())
	return di, incorporated(ch)
}

// domainRef resolves a blueprint's `kalka_domain:` reference against the domain
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
	docs := d.Store.FindDomain(ref.Team, ref.Domain)
	// Two teams of that name, or two documents claiming the domain (spec 6.12).
	v.Ambiguous = n > 1 || len(docs) > 1
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
		ch := merge.Releases(entriesOf(f.Doc), f.Doc.KalkaID(), h.team())
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

// storeDomains reads domain versions for the checker from the documents
// held here, as check.Folder reads them from files: a version's snapshot
// first, then the live document when it is that version (spec 12.4).
type storeDomains struct{ d *Node }

func (s storeDomains) live(team, domain string) *bp.Document {
	docs := s.d.Store.FindDomain(team, domain)
	if len(docs) == 0 {
		return nil
	}
	f, err := s.d.Store.Load(docs[0].Path)
	if err != nil {
		return nil
	}
	return f.Doc
}

func (s storeDomains) Version(team, domain string, v int64) *bp.Document {
	docs := s.d.Store.FindDomain(team, domain)
	if len(docs) == 0 {
		return nil
	}
	if snap, err := s.d.Store.Snapshot(docs[0].Path, v); err == nil && snap != nil {
		return snap
	}
	if doc := s.live(team, domain); doc != nil && doc.DomainVersion() == v {
		return doc
	}
	return nil
}

func (s storeDomains) Latest(team, domain string) int64 {
	docs := s.d.Store.FindDomain(team, domain)
	if len(docs) == 0 {
		return 0
	}
	var latest int64
	if doc := s.live(team, domain); doc != nil {
		latest = doc.DomainVersion()
	}
	for _, v := range s.d.Store.Snapshots(docs[0].Path) {
		latest = max(latest, v)
	}
	return latest
}

// Children are the live documents of the domains whose parent is domain
// (check.Siblings).
func (s storeDomains) Children(team, domain string) []*bp.Document {
	var out []*bp.Document
	for _, in := range s.d.Store.List() {
		if in.Domain == "" || in.Duplicate || (team != "" && in.Team != team) {
			continue
		}
		f, err := s.d.Store.Load(in.Path)
		if err != nil {
			continue
		}
		if up, ok := f.Doc.DomainParent(); ok && up.Domain == domain {
			out = append(out, f.Doc)
		}
	}
	return out
}

// blueprintsOf are the held blueprints whose `kalka_domain:` names dom's
// team and domain, by path relative to the folder.
func (d *Node) blueprintsOf(dom *bp.Document) map[string]*bp.Document {
	out := map[string]*bp.Document{}
	name, team := dom.DomainName(), dom.Team()
	if name == "" {
		return out
	}
	for _, in := range d.Store.List() {
		if in.Domain != "" || in.ParseError != "" {
			continue
		}
		f, err := d.Store.Load(in.Path)
		if err != nil {
			continue
		}
		if ref, ok := f.Doc.DomainRef(); ok && ref.Domain == name && (team == "" || ref.Team == team) {
			out[d.rel(in.Path)] = f.Doc
		}
	}
	return out
}

// DomainImpact is what releasing the current text of the domain document
// at path would mean for the held blueprints referencing it (spec 11.1):
// invariants they rely on that it removes (I040), aliases it introduces
// that they use (G030).
func (d *Node) DomainImpact(path string) ([]check.Affected, error) {
	f, err := d.Store.Load(d.resolve(path))
	if err != nil {
		return nil, err
	}
	if !f.Doc.IsDomain() {
		return nil, fmt.Errorf("%s is not a domain document", f.Path)
	}
	return check.Impact(f.Doc, d.blueprintsOf(f.Doc), storeDomains{d}), nil
}
