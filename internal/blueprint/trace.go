package blueprint

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
	"go.yaml.in/yaml/v3"
)

// TraceInfo is the info string of a trace block's fence (spec 6.10).
const TraceInfo = "trace"

// MaxTraceNodes is the most nodes one trace block may have.
const MaxTraceNodes = 40

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
	DivUnverified         = "unverified"
	DivStale              = "stale"
)

// Trace is one trace block of the body. A block that fails validation has
// Errors and no nodes; it renders as code and never blocks a sync.
type Trace struct {
	// Index is the block's position among the document's trace blocks.
	Index int `json:"index"`
	// Line is the file line of the opening fence.
	Line int `json:"line"`
	// Section is the slug of the top-level heading the block sits under
	// (the anchor prefix for its nodes); Title is that heading's text.
	Section string `json:"section,omitempty"`
	Title   string `json:"title,omitempty"`
	// Nested is set for a block inside a list or block quote.
	Nested      bool         `json:"nested,omitempty"`
	Source      string       `json:"source"`
	Lanes       []string     `json:"lanes,omitempty"`
	Nodes       []TraceNode  `json:"nodes,omitempty"`
	Errors      []TraceError `json:"errors,omitempty"`
	Divergences []Divergence `json:"divergences,omitempty"`
}

// TraceNode is one step in one lane.
type TraceNode struct {
	ID       string      `json:"id"`
	Lane     string      `json:"lane"`
	Text     string      `json:"text"`
	Next     []TraceNext `json:"next,omitempty"`
	Realizes []string    `json:"realizes,omitempty"`
	Ref      string      `json:"ref,omitempty"`
	Why      string      `json:"why,omitempty"`
	// Line is the file line where the node starts.
	Line int `json:"line"`
}

