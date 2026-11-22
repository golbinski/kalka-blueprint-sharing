package blueprint

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
)

// TraceInfo is the first word of a trace block's fence (spec 6.10).
const TraceInfo = "trace"

// Limits of a trace block (spec 6.10).
const (
	MaxTraceNodes     = 40
	AdvisedTraceNodes = 20
	MinTraceLanes     = 2
	MaxTraceLanes     = 5
	maxLaneName       = 40
	maxNodeID         = 32
	advisedText       = 100
	advisedWhy        = 300
	advisedLabel      = 40
)

// Values of a node's change key: what the plan does to the step. Absent
// means unchanged.
const (
	ChangeNew      = "new"
	ChangeModified = "modified"
	ChangeRemoved  = "removed"
)

// Divergence classes (spec 6.10). The library computes the graph classes;
// Unverified and Stale depend on the reader's plugins and are added by the
// daemon.
const (
	DivImplementationOnly = "implementation-only"
	DivUnrealized         = "unrealized"
	DivMerged             = "merged"
	DivExpanded           = "expanded"
	DivScattered          = "scattered"
	DivReordered          = "reordered"
	DivSkipped            = "skipped"
	DivUnverified         = "unverified"
	DivStale              = "stale"
)

// Trace is one trace block of the body. A block with errors has no nodes;
// it renders as code and never blocks a sync. Warnings leave it a trace.
type Trace struct {
	// Index is the block's position among the document's trace blocks.
	Index int `json:"index"`
	// Line is the file line of the opening fence.
	Line int `json:"line"`
	// Section is the slug of the nearest heading above the block (the
	// anchor prefix for its nodes); Title is that heading's text.
	Section string `json:"section,omitempty"`
	Title   string `json:"title,omitempty"`
	// Nested is set for a block inside a list or block quote.
	Nested bool   `json:"nested,omitempty"`
	Source string `json:"source"`
	// Version is the language version the fence names: 2 when it names
	// none, or the number this reader does not interpret (S012).
	Version     int          `json:"version"`
	Lanes       []string     `json:"lanes,omitempty"`
	Nodes       []TraceNode  `json:"nodes,omitempty"`
	Errors      []TraceError `json:"errors,omitempty"`
	Warnings    []TraceError `json:"warnings,omitempty"`
	Divergences []Divergence `json:"divergences,omitempty"`
}

// TraceNode is one step in one lane.
type TraceNode struct {
	ID       string      `json:"id"`
	Lane     string      `json:"lane"`
	Text     string      `json:"text"`
	Next     []TraceEdge `json:"next,omitempty"`
	Back     []TraceEdge `json:"back,omitempty"`
	Realizes []string    `json:"realizes,omitempty"`
	Ref      string      `json:"ref,omitempty"`
	Why      string      `json:"why,omitempty"`
	Change   string      `json:"change,omitempty"`
	// Guards are the invariants the step keeps true (spec 6.15): ids of the
	// document's own, or <domain>#<id>. The invariant rules resolve them.
	Guards []string `json:"guards,omitempty"`
	// Covers are the acceptance criteria the step demonstrates: ids of the
	// document's criteria (goals blocks).
	Covers []string `json:"covers,omitempty"`
	// Line is the file line where the node starts.
	Line int `json:"line"`
	// coversAt is the line of each covers, for diagnostics.
	coversAt map[string]int
}

// Live reports whether the step exists after the plan.
func (n *TraceNode) Live() bool { return n.Change != ChangeRemoved }

