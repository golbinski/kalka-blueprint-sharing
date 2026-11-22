package blueprint

import (
	"slices"
	"sort"
	"strings"
)

// reachability[a][b] is true when a path of one or more next edges leads
// from a to b. Back edges take no part. Removed nodes carry order.
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

// nextCycles finds the cycles of next edges, each as the nodes on it in
// flow order; the last one holds the edge that closes it.
func nextCycles(t *Trace) [][]string {
	const (
		white = iota
		grey
		black
	)
	colour := map[string]int{}
	var stack []string
	var out [][]string
	var visit func(id string)
	visit = func(id string) {
		colour[id] = grey
		stack = append(stack, id)
		for _, e := range t.Node(id).Next {
			switch colour[e.To] {
			case white:
				visit(e.To)
			case grey:
				i := slices.Index(stack, e.To)
				out = append(out, slices.Clone(stack[i:]))
			}
		}
		stack = stack[:len(stack)-1]
		colour[id] = black
	}
	for _, n := range t.Nodes {
		if colour[n.ID] == white {
			visit(n.ID)
		}
	}
	return out
}

// graph is what the divergence classes and the warnings share.
type graph struct {
	t      *Trace
	laneOf map[string]int
	pos    map[string]int
	reach  map[string]map[string]bool
}

func newGraph(t *Trace) *graph {
	g := &graph{t: t, laneOf: map[string]int{}, pos: map[string]int{}, reach: reachability(t)}
	for i, l := range t.Lanes {
		g.laneOf[l] = i
	}
	for i, n := range t.Nodes {
		g.pos[n.ID] = i
	}
	return g
}

func (g *graph) lane(id string) int { return g.laneOf[g.t.Node(id).Lane] }
func (g *graph) live(id string) bool {
	n := g.t.Node(id)
	return n != nil && n.Live()
}

