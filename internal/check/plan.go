package check

import (
	"fmt"
	"slices"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// The plan graph (spec 6.18): a document names its parent plan node with
// kalka_parent; goals are claimed by child goals and acceptance criteria,
// and their states are computed, never written.

// Docs finds documents of the plan graph by kalka_id.
type Docs interface {
	// ByID is the document with this kalka_id, nil when none is here.
	ByID(id string) *bp.Document
	// Children are the documents whose kalka_parent is id.
	Children(id string) []*bp.Document
}

// States of goals and criteria (spec 6.18).
const (
	GoalUnclaimed = "unclaimed" // nothing realizes it
	GoalClaimed   = "claimed"   // something does, not all of it delivered
	GoalReady     = "ready"     // every claimant delivered: ready to close
	GoalDelivered = "delivered" // ready, and its document closed
	CriterionOpen = "open"      // its document is not closed
)

// PlanPath is the chain of plan nodes from the root to doc's parent, root
// first. Cycle is set when kalka_parent leads back; Missing is the id of
// the first parent not here.
func PlanPath(doc *bp.Document, docs Docs) (path []*bp.Document, missing string, cycle bool) {
	seen := map[string]bool{doc.KalkaID(): true}
	cur := doc
	for len(path) < maxDepth {
		id := cur.PlanParent()
		if id == "" {
			return path, "", false
		}
		if seen[id] {
			return path, "", true
		}
		seen[id] = true
		var p *bp.Document
		if docs != nil {
			p = docs.ByID(id)
		}
		if p == nil {
			return path, id, false
		}
		path = append([]*bp.Document{p}, path...)
		cur = p
	}
	return path, "", true
}

// GoalNode is a goal or a criterion in the goal tree, with what claims it.
type GoalNode struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Text  string `json:"text"`
	State string `json:"state"`
	// Doc is the kalka_id of the document that holds it, Title its title,
	// Issue its kalka_issue; Own is set for the document viewed.
	Doc   string `json:"doc,omitempty"`
	Title string `json:"title,omitempty"`
	Issue string `json:"issue,omitempty"`
	Own   bool   `json:"own,omitempty"`
	// Section is the heading above its block: an own item's comment
	// anchor is §<section>#<id>.
	Section string `json:"section,omitempty"`
	Line    int    `json:"line,omitempty"`
	// Realizes are what it names; Steps a criterion's given/when/then.
	Realizes []string      `json:"realizes,omitempty"`
	Steps    []bp.GoalStep `json:"steps,omitempty"`
	// CoveredBy are the trace steps that cover a criterion.
	CoveredBy []bp.TraceRefNode `json:"covered_by,omitempty"`
	Claimants []GoalNode        `json:"claimants,omitempty"`
}

// GoalTree is doc's goals, each with the child goals and criteria that
// claim it, recursively, and the computed states (spec 6.18). Criteria
// that realize a goal of the parent are listed on their own.
func GoalTree(doc *bp.Document, docs Docs) []GoalNode {
	out := []GoalNode{}
	seen := map[string]bool{doc.KalkaID(): true}
	for _, g := range doc.Goals() {
		if g.Kind == bp.KindGoal {
			out = append(out, goalNode(doc, g, docs, seen, 0, true))
		}
	}
	for _, g := range doc.Goals() {
		if g.Kind == bp.KindCriterion && !realizesOwn(g) {
			out = append(out, criterionNode(doc, g, true))
		}
	}
	return out
}

func realizesOwn(g bp.Goal) bool {
	for _, r := range g.Realizes {
		if ref, ok := bp.ParseInvRef(r); ok && ref.Domain == "" {
			return true
		}
	}
	return false
}

func criterionNode(doc *bp.Document, g bp.Goal, own bool) GoalNode {
	n := GoalNode{Kind: g.Kind, ID: g.ID, Text: g.Text, Doc: doc.KalkaID(), Title: doc.Title(), Own: own, Section: g.Section, Line: g.Line,
		Realizes: g.Realizes, Steps: g.Steps, State: CriterionOpen}
	if r, ok := doc.Issue(); ok {
		n.Issue = r.String()
	}
	if doc.IsClosed() {
		n.State = GoalDelivered
	}
	n.CoveredBy = doc.CoveredBy(g.ID)
	return n
}