// TraceEdge is a next or back edge, optionally labelled.
type TraceEdge struct {
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

// TraceError is a validation error or warning: an identifier (T001…), a
// file line, the node it concerns if any, and what to write instead.
type TraceError struct {
	Code    string `json:"code"`
	Node    string `json:"node,omitempty"`
	Line    int    `json:"line"`
	Message string `json:"message"`
	// Fix is a suggested replacement, for the lexical rules.
	Fix *Fix `json:"fix,omitempty"`
}

// Divergence is a place where two lanes stop lining up. Node is the node
// the badge goes on; Nodes are the others involved, and Lane, for the
// classes that compare two lanes, is the other lane.
type Divergence struct {
	Class string   `json:"class"`
	Node  string   `json:"node"`
	Nodes []string `json:"nodes,omitempty"`
	Lane  string   `json:"lane,omitempty"`
	// Detail is set by the daemon for Unverified and Stale.
	Detail string `json:"detail,omitempty"`
}

// Valid reports whether the block passed validation.
func (t *Trace) Valid() bool { return len(t.Errors) == 0 }

// Node returns the node with the given id, or nil.
func (t *Trace) Node(id string) *TraceNode {
	for i := range t.Nodes {
		if t.Nodes[i].ID == id {
			return &t.Nodes[i]
		}
	}
	return nil
}

// Traces returns the body's trace blocks in document order.
func (d *Document) Traces() []Trace { return d.traces }

// HasNode reports whether a trace block under the section slug has a node
// with this id, which is what a §slug#node anchor needs.
func (d *Document) HasNode(slug, id string) bool {
	for i := range d.traces {
		t := &d.traces[i]
		if t.Section == slug && t.Node(id) != nil {
			return true
		}
	}
	return false
}

// HasAnchor reports whether an entry anchor (without §) names something
// in the body: a heading, and for slug#id a trace node or an invariant
// under it.
func (d *Document) HasAnchor(anchor string) bool {
	slug, id, hasID := strings.Cut(anchor, "#")
	if !d.HasSection(slug) {
		return false
	}
	return !hasID || d.HasNode(slug, id) || d.HasInvariant(slug, id) || d.HasGoal(slug, id)
}

// fence is a typed block's fence (trace, glossary, invariants, goals),
// found while parsing, before validation.
type fence struct {
	line   int    // file line of the opening fence
	info   string // the whole info string
	word   string // its first word: trace, glossary, invariants or goals
	nested bool
	source string
}

// findFences collects the typed blocks (trace, glossary, invariants,
// goals) from a parsed
// part of the body. lineAt maps a byte offset of src to a 0-based line of
// the part, and keep filters by that line (to leave out the review log).
func findFences(root ast.Node, src []byte, lineAt func(int) int, base int, keep func(int) bool) []fence {
	var out []fence
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		fc, ok := n.(*ast.FencedCodeBlock)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		info := ""
		if fc.Info != nil {
			info = strings.TrimSpace(string(fc.Info.Segment.Value(src)))
		}
		word, _, _ := strings.Cut(strings.ReplaceAll(info, "\t", " "), " ")
		if word != TraceInfo && word != GlossaryInfo && word != InvariantsInfo && word != GoalsInfo {
			return ast.WalkSkipChildren, nil
		}
		var b strings.Builder
		open := -1
		lines := fc.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			if i == 0 {
				open = lineAt(seg.Start) - 1
			}
			b.Write(seg.Value(src))
		}
		if open < 0 {
			open = lineAt(fc.Info.Segment.Start)
		}
		if !keep(open) {
			return ast.WalkSkipChildren, nil
		}
		out = append(out, fence{line: base + open + 1, info: info, word: word, nested: fc.Parent() != root, source: b.String()})
		return ast.WalkSkipChildren, nil
	})
	return out
}

