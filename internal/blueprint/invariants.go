package blueprint

import (
	"fmt"
	"regexp"
	"slices"
	"unicode/utf8"
)

// InvariantsInfo is the first word of an invariants block's fence (spec
// 6.15).
const InvariantsInfo = "invariants"

// Limits of invariants (spec 6.15).
const (
	MaxInvariants    = 100
	advisedStatement = 300
)

// Values of an invariant's status: what a blueprint does to it.
const (
	StatusNew     = "new"
	StatusChanged = "changed"
	StatusRemoved = "removed"
)

// ParentRef is the reserved name of a reference to the parent plan node,
// parent#<id> (spec 6.18); it cannot be a domain name.
const ParentRef = "parent"

// invRefRe is a reference to an invariant, goal or criterion: an id,
// parent#<id> or <domain>#<id>, the domain a dotted path.
var invRefRe = regexp.MustCompile(`^(?:([a-z0-9][a-z0-9-]*(?:\.[a-z0-9][a-z0-9-]*)*)#)?([A-Za-z][A-Za-z0-9_-]{0,31})$`)

// Invariant is one invariant of a document.
type Invariant struct {
	ID         string   `json:"id"`
	Statement  string   `json:"statement"`
	Why        string   `json:"why,omitempty"`
	VerifiedBy []string `json:"verified_by,omitempty"`
	// Refines are the ancestor domains' terms the invariant narrows.
	Refines []string `json:"refines,omitempty"`
	Status  string   `json:"status,omitempty"`
	// Section is the slug of the heading above its block: its comment
	// anchor is §<section>#<id>.
	Section string `json:"section,omitempty"`
	Line    int    `json:"line"`
}

// Live reports whether the invariant holds after the plan.
func (v *Invariant) Live() bool { return v.Status != StatusRemoved }

// InvRef is a parsed reference: Domain is "" for the document's own,
// "parent" for the parent plan node's, or a domain name.
type InvRef struct {
	Domain string
	ID     string
}

// IsParent reports a parent#<id> reference.
func (r InvRef) IsParent() bool { return r.Domain == ParentRef }

func (r InvRef) String() string {
	if r.Domain == "" {
		return r.ID
	}
	return r.Domain + "#" + r.ID
}

// ParseInvRef parses `I3`, `parent#E1` or `library.reservations#R1`.
func ParseInvRef(s string) (InvRef, bool) {
	m := invRefRe.FindStringSubmatch(s)
	if m == nil {
		return InvRef{}, false
	}
	return InvRef{Domain: m[1], ID: m[2]}, true
}

// Affect is one domain invariant a blueprint declares it touches.
type Affect struct {
	Ref  InvRef `json:"-"`
	Text string `json:"ref"`
	Line int    `json:"line"`
}

// Invariants returns the invariants of the document's valid blocks, in
// order.
func (d *Document) Invariants() []Invariant { return d.invariants }

// Affects returns the domain invariants the document declares it touches.
func (d *Document) Affects() []Affect { return d.affects }

// Invariant returns the document's invariant with this id, or nil.
func (d *Document) Invariant(id string) *Invariant {
	for i := range d.invariants {
		if d.invariants[i].ID == id {
			return &d.invariants[i]
		}
	}
	return nil
}

// buildInvariants reads the invariants blocks and checks what needs no
// other document (spec 6.15): ids, guards of the document's own
// invariants, unguarded and unverified invariants.
func (d *Document) buildInvariants(fences []fence) {
	n := 0
	byID := map[string]int{} // id → file line of its first definition
	for _, f := range fences {
		if f.word != InvariantsInfo {
			continue
		}
		n++
		what := fmt.Sprintf("invariants %d", n)
		p := &langParser{lang: invariantsLang, base: f.line}
		invs, affects := d.invariantBlock(p, f)
		section := ""
		if h := d.sectionHeading(f.line); h != nil {
			section = h.Slug
		}
		for _, v := range invs {
			if first, dup := byID[v.ID]; dup {
				p.fail("I011", v.ID, v.Line, "the id is used twice (first at line %d); give this invariant a new id", first)
				continue
			}
			byID[v.ID] = v.Line
			v.Section = section
			d.invariants = append(d.invariants, v)
		}
		d.affects = append(d.affects, affects...)
		d.blockProblems(what, "invariant ", p.errors, p.warns)
	}
	if len(d.invariants) > MaxInvariants {
		d.problem("I014", 0, fmt.Sprintf("the document has %d invariants; at most %d are read", len(d.invariants), MaxInvariants))
		d.invariants = d.invariants[:MaxInvariants]
	}
	d.invariantChecks()
}

