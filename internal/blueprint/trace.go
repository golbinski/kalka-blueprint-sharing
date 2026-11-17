package blueprint

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	"go.yaml.in/yaml/v3"
)

// TraceInfo is the first word of a trace block's fence (spec 6.10).
const TraceInfo = "trace"

// TraceVersion is the trace language version this reader implements.
const TraceVersion = 1

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
	// Version is the language version the fence names: 1 when it names
	// none, or the newer number this reader does not interpret (T005).
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
	// Line is the file line where the node starts.
	Line int `json:"line"`
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
// in the body: a heading, and for slug#node a node of a trace under it.
func (d *Document) HasAnchor(anchor string) bool {
	slug, node, hasNode := strings.Cut(anchor, "#")
	if !d.HasSection(slug) {
		return false
	}
	return !hasNode || d.HasNode(slug, node)
}

// fence is a trace fence, or a yaml one that may have been meant as a
// trace, found while parsing, before validation.
type fence struct {
	line   int    // file line of the opening fence
	info   string // the whole info string
	yaml   bool   // fenced as yaml or yml
	nested bool
	source string
}

// findFences collects ```trace blocks, and ```yaml ones, from a parsed
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
		isYAML := word == "yaml" || word == "yml"
		if word != TraceInfo && !isYAML {
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
		out = append(out, fence{line: base + open + 1, info: info, yaml: isYAML, nested: fc.Parent() != root, source: b.String()})
		return ast.WalkSkipChildren, nil
	})
	return out
}

// buildTraces validates the fences, places them in sections and computes
// divergences. Its errors and warnings are diagnostics of the document; a
// trace never blocks a sync (rules T…, which are not Blocks).
func (d *Document) buildTraces(fences []fence) {
	for _, f := range fences {
		if f.yaml {
			if looksLikeTrace(f.source) {
				d.problem("T122", f.line, "this yaml block has lanes and nodes, so it was probably meant as a trace; fence it as trace v1")
			}
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
					Message: fmt.Sprintf("trace %d under the same heading also has a node %s, so comments on it are ambiguous; use another id prefix in this block", first+1, n.ID)})
			}
			if _, ok := seen[k]; !ok {
				seen[k] = t.Index
			}
		}
	}
	for _, t := range d.traces {
		where := fmt.Sprintf("trace %d", t.Index+1)
		if t.Title != "" {
			where += " (" + t.Title + ")"
		}
		for _, e := range append(append([]TraceError(nil), t.Errors...), t.Warnings...) {
			msg := where + ": " + e.Message
			if e.Node != "" {
				msg = where + ", node " + e.Node + ": " + e.Message
			}
			p := NewProblem(e.Code, e.Line, msg)
			p.Node = e.Node
			d.problems = append(d.problems, p)
		}
	}
}