// buildTraces validates the fences, places them in sections and computes
// divergences. Its errors and warnings are diagnostics of the document; a
// trace never blocks a sync (rules T…, which are not Blocks).
func (d *Document) buildTraces(fences []fence) {
	for _, f := range fences {
		if f.word != TraceInfo {
			continue
		}
		t := parseTrace(f.source, f.line, f.info)
		t.Index, t.Line, t.Nested = len(d.traces), f.line, f.nested
		if h := d.sectionHeading(f.line); h != nil {
			t.Section, t.Title = h.Slug, h.Text
		} else if t.Valid() {
			t.Warnings = append(t.Warnings, TraceError{Code: "T121", Line: f.line, Message: "the block has no heading above it, so its steps cannot be commented on; put it under a heading"})
		}
		if t.Valid() {
			t.Divergences = divergences(&t)
		}
		d.traces = append(d.traces, t)
	}
	// Node anchors are §slug#id: the same id in two blocks under one
	// heading makes them ambiguous.
	seen := map[string]int{}
	for i := range d.traces {
		t := &d.traces[i]
		for _, n := range t.Nodes {
			k := t.Section + "#" + n.ID
			if first, dup := seen[k]; dup && first != t.Index {
				t.Warnings = append(t.Warnings, TraceError{Code: "T120", Node: n.ID, Line: n.Line,
					Message: fmt.Sprintf("trace %d under the same heading also has a step %s, so comments on it are ambiguous; use another id prefix in this block", first+1, n.ID)})
			}
			if _, ok := seen[k]; !ok {
				seen[k] = t.Index
			}
		}
	}
	for _, t := range d.traces {
		where := t.where()
		for _, e := range append(append([]TraceError(nil), t.Errors...), t.Warnings...) {
			d.problems = append(d.problems, e.problem(where, "step "))
		}
	}
}

// problem is the diagnostic of a block's error or warning; where names the
// block, and what the kind of item a node is ("step ").
func (e TraceError) problem(where, what string) Problem {
	msg := where + ": " + e.Message
	if e.Node != "" {
		msg = where + ", " + what + e.Node + ": " + e.Message
	}
	p := NewProblem(e.Code, e.Line, msg)
	p.Node = e.Node
	p.Fix = e.Fix
	if e.Fix != nil {
		p.Column, p.EndLine, p.EndColumn = e.Fix.Column, e.Fix.EndLine, e.Fix.EndColumn
	}
	return p
}

// sectionHeading is the nearest heading above a body line, or nil.
// Headings after the review log do not cover lines before it.
func (d *Document) sectionHeading(line int) *Heading {
	var h *Heading
	for i := range d.headings {
		hh := &d.headings[i]
		if hh.Nested || hh.Line > line {
			continue
		}
		if d.hasLog && hh.Line < d.tailLine && line >= d.tailLine {
			continue // a heading before the log does not own text after it
		}
		h = hh
	}
	return h
}

var refFormRe = regexp.MustCompile(`^[a-z][a-z0-9+.-]*:\S+$`)

