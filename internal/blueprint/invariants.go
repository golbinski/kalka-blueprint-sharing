package blueprint

import (
	"fmt"
	"regexp"
	"slices"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
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

var (
	invBlockKeys = []string{"invariants", "affects"}
	invKeys      = []string{"id", "statement", "context", "why", "verified_by", "status"}
	invCodes     = blockCodes{parse: "I001", feature: "I002", dupKey: "I003", shape: "I004", version: "I005", noVersion: "I006", unknownKey: "I007"}
	// invRefRe is an invariant reference: an id, or <domain>#<id>.
	invRefRe = regexp.MustCompile(`^(?:([a-z0-9][a-z0-9-]*)#)?([A-Za-z][A-Za-z0-9_-]{0,31})$`)
)

// Invariant is one invariant of a document.
type Invariant struct {
	ID         string   `json:"id"`
	Statement  string   `json:"statement"`
	Context    string   `json:"context,omitempty"`
	Why        string   `json:"why,omitempty"`
	VerifiedBy []string `json:"verified_by,omitempty"`
	Status     string   `json:"status,omitempty"`
	// Section is the slug of the heading above its block: its comment
	// anchor is §<section>#<id>.
	Section string `json:"section,omitempty"`
	Line    int    `json:"line"`
}

// Live reports whether the invariant holds after the plan.
func (v *Invariant) Live() bool { return v.Status != StatusRemoved }

// InvRef is a parsed invariant reference.
type InvRef struct {
	Domain string // "" for the document's own
	ID     string
}

func (r InvRef) String() string {
	if r.Domain == "" {
		return r.ID
	}
	return r.Domain + "#" + r.ID
}

// ParseInvRef parses `I3` or `parcel-delivery#I3`.
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
		p := &blockParser{codes: invCodes, base: f.line}
		invs, affects := d.invariantBlock(p, f)
		section := ""
		if h := d.sectionHeading(f.line); h != nil {
			section = h.Slug
		}
		var keep []Invariant
		for _, v := range invs {
			if first, dup := byID[v.ID]; dup {
				p.errors = append(p.errors, TraceError{Code: "I011", Node: v.ID, Line: v.Line, Message: fmt.Sprintf("the id is used twice (first at line %d); give this invariant a new id", first)})
				continue
			}
			byID[v.ID] = v.Line
			v.Section = section
			keep = append(keep, v)
		}
		if len(p.errors) == 0 || onlyCode(p.errors, "I011") {
			d.invariants = append(d.invariants, keep...)
			d.affects = append(d.affects, affects...)
		}
		d.blockProblems(what, p.errors, p.warns)
	}
	if len(d.invariants) > MaxInvariants {
		d.problem("I014", 0, fmt.Sprintf("the document has %d invariants; at most %d are read", len(d.invariants), MaxInvariants))
		d.invariants = d.invariants[:MaxInvariants]
	}
	d.invariantChecks()
}

// invariantBlock reads one block: a mapping with invariants and, in a
// blueprint, affects.
func (d *Document) invariantBlock(p *blockParser, f fence) ([]Invariant, []Affect) {
	if !p.version(f.info, InvariantsInfo) {
		return nil, nil
	}
	root := p.read(f.source)
	if root == nil {
		if len(p.errors) == 0 {
			p.fail("I004", "", 1, "the block is a mapping with invariants: and a list of them")
		}
		return nil, nil
	}
	if root.Kind != yaml.MappingNode {
		p.fail("I004", "", root.Line, "the block is a mapping with invariants: and a list of them")
		return nil, nil
	}
	p.unknownKeys(root, invBlockKeys, "")
	var affects []Affect
	if v := mapValue(root, "affects"); !absent(v) {
		if d.IsDomain() {
			p.warn("I008", "", v.Line, "affects is for blueprints; a domain document's invariants are its own, so it is ignored")
		} else if items, ok := p.texts(root, "affects", ""); ok {
			for _, s := range items {
				r, ok := ParseInvRef(s)
				if !ok || r.Domain == "" {
					p.fail("I004", "", v.Line, "affects names domain invariants as <domain>#<id>, such as \"loans#I3\", not %q", s)
					continue
				}
				if !slices.ContainsFunc(affects, func(a Affect) bool { return a.Text == s }) {
					affects = append(affects, Affect{Ref: r, Text: s, Line: p.base + v.Line})
				}
			}
		}
	}
	list := mapValue(root, "invariants")
	switch {
	case absent(list) && len(affects) > 0:
		return nil, affects // a plan may only touch the domain's invariants
	case absent(list) || list.Kind != yaml.SequenceNode || len(list.Content) == 0:
		line := root.Line
		if list != nil {
			line = list.Line
		}
		p.fail("I004", "", line, "invariants is a list, each entry starting \"- id: I1\"")
		return nil, nil
	}
	var out []Invariant
	for _, e := range list.Content {
		if e.Kind != yaml.MappingNode || e.Style&yaml.FlowStyle != 0 {
			p.fail("I004", "", e.Line, "an invariant is a block mapping, one key per line, starting \"- id: I1\"")
			continue
		}
		v := Invariant{Line: p.base + e.Line}
		var ok [6]bool
		v.ID, ok[0] = p.text(e, "id", "")
		node := v.ID
		p.unknownKeys(e, invKeys, node)
		v.Statement, ok[1] = p.text(e, "statement", node)
		v.Context, ok[2] = p.text(e, "context", node)
		v.Why, ok[3] = p.text(e, "why", node)
		v.VerifiedBy, ok[4] = p.texts(e, "verified_by", node)
		v.Status, ok[5] = p.text(e, "status", node)
		if ok != [6]bool{true, true, true, true, true, true} {
			continue
		}
		switch {
		case v.ID == "" || utf8.RuneCountInString(v.ID) > maxNodeID || !nodeIDRe.MatchString(v.ID):
			p.fail("I010", node, e.Line, "an id starts with a letter, uses only letters, digits, - and _, and has at most %d characters, such as I1", maxNodeID)
			continue
		case v.Statement == "":
			p.fail("I012", node, e.Line, "the invariant has no statement; say in one sentence what must hold")
			continue
		}
		switch v.Status {
		case "", StatusNew, StatusChanged, StatusRemoved:
		default:
			p.fail("I013", node, e.Line, "status is new, changed or removed, not %q; leave it out for an unchanged invariant", v.Status)
			continue
		}
		if v.Status != "" && d.IsDomain() {
			p.warn("I008", node, e.Line, "status is for blueprints; a domain document says what holds now, so it is ignored")
			v.Status = ""
		}
		if utf8.RuneCountInString(v.Statement) > advisedStatement {
			p.warn("I015", node, e.Line, "the statement has %d characters; keep it under %d and put the rest in why", utf8.RuneCountInString(v.Statement), advisedStatement)
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
			d.invProblem("I030", v, "no trace step guards it; add guards: [%s] to the steps that keep it true, or say in the text why none does", v.ID)
		}
		if len(v.VerifiedBy) == 0 {
			d.invProblem("I031", v, "nothing says how it is verified; add verified_by with a test reference or a check a reviewer makes")
		}
	}
	if hasTrace {
		for _, a := range d.affects {
			if !guarded[a.Text] {
				p := NewProblem("I030", a.Line, fmt.Sprintf("the plan affects %s, and no trace step guards it; add guards: [\"%s\"] to the steps that keep it true", a.Text, a.Text))
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
	var out []Affect
	add := func(s string, line int) {
		r, ok := ParseInvRef(s)
		if !ok || r.Domain == "" || (d.IsDomain() && r.Domain == d.DomainName()) {
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
