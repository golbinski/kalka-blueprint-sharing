package check

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// The agent context (spec 6.18, `kalka context`): what the focus
// document's work needs from the documents of its lens, how current each
// part is, and what is missing. The lens selects the documents; items are
// selected after it, from typed blocks and frontmatter only. The focus's
// own blocks, prose and frontmatter are never repeated: the agent reads
// the document itself.

// Context modes.
const (
	ContextFocused = "focused" // only what the focus's work needs
	ContextFull    = "full"    // every goal, invariant and term on the paths
)

// NotApplicable is the state of a document that has no approval state:
// Personal, a seed, a tombstone.
const NotApplicable = "approval not applicable"

// AuthorityNote heads every context, the same text every time.
var AuthorityNote = []string{
	"Domain terms and invariants are binding at the pinned versions.",
	"An approved blueprint describes an intended change as of its approval; the code may have moved since.",
}

// AgentContext is the context of one document for an agent. Its fields
// are in the order the Markdown prints its sections.
type AgentContext struct {
	Document   string            `json:"document"`
	Mode       string            `json:"mode"`
	Authority  []string          `json:"authority"`
	Focus      ContextFocus      `json:"focus"`
	Gaps       ContextGaps       `json:"gaps"`
	Goals      []GoalLevel       `json:"goals"`
	Invariants ContextInvariants `json:"invariants"`
	Terms      []ContextTerm     `json:"terms"`
	// TermsLeftOut are the names of the terms on the domain path that are
	// not printed: an agent asks for one with --term.
	TermsLeftOut []string `json:"terms_left_out"`
	Problems     []string `json:"problems,omitempty"`
}

// ContextDoc is a plan document named in the context, with how current it
// is: its approval state and its latest release as the file says.
type ContextDoc struct {
	KalkaID string `json:"kalka_id,omitempty"`
	Title   string `json:"title"`
	Issue   string `json:"issue,omitempty"`
	// State is the approval state in words, or "approval not applicable".
	State    string       `json:"state"`
	Approval *bp.Approval `json:"approval,omitempty"`
	// Released is the latest release's version, 0 when unreleased.
	Released   int64  `json:"released,omitempty"`
	ReleasedAt string `json:"released_at,omitempty"`
	// Domain is a domain focus's name and version, library@v2: it has a
	// version, not an approval.
	Domain string `json:"domain,omitempty"`
}

// ContextFocus is the focus document: its name, state and open items.
type ContextFocus struct {
	ContextDoc
	// Kind is leaf, plan-node, domain or seed.
	Kind string `json:"kind"`
	// Open are its open structural items, as the Graph lists them.
	Open []string `json:"open"`
}

// ContextGaps say what is missing and whose job the rest is.
type ContextGaps struct {
	// NotHere are the plan nodes and domain versions on the paths that
	// are not held, and the references that do not resolve.
	NotHere []string `json:"not_here"`
	// Claims are, for the direct parent and for the focus as a plan node,
	// which other documents claim the goals.
	Claims []ClaimGroup `json:"claims"`
	// Unclaimed are the direct parent's goals nothing claims.
	Unclaimed *UnclaimedGoals `json:"unclaimed,omitempty"`
}

// ClaimGroup is one document's goals with the children that claim them.
type ClaimGroup struct {
	Of ContextDoc `json:"of"`
	// Parent: the goals are the direct parent's (parent#<id>); otherwise
	// the focus's own.
	Parent bool           `json:"parent,omitempty"`
	Goals  []GoalClaims   `json:"goals"`
	Others int            `json:"others"`
	Looked bool           `json:"others_looked_for"`
	byKey  []*bp.Document // the children listed
}

// GoalClaims is one goal with the focus's items and the other documents
// that realize it.
type GoalClaims struct {
	Goal string   `json:"goal"`
	Here []string `json:"here,omitempty"`
	Also []Claim  `json:"also"`
}

// Claim is another document's items realizing a goal.
type Claim struct {
	Doc   ContextDoc `json:"doc"`
	Items []string   `json:"items"`
}

// UnclaimedGoals are the direct parent's goals nothing claims, when its
// children were read; LookedFor is false otherwise, with why.
type UnclaimedGoals struct {
	Of        string   `json:"of"`
	LookedFor bool     `json:"looked_for"`
	Why       string   `json:"why,omitempty"`
	Goals     []string `json:"goals"`
}

// GoalLevel is one plan node of the goal chain, from the direct parent up
// to the root.
type GoalLevel struct {
	Doc   ContextDoc    `json:"doc"`
	Goals []ContextGoal `json:"goals"`
	// LeftOut are the ids of its other goals and criteria.
	LeftOut []string `json:"left_out"`
}