// up is up(N, U): the live nodes of lane u that n names in realizes, in
// the order written.
func (g *graph) up(n *TraceNode, u int) []string {
	var out []string
	for _, r := range n.Realizes {
		if g.live(r) && g.lane(r) == u && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

// real is real(M, L): the live nodes of lane l that name m in realizes, in
// block order.
func (g *graph) real(m string, l int) []string {
	var out []string
	for _, n := range g.t.Nodes {
		if n.Live() && g.laneOf[n.Lane] == l && slices.Contains(n.Realizes, m) {
			out = append(out, n.ID)
		}
	}
	return out
}

// realizesLive reports whether n names a live node in realizes.
func (g *graph) realizesLive(n *TraceNode) bool {
	return slices.ContainsFunc(n.Realizes, g.live)
}

// closure is R⁺(n): the nodes reachable from n over realization links,
// through live nodes only when liveOnly is set.
func (g *graph) closure(id string, liveOnly bool) map[string]bool {
	seen := map[string]bool{}
	stack := slices.Clone(g.t.Node(id).Realizes)
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[v] || liveOnly && !g.live(v) {
			continue
		}
		seen[v] = true
		stack = append(stack, g.t.Node(v).Realizes...)
	}
	return seen
}

// divergences computes the graph classes of spec 6.10 for a valid block,
// ordered by class, then by the block position of the node, then by lane.
func divergences(t *Trace) []Divergence {
	g := newGraph(t)
	last := len(t.Lanes) - 1
	var out []Divergence
	add := func(class, node string, nodes []string, lane int) {
		d := Divergence{Class: class, Node: node, Nodes: nodes}
		if lane >= 0 {
			d.Lane = t.Lanes[lane]
		}
		out = append(out, d)
	}
	live := func(yield func(*TraceNode) bool) {
		for i := range t.Nodes {
			if t.Nodes[i].Live() && !yield(&t.Nodes[i]) {
				return
			}
		}
	}

	for n := range live {
		if g.lane(n.ID) > 0 && !g.realizesLive(n) {
			add(DivImplementationOnly, n.ID, nil, -1)
		}
	}
	for m := range live {
		if l := g.lane(m.ID); l < last {
			none := true
			for below := l + 1; below <= last; below++ {
				none = none && len(g.real(m.ID, below)) == 0
			}
			if none {
				add(DivUnrealized, m.ID, nil, -1)
			}
		}
	}
	for n := range live {
		for u := 0; u < g.lane(n.ID); u++ {
			if r := g.up(n, u); len(r) >= 2 {
				add(DivMerged, n.ID, r, u)
			}
		}
	}
	for m := range live {
		for l := g.lane(m.ID) + 1; l <= last; l++ {
			if r := g.real(m.ID, l); len(r) >= 2 {
				add(DivExpanded, m.ID, r, l)
			}
		}
	}
	// Scattered: another step of the lane, which itself realizes
	// something, lies between two realizers of m.
	for m := range live {
		for l := g.lane(m.ID) + 1; l <= last; l++ {
			r := g.real(m.ID, l)
			if len(r) < 2 {
				continue
			}
			for x := range live {
				if g.lane(x.ID) != l || slices.Contains(r, x.ID) || !g.realizesLive(x) {
					continue
				}
				if g.between(r, x.ID) {
					add(DivScattered, m.ID, append(slices.Clone(r), x.ID), l)
					break
				}
			}
		}
	}
	// Reordered: A before B above, a realizer of B before a realizer of A
	// in lane l and never the other way round; only minimal pairs.
	for l := 1; l <= last; l++ {
		type pair struct{ a, b string }
		inv := map[pair][2]string{}
		var order []pair
		for a := range live {
			for b := range live {
				if a.Lane != b.Lane || g.lane(a.ID) >= l || !g.reach[a.ID][b.ID] {
					continue
				}
				if w, ok := g.inverted(a.ID, b.ID, l); ok {
					inv[pair{a.ID, b.ID}] = w
					order = append(order, pair{a.ID, b.ID})
				}
			}
		}
		for _, pr := range order {
			shadowed := false
			for c := range live {
				if c.Lane != g.t.Node(pr.a).Lane || !g.reach[pr.a][c.ID] || !g.reach[c.ID][pr.b] {
					continue
				}
				_, ac := inv[pair{pr.a, c.ID}]
				_, cb := inv[pair{c.ID, pr.b}]
				if ac || cb {
					shadowed = true
					break
				}
			}
			if !shadowed {
				w := inv[pr]
				add(DivReordered, pr.a, []string{pr.b, w[0], w[1]}, l)
			}
		}
	}
	// Skipped: something below lane k realizes m directly, and k has no
	// step between them.
	for m := range live {
		for k := g.lane(m.ID) + 1; k < last; k++ {
			var j []string
			for n := range live {
				if g.lane(n.ID) <= k || !slices.Contains(n.Realizes, m.ID) {
					continue
				}
				via := false
				for s := range g.closure(n.ID, true) {
					if g.lane(s) == k && g.closure(s, true)[m.ID] {
						via = true
						break
					}
				}
				if !via {
					j = append(j, n.ID)
				}
			}
			if len(j) > 0 {
				add(DivSkipped, m.ID, j, k)
			}
		}
	}

	rank := map[string]int{DivImplementationOnly: 0, DivUnrealized: 1, DivMerged: 2, DivExpanded: 3, DivScattered: 4, DivReordered: 5, DivSkipped: 6}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if rank[a.Class] != rank[b.Class] {
			return rank[a.Class] < rank[b.Class]
		}
		if g.pos[a.Node] != g.pos[b.Node] {
			return g.pos[a.Node] < g.pos[b.Node]
		}
		return slices.Index(t.Lanes, a.Lane) < slices.Index(t.Lanes, b.Lane)
	})
	return out
}