// TraceNext is a flow edge, optionally naming the branch.
type TraceNext struct {
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

// TraceError is a validation failure, with the node it concerns if any.
type TraceError struct {
	Node    string `json:"node,omitempty"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// Divergence is a place where two lanes stop lining up. Node is the node
// the badge goes on; Nodes are the others involved, and Lane, for the
// classes that compare flows, is the lane where the difference shows.
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

// fence is a trace fence found while parsing, before validation.
type fence struct {
	line   int // file line of the opening fence
	nested bool
	source string
}

// findFences collects ```trace blocks from a parsed part of the body.
// lineAt maps a byte offset of src to a 0-based line of the part, and keep
// filters by that line (to leave out the review log).
func findFences(root ast.Node, src []byte, lineAt func(int) int, base int, keep func(int) bool) []fence {
	var out []fence
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		fc, ok := n.(*ast.FencedCodeBlock)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		info := ""
		if fc.Info != nil {
			info, _, _ = strings.Cut(strings.TrimSpace(string(fc.Info.Segment.Value(src))), " ")
		}
		if info != TraceInfo {
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
		out = append(out, fence{line: base + open + 1, nested: fc.Parent() != root, source: b.String()})
		return ast.WalkSkipChildren, nil
	})
	return out
}

// buildTraces validates the fences, places them in sections and computes
// divergences. Problems are warnings: a trace never blocks a sync.
func (d *Document) buildTraces(fences []fence) {
	for i, f := range fences {
		t := parseTrace(f.source, f.line)
		t.Index, t.Line, t.Nested = i, f.line, f.nested
		if h := d.sectionHeading(f.line); h != nil {
			t.Section, t.Title = h.Slug, h.Text
		}
		where := fmt.Sprintf("trace %d", i+1)
		if t.Title != "" {
			where += " (" + t.Title + ")"
		}
		for _, e := range t.Errors {
			msg := where + ": " + e.Message
			if e.Node != "" {
				msg = where + ", node " + e.Node + ": " + e.Message
			}
			d.problem(Warning, "trace-invalid", e.Line, msg)
		}
		if t.Valid() {
			t.Divergences = divergences(&t)
		}
		d.traces = append(d.traces, t)
	}
	// Node anchors are §slug#id: the same id twice under one heading makes
	// them ambiguous.
	seen := map[string]int{}
	for _, t := range d.traces {
		for _, n := range t.Nodes {
			k := t.Section + "#" + n.ID
			if first, dup := seen[k]; dup && first != t.Index {
				d.problem(Warning, "trace-ambiguous-id", n.Line, fmt.Sprintf("node id %s is used by two trace blocks under §%s; comments on it are ambiguous", n.ID, t.Section))
			}
			if _, ok := seen[k]; !ok {
				seen[k] = t.Index
			}
		}
	}
}

// sectionHeading is the top-level heading a body line sits under, or nil.
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

// parseTrace reads and validates one block. Errors carry file lines.
func parseTrace(src string, fenceLine int) Trace {
	t := Trace{Source: src}
	fail := func(node string, line int, format string, a ...any) {
		t.Errors = append(t.Errors, TraceError{Node: node, Line: fenceLine + line, Message: fmt.Sprintf(format, a...)})
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		fail("", 0, "YAML does not parse: %s", strings.TrimPrefix(err.Error(), "yaml: "))
		return t
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		fail("", 1, "the block must be a mapping with lanes and nodes")
		return t
	}
	root := doc.Content[0]
	lanesN, nodesN := mapValue(root, "lanes"), mapValue(root, "nodes")

	var lanes []string
	switch {
	case lanesN == nil:
		fail("", root.Line, "lanes is missing")
	case lanesN.Kind != yaml.SequenceNode:
		fail("", lanesN.Line, "lanes must be a list")
	default:
		for _, l := range lanesN.Content {
			name := yscalar(l)
			if name == "" {
				fail("", l.Line, "a lane name must be a non-empty text")
				continue
			}
			if slices.Contains(lanes, name) {
				fail("", l.Line, "lane %q is listed twice", name)
				continue
			}
			lanes = append(lanes, name)
		}
		if len(lanes) < 2 && len(t.Errors) == 0 {
			fail("", lanesN.Line, "a trace needs at least two lanes")
		}
	}

	var nodes []TraceNode
	switch {
	case nodesN == nil:
		fail("", root.Line, "nodes is missing")
	case nodesN.Kind != yaml.SequenceNode:
		fail("", nodesN.Line, "nodes must be a list")
	case len(nodesN.Content) > MaxTraceNodes:
		fail("", nodesN.Line, "%d nodes; a block may have at most %d (split it into several blocks)", len(nodesN.Content), MaxTraceNodes)
	default:
		for _, nn := range nodesN.Content {
			if nn.Kind != yaml.MappingNode {
				fail("", nn.Line, "each node must be a mapping with id, lane and text")
				continue
			}
			n := TraceNode{ID: yscalar(mapValue(nn, "id")), Lane: yscalar(mapValue(nn, "lane")), Text: yscalar(mapValue(nn, "text")),
				Ref: yscalar(mapValue(nn, "ref")), Why: yscalar(mapValue(nn, "why")), Line: fenceLine + nn.Line}
			if v := mapValue(nn, "next"); v != nil {
				for _, it := range listItems(v) {
					if it.Kind == yaml.MappingNode {
						n.Next = append(n.Next, TraceNext{To: yscalar(mapValue(it, "to")), Label: yscalar(mapValue(it, "label"))})
					} else {
						n.Next = append(n.Next, TraceNext{To: yscalar(it)})
					}
				}
			}
			if v := mapValue(nn, "realizes"); v != nil {
				for _, it := range listItems(v) {
					n.Realizes = append(n.Realizes, yscalar(it))
				}
			}
			nodes = append(nodes, n)
		}
	}
	if len(t.Errors) > 0 {
		return t
	}

	laneOf := map[string]int{}
	for i, l := range lanes {
		laneOf[l] = i
	}
	byID := map[string]*TraceNode{}
	for i := range nodes {
		n := &nodes[i]
		rel := n.Line - fenceLine
		switch {
		case n.ID == "":
			fail("", rel, "a node has no id")
			continue
		case !nodeIDRe.MatchString(n.ID):
			fail(n.ID, rel, "id must start with a letter and use only letters, digits, - and _")
		case byID[n.ID] != nil:
			fail(n.ID, rel, "id is used twice")
		}
		byID[n.ID] = n
		if _, ok := laneOf[n.Lane]; !ok {
			if n.Lane == "" {
				fail(n.ID, rel, "lane is missing")
			} else {
				fail(n.ID, rel, "lane %q is not in lanes", n.Lane)
			}
		}
		if strings.TrimSpace(n.Text) == "" {
			fail(n.ID, rel, "text is missing")
		}
	}
	for i := range nodes {
		n := &nodes[i]
		rel := n.Line - fenceLine
		for _, x := range n.Next {
			m := byID[x.To]
			switch {
			case x.To == "":
				fail(n.ID, rel, "a next item names no node")
			case m == nil:
				fail(n.ID, rel, "next names %s, which is not a node", x.To)
			case m.Lane != n.Lane:
				fail(n.ID, rel, "next goes to %s in lane %s; next stays within one lane, use realizes across lanes", x.To, m.Lane)
			}
		}
		for _, r := range n.Realizes {
			m := byID[r]
			switch {
			case r == "":
				fail(n.ID, rel, "a realizes item names no node")
			case m == nil:
				fail(n.ID, rel, "realizes %s, which is not a node", r)
			case laneOf[m.Lane] >= laneOf[n.Lane]:
				fail(n.ID, rel, "realizes %s in lane %s, which is not more abstract than %s", r, m.Lane, n.Lane)
			}
		}
	}
	if len(t.Errors) > 0 {
		return t
	}
	t.Lanes, t.Nodes = lanes, nodes
	return t
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

// scalar returns a scalar's text; anything else is "".
func yscalar(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
		return ""
	}
	return strings.TrimSpace(n.Value)
}

// listItems accepts a list, or a single item written without brackets.
func listItems(n *yaml.Node) []*yaml.Node {
	if n.Kind == yaml.SequenceNode {
		return n.Content
	}
	return []*yaml.Node{n}
}

// divergences computes the graph classes of spec 6.10 for a valid block,
// ordered by class, then by node order in the block.
func divergences(t *Trace) []Divergence {
	laneOf := map[string]int{}
	for i, l := range t.Lanes {
		laneOf[l] = i
	}
	pos := map[string]int{}
	for i, n := range t.Nodes {
		pos[n.ID] = i
	}
	realizers := map[string][]string{} // abstract id -> realizer ids, block order
	for _, n := range t.Nodes {
		for _, r := range n.Realizes {
			if !slices.Contains(realizers[r], n.ID) {
				realizers[r] = append(realizers[r], n.ID)
			}
		}
	}
	reach := reachability(t)
	last := len(t.Lanes) - 1
	var out []Divergence

	for _, n := range t.Nodes {
		if laneOf[n.Lane] > 0 && len(n.Realizes) == 0 {
			out = append(out, Divergence{Class: DivImplementationOnly, Node: n.ID})
		}
	}
	for _, n := range t.Nodes {
		if laneOf[n.Lane] < last && len(realizers[n.ID]) == 0 {
			out = append(out, Divergence{Class: DivUnrealized, Node: n.ID})
		}
	}
	for _, n := range t.Nodes {
		if r := uniq(n.Realizes); len(r) >= 2 {
			out = append(out, Divergence{Class: DivMerged, Node: n.ID, Nodes: r})
		}
	}
	for _, n := range t.Nodes {
		if r := realizers[n.ID]; len(r) >= 2 {
			out = append(out, Divergence{Class: DivExpanded, Node: n.ID, Nodes: r})
		}
	}
	for _, n := range t.Nodes {
		for _, lane := range t.Lanes {
			var in []string
			for _, r := range realizers[n.ID] {
				if t.Node(r).Lane == lane {
					in = append(in, r)
				}
			}
			if len(in) >= 2 && !connected(t, in) {
				out = append(out, Divergence{Class: DivScattered, Node: n.ID, Nodes: in, Lane: lane})
			}
		}
	}
	// Reordered: A before B above, but a realizer of B before a realizer of
	// A below, and never the other way round.
	for _, a := range t.Nodes {
		for _, b := range t.Nodes {
			if a.Lane != b.Lane || !reach[a.ID][b.ID] || reach[b.ID][a.ID] {
				continue
			}
			for _, lane := range t.Lanes[laneOf[a.Lane]+1:] {
				var early, late string
				backward, forward := false, false
				for _, rb := range realizers[b.ID] {
					for _, ra := range realizers[a.ID] {
						if ra == rb || t.Node(ra).Lane != lane || t.Node(rb).Lane != lane {
							continue
						}
						if reach[rb][ra] && !backward {
							backward, early, late = true, rb, ra
						}
						if reach[ra][rb] {
							forward = true
						}
					}
				}
				if backward && !forward {
					out = append(out, Divergence{Class: DivReordered, Node: a.ID, Nodes: []string{b.ID, early, late}, Lane: lane})
				}
			}
		}
	}
	rank := map[string]int{DivImplementationOnly: 0, DivUnrealized: 1, DivMerged: 2, DivExpanded: 3, DivScattered: 4, DivReordered: 5}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Class] != rank[out[j].Class] {
			return rank[out[i].Class] < rank[out[j].Class]
		}
		return pos[out[i].Node] < pos[out[j].Node]
	})
	return out
}