// ContextGoal is a goal of the chain and what below realizes it.
type ContextGoal struct {
	Kind       string        `json:"kind"`
	ID         string        `json:"id"`
	Text       string        `json:"text"`
	Realizes   []string      `json:"realizes,omitempty"`
	RealizedBy []string      `json:"realized_by,omitempty"`
	Steps      []bp.GoalStep `json:"steps,omitempty"`
}

// ContextInvariants are the invariants that bind the focus: the plan
// path's, root first, then each domain path's.
type ContextInvariants struct {
	Plan    []InvariantLine `json:"plan"`
	Domains []DomainPathCtx `json:"domains"`
}

// InvariantLine is one invariant, as the focus names it, with whether the
// focus affects it and which of its steps guard it.
type InvariantLine struct {
	Ref       string   `json:"ref"`
	Of        string   `json:"of"`
	Statement string   `json:"statement"`
	Refines   []string `json:"refines,omitempty"`
	Affects   bool     `json:"affects,omitempty"`
	GuardedBy []string `json:"guarded_by,omitempty"`
	// Full context only.
	Why        string   `json:"why,omitempty"`
	VerifiedBy []string `json:"verified_by,omitempty"`
	Status     string   `json:"status,omitempty"`
}

// DomainPathCtx is one domain path at the pinned versions, root first.
type DomainPathCtx struct {
	Ref   string   `json:"ref"`
	Nodes []string `json:"nodes"`
	// Newer says a newer version of the pinned node changes something the
	// focus uses (the condition of R003).
	Newer      string          `json:"newer,omitempty"`
	Invariants []InvariantLine `json:"invariants"`
}

// ContextTerm is a term of the domain path with its narrowings.
type ContextTerm struct {
	Ref        string          `json:"ref"`
	Term       string          `json:"term"`
	Definition string          `json:"definition"`
	Aliases    []string        `json:"aliases,omitempty"`
	Forms      []string        `json:"forms,omitempty"`
	See        []string        `json:"see,omitempty"`
	Refined    []bp.Refinement `json:"refined,omitempty"`
}

// ctx is one context being built.
type ctx struct {
	c    *AgentContext
	doc  *bp.Document
	o    Options
	lens Lens
	full bool
}

// Context builds the agent context of doc (file is how it is named):
// focused, or with full every goal, invariant and term on the paths.
func Context(file string, doc *bp.Document, o Options, full bool) AgentContext {
	c := AgentContext{Document: file, Mode: ContextFocused, Authority: slices.Clone(AuthorityNote), Goals: []GoalLevel{},
		Invariants: ContextInvariants{Plan: []InvariantLine{}, Domains: []DomainPathCtx{}}, Terms: []ContextTerm{}, TermsLeftOut: []string{},
		Gaps: ContextGaps{NotHere: []string{}, Claims: []ClaimGroup{}}}
	if full {
		c.Mode = ContextFull
	}
	x := &ctx{c: &c, doc: doc, o: o, lens: LensOf(doc, o), full: full}
	x.focus(file)
	x.notHere()
	chain := x.goals()
	x.claims(chain)
	x.unclaimed()
	x.planInvariants()
	x.domainInvariants()
	x.terms()
	return c
}

// docOf names a plan document with how current it is.
func (x *ctx) docOf(d *bp.Document) ContextDoc {
	c := ContextDoc{KalkaID: d.KalkaID(), Title: d.Title(), State: NotApplicable}
	if r, ok := d.Issue(); ok {
		c.Issue = r.String()
	}
	if a := x.o.approval(d); a != nil {
		c.Approval, c.State = a, a.Text()
	}
	if r, at, ok := d.FileRelease(); ok && !r.Deleted {
		c.Released, c.ReleasedAt = r.Version, at
	}
	return c
}

// Name is a document as a heading names it: issue key and title.
func (c ContextDoc) Name() string {
	if k := c.short(); k != "" {
		return k + " " + c.Title
	}
	return c.Title
}

// short is the issue key, or "" when it has none.
func (c ContextDoc) short() string {
	if _, p, ok := strings.Cut(c.Issue, ":"); ok {
		return p
	}
	return ""
}

// Ref is a document as a line refers to it: its issue key, or its title.
func (c ContextDoc) Ref() string {
	if k := c.short(); k != "" {
		return k
	}
	return fmt.Sprintf("%q", c.Title)
}

// Currency is the state with the latest release: "approved v1, released
// v1 at …", "draft, unreleased".
func (c ContextDoc) Currency() string {
	if c.Domain != "" {
		return "domain node " + c.Domain + ", " + c.State
	}
	if c.Released == 0 {
		return c.State + ", unreleased"
	}
	return fmt.Sprintf("%s, released v%d at %s", c.State, c.Released, c.ReleasedAt)
}