// looksLikeTrace reports whether YAML has a root mapping with both lanes
// and nodes (T122).
func looksLikeTrace(src string) bool {
	var doc yaml.Node
	if yaml.Unmarshal([]byte(src), &doc) != nil || len(doc.Content) == 0 {
		return false
	}
	root := doc.Content[0]
	return root.Kind == yaml.MappingNode && mapValue(root, "lanes") != nil && mapValue(root, "nodes") != nil
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

var (
	traceVersionRe = regexp.MustCompile(`^v[1-9][0-9]*$`)
	yamlLineRe     = regexp.MustCompile(`^yaml: line (\d+): `)
	refFormRe      = regexp.MustCompile(`^[a-z][a-z0-9+.-]*:\S+$`)
)

var (
	traceBlockKeys = []string{"lanes", "nodes"}
	traceNodeKeys  = []string{"id", "lane", "text", "next", "back", "realizes", "ref", "why", "change"}
)

// traceParser holds what one block's validation has found.
type traceParser struct {
	t         *Trace
	base      int // file line of the opening fence; YAML lines count from it
	lanesLine int // YAML line of lanes
}

// fail records an error at a YAML line (1 is the first line of content).
func (p *traceParser) fail(code, node string, line int, format string, a ...any) {
	p.t.Errors = append(p.t.Errors, TraceError{Code: code, Node: node, Line: p.base + line, Message: fmt.Sprintf(format, a...)})
}

func (p *traceParser) warn(code, node string, line int, format string, a ...any) {
	p.t.Warnings = append(p.t.Warnings, TraceError{Code: code, Node: node, Line: p.base + line, Message: fmt.Sprintf(format, a...)})
}

// rawNode is a node as read, with the YAML lines its parts came from.
type rawNode struct {
	TraceNode
	line                          int // YAML line of the node
	nextLine, backLine, realsLine int
}

// parseTrace reads and validates one block (spec 6.10, validation). The
// stages run in order, each only if the ones before found no error.
func parseTrace(src string, fenceLine int, info string) Trace {
	t := Trace{Source: src, Version: TraceVersion}
	p := &traceParser{t: &t, base: fenceLine}

	// Stage 0: the fence.
	words := strings.Fields(info)
	switch {
	case len(words) < 2:
		p.warn("T101", "", 0, "the fence names no version and is read as v1; write ```trace v1")
	case words[1] == "v1":
	case traceVersionRe.MatchString(words[1]):
		n, err := strconv.Atoi(words[1][1:])
		if err != nil {
			n = 0
		}
		t.Version = n
		p.fail("T005", "", 0, "trace %s: needs a newer kalka", words[1])
		return t
	default:
		t.Version = 0
		p.fail("T005", "", 0, "%q after trace is not a version; write ```trace v1", words[1])
		return t
	}

	// Stage 1: YAML, in the restricted form.
	dec := yaml.NewDecoder(strings.NewReader(src))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		p.yamlError(err)
		return t
	}
	var extra yaml.Node
	switch err := dec.Decode(&extra); {
	case errors.Is(err, io.EOF):
	case err != nil:
		p.yamlError(err)
		return t
	default:
		p.fail("T002", "", extra.Line, "the block holds more than one YAML document; remove the --- line")
		return t
	}
	walkYAML(&doc, func(n *yaml.Node, key bool) {
		switch {
		case n.Kind == yaml.AliasNode:
			p.fail("T002", "", n.Line, "aliases (*%s) are not allowed; write the value out", n.Value)
		case n.Anchor != "":
			p.fail("T002", "", n.Line, "anchors (&%s) are not allowed; write the value out", n.Anchor)
		case key && n.Tag == "!!merge":
			p.fail("T002", "", n.Line, "merge keys (<<) are not allowed; write the keys out")
		case n.Style&yaml.TaggedStyle != 0:
			p.fail("T002", "", n.Line, "explicit tags (%s) are not allowed; remove the tag", n.Tag)
		}
	})
	if len(t.Errors) > 0 {
		return t
	}

	// Stage 2: structure and the shape of every value.
	walkYAML(&doc, func(n *yaml.Node, _ bool) {
		if n.Kind != yaml.MappingNode {
			return
		}
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if seen[k.Value] {
				p.fail("T003", "", k.Line, "key %s appears twice in one mapping; keep one", k.Value)
			}
			seen[k.Value] = true
		}
	})
	walkYAML(&doc, func(n *yaml.Node, key bool) {
		if !key && n.LineComment != "" && n.Kind != yaml.DocumentNode {
			p.warn("T103", "", n.Line, "a comment follows a value on the same line; put it on a line of its own, since YAML cuts an unquoted text at \" #\"")
		}
	})
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		p.fail("T004", "", 1, "the block must be a mapping with lanes and nodes")
		return t
	}
	root := doc.Content[0]
	p.unknownKeys(root, traceBlockKeys, "")
	lanes := p.lanes(root)
	raws := p.nodes(root)
	if len(t.Errors) > 0 {
		return t
	}

	// Stage 3: each node's own fields.
	laneOf := map[string]int{}
	for i, l := range lanes {
		laneOf[l] = i
	}
	byID := map[string]*rawNode{}
	for i := range raws {
		n := &raws[i]
		switch {
		case n.ID == "":
			p.fail("T030", "", n.line, "the node has no id; add id: with a name such as C1")
		case len(n.ID) > maxNodeID || !nodeIDRe.MatchString(n.ID):
			p.fail("T030", n.ID, n.line, "an id starts with a letter, uses only letters, digits, - and _, and has at most %d characters", maxNodeID)
		case byID[n.ID] != nil:
			p.fail("T031", n.ID, n.line, "the id is used twice (first at line %d); give this step a new id", fenceLine+byID[n.ID].line)
		default:
			byID[n.ID] = n
		}
		if _, ok := laneOf[n.Lane]; !ok {
			if n.Lane == "" {
				p.fail("T032", n.ID, n.line, "lane is missing; name one of %s", strings.Join(lanes, ", "))
			} else {
				p.fail("T032", n.ID, n.line, "lane %q is not in lanes (%s)", n.Lane, strings.Join(lanes, ", "))
			}
		}
		if n.Text == "" {
			p.fail("T033", n.ID, n.line, "text is missing; say in one short line what the step does")
		}
		if n.Change != "" && n.Change != ChangeNew && n.Change != ChangeModified && n.Change != ChangeRemoved {
			p.fail("T046", n.ID, n.line, "change is new, modified or removed, not %q; leave it out for an unchanged step", n.Change)
		}
	}
	if len(t.Errors) > 0 {
		return t
	}

	// Stage 4: what the edges name.
	for i := range raws {
		n := &raws[i]
		for _, e := range n.Next {
			m := byID[e.To]
			switch {
			case m == nil:
				p.fail("T040", n.ID, n.nextLine, "next names %s, which is not a node of this block", e.To)
			case m.Lane != n.Lane:
				p.fail("T041", n.ID, n.nextLine, "next names %s in lane %s; next stays within one lane, use realizes across lanes", e.To, m.Lane)
			case m.ID == n.ID:
				p.fail("T042", n.ID, n.nextLine, "next names the node itself; a step that repeats goes under back")
			}
		}
		for _, e := range n.Back {
			m := byID[e.To]
			switch {
			case m == nil:
				p.fail("T040", n.ID, n.backLine, "back names %s, which is not a node of this block", e.To)
			case m.Lane != n.Lane:
				p.fail("T041", n.ID, n.backLine, "back names %s in lane %s; back stays within one lane", e.To, m.Lane)
			}
		}
		for _, r := range n.Realizes {
			m := byID[r]
			switch {
			case m == nil:
				p.fail("T040", n.ID, n.realsLine, "realizes %s, which is not a node of this block", r)
			case laneOf[m.Lane] >= laneOf[n.Lane]:
				p.fail("T045", n.ID, n.realsLine, "realizes %s in lane %s, which is not above %s; realizes names nodes of more abstract lanes", r, m.Lane, n.Lane)
			}
		}
	}
	if len(t.Errors) > 0 {
		return t
	}

	nodes := make([]TraceNode, len(raws))
	for i := range raws {
		nodes[i] = raws[i].TraceNode
	}
	t.Lanes, t.Nodes = lanes, nodes

	// Stage 5: order. next has no cycles; back returns to an earlier step.
	for _, c := range nextCycles(&t) {
		n := byID[c[len(c)-1]]
		p.fail("T043", n.ID, n.nextLine, "next edges form a cycle through %s; write the edge that returns under back", strings.Join(c, ", "))
	}
	if len(t.Errors) == 0 {
		reach := reachability(&t)
		for i := range raws {
			n := &raws[i]
			for _, e := range n.Back {
				if e.To != n.ID && !reach[e.To][n.ID] {
					p.fail("T044", n.ID, n.backLine, "back names %s, which does not come before %s; a forward edge belongs under next", e.To, n.ID)
				}
			}
		}
	}
	if len(t.Errors) > 0 {
		t.Lanes, t.Nodes = nil, nil
		return t
	}
	p.graphWarnings(raws)
	return t
}