// invariantBlock reads one block: invariant lines and, in a blueprint,
// affects lines.
func (d *Document) invariantBlock(p *langParser, f fence) ([]Invariant, []Affect) {
	if !p.version(f.info) {
		return nil, nil
	}
	items := p.parse(f.source)
	if len(items) == 0 && len(p.errors) == 0 {
		p.fail("S031", "", f.line, "the block is empty; write invariant I1: <what must hold>, or affects <domain>#<id>")
		return nil, nil
	}
	var out []Invariant
	var affects []Affect
	for _, it := range items {
		if it.Keyword == "affects" {
			if d.IsDomain() {
				p.warn("I008", "", it.Line, "affects is for blueprints; a domain document's invariants are its own, so it is ignored")
				continue
			}
			r, ok := ParseInvRef(it.Text)
			switch {
			case !ok:
				p.fail("S009", "", it.Line, "affects names an invariant as parent#<id> or <domain>#<id>, such as library#L1, not %q", it.Text)
				continue
			case r.Domain == "":
				p.fail("I017", "", it.Line, "affects names another document's invariant, parent#%s or <domain>#%s; the document's own invariants are written as invariant lines", r.ID, r.ID)
				continue
			}
			if !slices.ContainsFunc(affects, func(a Affect) bool { return a.Text == it.Text }) {
				affects = append(affects, Affect{Ref: r, Text: it.Text, Line: it.Line})
			}
			continue
		}
		v := Invariant{ID: it.Name, Statement: it.Text, Line: it.Line, Why: it.one("why"),
			VerifiedBy: it.values("verified-by"), Refines: it.values("refines"), Status: it.one("status")}
		if v.Status != "" && d.IsDomain() {
			p.warn("I008", v.ID, it.Line, "status is for blueprints; a domain document says what holds now, so it is ignored")
			v.Status = ""
		}
		if utf8.RuneCountInString(v.Statement) > advisedStatement {
			p.warn("I015", v.ID, it.Line, "the statement has %d characters; keep it under %d and put the rest in why", utf8.RuneCountInString(v.Statement), advisedStatement)
		}
		out = append(out, v)
	}
	return out, affects
}

// invariantChecks are the checks of spec 6.15 that need no other
// document: guards naming the document's own invariants (I020), anchors
// shared with trace steps (I016), unguarded (I030) and unverified (I031).
func (d *Document) invariantChecks() {
	own := d.DomainName() // a domain document may name itself
	guarded := map[string]bool{}
	hasTrace := false
	for _, t := range d.traces {
		if !t.Valid() {
			continue
		}
		hasTrace = true
		for _, n := range t.Nodes {
			for _, g := range n.Guards {
				r, _ := ParseInvRef(g)
				if r.Domain != "" && r.Domain == own {
					r.Domain = ""
				}
				guarded[r.String()] = true
				if r.Domain == "" && d.Invariant(r.ID) == nil {
					p := NewProblem("I020", n.Line, fmt.Sprintf("trace %d, node %s: guards %s, which is not an invariant of this document", t.Index+1, n.ID, g))
					p.Node = n.ID
					d.problems = append(d.problems, p)
				}
			}
		}
	}
	for _, v := range d.invariants {
		if v.Section != "" && d.HasNode(v.Section, v.ID) {
			d.invProblem("I016", v, "a trace step under the same heading has the id %s, so comments on either are ambiguous; give the invariant another id", v.ID)
		}
		if !v.Live() {
			continue
		}
		if hasTrace && !guarded[v.ID] {
			d.invProblem("I030", v, "no trace step guards it; add guards %s to the steps that keep it true, or say in the text why none does", v.ID)
		}
		if len(v.VerifiedBy) == 0 {
			d.invProblem("I031", v, "nothing says how it is verified; add verified-by with a test reference or a check a reviewer makes")
		}
	}
	if hasTrace {
		for _, a := range d.affects {
			if !guarded[a.Text] {
				p := NewProblem("I030", a.Line, fmt.Sprintf("the plan affects %s, and no trace step guards it; add guards %s to the steps that keep it true", a.Text, a.Text))
				p.Node = a.Text
				d.problems = append(d.problems, p)
			}
		}
	}
}

func (d *Document) invProblem(rule string, v Invariant, format string, a ...any) {
	p := NewProblem(rule, v.Line, "invariant "+v.ID+": "+fmt.Sprintf(format, a...))
	p.Node = v.ID
	d.problems = append(d.problems, p)
}

// DomainRefs are the references to domain invariants in affects and in
// trace guards, each once, with the line of its first use.
func (d *Document) DomainRefs() []Affect {
	return d.invRefs(func(r InvRef) bool {
		return r.Domain != "" && !r.IsParent() && !(d.IsDomain() && r.Domain == d.DomainName())
	})
}

// ParentRefs are the references to the parent plan node's invariants,
// parent#<id>, in affects and in trace guards.
func (d *Document) ParentRefs() []Affect {
	return d.invRefs(InvRef.IsParent)
}

func (d *Document) invRefs(keep func(InvRef) bool) []Affect {
	var out []Affect
	add := func(s string, line int) {
		r, ok := ParseInvRef(s)
		if !ok || !keep(r) {
			return
		}
		if !slices.ContainsFunc(out, func(a Affect) bool { return a.Text == s }) {
			out = append(out, Affect{Ref: r, Text: s, Line: line})
		}
	}
	for _, a := range d.affects {
		add(a.Text, a.Line)
	}
	for _, t := range d.traces {
		for _, n := range t.Nodes {
			for _, g := range n.Guards {
				add(g, n.Line)
			}
		}
	}
	return out
}

// HasInvariant reports whether slug#id names an invariant under that
// heading: a comment anchor (spec 6.15).
func (d *Document) HasInvariant(slug, id string) bool {
	v := d.Invariant(id)
	return v != nil && v.Section == slug
}

// GuardedBy are the trace steps that guard an invariant reference, as
// "trace index/node id".
func (d *Document) GuardedBy(ref string) []TraceRefNode {
	var out []TraceRefNode
	r, _ := ParseInvRef(ref)
	for _, t := range d.traces {
		for _, n := range t.Nodes {
			for _, g := range n.Guards {
				gr, _ := ParseInvRef(g)
				if gr.Domain == d.DomainName() && d.IsDomain() {
					gr.Domain = ""
				}
				if gr == r {
					out = append(out, TraceRefNode{Trace: t.Index, Node: n.ID, Section: t.Section})
				}
			}
		}
	}
	return out
}

// TraceRefNode names one step of one trace block.
type TraceRefNode struct {
	Trace   int    `json:"trace"`
	Node    string `json:"node"`
	Section string `json:"section,omitempty"`
}
