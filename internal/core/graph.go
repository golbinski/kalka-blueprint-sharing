package core

// The graph canvas (spec 6.18, 11.1): the lens around a document as the
// app draws it. The selection and the states are check.BuildGraph's, the
// same kalka graph prints; the app adds what only it knows: where each
// document is held, the reader's reference divergences, and what is new
// since the user last looked.

import (
	"errors"
	"path/filepath"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/check"
)

// Graph is the graph around the document at path, and where the document
// is held relative to the folder.
func (d *Node) Graph(path string) (check.Graph, string, error) {
	f, err := d.Store.Load(d.resolve(path))
	if err != nil {
		return check.Graph{}, "", err
	}
	o := check.GraphOptions{
		Options:     check.Options{Domains: storeDomains{d}, Docs: d.docsAt(f.Path), Approval: d.approvalOf},
		Where:       d.graphWhere,
		Divergences: d.readerDivergences,
		Unseen:      d.unseen,
	}
	return check.BuildGraph(d.rel(f.Path), f.Doc, o), d.rel(f.Path), nil
}

// Locate is the document of a scope with kalka_id id, by path relative to
// the folder: the graph route names documents by id (spec 11.1).
func (d *Node) Locate(sc Scope, id string) (string, error) {
	for _, in := range d.Store.List() {
		if in.KalkaID != "" && strings.EqualFold(in.KalkaID, id) && !in.Duplicate && inScope(sc, in.Scope) {
			return in.Rel, nil
		}
	}
	return "", errors.New("no document with kalka_id " + id + " is held in this scope")
}

// inScope reports whether a file of the given store scope (its top folder,
// "" for Personal) is in sc.
func inScope(sc Scope, scope string) bool {
	switch {
	case sc.All:
		return true
	case sc.Personal:
		return scope == ""
	}
	return scope == filepath.Base(sc.Team.Dir)
}

// graphWhere is where a card's document is held: a plan node by kalka_id,
// a domain node at its live document, kept when the card is another
// version, and the scope.
func (d *Node) graphWhere(doc *bp.Document) check.Where {
	var w check.Where
	path := ""
	if doc.IsDomain() {
		if docs := d.Store.FindDomain(doc.Team(), doc.DomainName()); len(docs) > 0 {
			w.Rel, w.Kept, path = docs[0].Rel, docs[0].DomainVersion != doc.DomainVersion(), docs[0].Path
		}
	} else {
		for _, in := range d.Store.List() {
			if in.KalkaID != "" && strings.EqualFold(in.KalkaID, doc.KalkaID()) && !in.Duplicate && in.Domain == "" {
				w.Rel, path = in.Rel, in.Path
				break
			}
		}
	}
	if path != "" {
		w.Scope = ScopePersonal
		if t, ok := d.teamDirAt(path); ok {
			w.Scope = t.Name
		}
	}
	return w
}

// readerDivergences are a document's unverified and stale references, by
// trace index: what the reader's plugins say.
func (d *Node) readerDivergences(doc *bp.Document) map[int][]bp.Divergence {
	out := map[int][]bp.Divergence{}
	tvs, _ := d.refViews(doc, func() {})
	for _, tv := range tvs {
		if tv.Graph < len(tv.Divergences) {
			out[tv.Index] = tv.Divergences[tv.Graph:]
		}
	}
	return out
}

// unseen are the comments and deviations by others on a team document
// that the user has not seen here (spec 11.3, 11.4).
func (d *Node) unseen(doc *bp.Document) map[string]bool {
	id := doc.KalkaID()
	if id == "" {
		return nil
	}
	d.readMu.Lock()
	m := d.loadRead()
	d.readMu.Unlock()
	seen, opened := m.Docs[id]
	if !opened {
		return nil // never opened here: Needs you says so, not each item
	}
	known := map[string]bool{}
	for _, x := range seen {
		known[x] = true
	}
	out := map[string]bool{}
	for _, it := range doc.Items() {
		e := it.Entry
		if e == nil || known[e.ID] || !newKinds[e.Kind] || bp.SameEmail(e.Author, d.self.Email) {
			continue
		}
		out[e.ID] = true
	}
	return out
}
