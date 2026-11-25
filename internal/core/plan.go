package core

// The plan graph in the document view (spec 6.18): breadcrumbs from the
// root plan node, the goal tree with computed states, and the children.

import (
	"path/filepath"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/check"
	"github.com/golbinski/kalka/internal/store"
)

// Crumb is one plan node on the way to a document, or one of its children.
type Crumb struct {
	KalkaID string `json:"kalka_id"`
	Rel     string `json:"rel,omitempty"`
	Title   string `json:"title"`
	Issue   string `json:"issue,omitempty"`
}

// PlanView is the document's place in the plan graph.
type PlanView struct {
	// Path runs from the root plan node to the parent; Missing is the id of
	// a parent not held here, Cycle set when kalka_parent leads back.
	Path    []Crumb `json:"path"`
	Missing string  `json:"missing,omitempty"`
	Cycle   bool    `json:"cycle,omitempty"`
	// Goals is the goal tree with computed states; Children the documents
	// naming this one as their parent.
	Goals     []check.GoalNode `json:"goals"`
	Children  []Crumb          `json:"children"`
	Breakdown string           `json:"breakdown,omitempty"`
}

// storeDocs serves the plan graph to the checker from the documents held
// here, in one scope (spec 14): a parent is looked up there first, then
// anywhere (a Personal draft may sit under a team epic), but children only
// count in the scope, so a draft does not change a team plan's states.
// A copy's former id (kalka_from) is not followed.
type storeDocs struct {
	d     *Node
	scope string
}

// docsAt is the plan graph seen from the document at path.
func (d *Node) docsAt(path string) storeDocs {
	rel, _ := filepath.Rel(d.Store.Dir(), path)
	return storeDocs{d, store.ScopeOf(rel)}
}

func (s storeDocs) ByID(id string) *bp.Document {
	var here, elsewhere []string
	for _, in := range s.d.Store.List() {
		if !strings.EqualFold(in.KalkaID, id) || in.ParseError != "" {
			continue
		}
		if in.Scope == s.scope {
			if in.Duplicate {
				return nil
			}
			here = append(here, in.Path)
		} else {
			elsewhere = append(elsewhere, in.Path)
		}
	}
	paths := here
	if len(paths) == 0 {
		paths = elsewhere
	}
	if len(paths) != 1 {
		return nil
	}
	f, err := s.d.Store.Load(paths[0])
	if err != nil {
		return nil
	}
	return f.Doc
}

func (s storeDocs) Children(id string) []*bp.Document {
	var out []*bp.Document
	for _, in := range s.d.Store.List() {
		if !strings.EqualFold(in.Parent, id) || in.Scope != s.scope || in.Duplicate || in.ParseError != "" {
			continue
		}
		if f, err := s.d.Store.Load(in.Path); err == nil {
			out = append(out, f.Doc)
		}
	}
	return out
}

func (d *Node) crumb(doc *bp.Document) Crumb {
	c := Crumb{KalkaID: doc.KalkaID(), Title: doc.Title()}
	if r, ok := doc.Issue(); ok {
		c.Issue = r.String()
	}
	for _, in := range d.Store.List() {
		if in.KalkaID != "" && strings.EqualFold(in.KalkaID, doc.KalkaID()) && !in.Duplicate {
			c.Rel = in.Rel
			break
		}
	}
	return c
}

// planView is the plan graph around doc, nil for a document outside it:
// no parent, no goals and no children.
func (d *Node) planView(at string, doc *bp.Document) *PlanView {
	docs := d.docsAt(at)
	var kids []*bp.Document
	if id := doc.KalkaID(); id != "" {
		kids = docs.Children(id)
	}
	if doc.PlanParent() == "" && len(doc.Goals()) == 0 && len(kids) == 0 {
		return nil
	}
	v := &PlanView{Path: []Crumb{}, Children: []Crumb{}, Breakdown: doc.Breakdown()}
	path, missing, cycle := check.PlanPath(doc, docs)
	for _, p := range path {
		v.Path = append(v.Path, d.crumb(p))
	}
	v.Missing, v.Cycle = missing, cycle
	v.Goals = check.GoalTree(doc, docs)
	for _, k := range kids {
		v.Children = append(v.Children, d.crumb(k))
	}
	return v
}

// AgentContext is the agent context of the document at path (spec 6.18),
// focused or full.
func (d *Node) AgentContext(path string, full bool) (check.AgentContext, error) {
	f, err := d.Store.Load(d.resolve(path))
	if err != nil {
		return check.AgentContext{}, err
	}
	return check.Context(d.rel(f.Path), f.Doc, check.Options{Domains: storeDomains{d}, Docs: d.docsAt(f.Path), Approval: d.approvalOf}, full), nil
}