func (x *ctx) focus(file string) {
	f := ContextFocus{ContextDoc: x.docOf(x.doc), Kind: "leaf", Open: []string{}}
	switch {
	case x.doc.IsDomain():
		f.Kind = "domain"
		f.State, f.Released, f.ReleasedAt = NotApplicable, 0, ""
		f.Domain = fmt.Sprintf("%s@v%d", x.doc.DomainName(), x.doc.DomainVersion())
	case x.doc.IsSeed():
		f.Kind = "seed"
	case hasGoal(x.doc):
		f.Kind = "plan-node"
	}
	g := BuildGraph(file, x.doc, GraphOptions{Options: x.o})
	for _, it := range g.Open {
		if it.Card == g.Focus {
			f.Open = append(f.Open, it.Text)
		}
	}
	x.c.Focus = f
}

// parent is the direct parent when it is held, else nil.
func (x *ctx) parent() *bp.Document {
	l := x.lens
	if l.Cycle || len(l.Plan) == 0 || !strings.EqualFold(l.Plan[len(l.Plan)-1].KalkaID(), x.doc.PlanParent()) {
		return nil
	}
	return l.Plan[len(l.Plan)-1]
}

// searched reports whether the scope was read beyond the files given
// (the condition of P011), so a child not found is not there.
func (x *ctx) searched() bool {
	if x.o.Docs == nil {
		return false
	}
	if s, ok := x.o.Docs.(Searcher); ok {
		return s.Searched()
	}
	return true
}

// personal reports a document of no team: never listed as a sibling, and
// a focus in Personal gets no sibling lines.
func personal(d *bp.Document) bool { return d.Team() == "" }

func (x *ctx) notHere() {
	l := x.lens
	if l.Missing != "" {
		if len(l.Plan) == 0 {
			x.gap("the parent plan node %s (kalka_parent)", l.Missing)
		} else {
			x.gap("plan node %s, the parent of %s", l.Missing, x.docOf(l.Plan[0]).Ref())
		}
	}
	if l.Cycle {
		x.c.Problems = append(x.c.Problems, "kalka_parent leads back to the document; the plan path stops there")
	}
	for _, p := range l.Domains.Paths {
		if p.Missing != nil {
			x.gap("domain version %s", p.Missing.String())
		}
		if p.Cycle {
			x.c.Problems = append(x.c.Problems, "the domain pins of "+p.Ref.Domain+" lead back; the domain path stops there")
		}
	}
	if x.doc.IsDomain() {
		return
	}
	// parent#<id> realized, affected or guarded that does not resolve.
	parent := x.parent()
	if parent != nil {
		for _, g := range x.doc.Goals() {
			for _, r := range g.Realizes {
				if ref, ok := bp.ParseInvRef(r); ok && ref.IsParent() {
					if pg := parent.Goal(ref.ID); pg == nil || pg.Kind != bp.KindGoal {
						x.gap("%s, which %s realizes, is not a goal of %s", r, g.ID, x.docOf(parent).Ref())
					}
				}
			}
		}
	}
	if l.Missing == "" && !l.Cycle && len(l.Plan) > 0 {
		for _, a := range x.doc.ParentRefs() {
			if !slices.ContainsFunc(l.Plan, func(d *bp.Document) bool { return d.Invariant(a.Ref.ID) != nil }) {
				x.gap("%s, which the document names, is not an invariant on the plan path", a.Text)
			}
		}
	}
	if l.Domains.Complete() {
		for _, a := range x.doc.DomainRefs() {
			n := l.Domains.ByName[a.Ref.Domain]
			switch {
			case n == nil:
				x.gap("%s: %s is not on the domain path", a.Text, a.Ref.Domain)
			case n.Invariant(a.Ref.ID) == nil:
				x.gap("%s: %s@v%d has no invariant %s", a.Text, n.DomainName(), n.DomainVersion(), a.Ref.ID)
			}
		}
	}
}

func (x *ctx) gap(format string, a ...any) {
	s := fmt.Sprintf(format, a...)
	if !slices.Contains(x.c.Gaps.NotHere, s) {
		x.c.Gaps.NotHere = append(x.c.Gaps.NotHere, s)
	}
}