// yamlError reports a YAML parse error at the line it names.
func (p *traceParser) yamlError(err error) {
	msg := err.Error()
	line := 0
	if m := yamlLineRe.FindStringSubmatch(msg); m != nil {
		line, _ = strconv.Atoi(m[1])
		msg = msg[len(m[0]):]
	}
	msg = strings.TrimPrefix(msg, "yaml: ")
	p.fail("T001", "", line, "the YAML does not parse: %s; put text values in double quotes", msg)
}

// walkYAML visits every node; key is set for mapping keys.
func walkYAML(n *yaml.Node, visit func(n *yaml.Node, key bool)) {
	var walk func(n *yaml.Node, key bool)
	walk = func(n *yaml.Node, key bool) {
		visit(n, key)
		for i, c := range n.Content {
			walk(c, n.Kind == yaml.MappingNode && i%2 == 0)
		}
	}
	walk(n, false)
}

// unknownKeys warns about keys that are not known and do not start with
// x- (T102), suggesting the nearest known key.
func (p *traceParser) unknownKeys(m *yaml.Node, known []string, node string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i].Value
		if slices.Contains(known, k) || strings.HasPrefix(k, "x-") {
			continue
		}
		msg := fmt.Sprintf("unknown key %s is ignored", k)
		if s := nearest(k, known); s != "" {
			msg += fmt.Sprintf("; did you mean %s?", s)
		} else {
			msg += "; a key of your own starts with x-"
		}
		p.warn("T102", node, m.Content[i].Line, "%s", msg)
	}
}

