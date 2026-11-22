package blueprint

import (
	"fmt"
	"slices"
)

// GoalsInfo is the first word of a goals block's fence (spec 6.18).
const GoalsInfo = "goals"

// Kinds of goal items.
const (
	KindGoal      = "goal"
	KindCriterion = "criterion"
)

// Goal is a goal of a plan node, or an acceptance criterion of a leaf
// (spec 6.18): Kind says which.
type Goal struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Text string `json:"text"`
	// Realizes are the goals it serves: parent#<id> for the parent plan
	// node's, or, for a criterion, the id of a goal of the same document.
	Realizes []string `json:"realizes,omitempty"`
	// Steps are a criterion's given, when and then lines, in order.
	Steps []GoalStep `json:"steps,omitempty"`
	// Section is the slug of the heading above its block: its comment
	// anchor is §<section>#<id>.
	Section string `json:"section,omitempty"`
	Line    int    `json:"line"`
	// realizesAt is the line of each realizes, for diagnostics.
	realizesAt map[string]int
}

// RealizesLine is the line of g's realizes r, or g's own line.
func (g Goal) RealizesLine(r string) int {
	if l, ok := g.realizesAt[r]; ok {
		return l
	}
	return g.Line
}

// GoalStep is one given, when or then line of a criterion.
type GoalStep struct {
	Keyword string `json:"keyword"`
	Text    string `json:"text"`
	Line    int    `json:"line"`
}

// Goals returns the goals and criteria of the document's goals blocks, in
// order.
func (d *Document) Goals() []Goal { return d.goals }

// Goal returns the document's goal or criterion with this id, or nil.
func (d *Document) Goal(id string) *Goal {
	for i := range d.goals {
		if d.goals[i].ID == id {
			return &d.goals[i]
		}
	}
	return nil
}

// HasGoal reports whether slug#id names a goal or criterion under that
// heading: a comment anchor.
func (d *Document) HasGoal(slug, id string) bool {
	g := d.Goal(id)
	return g != nil && g.Section == slug
}

var stepOrder = map[string]int{"given": 0, "when": 1, "then": 2}

// buildGoals reads the goals blocks and checks what needs no other
// document: ids, the order of a criterion's steps, realizes naming the
// document's own goals, and covers in the trace (spec 6.18).
func (d *Document) buildGoals(fences []fence) {
	n := 0
	byID := map[string]int{}
	for _, f := range fences {
		if f.word != GoalsInfo {
			continue
		}
		n++
		what := fmt.Sprintf("goals %d", n)
		p := &langParser{lang: goalsLang, base: f.line}
		section := ""
		if h := d.sectionHeading(f.line); h != nil {
			section = h.Slug
		}
		if p.version(f.info) {
			items := p.parse(f.source)
			if len(items) == 0 && len(p.errors) == 0 {
				p.fail("S031", "", f.line, "the block is empty; write goal G1: <the outcome>, or criterion AC1: <what is accepted>")
			}
			for _, it := range items {
				g := Goal{Kind: it.Keyword, ID: it.Name, Text: it.Text, Line: it.Line, Section: section, Realizes: it.values("realizes"), realizesAt: it.lines("realizes")}
				last := 0
				for _, a := range it.Attrs {
					o, ok := stepOrder[a.Keyword]
					if !ok {
						continue
					}
					if o < last {
						p.fail("P002", g.ID, a.Line, "%s comes after a %s; a criterion's steps are given, then when, then then", a.Keyword, map[int]string{1: "when", 2: "then"}[last])
					}
					last = max(last, o)
					g.Steps = append(g.Steps, GoalStep{Keyword: a.Keyword, Text: a.Value, Line: a.Line})
				}
				if g.Kind == KindCriterion && !slices.ContainsFunc(g.Steps, func(s GoalStep) bool { return s.Keyword == "then" }) {
					p.warn("P003", g.ID, g.Line, "the criterion has no then; say what is observed when it is met: then <outcome>")
				}
				for _, r := range g.Realizes {
					ref, ok := ParseInvRef(r)
					switch {
					case !ok || (ref.Domain != "" && !ref.IsParent()):
						p.fail("P004", g.ID, g.RealizesLine(r), "realizes names a goal as parent#<id> (a goal of the parent plan node), not %q", r)
					case g.Kind == KindGoal && ref.Domain == "":
						p.fail("P004", g.ID, g.RealizesLine(r), "a goal realizes a goal of the parent plan node: write realizes parent#%s", r)
					}
				}
				if i := slices.IndexFunc(d.invariants, func(v Invariant) bool { return v.ID == g.ID }); i >= 0 {
					p.fail("P001", g.ID, g.Line, "the id is used by an invariant (line %d); goals, criteria and invariants share one set of ids, so give this %s a new id", d.invariants[i].Line, g.Kind)
					continue
				}
				if first, dup := byID[g.ID]; dup {
					p.fail("P001", g.ID, g.Line, "the id is used twice (first at line %d); give this %s a new id", first, g.Kind)
					continue
				}
				byID[g.ID] = g.Line
				d.goals = append(d.goals, g)
			}
		}
		d.blockProblems(what, "", p.errors, p.warns)
	}
	// A criterion's own realizes names a goal of this document.
	for _, g := range d.goals {
		for _, r := range g.Realizes {
			ref, ok := ParseInvRef(r)
			if !ok || ref.Domain != "" {
				continue
			}
			if o := d.Goal(ref.ID); o == nil || o.Kind != KindGoal {
				p := NewProblem("P020", g.RealizesLine(r), fmt.Sprintf("%s %s: realizes %s, which is not a goal of this document; a goal of the parent is parent#%s", g.Kind, g.ID, r, r))
				p.Node = g.ID
				d.problems = append(d.problems, p)
			}
		}
	}
	// covers names a criterion of this document.
	for _, t := range d.traces {
		for _, s := range t.Nodes {
			for _, c := range s.Covers {
				if g := d.Goal(c); g == nil || g.Kind != KindCriterion {
					line := s.Line
					if l, ok := s.coversAt[c]; ok {
						line = l
					}
					p := NewProblem("P022", line, fmt.Sprintf("%s, step %s: covers %s, which is not a criterion of this document", t.where(), s.ID, c))
					p.Node = s.ID
					d.problems = append(d.problems, p)
				}
			}
		}
	}
}

// CoveredBy are the trace steps that cover a criterion.
func (d *Document) CoveredBy(id string) []TraceRefNode {
	var out []TraceRefNode
	for _, t := range d.traces {
		for _, n := range t.Nodes {
			if slices.Contains(n.Covers, id) {
				out = append(out, TraceRefNode{Trace: t.Index, Node: n.ID, Section: t.Section})
			}
		}
	}
	return out
}