// goals builds the chain from the direct parent up to the root: the goals
// the focus realizes, and upwards the goals those realize. It returns the
// ids selected at the direct parent with the focus's items realizing them.
func (x *ctx) goals() map[string][]string {
	path := x.lens.Plan
	if x.doc.IsDomain() || x.parent() == nil {
		return nil
	}
	// want[i] are the ids selected at path[i], with what realizes them.
	want := make([]map[string][]string, len(path))
	for i := range want {
		want[i] = map[string][]string{}
	}
	top := len(path) - 1
	here := map[string][]string{}
	for _, g := range x.doc.Goals() {
		for _, r := range g.Realizes {
			if ref, ok := bp.ParseInvRef(r); ok && ref.IsParent() {
				if pg := path[top].Goal(ref.ID); pg != nil && pg.Kind == bp.KindGoal {
					want[top][ref.ID] = append(want[top][ref.ID], g.ID+" realizes "+r)
					here[ref.ID] = append(here[ref.ID], g.ID)
				}
			}
		}
	}
	for i := top; i >= 0; i-- {
		d := path[i]
		lv := GoalLevel{Doc: x.docOf(d), Goals: []ContextGoal{}, LeftOut: []string{}}
		for _, g := range d.Goals() {
			by, ok := want[i][g.ID]
			if !ok && !x.full {
				lv.LeftOut = append(lv.LeftOut, g.ID)
				continue
			}
			cg := ContextGoal{Kind: g.Kind, ID: g.ID, Text: g.Text, Realizes: g.Realizes, RealizedBy: by}
			if x.full {
				cg.Steps = g.Steps
			}
			lv.Goals = append(lv.Goals, cg)
			if i == 0 || !ok && !x.full {
				continue
			}
			for _, r := range g.Realizes {
				if ref, ok := bp.ParseInvRef(r); ok && ref.IsParent() {
					if pg := path[i-1].Goal(ref.ID); pg != nil && pg.Kind == bp.KindGoal {
						want[i-1][ref.ID] = append(want[i-1][ref.ID], lv.Doc.Ref()+" "+g.ID+" realizes "+r)
					}
				}
			}
		}
		x.c.Goals = append(x.c.Goals, lv)
	}
	return here
}

// claims lists, under each selected goal of the direct parent, the other
// children that claim it, and for a plan node its own goals' claims by its
// children. A Personal document is never listed, and a focus in Personal
// gets no such lines.
func (x *ctx) claims(here map[string][]string) {
	if x.doc.IsDomain() || personal(x.doc) {
		return
	}
	looked := x.searched()
	if parent := x.parent(); parent != nil && len(here) > 0 && x.o.Docs != nil {
		grp := ClaimGroup{Of: x.docOf(parent), Parent: true, Goals: []GoalClaims{}, Looked: looked}
		sibs := x.teamChildren(parent)
		for _, g := range parent.Goals() {
			ids, ok := here[g.ID]
			if !ok {
				continue
			}
			grp.Goals = append(grp.Goals, x.goalClaims(&grp, bp.ParentRef+"#"+g.ID, g.ID, ids, sibs))
		}
		grp.Others = len(sibs) - len(grp.byKey)
		x.c.Gaps.Claims = append(x.c.Gaps.Claims, grp)
	}
	if hasGoal(x.doc) && x.o.Docs != nil && x.doc.KalkaID() != "" {
		kids := x.teamChildren(x.doc)
		if len(kids) == 0 {
			return
		}
		grp := ClaimGroup{Of: x.c.Focus.ContextDoc, Goals: []GoalClaims{}, Looked: looked}
		for _, g := range x.doc.Goals() {
			if g.Kind != bp.KindGoal {
				continue
			}
			var own []string
			for _, c := range x.doc.Goals() {
				if c.Kind == bp.KindCriterion && slices.Contains(c.Realizes, g.ID) {
					own = append(own, c.ID)
				}
			}
			gc := x.goalClaims(&grp, g.ID, g.ID, own, kids)
			if len(gc.Also) > 0 {
				grp.Goals = append(grp.Goals, gc)
			}
		}
		grp.Others = len(kids) - len(grp.byKey)
		x.c.Gaps.Claims = append(x.c.Gaps.Claims, grp)
	}
}