// nearest is the known key within two edits of k, if any.
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

// absent reports a missing value, or one written as ~, null or nothing.
func absent(n *yaml.Node) bool {
	if n == nil {
		return true
	}
	if n.Kind != yaml.ScalarNode || n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return false
	}
	switch n.Value {
	case "", "~", "null", "Null", "NULL":
		return true
	}
	return false
}

// textOf is a scalar's text with white space collapsed; ok is false when
// the value is not a single text.
func textOf(n *yaml.Node) (s string, ok bool) {
	if absent(n) {
		return "", true
	}
	if n.Kind != yaml.ScalarNode {
		return "", false
	}
	return strings.Join(strings.Fields(n.Value), " "), true
}

// lanes reads the block's lanes (T010, T011, T012).
func (p *traceParser) lanes(root *yaml.Node) []string {
	v := mapValue(root, "lanes")
	if absent(v) {
		p.fail("T010", "", root.Line, "lanes is missing; list the levels, most abstract first: lanes: [business, code]")
		return nil
	}
	if v.Kind != yaml.SequenceNode {
		p.fail("T010", "", v.Line, "lanes is a list of names: lanes: [business, code]")
		return nil
	}
	p.lanesLine = v.Line
	var out []string
	for _, l := range v.Content {
		name, ok := textOf(l)
		if !ok {
			p.fail("T010", "", l.Line, "lanes is a list of names: lanes: [business, code]")
			return nil
		}
		switch {
		case name == "":
			p.fail("T012", "", l.Line, "a lane name is empty")
		case len([]rune(name)) > maxLaneName:
			p.fail("T012", "", l.Line, "lane name %q is longer than %d characters", name, maxLaneName)
		case slices.Contains(out, name):
			p.fail("T012", "", l.Line, "lane %q is listed twice", name)
		}
		out = append(out, name)
	}
	if len(out) < MinTraceLanes || len(out) > MaxTraceLanes {
		p.fail("T011", "", v.Line, "%d lanes; a trace has %d to %d", len(out), MinTraceLanes, MaxTraceLanes)
	}
	return out
}