// parseTrace reads and validates one block (docs/trace.md, 4). The stages
// run in order, each only if the ones before found no error.
func parseTrace(src string, fenceLine int, info string) Trace {
	t := Trace{Source: src, Version: LangVersion, Line: fenceLine}
	lp := &langParser{lang: traceLang, base: fenceLine}
	ok := lp.version(info)
	var items []langItem
	if ok {
		items = lp.parse(src)
	}
	t.Errors, t.Warnings = lp.errors, lp.warns
	if !ok {
		t.Version = 0
		if v := strings.Fields(info); len(v) > 1 && langVerRe.MatchString(v[1]) {
			fmt.Sscanf(v[1], "v%d", &t.Version)
		}
		return t
	}
	p := &traceParser{t: &t}

	// Stage 1: lanes and steps.
	var lanes []string
	var raws []TraceNode
	for _, it := range items {
		switch it.Keyword {
		case "lane":
			switch {
			case utf8.RuneCountInString(it.Text) > maxLaneName:
				p.fail("T012", "", it.Line, "lane name %q is longer than %d characters", it.Text, maxLaneName)
			case slices.Contains(lanes, it.Text):
				p.fail("T012", "", it.Line, "lane %s is opened twice; put all its steps under one lane line", it.Text)
			}
			lanes = append(lanes, it.Text)
			p.laneLine = append(p.laneLine, it.Line)
		case "step":
			if it.Section == "" {
				p.fail("T032", it.Name, it.Line, "the step comes before any lane; start the block with lane business (the most abstract level)")
				continue
			}
			n := TraceNode{ID: it.Name, Lane: it.Section, Text: it.Text, Line: it.Line,
				Ref: it.one("ref"), Why: it.one("why"), Change: it.one("change")}
			for _, a := range it.all("next") {
				n.Next = p.edge(n.Next, "next", n.ID, a)
			}
			for _, a := range it.all("back") {
				n.Back = p.edge(n.Back, "back", n.ID, a)
			}
			n.Realizes = p.ids(it, "realizes")
			n.Covers = p.ids(it, "covers")
			n.coversAt = it.lines("covers")
			for _, g := range p.ids(it, "guards") {
				if _, ok := ParseInvRef(g); !ok {
					p.fail("S009", n.ID, it.Line, "guards names an invariant by id (I1), as parent#<id> or as <domain>#<id> (library#L1), not %q", g)
					continue
				}
				n.Guards = append(n.Guards, g)
			}
			p.stepWarnings(&n, it)
			raws = append(raws, n)
		}
	}
	if len(raws) == 0 && len(t.Errors) == 0 {
		p.fail("T020", "", fenceLine, "the block has no steps; write lane business, then step B1: <what happens> under it")
	}
	if len(raws) > MaxTraceNodes {
		p.fail("T022", "", fenceLine, "%d steps; a block has at most %d, so split it under several headings", len(raws), MaxTraceNodes)
	}
	if len(lanes) > 0 && (len(lanes) < MinTraceLanes || len(lanes) > MaxTraceLanes) {
		p.fail("T011", "", p.laneLine[0], "%d lanes; a trace has %d to %d, most abstract first", len(lanes), MinTraceLanes, MaxTraceLanes)
	}
	if len(t.Errors) > 0 {
		return t
	}

	// Stage 2: ids.
	laneOf := map[string]int{}
	for i, l := range lanes {
		laneOf[l] = i
	}
	byID := map[string]*TraceNode{}
	for i := range raws {
		n := &raws[i]
		if byID[n.ID] != nil {
			p.fail("T031", n.ID, n.Line, "the id is used twice (first at line %d); give this step a new id", byID[n.ID].Line)
			continue
		}
		byID[n.ID] = n
	}
	if len(t.Errors) > 0 {
		return t
	}

	// Stage 3: what the edges name.
	for i := range raws {
		n := &raws[i]
		for _, e := range n.Next {
			m := byID[e.To]
			switch {
			case m == nil:
				p.fail("T040", n.ID, n.Line, "next names %s, which is not a step of this block", e.To)
			case m.Lane != n.Lane:
				p.fail("T041", n.ID, n.Line, "next names %s in lane %s; next stays within one lane, use realizes across lanes", e.To, m.Lane)
			case m.ID == n.ID:
				p.fail("T042", n.ID, n.Line, "next names the step itself; a step that repeats goes under back")
			}
		}
		for _, e := range n.Back {
			m := byID[e.To]
			switch {
			case m == nil:
				p.fail("T040", n.ID, n.Line, "back names %s, which is not a step of this block", e.To)
			case m.Lane != n.Lane:
				p.fail("T041", n.ID, n.Line, "back names %s in lane %s; back stays within one lane", e.To, m.Lane)
			}
		}
		for _, r := range n.Realizes {
			m := byID[r]
			switch {
			case m == nil:
				p.fail("T040", n.ID, n.Line, "realizes %s, which is not a step of this block", r)
			case laneOf[m.Lane] >= laneOf[n.Lane]:
				p.fail("T045", n.ID, n.Line, "realizes %s in lane %s, which is not above %s; realizes names steps of more abstract lanes", r, m.Lane, n.Lane)
			}
		}
	}
	if len(t.Errors) > 0 {
		return t
	}
	t.Lanes, t.Nodes = lanes, raws

	// Stage 4: order. next has no cycles; back returns to an earlier step.
	for _, c := range nextCycles(&t) {
		n := byID[c[len(c)-1]]
		p.fail("T043", n.ID, n.Line, "next edges form a cycle through %s; write the edge that returns as back", strings.Join(c, ", "))
	}
	if len(t.Errors) == 0 {
		reach := reachability(&t)
		for i := range raws {
			n := &raws[i]
			for _, e := range n.Back {
				if e.To != n.ID && !reach[e.To][n.ID] {
					p.fail("T044", n.ID, n.Line, "back names %s, which does not come before %s; a forward edge is a next", e.To, n.ID)
				}
			}
		}
	}
	if len(t.Errors) > 0 {
		t.Lanes, t.Nodes = nil, nil
		return t
	}
	p.graphWarnings()
	return t
}