// teamChildren are d's children in the focus's team, the focus left out.
func (x *ctx) teamChildren(d *bp.Document) []*bp.Document {
	var out []*bp.Document
	for _, c := range children(x.o.Docs, d.KalkaID()) {
		if c == x.doc || strings.EqualFold(c.KalkaID(), x.doc.KalkaID()) {
			continue
		}
		if c.IsDomain() || personal(c) || !strings.EqualFold(c.Team(), x.doc.Team()) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (x *ctx) goalClaims(grp *ClaimGroup, name, id string, here []string, kids []*bp.Document) GoalClaims {
	gc := GoalClaims{Goal: name, Here: here, Also: []Claim{}}
	for _, k := range kids {
		var items []string
		for _, c := range k.Goals() {
			if slices.Contains(c.Realizes, bp.ParentRef+"#"+id) {
				items = append(items, c.ID)
			}
		}
		if len(items) == 0 {
			continue
		}
		gc.Also = append(gc.Also, Claim{Doc: x.docOf(k), Items: items})
		if !slices.Contains(grp.byKey, k) {
			grp.byKey = append(grp.byKey, k)
		}
	}
	return gc
}

// unclaimed are the direct parent's goals nothing claims, only when its
// children were read.
func (x *ctx) unclaimed() {
	if x.doc.IsDomain() || x.doc.PlanParent() == "" || x.lens.Cycle {
		return
	}
	u := &UnclaimedGoals{Goals: []string{}}
	parent := x.parent()
	switch {
	case parent == nil:
		u.Of, u.Why = "the parent", "the parent is not here"
	case !x.searched():
		u.Of, u.Why = x.docOf(parent).Ref(), "only the files given were read"
	default:
		u.Of, u.LookedFor = x.docOf(parent).Ref(), true
		for _, n := range GoalTree(parent, x.o.Docs) {
			if n.Kind == bp.KindGoal && n.State == GoalUnclaimed {
				u.Goals = append(u.Goals, bp.ParentRef+"#"+n.ID)
			}
		}
	}
	x.c.Gaps.Unclaimed = u
}

// marks say how the focus names an invariant: affects, and the steps
// that guard it.
func (x *ctx) marks(l *InvariantLine) {
	for _, a := range x.doc.Affects() {
		if norm(a.Text) == l.Ref {
			l.Affects = true
		}
	}
	for _, t := range x.doc.Traces() {
		for _, n := range t.Nodes {
			for _, g := range n.Guards {
				if norm(g) == l.Ref && !slices.Contains(l.GuardedBy, n.ID) {
					l.GuardedBy = append(l.GuardedBy, n.ID)
				}
			}
		}
	}
}

func norm(ref string) string {
	if r, ok := bp.ParseInvRef(ref); ok {
		return r.String()
	}
	return ref
}

func (x *ctx) line(ref, of string, v bp.Invariant) InvariantLine {
	l := InvariantLine{Ref: ref, Of: of, Statement: v.Statement, Refines: v.Refines}
	if x.full {
		l.Why, l.VerifiedBy, l.Status = v.Why, v.VerifiedBy, v.Status
	}
	x.marks(&l)
	return l
}

func (x *ctx) planInvariants() {
	if x.doc.IsDomain() {
		return
	}
	for _, d := range x.lens.Plan {
		for _, v := range d.Invariants() {
			if !v.Live() && !x.full {
				continue
			}
			x.c.Invariants.Plan = append(x.c.Invariants.Plan, x.line(bp.ParentRef+"#"+v.ID, x.docOf(d).Ref(), v))
		}
	}
}

func (x *ctx) domainInvariants() {
	if !x.doc.IsDomain() && len(bp.DomainList(fmValue(x.doc, bp.KeyDomain))) == 0 {
		return
	}
	g := GlossaryOf(x.doc, x.lens.Domains)
	shown := map[string]bool{}
	for i, p := range x.lens.Domains.Paths {
		ref := p.Ref.String()
		if x.doc.IsDomain() {
			ref = fmt.Sprintf("%s@v%d", x.doc.DomainName(), x.doc.DomainVersion())
		}
		dp := DomainPathCtx{Ref: ref, Nodes: []string{}, Invariants: []InvariantLine{}}
		for _, n := range p.Nodes {
			if x.doc.IsDomain() && n.DomainName() == x.doc.DomainName() {
				continue // the focus itself
			}
			k := fmt.Sprintf("%s@v%d", n.DomainName(), n.DomainVersion())
			dp.Nodes = append(dp.Nodes, k)
			if shown[k] {
				continue
			}
			shown[k] = true
			for _, v := range n.Invariants() {
				if !v.Live() && !x.full {
					continue
				}
				dp.Invariants = append(dp.Invariants, x.line(n.DomainName()+"#"+v.ID, k, v))
			}
		}
		if !x.doc.IsDomain() && x.o.Domains != nil {
			if latest := x.o.Domains.Latest(p.Ref.Team, p.Ref.Domain); latest > p.Ref.Version {
				newer := x.o.Domains.Version(p.Ref.Team, p.Ref.Domain, latest)
				if what := touched(x.doc, x.lens.Domains.node(i), newer, g); what != "" {
					dp.Newer = fmt.Sprintf("newer version available: v%d of %s/%s changes %s, which the plan uses", latest, p.Ref.Team, p.Ref.Domain, what)
				}
			}
		}
		x.c.Invariants.Domains = append(x.c.Invariants.Domains, dp)
	}
}

// pathTerms are the terms the focus is read against: those of its domain
// paths; for a domain document, its ancestors' only. None for a document
// naming no domain: its own glossary stays in its file.
func (x *ctx) pathTerms() []bp.Term {
	if !x.doc.IsDomain() && len(bp.DomainList(fmValue(x.doc, bp.KeyDomain))) == 0 {
		return nil
	}
	g := GlossaryOf(x.doc, x.lens.Domains)
	if g.State != GlossaryHeld {
		return nil
	}
	var out []bp.Term
	for _, t := range g.Terms {
		if x.doc.IsDomain() && t.Domain == x.doc.DomainName() {
			continue
		}
		out = append(out, t)
	}
	return out
}

// terms selects the terms used: those Document.TermUses finds in the
// focus (and, for a domain document, in its own glossary), then one more
// pass over the inherited statements printed, so none of them arrives
// with a term undefined. A qualified use selects its domain's term; an
// unqualified one every term written that way.
func (x *ctx) terms() {
	terms := x.pathTerms()
	if len(terms) == 0 {
		return
	}
	m := bp.NewMatcher(terms)
	used := make([]bool, len(terms))
	pick := func(surface, qualifier string, term int) {
		k := bp.PhraseKey(surface)
		hit := false
		for j, t := range terms {
			if !slices.ContainsFunc(append(append([]string{t.Term}, t.Forms...), t.Aliases...), func(p string) bool { return bp.PhraseKey(p) == k }) {
				continue
			}
			if qualifier == "" || t.Domain == qualifier {
				used[j], hit = true, true
			}
		}
		if !hit {
			used[term] = true
		}
	}
	for _, u := range x.doc.TermUses(m) {
		pick(u.Surface, u.Qualifier, u.Term)
	}
	match := func(s string) {
		for _, mt := range m.Match(s) {
			q := ""
			if mt.Start > 0 && s[mt.Start-1] == '#' {
				q = qualifierOf(s[:mt.Start])
			}
			pick(s[mt.Start:mt.End], q, mt.Term)
		}
	}
	if x.doc.IsDomain() {
		for _, t := range x.doc.Glossary() {
			match(t.Definition)
		}
	}
	for _, lv := range x.c.Goals {
		for _, g := range lv.Goals {
			match(g.Text)
			for _, s := range g.Steps {
				match(s.Text)
			}
		}
	}
	for _, l := range x.c.Invariants.Plan {
		match(l.Statement)
	}
	for _, dp := range x.c.Invariants.Domains {
		for _, l := range dp.Invariants {
			match(l.Statement)
		}
	}
	for i, t := range terms {
		if x.doc.IsDomain() {
			// Its own narrowings are in its own file.
			t.Refined = slices.DeleteFunc(slices.Clone(t.Refined), func(r bp.Refinement) bool { return r.Domain == x.doc.DomainName() })
		}
		if used[i] || x.full {
			x.c.Terms = append(x.c.Terms, termOf(t))
		} else if !slices.Contains(x.c.TermsLeftOut, t.Term) {
			x.c.TermsLeftOut = append(x.c.TermsLeftOut, t.Term)
		}
	}
}

// qualifierOf is the domain written right before a # at the end of s.
func qualifierOf(s string) string {
	s = strings.TrimSuffix(s, "#")
	i := strings.LastIndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.')
	})
	return s[i+1:]
}