// between reports whether x lies between two nodes of r: r1 ≺ x ≺ r2.
func (g *graph) between(r []string, x string) bool {
	before, after := false, false
	for _, v := range r {
		before = before || g.reach[v][x]
		after = after || g.reach[x][v]
	}
	return before && after
}

// inverted reports whether the pair a ≺ b is inverted in lane l, with the
// first witness (rb, ra) in block order.
func (g *graph) inverted(a, b string, l int) ([2]string, bool) {
	ra, rb := g.real(a, l), g.real(b, l)
	var w [2]string
	found := false
	for _, y := range rb {
		for _, x := range ra {
			if x == y {
				continue
			}
			if g.reach[x][y] {
				return w, false // some realizer of a comes before one of b
			}
			if !found && g.reach[y][x] {
				w, found = [2]string{y, x}, true
			}
		}
	}
	return w, found
}

// graphWarnings are the warnings of a valid block that look at more than
// one node (T105 to T108, T110, T111).
func (p *traceParser) graphWarnings() {
	t := p.t
	g := newGraph(t)
	line := map[string]int{}
	for _, n := range t.Nodes {
		line[n.ID] = n.Line
	}
	for _, n := range t.Nodes {
		for _, m := range n.Realizes {
			for _, k := range n.Realizes {
				if k != m && g.closure(k, false)[m] {
					p.warn("T105", n.ID, line[n.ID], "realizes %s directly and also through %s; drop the direct link to %s", m, k, m)
					break
				}
			}
		}
	}
	for li, l := range t.Lanes {
		var ids []string
		for _, n := range t.Nodes {
			if n.Lane == l {
				ids = append(ids, n.ID)
			}
		}
		if len(ids) == 0 {
			p.warn("T106", "", p.laneLine[li], "lane %s has no steps; give it steps or remove the lane line", l)
			continue
		}
		if groups := g.groups(ids); len(groups) > 1 {
			var parts []string
			for _, gr := range groups {
				parts = append(parts, strings.Join(gr, ", "))
			}
			p.warn("T107", groups[1][0], line[groups[1][0]], "the steps of lane %s fall into %d groups that no next or back edge joins (%s); a next is probably missing", t.Lanes[li], len(groups), strings.Join(parts, " | "))
		}
	}
	if len(t.Nodes) > AdvisedTraceNodes {
		p.warn("T108", "", t.Line, "%d nodes; aim for %d or fewer, and split a larger process into one block per phase", len(t.Nodes), AdvisedTraceNodes)
	}
	for _, n := range t.Nodes {
		if n.Live() && g.lane(n.ID) > 0 && !g.realizesLive(&n) && n.Why == "" {
			p.warn("T110", n.ID, line[n.ID], "the step realizes nothing above it; add a why saying why it exists")
		}
	}
	seen := map[string]string{}
	for _, n := range t.Nodes {
		k := strings.ToLower(n.ID)
		if o, ok := seen[k]; ok {
			p.warn("T111", n.ID, line[n.ID], "ids %s and %s differ only in letter case; rename one", o, n.ID)
			continue
		}
		seen[k] = n.ID
	}
}

// groups splits ids into the pieces that next and back edges join, each
// in block order, the pieces ordered by their first node.
func (g *graph) groups(ids []string) [][]string {
	adj := map[string][]string{}
	for _, id := range ids {
		n := g.t.Node(id)
		for _, e := range append(slices.Clone(n.Next), n.Back...) {
			adj[id] = append(adj[id], e.To)
			adj[e.To] = append(adj[e.To], id)
		}
	}
	group := map[string]int{}
	var out [][]string
	for _, id := range ids {
		if _, ok := group[id]; ok {
			continue
		}
		k := len(out)
		stack := []string{id}
		group[id] = k
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, w := range adj[v] {
				if _, ok := group[w]; !ok {
					group[w] = k
					stack = append(stack, w)
				}
			}
		}
		out = append(out, nil)
	}
	for _, id := range ids {
		out[group[id]] = append(out[group[id]], id)
	}
	return out
}
