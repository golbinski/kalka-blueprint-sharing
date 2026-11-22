package core

// The plan graph in the document view (spec 6.18): breadcrumbs from the
// root plan node, the goal tree with computed states, and the owner's
// confirmation that a document is delivered.

import (
	"errors"
	"fmt"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/check"
	"github.com/golbinski/kalka/internal/fsutil"
)

// Crumb is one plan node on the way to a document, or one of its children.
type Crumb struct {
	KalkaID string `json:"kalka_id"`
	Rel     string `json:"rel,omitempty"`
	Title   string `json:"title"`
	Issue   string `json:"issue,omitempty"`
	Closed  bool   `json:"closed,omitempty"`
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
	Closed    bool             `json:"closed,omitempty"`
}

// storeDocs serves the plan graph to the checker from the documents held
// here.
type storeDocs struct{ d *Node }

func (s storeDocs) ByID(id string) *bp.Document {
	f, err := s.d.Store.Resolve(id)
	if err != nil || f == nil {
		return nil
	}
	return f.Doc
}

func (s storeDocs) Children(id string) []*bp.Document {
	var out []*bp.Document
	for _, in := range s.d.Store.List() {
		if in.Parent != id || in.Duplicate || in.ParseError != "" {
			continue
		}
		if f, err := s.d.Store.Load(in.Path); err == nil {
			out = append(out, f.Doc)
		}
	}
	return out
}

func (d *Node) crumb(doc *bp.Document) Crumb {
	c := Crumb{KalkaID: doc.KalkaID(), Title: doc.Title(), Closed: doc.IsClosed()}
	if r, ok := doc.Issue(); ok {
		c.Issue = r.String()
	}
	if f, err := d.Store.Resolve(doc.KalkaID()); err == nil && f != nil {
		c.Rel = d.rel(f.Path)
	}
	return c
}

// planView is the plan graph around doc, nil for a document outside it:
// no parent, no goals and no children.
func (d *Node) planView(doc *bp.Document) *PlanView {
	docs := storeDocs{d}
	var kids []*bp.Document
	if id := doc.KalkaID(); id != "" {
		kids = docs.Children(id)
	}
	if doc.PlanParent() == "" && len(doc.Goals()) == 0 && len(kids) == 0 {
		return nil
	}
	v := &PlanView{Path: []Crumb{}, Children: []Crumb{}, Breakdown: doc.Breakdown(), Closed: doc.IsClosed()}
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

// Close records that the owner confirms the document delivered
// (kalka_status: closed), or reopens it; a team document is released at
// once, as a promotion is (spec 6.18).
func (d *Node) Close(path string, reopen bool) error {
	for attempt := 0; ; attempt++ {
		f, err := d.Store.Load(d.resolve(path))
		if err != nil {
			return err
		}
		doc, err := bp.Parse(f.Raw)
		if err != nil {
			return err
		}
		if !d.ownsAt(f.Path, doc) {
			return fmt.Errorf("only the owner (%s) can close this document", doc.Owner())
		}
		if err := bp.Close(doc, reopen); err != nil {
			return err
		}
		err = d.Store.Update(f, doc)
		if errors.Is(err, fsutil.ErrChanged) && attempt < 3 {
			continue
		}
		if err == nil {
			what := "closed"
			if reopen {
				what = "reopened"
			}
			d.logf("%s %s", what, f.Path)
			d.releaseOwn(f.Path, what)
		}
		return err
	}
}

// AgentContext is the agent context of the document at path (spec 6.18).
func (d *Node) AgentContext(path string) (check.AgentContext, error) {
	f, err := d.Store.Load(d.resolve(path))
	if err != nil {
		return check.AgentContext{}, err
	}
	return check.Context(d.rel(f.Path), f.Doc, check.Options{Domains: storeDomains{d}, Docs: storeDocs{d}}), nil
}