// reachability[a][b] is true when b follows a, directly or not, in a's lane.
func reachability(t *Trace) map[string]map[string]bool {
	next := map[string][]string{}
	for _, n := range t.Nodes {
		for _, x := range n.Next {
			next[n.ID] = append(next[n.ID], x.To)
		}
	}
	out := map[string]map[string]bool{}
	for _, n := range t.Nodes {
		seen := map[string]bool{}
		stack := slices.Clone(next[n.ID])
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[v] {
				continue
			}
			seen[v] = true
			stack = append(stack, next[v]...)
		}
		out[n.ID] = seen
	}
	return out
}

// connected reports whether ids form one piece of their lane's flow, with
// edges taken in both directions.
func connected(t *Trace, ids []string) bool {
	in := map[string]bool{}
	for _, id := range ids {
		in[id] = true
	}
	adj := map[string][]string{}
	for _, n := range t.Nodes {
		for _, x := range n.Next {
			if in[n.ID] && in[x.To] {
				adj[n.ID] = append(adj[n.ID], x.To)
				adj[x.To] = append(adj[x.To], n.ID)
			}
		}
	}
	seen := map[string]bool{ids[0]: true}
	stack := []string{ids[0]}
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, w := range adj[v] {
			if !seen[w] {
				seen[w] = true
				stack = append(stack, w)
			}
		}
	}
	return len(seen) == len(ids)
}

func uniq(s []string) []string {
	var out []string
	for _, v := range s {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}