func termOf(t bp.Term) ContextTerm {
	return ContextTerm{Ref: t.Domain + "#" + t.Term, Term: t.Term, Definition: t.Definition, Aliases: t.Aliases, Refined: t.Refined}
}

// TermContext is one term of doc's domain path, named by the term, a
// form or an alias, qualified or not (--term): its definition, forms,
// aliases and see, and the invariants that narrow it.
func TermContext(doc *bp.Document, o Options, name string) (ContextTerm, error) {
	x := &ctx{doc: doc, o: o, lens: LensOf(doc, o)}
	terms := x.pathTerms()
	if len(terms) == 0 {
		return ContextTerm{}, errors.New("the document has no domain path here, so it has no terms to ask for")
	}
	q, word, ok := strings.Cut(name, "#")
	if !ok {
		q, word = "", name
	}
	k := bp.PhraseKey(word)
	var hits []bp.Term
	for _, t := range terms {
		if q != "" && t.Domain != q {
			continue
		}
		if slices.ContainsFunc(append(append([]string{t.Term}, t.Forms...), t.Aliases...), func(p string) bool { return bp.PhraseKey(p) == k }) {
			hits = append(hits, t)
		}
	}
	switch len(hits) {
	case 0:
		return ContextTerm{}, fmt.Errorf("no term %q on the domain path", name)
	case 1:
	default:
		var refs []string
		for _, h := range hits {
			refs = append(refs, h.Domain+"#"+h.Term)
		}
		return ContextTerm{}, fmt.Errorf("%q is a term of more than one domain; ask for %s", name, strings.Join(refs, " or "))
	}
	t := termOf(hits[0])
	t.Forms, t.See = hits[0].Forms, hits[0].See
	return t, nil
}

