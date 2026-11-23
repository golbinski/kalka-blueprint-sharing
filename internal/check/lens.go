package check

import (
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// Lens is the part of the document graph around one document, the focus
// (spec 6.18): the plan path from the root to its parent, the domain paths
// it is read against, and its own children. kalka context and the graph
// canvas (kalka graph) select documents with this one function, so what an
// agent reads and what a person sees cannot drift apart. Siblings and
// documents off the paths are never in it.
type Lens struct {
	Focus *bp.Document
	// Plan runs from the root plan node to the focus's parent. Missing is
	// the id of the first plan node above that is not here; Cycle is set
	// when kalka_parent leads back.
	Plan    []*bp.Document
	Missing string
	Cycle   bool
	// Domains are the domain paths, root first, at the pinned versions; for
	// a domain node, its own path to the root.
	Domains DomainContext
	// Children are the focus's direct children in the plan tree, or its
	// direct subdomains when it is a domain node, in a stable order.
	Children []*bp.Document
}

// LensOf selects the lens around doc.
func LensOf(doc *bp.Document, o Options) Lens {
	l := Lens{Focus: doc}
	if !doc.IsDomain() {
		l.Plan, l.Missing, l.Cycle = PlanPath(doc, o.Docs)
	}
	l.Domains = domainContext(doc, o.Domains)
	switch {
	case doc.IsDomain():
		if sib, ok := o.Domains.(Siblings); ok && doc.DomainName() != "" {
			for _, c := range sib.Children(doc.Team(), doc.DomainName()) {
				if c != doc {
					l.Children = append(l.Children, c)
				}
			}
		}
	case o.Docs != nil && doc.KalkaID() != "":
		for _, c := range children(o.Docs, doc.KalkaID()) {
			if c != doc && !strings.EqualFold(c.KalkaID(), doc.KalkaID()) {
				l.Children = append(l.Children, c)
			}
		}
	}
	return l
}

// DomainNodes are the domain nodes of the lens's paths, root first, each
// version once.
func (l Lens) DomainNodes() []*bp.Document {
	var out []*bp.Document
	seen := map[string]bool{}
	for _, p := range l.Domains.Paths {
		for _, n := range p.Nodes {
			if k := domainKey(n.DomainName(), n.DomainVersion()); !seen[k] {
				seen[k] = true
				out = append(out, n)
			}
		}
	}
	return out
}
