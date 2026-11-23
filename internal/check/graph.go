package check

import (
	"fmt"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// The graph canvas (spec 6.18, 11.1): the lens drawn instead of printed.
// Graph is what the canvas draws and kalka graph --json prints: the
// documents of the lens as cards, their goals, criteria, invariants, terms
// and trace steps as items, the edges between them, and the states and
// open items the rules already compute. Nothing here is new information;
// nothing about layout is in it.

// Kinds of graph nodes.
const (
	GraphDomain    = "domain"    // a domain node
	GraphPlan      = "plan"      // a plan node, leaf or not
	GraphGoal      = "goal"      // items of a card …
	GraphCriterion = "criterion" //
	GraphInvariant = "invariant" //
	GraphTerm      = "term"      //
	GraphStep      = "step"      // … and a trace step
)

// Kinds of graph edges.
const (
	EdgeParent       = "parent"        // plan tree: parent → child
	EdgeDomainParent = "domain-parent" // domain tree: parent → subdomain, Label the pinned version
	EdgePin          = "pin"           // kalka_domain: plan node → domain node
	EdgeRealizes     = "realizes"      // criterion or child goal → the goal it realizes
	EdgeRefines      = "refines"       // invariant → the ancestor term it narrows
	EdgeAffects      = "affects"       // document → an invariant it touches
	EdgeCovers       = "covers"        // trace step → criterion
	EdgeStep         = "step-realizes" // trace step → the step a lane above it realizes
	EdgeGuards       = "guards"        // trace step → invariant
)

// Markers on nodes and edges.
const (
	MarkUncovered    = "uncovered"    // a criterion no trace step covers
	MarkUnguarded    = "unguarded"    // an invariant no step guards
	MarkUnverified   = "unverified"   // an invariant with no verified-by
	MarkParentNewer  = "parent-newer" // D030, on a domain-tree edge
	MarkInconsistent = "inconsistent" // D021: two versions of one ancestor
	MarkCycle        = "cycle"        // kalka_parent leads back
	MarkBlockErrors  = "block-errors" // a typed block that was not read
	MarkAmbiguous    = "ambiguous"    // two documents claim one domain version
	MarkDivergence   = "divergence:"  // prefix: a trace divergence, by class
	MarkNotHere      = "not-here"     // a placeholder
	MarkArchived     = "archived"     //
	MarkClosed       = "closed"       //
	MarkGoalPrefix   = "state:"       // prefix: a goal or criterion state
	MarkDeviation    = "deviation"    // a deviation entry on the item
	MarkUnseenPrefix = "new:"         // prefix: entries not seen yet (app only)
)

// Graph is the lens around one document as nodes and edges.
type Graph struct {
	// Focus is the id of the focus's card.
	Focus string      `json:"focus"`
	Title string      `json:"title"`
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
	// Open are the places that need a person, in the documents held here,
	// grouped by card in lens order.
	Open []OpenItem `json:"open"`
	// Alone is set when the focus has no parent, no domain, no goals and
	// no children: it is not part of a graph.
	Alone bool `json:"alone,omitempty"`
}

// GraphNode is a card (a document) or an item of one.
type GraphNode struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Card is the card an item belongs to; empty for a card.
	Card string `json:"card,omitempty"`
	// Band is "domain" or "plan" for a card.
	Band string `json:"band,omitempty"`
	// A card: its document. Rel is where it is held, when known.
	KalkaID string `json:"kalka_id,omitempty"`
	Rel     string `json:"rel,omitempty"`
	// Kept: the card is a kept domain version, not the live document at
	// Rel (open it with its version). Scope is where it is held: personal
	// or a team's name.
	Kept    bool   `json:"kept,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Title   string `json:"title,omitempty"`
	Issue   string `json:"issue,omitempty"`
	Owner   string `json:"owner,omitempty"`
	Domain  string `json:"domain,omitempty"`
	Version int64  `json:"version,omitempty"`
	// Focus marks the focus's card; Child a child of it; Placeholder a
	// document this copy does not hold.
	Focus       bool `json:"focus,omitempty"`
	Child       bool `json:"child,omitempty"`
	Placeholder bool `json:"placeholder,omitempty"`
	// Errors and Warnings count the document's diagnostics.
	Errors   int `json:"errors,omitempty"`
	Warnings int `json:"warnings,omitempty"`
	// Prose, General and Orphaned count review threads not on an item: in
	// prose sections, on the whole document, on removed sections.
	Prose    int `json:"prose,omitempty"`
	General  int `json:"general,omitempty"`
	Orphaned int `json:"orphaned,omitempty"`
	// An item: its id in the document, its text, and its state.
	Item  string `json:"item,omitempty"`
	Text  string `json:"text,omitempty"`
	State string `json:"state,omitempty"`
	// Anchor is the comment anchor, <slug>#<id>; Threads the threads on it.
	Anchor  string `json:"anchor,omitempty"`
	Threads int    `json:"threads,omitempty"`
	// A step: its trace (1-based), lane, and code reference.
	Trace int    `json:"trace,omitempty"`
	Lane  string `json:"lane,omitempty"`
	Ref   string `json:"ref,omitempty"`
	// Lanes are the step's trace lanes, and Divergences those on the step,
	// so the canvas says them in the Trace view's words.
	Lanes       []string        `json:"lanes,omitempty"`
	Divergences []bp.Divergence `json:"divergences,omitempty"`
	// Markers say what needs attention, and the quiet facts of a card.
	Markers []string `json:"markers,omitempty"`
	// Open counts the open items inside a card.
	Open int `json:"open,omitempty"`
	// New counts entries not seen yet (the app's read marks; never in
	// kalka graph).
	New int `json:"new,omitempty"`
}

// GraphEdge links two nodes.
type GraphEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Kind   string `json:"kind"`
	Label  string `json:"label,omitempty"`
	Dashed bool   `json:"dashed,omitempty"`
	Marker string `json:"marker,omitempty"`
}

// OpenItem is a place that needs a person.
type OpenItem struct {
	Node string `json:"node"`
	Card string `json:"card"`
	// Kind is unclaimed, uncovered, unguarded, unverified or the class of
	// a trace divergence.
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// GraphOptions are Options and what only the app knows.
type GraphOptions struct {
	Options
	// Where says where a document is held, for links; nil leaves it empty.
	Where func(*bp.Document) Where
	// Divergences adds the reader-dependent divergences (unverified and
	// stale references) of a document's trace blocks, by trace index. They
	// are markers on the steps, not open items.
	Divergences func(*bp.Document) map[int][]bp.Divergence
	// Unseen are the entry ids of a document not seen yet.
	Unseen func(*bp.Document) map[string]bool
}

// Where is where a card's document is held: its path relative to the
// folder, whether the card is a kept domain version rather than the live
// document there, and its scope.
type Where struct {
	Rel   string
	Kept  bool
	Scope string
}

func domainKey(name string, v int64) string { return fmt.Sprintf("domain:%s@v%d", name, v) }

func planKey(d *bp.Document, file string) string {
	if id := d.KalkaID(); id != "" {
		return "plan:" + id
	}
	return "plan:file:" + file
}

// graphBuilder holds the graph while it is put together.
type graphBuilder struct {
	g     *Graph
	o     GraphOptions
	lens  Lens
	index map[string]int // node id → index in g.Nodes
	cards map[*bp.Document]string
	// up is each plan card's ancestors, nearest first.
	up map[*bp.Document][]*bp.Document
	// domains by name, the first version in the lens.
	domains map[string]*bp.Document
}

// BuildGraph is the graph of the lens around doc; file names it.
func BuildGraph(file string, doc *bp.Document, o GraphOptions) Graph {
	lens := LensOf(doc, o.Options)
	b := &graphBuilder{g: &Graph{Title: doc.Title(), Nodes: []GraphNode{}, Edges: []GraphEdge{}, Open: []OpenItem{}},
		o: o, lens: lens, index: map[string]int{}, cards: map[*bp.Document]string{}, up: map[*bp.Document][]*bp.Document{}, domains: map[string]*bp.Document{}}
	b.cardsOf(file)
	var docs []*bp.Document // every card with a document, in lens order
	for _, n := range b.g.Nodes {
		for d, id := range b.cards {
			if id == n.ID {
				docs = append(docs, d)
			}
		}
	}
	for _, d := range docs {
		b.items(d)
	}
	for _, d := range docs {
		b.links(d)
	}
	b.openItems(docs)
	b.g.Alone = !doc.IsDomain() && doc.PlanParent() == "" && len(doc.DomainRefsNamed()) == 0 && len(doc.Goals()) == 0 && len(lens.Children) == 0
	return *b.g
}

func (b *graphBuilder) add(n GraphNode) int {
	if i, ok := b.index[n.ID]; ok {
		return i
	}
	b.index[n.ID] = len(b.g.Nodes)
	b.g.Nodes = append(b.g.Nodes, n)
	return len(b.g.Nodes) - 1
}

func (b *graphBuilder) node(id string) *GraphNode {
	if i, ok := b.index[id]; ok {
		return &b.g.Nodes[i]
	}
	return nil
}

func (b *graphBuilder) edge(e GraphEdge) {
	if b.node(e.From) == nil || b.node(e.To) == nil {
		return
	}
	for _, x := range b.g.Edges {
		if x.From == e.From && x.To == e.To && x.Kind == e.Kind {
			return
		}
	}
	b.g.Edges = append(b.g.Edges, e)
}

// card adds a document's card.
func (b *graphBuilder) card(d *bp.Document, id, band string) {
	if prev := b.node(id); prev != nil {
		// Two documents claim one domain version: one card, marked. The
		// same document found again (by another path) is no second claim.
		if !prev.Placeholder && !strings.EqualFold(prev.KalkaID, d.KalkaID()) && !hasMarker(prev.Markers, MarkAmbiguous) {
			prev.Markers = append(prev.Markers, MarkAmbiguous)
		}
		return
	}
	n := GraphNode{ID: id, Kind: GraphPlan, Band: band, KalkaID: d.KalkaID(), Title: d.Title(), Owner: d.Owner()}
	if band == "domain" {
		n.Kind, n.Domain, n.Version = GraphDomain, d.DomainName(), d.DomainVersion()
	}
	if r, ok := d.Issue(); ok {
		n.Issue = r.String()
	}
	if b.o.Where != nil {
		w := b.o.Where(d)
		n.Rel, n.Kept, n.Scope = w.Rel, w.Kept, w.Scope
	}
	if _, ok := d.Archived(); ok {
		n.Markers = append(n.Markers, MarkArchived)
	}
	if d.IsClosed() {
		n.Markers = append(n.Markers, MarkClosed)
	}
	for _, p := range Document(d, b.o.Options).Problems {
		switch p.Severity {
		case bp.Error:
			n.Errors++
		case bp.Warning:
			n.Warnings++
		}
		if (strings.HasPrefix(p.Rule, "S") && p.Severity == bp.Error) && !hasMarker(n.Markers, MarkBlockErrors) {
			n.Markers = append(n.Markers, MarkBlockErrors)
		}
	}
	for _, t := range d.Traces() {
		if !t.Valid() && !hasMarker(n.Markers, MarkBlockErrors) {
			n.Markers = append(n.Markers, MarkBlockErrors)
		}
	}
	b.add(n)
	b.cards[d] = id
}

func hasMarker(ms []string, m string) bool {
	for _, x := range ms {
		if x == m {
			return true
		}
	}
	return false
}

// cardsOf adds the cards of the lens and the tree edges between them:
// the domain band first, root at the top, then the plan tree.
func (b *graphBuilder) cardsOf(file string) {
	l, doc := b.lens, b.lens.Focus
	// Domain band.
	for _, p := range l.Domains.Paths {
		if p.Missing != nil {
			id := domainKey(p.Missing.Domain, p.Missing.Version)
			b.add(GraphNode{ID: id, Kind: GraphDomain, Band: "domain", Domain: p.Missing.Domain, Version: p.Missing.Version, Title: p.Missing.Domain, Placeholder: true, Markers: []string{MarkNotHere}})
		}
		for _, n := range p.Nodes {
			b.card(n, domainKey(n.DomainName(), n.DomainVersion()), "domain")
			if _, ok := b.domains[n.DomainName()]; !ok {
				b.domains[n.DomainName()] = n
			}
		}
	}
	focusID := planKey(doc, file)
	if doc.IsDomain() {
		focusID = domainKey(doc.DomainName(), doc.DomainVersion())
		b.card(doc, focusID, "domain")
	}
	// Domain-tree edges, with the pinned version and "parent newer".
	for _, p := range l.Domains.Paths {
		for i, n := range p.Nodes {
			up, ok := n.DomainParent()
			if !ok {
				continue
			}
			e := GraphEdge{From: domainKey(up.Domain, up.Version), To: domainKey(n.DomainName(), n.DomainVersion()), Kind: EdgeDomainParent, Label: fmt.Sprintf("v%d", up.Version)}
			if i == 0 {
				e.Dashed = true // the parent is the placeholder above
			}
			if b.o.Domains != nil {
				if latest := b.o.Domains.Latest(n.Team(), up.Domain); latest > up.Version {
					e.Marker = MarkParentNewer
					e.Label = fmt.Sprintf("v%d, v%d available", up.Version, latest)
				}
			}
			b.edge(e)
		}
	}
	// Inconsistent ancestor versions (D021): both paths marked.
	for i := 0; i < len(l.Domains.Paths); i++ {
		for j := i + 1; j < len(l.Domains.Paths); j++ {
			for _, x := range l.Domains.Paths[i].Nodes {
				for _, y := range l.Domains.Paths[j].Nodes {
					if x.DomainName() == y.DomainName() && x.DomainVersion() != y.DomainVersion() {
						for _, p := range []DomainPath{l.Domains.Paths[i], l.Domains.Paths[j]} {
							for _, n := range p.Nodes {
								if c := b.node(domainKey(n.DomainName(), n.DomainVersion())); c != nil && !hasMarker(c.Markers, MarkInconsistent) {
									c.Markers = append(c.Markers, MarkInconsistent)
								}
							}
						}
					}
				}
			}
		}
	}
	// Plan band: a missing node above, the path, the focus, the children.
	prev := ""
	if l.Missing != "" {
		prev = "plan:" + strings.ToLower(l.Missing)
		b.add(GraphNode{ID: prev, Kind: GraphPlan, Band: "plan", KalkaID: strings.ToLower(l.Missing), Title: l.Missing, Placeholder: true, Markers: []string{MarkNotHere}})
	}
	var chain []*bp.Document
	for _, p := range l.Plan {
		id := planKey(p, "")
		b.card(p, id, "plan")
		if prev != "" {
			b.edge(GraphEdge{From: prev, To: id, Kind: EdgeParent, Dashed: b.node(prev).Placeholder})
		}
		b.up[p] = reversed(chain)
		chain = append(chain, p)
		prev = id
	}
	if !doc.IsDomain() {
		b.card(doc, focusID, "plan")
		b.up[doc] = reversed(chain)
		if prev != "" {
			b.edge(GraphEdge{From: prev, To: focusID, Kind: EdgeParent, Dashed: b.node(prev).Placeholder})
		}
		if l.Cycle && len(l.Plan) > 0 {
			// Cut at the first repeated node: the edge back, marked.
			top := l.Plan[0]
			for d, id := range b.cards {
				if strings.EqualFold(d.KalkaID(), top.PlanParent()) {
					b.g.Edges = append(b.g.Edges, GraphEdge{From: id, To: b.cards[top], Kind: EdgeParent, Marker: MarkCycle})
					break
				}
			}
		}
	}
	b.node(focusID).Focus = true
	b.g.Focus = focusID
	for _, c := range l.Children {
		var id string
		if doc.IsDomain() {
			id = domainKey(c.DomainName(), c.DomainVersion())
			b.card(c, id, "domain")
			e := GraphEdge{From: focusID, To: id, Kind: EdgeDomainParent}
			if up, ok := c.DomainParent(); ok {
				e.Label = fmt.Sprintf("v%d", up.Version)
			}
			b.edge(e)
		} else {
			id = planKey(c, "")
			b.card(c, id, "plan")
			b.up[c] = append([]*bp.Document{doc}, reversed(chain)...)
			b.edge(GraphEdge{From: focusID, To: id, Kind: EdgeParent})
		}
		if n := b.node(id); n != nil {
			n.Child = true
		}
	}
	// kalka_domain pins of the plan cards, to domain cards in the lens.
	for _, n := range b.g.Nodes {
		if n.Band != "plan" || n.Placeholder {
			continue
		}
		for d, id := range b.cards {
			if id != n.ID {
				continue
			}
			for _, r := range d.DomainRefsNamed() {
				b.edge(GraphEdge{From: id, To: domainKey(r.Domain, r.Version), Kind: EdgePin, Label: fmt.Sprintf("v%d", r.Version)})
			}
		}
	}
}

func reversed(ds []*bp.Document) []*bp.Document {
	out := make([]*bp.Document, 0, len(ds))
	for i := len(ds) - 1; i >= 0; i-- {
		out = append(out, ds[i])
	}
	return out
}

// review counts a document's threads: root comments and deviations, by
// anchor.
type review struct {
	threads    map[string]int
	deviations map[string]bool
	unseen     map[string]int
	prose      int
	general    int
	orphaned   int
	unseenAll  int
}

func (b *graphBuilder) reviewOf(d *bp.Document, anchors map[string]bool) review {
	r := review{threads: map[string]int{}, deviations: map[string]bool{}, unseen: map[string]int{}}
	slugs := map[string]bool{}
	for _, h := range d.Headings() {
		slugs[h.Slug] = true
	}
	var unseen map[string]bool
	if b.o.Unseen != nil {
		unseen = b.o.Unseen(d)
	}
	for _, it := range d.Items() {
		e := it.Entry
		if e == nil || (e.Kind != "comment" && e.Kind != "deviation") {
			continue
		}
		isNew := unseen[e.ID]
		if isNew {
			r.unseenAll++
		}
		if e.ReplyTo() != "" {
			if isNew && anchors[e.Anchor] {
				r.unseen[e.Anchor]++
			}
			continue
		}
		switch {
		case e.Anchor == "":
			r.general++
		case anchors[e.Anchor]:
			r.threads[e.Anchor]++
			if e.Kind == "deviation" {
				r.deviations[e.Anchor] = true
			}
			if isNew {
				r.unseen[e.Anchor]++
			}
		case slugs[e.Section()]:
			r.prose++
		default:
			r.orphaned++
		}
	}
	return r
}

// items adds the goals, criteria, invariants, terms and trace steps of a
// document's card.
func (b *graphBuilder) items(d *bp.Document) {
	cardID := b.cards[d]
	card := b.node(cardID)
	if card == nil || card.Placeholder {
		return
	}
	var add []GraphNode
	anchor := func(section, id string) string {
		if section == "" {
			return ""
		}
		return section + "#" + id
	}
	states := map[string]string{}
	if !d.IsDomain() {
		var walk func(ns []GoalNode)
		walk = func(ns []GoalNode) {
			for _, n := range ns {
				if n.Own {
					states[n.Kind+":"+n.ID] = n.State
				}
				walk(n.Claimants)
			}
		}
		walk(GoalTree(d, b.o.Docs))
	}
	for _, g := range d.Goals() {
		n := GraphNode{ID: cardID + "/" + g.Kind + ":" + g.ID, Kind: g.Kind, Card: cardID, Item: g.ID, Text: g.Text, State: states[g.Kind+":"+g.ID], Anchor: anchor(g.Section, g.ID)}
		if g.Kind == bp.KindCriterion && len(d.CoveredBy(g.ID)) == 0 {
			n.Markers = append(n.Markers, MarkUncovered)
		}
		add = append(add, n)
	}
	gaps := map[string][]string{}
	if len(d.Invariants()) > 0 {
		for _, r := range Matrix(d, domainContext(d, b.o.Domains), b.o.Docs) {
			if r.Domain == "" {
				gaps[r.ID] = r.Gaps
			}
		}
	}
	for _, v := range d.Invariants() {
		n := GraphNode{ID: cardID + "/invariant:" + v.ID, Kind: GraphInvariant, Card: cardID, Item: v.ID, Text: v.Statement, State: v.Status, Anchor: anchor(v.Section, v.ID)}
		n.Markers = append(n.Markers, gaps[v.ID]...)
		add = append(add, n)
	}
	for _, t := range d.Glossary() {
		add = append(add, GraphNode{ID: cardID + "/term:" + t.Term, Kind: GraphTerm, Card: cardID, Item: t.Term, Text: t.Definition})
	}
	var extra map[int][]bp.Divergence
	if b.o.Divergences != nil {
		extra = b.o.Divergences(d)
	}
	for _, t := range d.Traces() {
		if !t.Valid() {
			continue
		}
		divs := append(append([]bp.Divergence(nil), t.Divergences...), extra[t.Index]...)
		for _, s := range t.Nodes {
			n := GraphNode{ID: stepKey(cardID, t.Index, s.ID), Kind: GraphStep, Card: cardID, Item: s.ID, Text: s.Text, State: s.Change, Trace: t.Index + 1, Lane: s.Lane, Ref: s.Ref, Anchor: anchor(t.Section, s.ID), Lanes: t.Lanes}
			for _, dv := range divs {
				if dv.Node == s.ID {
					n.Markers = append(n.Markers, MarkDivergence+dv.Class)
					n.Divergences = append(n.Divergences, dv)
				}
			}
			add = append(add, n)
		}
	}
	anchors := map[string]bool{}
	for _, n := range add {
		if n.Anchor != "" {
			anchors[n.Anchor] = true
		}
	}
	rv := b.reviewOf(d, anchors)
	card.Prose, card.General, card.Orphaned, card.New = rv.prose, rv.general, rv.orphaned, rv.unseenAll
	for _, n := range add {
		if n.Anchor != "" {
			n.Threads = rv.threads[n.Anchor]
			if rv.deviations[n.Anchor] {
				n.Markers = append(n.Markers, MarkDeviation)
			}
			n.New = rv.unseen[n.Anchor]
		}
		b.add(n)
	}
}

func stepKey(card string, trace int, id string) string {
	return fmt.Sprintf("%s/step:%d:%s", card, trace+1, id)
}

// invariantNode resolves an invariant reference written in d: its own, a
// plan node's above (parent#), or a domain node's.
func (b *graphBuilder) invariantNode(d *bp.Document, ref string) string {
	r, ok := bp.ParseInvRef(ref)
	if !ok {
		return ""
	}
	switch {
	case r.Domain == "":
		return b.cards[d] + "/invariant:" + r.ID
	case r.IsParent():
		for _, a := range b.up[d] {
			if a.Invariant(r.ID) != nil {
				return b.cards[a] + "/invariant:" + r.ID
			}
		}
	default:
		if n := b.domains[r.Domain]; n != nil {
			return b.cards[n] + "/invariant:" + r.ID
		}
	}
	return ""
}

// links adds the item edges written in d.
func (b *graphBuilder) links(d *bp.Document) {
	cardID := b.cards[d]
	if n := b.node(cardID); n == nil || n.Placeholder {
		return
	}
	var parent *bp.Document
	if up := b.up[d]; len(up) > 0 {
		parent = up[0]
	}
	for _, g := range d.Goals() {
		from := cardID + "/" + g.Kind + ":" + g.ID
		for _, r := range g.Realizes {
			ref, ok := bp.ParseInvRef(r)
			switch {
			case !ok:
			case ref.Domain == "":
				b.edge(GraphEdge{From: from, To: cardID + "/goal:" + ref.ID, Kind: EdgeRealizes})
			case ref.IsParent() && parent != nil:
				b.edge(GraphEdge{From: from, To: b.cards[parent] + "/goal:" + ref.ID, Kind: EdgeRealizes})
			}
		}
	}
	for _, v := range d.Invariants() {
		for _, t := range v.Refines {
			if to := b.termNode(d, t); to != "" {
				b.edge(GraphEdge{From: cardID + "/invariant:" + v.ID, To: to, Kind: EdgeRefines})
			}
		}
	}
	for _, a := range d.Affects() {
		if to := b.invariantNode(d, a.Text); to != "" {
			b.edge(GraphEdge{From: cardID, To: to, Kind: EdgeAffects})
		}
	}
	for _, t := range d.Traces() {
		if !t.Valid() {
			continue
		}
		for _, s := range t.Nodes {
			from := stepKey(cardID, t.Index, s.ID)
			for _, c := range s.Covers {
				b.edge(GraphEdge{From: from, To: cardID + "/criterion:" + c, Kind: EdgeCovers})
			}
			for _, r := range s.Realizes {
				b.edge(GraphEdge{From: from, To: stepKey(cardID, t.Index, r), Kind: EdgeStep})
			}
			for _, g := range s.Guards {
				if to := b.invariantNode(d, g); to != "" {
					b.edge(GraphEdge{From: from, To: to, Kind: EdgeGuards})
				}
			}
		}
	}
}

// termNode finds a term an invariant of d refines: on d's own domain path,
// nearest first.
func (b *graphBuilder) termNode(d *bp.Document, term string) string {
	k := bp.PhraseKey(term)
	var path []*bp.Document
	for _, p := range domainContext(d, b.o.Domains).Paths {
		path = append(path, p.Nodes...)
	}
	for i := len(path) - 1; i >= 0; i-- {
		n := path[i]
		for _, t := range n.Glossary() {
			if bp.PhraseKey(t.Term) == k || containsKey(t.Forms, k) {
				return domainKey(n.DomainName(), n.DomainVersion()) + "/term:" + t.Term
			}
		}
	}
	return ""
}

func containsKey(forms []string, k string) bool {
	for _, f := range forms {
		if bp.PhraseKey(f) == k {
			return true
		}
	}
	return false
}

// openItems lists what needs a person, card by card, and counts it on
// each card. Reader-dependent divergences (unverified and stale
// references) are markers only: they depend on the reader's plugins.
func (b *graphBuilder) openItems(docs []*bp.Document) {
	for _, d := range docs {
		cardID := b.cards[d]
		card := b.node(cardID)
		if card == nil || card.Placeholder {
			continue
		}
		readerDependent := map[string]bool{}
		if b.o.Divergences != nil {
			for _, divs := range b.o.Divergences(d) {
				for _, dv := range divs {
					readerDependent[dv.Class+":"+dv.Node] = true
				}
			}
		}
		graphDivs := map[string][]string{}
		for _, t := range d.Traces() {
			for _, dv := range t.Divergences {
				graphDivs[stepKey(cardID, t.Index, dv.Node)] = append(graphDivs[stepKey(cardID, t.Index, dv.Node)], dv.Class)
			}
		}
		count := 0
		for i := range b.g.Nodes {
			n := &b.g.Nodes[i]
			if n.Card != cardID {
				continue
			}
			var add []OpenItem
			switch n.Kind {
			case GraphGoal:
				if n.State == GoalUnclaimed {
					add = append(add, OpenItem{Kind: "unclaimed", Text: "goal " + n.Item + " is unclaimed"})
				}
			case GraphCriterion:
				if hasMarker(n.Markers, MarkUncovered) {
					add = append(add, OpenItem{Kind: MarkUncovered, Text: "criterion " + n.Item + ": no trace step covers it"})
				}
			case GraphInvariant:
				for _, m := range []string{MarkUnguarded, MarkUnverified} {
					if hasMarker(n.Markers, m) {
						add = append(add, OpenItem{Kind: m, Text: "invariant " + n.Item + " is " + m})
					}
				}
			case GraphStep:
				for _, c := range graphDivs[n.ID] {
					add = append(add, OpenItem{Kind: c, Text: "step " + n.Item + ": " + c})
				}
			}
			for _, o := range add {
				o.Node, o.Card = n.ID, cardID
				b.g.Open = append(b.g.Open, o)
				count++
			}
		}
		card.Open = count
	}
}