// nodes reads the block's nodes with the shape of each value (T020, T021,
// T022, T034) and the warnings that need only one node (T102, T104, T109,
// T112, T113).
func (p *traceParser) nodes(root *yaml.Node) []rawNode {
	v := mapValue(root, "nodes")
	switch {
	case absent(v):
		p.fail("T020", "", root.Line, "nodes is missing; list the steps of all lanes under nodes:")
		return nil
	case v.Kind != yaml.SequenceNode:
		p.fail("T020", "", v.Line, "nodes is a list of steps, each starting with - id:")
		return nil
	case len(v.Content) == 0:
		p.fail("T020", "", v.Line, "nodes is empty; a trace has at least one step")
		return nil
	case len(v.Content) > MaxTraceNodes:
		p.fail("T022", "", v.Line, "%d nodes; a block has at most %d, so split it under several headings", len(v.Content), MaxTraceNodes)
	}
	var out []rawNode
	for _, nn := range v.Content {
		if nn.Kind != yaml.MappingNode || nn.Style&yaml.FlowStyle != 0 {
			p.fail("T021", "", nn.Line, "write each node as a block mapping, one key per line (- id: C1, then lane:, text: … below it), not as {…}")
			continue
		}
		n := rawNode{line: nn.Line}
		n.Line = p.base + nn.Line
		str := func(key string) string {
			s, ok := textOf(mapValue(nn, key))
			if !ok {
				p.fail("T034", "", mapValue(nn, key).Line, "%s is a single text; put it in double quotes", key)
			}
			return s
		}
		n.ID, n.Lane, n.Text = str("id"), str("lane"), str("text")
		n.Ref, n.Why, n.Change = str("ref"), str("why"), str("change")
		id := n.ID
		if x := mapValue(nn, "next"); !absent(x) {
			n.nextLine = x.Line
			n.Next = p.edges(x, "next", id)
		} else {
			n.nextLine = nn.Line
		}
		if x := mapValue(nn, "back"); !absent(x) {
			n.backLine = x.Line
			n.Back = p.edges(x, "back", id)
		} else {
			n.backLine = nn.Line
		}
		if x := mapValue(nn, "realizes"); !absent(x) {
			n.realsLine = x.Line
			n.Realizes = p.idList(x, id)
		} else {
			n.realsLine = nn.Line
		}
		p.unknownKeys(nn, traceNodeKeys, id)
		for _, e := range n.Next {
			if slices.ContainsFunc(n.Back, func(b TraceEdge) bool { return b.To == e.To }) {
				p.warn("T104", id, n.backLine, "%s is under both next and back; keep it under one", e.To)
			}
		}
		if len([]rune(n.Text)) > advisedText {
			p.warn("T109", id, nn.Line, "text has %d characters; keep it under %d and put the rest in why or the prose", len([]rune(n.Text)), advisedText)
		}
		if len([]rune(n.Why)) > advisedWhy {
			p.warn("T109", id, nn.Line, "why has %d characters; keep it under %d and put the rest in the prose", len([]rune(n.Why)), advisedWhy)
		}
		for _, e := range append(slices.Clone(n.Next), n.Back...) {
			if len([]rune(e.Label)) > advisedLabel {
				p.warn("T109", id, nn.Line, "the label of the edge to %s has %d characters; keep it under %d", e.To, len([]rune(e.Label)), advisedLabel)
			}
		}
		if n.Ref != "" && !refFormRe.MatchString(n.Ref) {
			p.warn("T112", id, nn.Line, "ref %q is not of the form scheme:payload without spaces, such as gh:src/file.go#L10-L20", n.Ref)
		}
		if labelled := countLabels(n.Next); len(n.Next) > 1 && labelled > 0 && labelled < len(n.Next) {
			p.warn("T113", id, n.nextLine, "only some of the next edges have a label; say when each one is taken")
		}
		out = append(out, n)
	}
	return out
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

// edges reads next or back: an id, a list of ids, or a mapping of id to
// label. Duplicates count once (T104).
func (p *traceParser) edges(v *yaml.Node, key, node string) []TraceEdge {
	var out []TraceEdge
	add := func(e TraceEdge, line int) {
		if slices.ContainsFunc(out, func(o TraceEdge) bool { return o.To == e.To }) {
			p.warn("T104", node, line, "%s names %s twice", key, e.To)
			return
		}
		out = append(out, e)
	}
	shape := func(line int) {
		p.fail("T034", node, line, "%s is a list of ids, or a mapping of id to label, such as %s: {C2: \"file is small\"}", key, key)
	}
	switch v.Kind {
	case yaml.ScalarNode:
		s, _ := textOf(v)
		add(TraceEdge{To: s}, v.Line)
	case yaml.SequenceNode:
		for _, it := range v.Content {
			s, ok := textOf(it)
			if !ok || s == "" {
				shape(it.Line)
				return nil
			}
			add(TraceEdge{To: s}, it.Line)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(v.Content); i += 2 {
			to, ok1 := textOf(v.Content[i])
			label, ok2 := textOf(v.Content[i+1])
			if !ok1 || !ok2 || to == "" {
				shape(v.Content[i].Line)
				return nil
			}
			add(TraceEdge{To: to, Label: label}, v.Content[i].Line)
		}
	default:
		shape(v.Line)
	}
	return out
}

// idList reads realizes: an id or a list of ids. The mapping form is
// reserved. Duplicates count once (T104).
func (p *traceParser) idList(v *yaml.Node, node string) []string {
	var out []string
	add := func(s string, line int) {
		if slices.Contains(out, s) {
			p.warn("T104", node, line, "realizes names %s twice", s)
			return
		}
		out = append(out, s)
	}
	switch v.Kind {
	case yaml.ScalarNode:
		s, _ := textOf(v)
		add(s, v.Line)
		return out
	case yaml.SequenceNode:
		good := true
		for _, it := range v.Content {
			if s, ok := textOf(it); !ok || s == "" {
				good = false
			}
		}
		if good {
			for _, it := range v.Content {
				s, _ := textOf(it)
				add(s, it.Line)
			}
			return out
		}
	}
	p.fail("T034", node, v.Line, "realizes is a list of ids, such as realizes: [B1, B2]")
	return nil
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