func goalNode(doc *bp.Document, g bp.Goal, docs Docs, seen map[string]bool, depth int, own bool) GoalNode {
	n := GoalNode{Kind: g.Kind, ID: g.ID, Text: g.Text, Doc: doc.KalkaID(), Title: doc.Title(), Own: own, Section: g.Section, Line: g.Line, Realizes: g.Realizes}
	if r, ok := doc.Issue(); ok {
		n.Issue = r.String()
	}
	// Own criteria realizing it.
	for _, c := range doc.Goals() {
		if c.Kind == bp.KindCriterion && slices.Contains(c.Realizes, g.ID) {
			n.Claimants = append(n.Claimants, criterionNode(doc, c, own))
		}
	}
	// Children's goals and criteria realizing parent#<id>.
	if docs != nil && doc.KalkaID() != "" && depth < maxDepth {
		for _, ch := range children(docs, doc.KalkaID()) {
			if seen[ch.KalkaID()] {
				continue
			}
			for _, c := range ch.Goals() {
				if !slices.Contains(c.Realizes, bp.ParentRef+"#"+g.ID) {
					continue
				}
				if c.Kind == bp.KindGoal {
					seen[ch.KalkaID()] = true
					n.Claimants = append(n.Claimants, goalNode(ch, c, docs, seen, depth+1, false))
					delete(seen, ch.KalkaID())
				} else {
					n.Claimants = append(n.Claimants, criterionNode(ch, c, false))
				}
			}
		}
	}
	n.State = goalState(doc, n.Claimants)
	return n
}

// children are a document's children in a stable order: by title, then id.
func children(docs Docs, id string) []*bp.Document {
	cs := slices.Clone(docs.Children(id))
	slices.SortStableFunc(cs, func(a, b *bp.Document) int {
		if c := strings.Compare(a.Title(), b.Title()); c != 0 {
			return c
		}
		return strings.Compare(a.KalkaID(), b.KalkaID())
	})
	return cs
}

func goalState(doc *bp.Document, claimants []GoalNode) string {
	if len(claimants) == 0 {
		return GoalUnclaimed
	}
	for _, c := range claimants {
		if c.State != GoalDelivered && c.State != GoalReady {
			return GoalClaimed
		}
	}
	if doc.IsClosed() {
		return GoalDelivered
	}
	return GoalReady
}