// TermMarkdown is one term as `kalka context --term` prints it.
func TermMarkdown(t ContextTerm) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Term %s\n\n", t.Ref)
	for _, l := range AuthorityNote[:1] {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s: %s\n\n", t.Term, t.Definition)
	if len(t.Forms) > 0 {
		fmt.Fprintf(&b, "- forms: %s\n", strings.Join(t.Forms, ", "))
	}
	if len(t.Aliases) > 0 {
		fmt.Fprintf(&b, "- not: %s\n", strings.Join(t.Aliases, ", "))
	}
	if len(t.See) > 0 {
		fmt.Fprintf(&b, "- see: %s\n", strings.Join(t.See, ", "))
	}
	if len(t.Refined) == 0 {
		b.WriteString("- no invariant refines it\n")
	}
	for _, r := range t.Refined {
		fmt.Fprintf(&b, "- %s#%s refines %s: %s\n", r.Domain, r.ID, t.Term, r.Statement)
	}
	return b.String()
}

// Markdown is the context as text an agent reads, in a fixed order:
// the authority note, the focus, gaps and boundaries, the goal chain,
// invariants, terms.
func (c AgentContext) Markdown() string {
	var b strings.Builder
	b.WriteString("# Agent context\n\n")
	for _, l := range c.Authority {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n")

	f := c.Focus
	fmt.Fprintf(&b, "## Focus: %s\n\n", f.Name())
	fmt.Fprintf(&b, "%s. Read the document itself for its own goals, trace, invariants and prose.\n\n", capital(f.Currency()))
	if len(f.Open) == 0 {
		b.WriteString("Open items: none.\n\n")
	} else {
		b.WriteString("Open items:\n\n")
		for _, o := range f.Open {
			fmt.Fprintf(&b, "- %s\n", o)
		}
		b.WriteString("\n")
	}
	for _, p := range c.Problems {
		fmt.Fprintf(&b, "Note: %s.\n\n", p)
	}

	b.WriteString("## Gaps and boundaries\n\n")
	if len(c.Gaps.NotHere) == 0 {
		b.WriteString("- Not here: nothing; every document on the paths is held.\n")
	}
	for _, s := range c.Gaps.NotHere {
		fmt.Fprintf(&b, "- Not here: %s.\n", s)
	}
	for _, g := range c.Gaps.Claims {
		whose := "the goals of " + g.Of.Ref() + " (the parent)"
		if !g.Parent {
			whose = "this document's goals"
		}
		fmt.Fprintf(&b, "- Claims on %s:\n", whose)
		for _, gc := range g.Goals {
			var parts []string
			if len(gc.Here) > 0 {
				who := "this document"
				if !g.Parent {
					who = "its own criteria"
				}
				parts = append(parts, fmt.Sprintf("claimed by %s (%s)", who, strings.Join(gc.Here, ", ")))
			}
			for _, a := range gc.Also {
				verb := "also claimed by"
				if !g.Parent && len(parts) == 0 {
					verb = "claimed by"
				}
				parts = append(parts, fmt.Sprintf("%s %s %s (%s)", verb, a.Doc.Ref(), strings.Join(a.Items, ", "), a.Doc.State))
			}
			if g.Parent && len(gc.Also) == 0 {
				parts = append(parts, "no other child claims it")
			}
			fmt.Fprintf(&b, "  - %s: %s.\n", gc.Goal, strings.Join(parts, "; "))
		}
		of := g.Of.Ref()
		if !g.Parent {
			of = "this document"
		}
		switch {
		case !g.Looked:
			fmt.Fprintf(&b, "  - Other children of %s: not looked for.\n", of)
		case g.Others == 0:
			fmt.Fprintf(&b, "  - No other children of %s.\n", of)
		case g.Others == 1:
			fmt.Fprintf(&b, "  - 1 other child of %s.\n", of)
		default:
			fmt.Fprintf(&b, "  - %d other children of %s.\n", g.Others, of)
		}
	}
	if u := c.Gaps.Unclaimed; u != nil {
		switch {
		case !u.LookedFor:
			fmt.Fprintf(&b, "- Unclaimed goals of %s: not looked for (%s).\n", u.Of, u.Why)
		case len(u.Goals) == 0:
			fmt.Fprintf(&b, "- Unclaimed goals of %s: none.\n", u.Of)
		default:
			fmt.Fprintf(&b, "- Unclaimed goals of %s: %s.\n", u.Of, strings.Join(u.Goals, ", "))
		}
	}
	b.WriteString("\n")

	if len(c.Goals) > 0 {
		b.WriteString("## Goal chain\n\n")
		for i, l := range c.Goals {
			rel := "parent"
			if i > 0 {
				rel = "above " + c.Goals[i-1].Doc.Ref()
			}
			fmt.Fprintf(&b, "### %s (%s)\n\n%s.\n\n", l.Doc.Name(), rel, capital(l.Doc.Currency()))
			for _, g := range l.Goals {
				fmt.Fprintf(&b, "- %s %s: %s", g.Kind, g.ID, g.Text)
				var rs []string
				for _, r := range g.Realizes {
					rs = append(rs, "realizes "+r)
				}
				rs = append(rs, g.RealizedBy...)
				if len(rs) > 0 {
					fmt.Fprintf(&b, " (%s)", strings.Join(rs, "; "))
				}
				b.WriteString("\n")
				for _, s := range g.Steps {
					fmt.Fprintf(&b, "  - %s %s\n", s.Keyword, s.Text)
				}
			}
			if len(l.LeftOut) > 0 {
				fmt.Fprintf(&b, "- Left out: %s.\n", strings.Join(l.LeftOut, ", "))
			}
			b.WriteString("\n")
		}
	}

	inv := c.Invariants
	if len(inv.Plan) > 0 || len(inv.Domains) > 0 {
		b.WriteString("## Invariants\n\n")
		if len(inv.Plan) > 0 {
			b.WriteString("### Plan path\n\n")
			for _, l := range inv.Plan {
				writeInvariant(&b, l, true)
			}
			b.WriteString("\n")
		}
		for _, d := range inv.Domains {
			fmt.Fprintf(&b, "### Domain path %s\n\n", d.Ref)
			if len(d.Nodes) > 0 {
				fmt.Fprintf(&b, "Pinned: %s.\n", strings.Join(d.Nodes, ", "))
			}
			if d.Newer != "" {
				fmt.Fprintf(&b, "%s.\n", capital(d.Newer))
			}
			b.WriteString("\n")
			for _, l := range d.Invariants {
				writeInvariant(&b, l, false)
			}
			if len(d.Invariants) > 0 {
				b.WriteString("\n")
			}
		}
	}

	if len(c.Terms) > 0 || len(c.TermsLeftOut) > 0 {
		b.WriteString("## Terms used\n\n")
		for _, t := range c.Terms {
			fmt.Fprintf(&b, "- %s: %s", t.Ref, t.Definition)
			if len(t.Aliases) > 0 {
				fmt.Fprintf(&b, " (not: %s)", strings.Join(t.Aliases, ", "))
			}
			b.WriteString("\n")
			for _, r := range t.Refined {
				fmt.Fprintf(&b, "  - %s#%s refines %s: %s\n", r.Domain, r.ID, t.Term, r.Statement)
			}
		}
		if len(c.Terms) > 0 {
			b.WriteString("\n")
		}
		left := "none"
		if len(c.TermsLeftOut) > 0 {
			left = strings.Join(c.TermsLeftOut, ", ")
		}
		fmt.Fprintf(&b, "Terms left out: %s. Ask for one with `kalka context %s --term <name>`.\n", left, c.Document)
	}
	return b.String()
}

func writeInvariant(b *strings.Builder, l InvariantLine, plan bool) {
	fmt.Fprintf(b, "- %s: %s", l.Ref, l.Statement)
	var notes []string
	if plan {
		notes = append(notes, "of "+l.Of)
	}
	for _, r := range l.Refines {
		notes = append(notes, "refines "+r)
	}
	if l.Affects {
		notes = append(notes, "this document affects it")
	}
	if len(l.GuardedBy) > 0 {
		notes = append(notes, strings.Join(l.GuardedBy, ", ")+" guards it")
	}
	if l.Status != "" {
		notes = append(notes, "status "+l.Status)
	}
	if len(notes) > 0 {
		fmt.Fprintf(b, " (%s)", strings.Join(notes, "; "))
	}
	b.WriteString("\n")
	if l.Why != "" {
		fmt.Fprintf(b, "  - why %s\n", l.Why)
	}
	for _, v := range l.VerifiedBy {
		fmt.Fprintf(b, "  - verified-by %s\n", v)
	}
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