// traceParser holds what one block's validation has found.
type traceParser struct {
	t        *Trace
	laneLine []int // file lines of the lane headers
}

func (p *traceParser) fail(code, node string, line int, format string, a ...any) {
	p.t.Errors = append(p.t.Errors, TraceError{Code: code, Node: node, Line: line, Message: fmt.Sprintf(format, a...)})
}

func (p *traceParser) warn(code, node string, line int, format string, a ...any) {
	p.t.Warnings = append(p.t.Warnings, TraceError{Code: code, Node: node, Line: line, Message: fmt.Sprintf(format, a...)})
}

// edge adds a next or back edge; one named twice counts once (T104).
func (p *traceParser) edge(es []TraceEdge, kw, node string, a langAttr) []TraceEdge {
	if slices.ContainsFunc(es, func(o TraceEdge) bool { return o.To == a.Value }) {
		p.warn("T104", node, a.Line, "%s names %s twice", kw, a.Value)
		return es
	}
	return append(es, TraceEdge{To: a.Value, Label: a.Label})
}

// ids are the values of a repeated reference keyword, each once (T104).
func (p *traceParser) ids(it langItem, kw string) []string {
	var out []string
	for _, a := range it.all(kw) {
		if slices.Contains(out, a.Value) {
			p.warn("T104", it.Name, a.Line, "%s names %s twice", kw, a.Value)
			continue
		}
		out = append(out, a.Value)
	}
	return out
}

// stepWarnings are the warnings that need only one step (T104, T109,
// T112, T113).
func (p *traceParser) stepWarnings(n *TraceNode, it langItem) {
	for _, e := range n.Next {
		if slices.ContainsFunc(n.Back, func(b TraceEdge) bool { return b.To == e.To }) {
			p.warn("T104", n.ID, n.Line, "%s is both a next and a back; keep one", e.To)
		}
	}
	if utf8.RuneCountInString(n.Text) > advisedText {
		p.warn("T109", n.ID, n.Line, "the text has %d characters; keep it under %d and put the rest in why or the prose", utf8.RuneCountInString(n.Text), advisedText)
	}
	if utf8.RuneCountInString(n.Why) > advisedWhy {
		p.warn("T109", n.ID, n.Line, "why has %d characters; keep it under %d and put the rest in the prose", utf8.RuneCountInString(n.Why), advisedWhy)
	}
	for _, e := range append(slices.Clone(n.Next), n.Back...) {
		if utf8.RuneCountInString(e.Label) > advisedLabel {
			p.warn("T109", n.ID, n.Line, "the label of the edge to %s has %d characters; keep it under %d", e.To, utf8.RuneCountInString(e.Label), advisedLabel)
		}
	}
	if n.Ref != "" && !refFormRe.MatchString(n.Ref) {
		p.warn("T112", n.ID, n.Line, "ref %q is not of the form scheme:payload without spaces, such as gh:src/file.go#L10-L20", n.Ref)
	}
	if labelled := countLabels(n.Next); len(n.Next) > 1 && labelled > 0 && labelled < len(n.Next) {
		p.warn("T113", n.ID, n.Line, "only some of the next edges have a label; say when each one is taken")
	}
	_ = it
}

// nearest is the known keyword within two edits of k, if any.
func nearest(k string, known []string) string {
	best, dist := "", 3
	for _, c := range known {
		if d := editDistance(strings.ToLower(k), c); d < dist {
			best, dist = c, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			sub := 1
			if a[i-1] == b[j-1] {
				sub = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+sub)
		}
		prev = cur
	}
	return prev[len(b)]
}

func countLabels(es []TraceEdge) int {
	n := 0
	for _, e := range es {
		if e.Label != "" {
			n++
		}
	}
	return n
}

// where names the trace in messages: its number, and the heading above it.
func (t *Trace) where() string {
	s := fmt.Sprintf("trace %d", t.Index+1)
	if t.Title != "" {
		s += " (" + t.Title + ")"
	}
	return s
}