// planProblems are the plan-graph rules of doc (P010 to P032, and I020
// for parent#<id> invariants).
func planProblems(doc *bp.Document, docs Docs) []bp.Problem {
	var out []bp.Problem
	fm := doc.Frontmatter()
	parentLine := fm.KeyLine(bp.KeyParent)
	path, missing, cycle := PlanPath(doc, docs)
	hasParent := doc.PlanParent() != ""
	checked := docs != nil
	switch {
	case !hasParent:
	case cycle:
		out = append(out, bp.NewProblem("P012", parentLine, "following kalka_parent leads back to this document; the plan is a tree, so its root has no kalka_parent"))
	case !checked:
		out = append(out, bp.NewProblem("P011", parentLine, "the parent plan node was not looked up, so the rules that need it were not run"))
	case missing != "" && len(path) == 0:
		out = append(out, bp.NewProblem("P010", parentLine, fmt.Sprintf("kalka_parent names %s, which is not here; check the document together with its parent, or fix the id", missing)))
	}
	var parent *bp.Document
	if len(path) > 0 && !cycle && path[len(path)-1].KalkaID() == doc.PlanParent() {
		parent = path[len(path)-1]
	}
	// parent#<id> with no parent (P021), goals of the parent (P020).
	usesParent := len(doc.ParentRefs()) > 0
	for _, g := range doc.Goals() {
		for _, r := range g.Realizes {
			ref, ok := bp.ParseInvRef(r)
			if !ok || !ref.IsParent() {
				continue
			}
			usesParent = true
			if parent == nil {
				continue
			}
			if pg := parent.Goal(ref.ID); pg == nil || pg.Kind != bp.KindGoal {
				msg := fmt.Sprintf("%s %s: realizes %s, and %q has no goal %s", g.Kind, g.ID, r, parent.Title(), ref.ID)
				for i := len(path) - 2; i >= 0; i-- {
					if ag := path[i].Goal(ref.ID); ag != nil && ag.Kind == bp.KindGoal {
						msg = fmt.Sprintf("%s %s: realizes %s, a goal of %q, which is not the direct parent; realize a goal of %q that serves it", g.Kind, g.ID, r, path[i].Title(), parent.Title())
						break
					}
				}
				p := bp.NewProblem("P020", g.Line, msg)
				p.Node = g.ID
				out = append(out, p)
			}
		}
	}
	if usesParent && !hasParent {
		out = append(out, bp.NewProblem("P021", 1, "the document writes parent#<id> and has no kalka_parent; name the parent plan node with kalka_parent: <its kalka_id>"))
	}
	// Inherited invariants: parent#<id> resolves on the plan path; an own
	// invariant may not reuse an inherited id (P023).
	if !cycle && len(path) > 0 {
		inherited := map[string]*bp.Document{}
		for _, a := range path {
			for _, v := range a.Invariants() {
				if _, ok := inherited[v.ID]; !ok {
					inherited[v.ID] = a
				}
			}
		}
		for _, v := range doc.Invariants() {
			if a, ok := inherited[v.ID]; ok {
				p := bp.NewProblem("P023", v.Line, fmt.Sprintf("invariant %s: %q already has an invariant %s, which this document inherits and may not loosen or remove; name it with affects parent#%s, and give a new invariant a new id", v.ID, a.Title(), v.ID, v.ID))
				p.Node = v.ID
				out = append(out, p)
			}
		}
		if parent != nil {
			for _, a := range doc.ParentRefs() {
				if _, ok := inherited[a.Ref.ID]; !ok {
					p := bp.NewProblem("I020", a.Line, fmt.Sprintf("%s: %q and the plan nodes above it have no invariant %s", a.Text, parent.Title(), a.Ref.ID))
					p.Node = a.Text
					out = append(out, p)
				}
			}
		}
	}
	// Goal coverage: unclaimed is a warning while open, an error once
	// closed; with an adopted breakdown, a finding to decide.
	if !checked && len(doc.Goals()) > 0 && doc.KalkaID() != "" {
		return out
	}
	adopted := doc.Breakdown() == bp.BreakdownAdopted
	for _, n := range GoalTree(doc, docs) {
		if n.Kind != bp.KindGoal {
			continue
		}
		var rule, msg string
		switch {
		case n.State == GoalUnclaimed && adopted:
			rule, msg = "P032", "no story claims it; decide whether one is missing, or say in the plan why none is needed"
		case n.State == GoalUnclaimed && doc.IsClosed():
			rule, msg = "P031", "the document is closed and nothing claims this goal; claim it with a child goal or a criterion (realizes parent#"+n.ID+"), or reopen the document"
		case n.State == GoalUnclaimed:
			rule, msg = "P030", "no child goal and no criterion claims it yet (realizes parent#"+n.ID+")"
		case n.State == GoalClaimed && doc.IsClosed() && adopted:
			rule, msg = "P032", "the document is closed and not everything claiming this goal is delivered: "+undelivered(n)
		case n.State == GoalClaimed && doc.IsClosed():
			rule, msg = "P031", "the document is closed and not everything claiming this goal is delivered: "+undelivered(n)
		default:
			continue
		}
		p := bp.NewProblem(rule, n.Line, fmt.Sprintf("goal %s: %s", n.ID, msg))
		p.Node = n.ID
		out = append(out, p)
	}
	return out
}

// undelivered names a goal's claimants that are not delivered.
func undelivered(n GoalNode) string {
	var out []string
	for _, c := range n.Claimants {
		if c.State != GoalDelivered && c.State != GoalReady {
			s := c.Kind + " " + c.ID
			if !c.Own {
				s += " of " + strconvQuote(c.Title)
			}
			out = append(out, s+" ("+c.State+")")
		}
	}
	return strings.Join(out, ", ")
}

func strconvQuote(s string) string { return fmt.Sprintf("%q", s) }
