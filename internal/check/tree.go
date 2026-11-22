package check

import (
	"fmt"
	"slices"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// The domain tree (spec 6.12): a domain node pins its parent's version
// with kalka_parent, so one pin names a whole path from the root. A
// document is read against the paths of the domain nodes it names.

// maxDepth bounds a path, so a cycle or a very deep tree cannot run away.
const maxDepth = 32

// DomainPath is the path from the root to one domain node, at the pinned
// versions: Nodes[0] is the root, the last the node itself. Missing names
// the first version on the way that is not available (Nodes then holds
// the part below it); Cycle is set when the pins lead back.
type DomainPath struct {
	Ref     bp.DomainRef
	Nodes   []*bp.Document
	Missing *bp.DomainRef
	Cycle   bool
}

// Complete reports whether every version on the path is available.
func (p DomainPath) Complete() bool { return p.Missing == nil && !p.Cycle && len(p.Nodes) > 0 }

// Names are the domain names on the path, root first.
func (p DomainPath) Names() []string {
	var out []string
	for _, n := range p.Nodes {
		out = append(out, n.DomainName())
	}
	return out
}

// String shows the path with its versions, root first.
func (p DomainPath) String() string {
	var parts []string
	for _, n := range p.Nodes {
		parts = append(parts, fmt.Sprintf("%s@v%d", n.DomainName(), n.DomainVersion()))
	}
	return strings.Join(parts, " › ")
}

// pathFrom follows the pins up from a domain node's document (the node at
// its version v).
func pathFrom(ref bp.DomainRef, node *bp.Document, ds Domains) DomainPath {
	p := DomainPath{Ref: ref}
	seen := map[string]bool{}
	cur, curRef := node, ref
	for cur != nil {
		if seen[curRef.Domain] || len(p.Nodes) >= maxDepth {
			p.Cycle = true
			break
		}
		seen[curRef.Domain] = true
		p.Nodes = append([]*bp.Document{cur}, p.Nodes...)
		up, ok := cur.DomainParent()
		if !ok {
			break
		}
		if up.Team != curRef.Team {
			up.Team = curRef.Team // a domain tree is one team's
		}
		var next *bp.Document
		if ds != nil {
			next = ds.Version(up.Team, up.Domain, up.Version)
		}
		if next == nil {
			m := up
			p.Missing = &m
			break
		}
		cur, curRef = next, up
	}
	return p
}

// ResolvePath is the path of the node a reference names, or a path with
// Missing set to the reference itself when the node is not available.
func ResolvePath(ref bp.DomainRef, ds Domains) DomainPath {
	var node *bp.Document
	if ds != nil {
		node = ds.Version(ref.Team, ref.Domain, ref.Version)
	}
	if node == nil {
		m := ref
		return DomainPath{Ref: ref, Missing: &m}
	}
	return pathFrom(ref, node, ds)
}

// ownPath is a domain document's own path: its ancestors at the versions
// its pin names, and itself.
func ownPath(doc *bp.Document, ds Domains) DomainPath {
	ref := bp.DomainRef{Team: doc.Team(), Domain: doc.DomainName(), Version: doc.DomainVersion()}
	return pathFrom(ref, doc, ds)
}

// DomainContext is what a document is read against: the paths of the
// domain nodes it names (for a domain document, its own path), and every
// node on them by name.
type DomainContext struct {
	Paths  []DomainPath
	ByName map[string]*bp.Document
}

func domainContext(doc *bp.Document, ds Domains) DomainContext {
	c := DomainContext{ByName: map[string]*bp.Document{}}
	if doc.IsDomain() {
		if doc.DomainName() != "" {
			c.Paths = []DomainPath{ownPath(doc, ds)}
		}
	} else {
		for _, r := range doc.DomainRefsNamed() {
			c.Paths = append(c.Paths, ResolvePath(r, ds))
		}
	}
	for _, p := range c.Paths {
		for _, n := range p.Nodes {
			if _, ok := c.ByName[n.DomainName()]; !ok {
				c.ByName[n.DomainName()] = n
			}
		}
	}
	return c
}

// Complete reports whether every path is complete.
func (c DomainContext) Complete() bool {
	for _, p := range c.Paths {
		if !p.Complete() {
			return false
		}
	}
	return true
}

// Node is the pinned node of the first path, the document R003 and I040
// compare with newer versions.
func (c DomainContext) node(i int) *bp.Document {
	if i >= len(c.Paths) || len(c.Paths[i].Nodes) == 0 || c.Paths[i].Missing != nil && len(c.Paths[i].Nodes) == 0 {
		return nil
	}
	p := c.Paths[i]
	last := p.Nodes[len(p.Nodes)-1]
	if last.DomainName() != p.Ref.Domain {
		return nil
	}
	return last
}

// termsOf are a path's terms, root first, each with the node that defines
// it; a term an ancestor already defines (D010) keeps the ancestor's.
func termsOf(p DomainPath) []bp.Term {
	var out []bp.Term
	seen := map[string]bool{}
	for _, n := range p.Nodes {
		for _, t := range n.Glossary() {
			if seen[bp.PhraseKey(t.Term)] {
				continue
			}
			seen[bp.PhraseKey(t.Term)] = true
			t.Domain = n.DomainName()
			out = append(out, t)
		}
	}
	return refined(out, p.Nodes)
}

// refined fills each term's narrowings: the invariants of the nodes that
// name it with refines.
func refined(terms []bp.Term, nodes []*bp.Document) []bp.Term {
	for i := range terms {
		t := &terms[i]
		t.Refined = nil
		keys := map[string]bool{bp.PhraseKey(t.Term): true}
		for _, f := range t.Forms {
			keys[bp.PhraseKey(f)] = true
		}
		for _, n := range nodes {
			for _, v := range n.Invariants() {
				for _, r := range v.Refines {
					if keys[bp.PhraseKey(r)] {
						t.Refined = append(t.Refined, bp.Refinement{Domain: n.DomainName(), ID: v.ID, Statement: v.Statement})
					}
				}
			}
		}
	}
	return terms
}

// treeProblems are the domain-tree rules (D…) of a document.
func treeProblems(doc *bp.Document, c DomainContext, ds Domains) []bp.Problem {
	var out []bp.Problem
	fm := doc.Frontmatter()
	for _, p := range c.Paths {
		switch {
		case p.Cycle:
			out = append(out, bp.NewProblem("D003", fm.KeyLine(bp.KeyParent), fmt.Sprintf("the domain pins lead back to %s: %s; a domain tree has one root", p.Ref.Domain, p.String())))
		case p.Missing != nil && len(p.Nodes) > 0:
			out = append(out, bp.NewProblem("D002", fm.KeyLine(bp.KeyDomain), fmt.Sprintf("%s, on the path of %s to the root, is not kept here, so the rules that need it were not run", p.Missing, p.Ref)))
		}
	}
	if doc.IsDomain() {
		if len(c.Paths) == 1 {
			out = append(out, nodeProblems(doc, c.Paths[0], ds)...)
		}
		return out
	}
	out = append(out, diamond(doc, c)...)
	return out
}

// nodeProblems are the rules of one domain node against its ancestors and
// siblings: D010 to D012, D030, D031.
func nodeProblems(doc *bp.Document, p DomainPath, ds Domains) []bp.Problem {
	var out []bp.Problem
	name := doc.DomainName()
	var anc []*bp.Document
	if len(p.Nodes) > 1 && p.Nodes[len(p.Nodes)-1] == doc {
		anc = p.Nodes[:len(p.Nodes)-1]
	}
	// Words the ancestors define, with the node that defines each.
	defined := map[string]string{}
	var ancTerms []bp.Term
	for _, a := range anc {
		for _, t := range a.Glossary() {
			for _, ph := range append(append([]string{t.Term}, t.Forms...), t.Aliases...) {
				if _, ok := defined[bp.PhraseKey(ph)]; !ok {
					defined[bp.PhraseKey(ph)] = a.DomainName() + "#" + t.Term
				}
			}
			tt := t
			tt.Domain = a.DomainName()
			ancTerms = append(ancTerms, tt)
		}
	}
	for _, t := range doc.Glossary() {
		for _, ph := range append(append([]string{t.Term}, t.Forms...), t.Aliases...) {
			if where, ok := defined[bp.PhraseKey(ph)]; ok {
				pr := bp.NewProblem("D010", t.Line, fmt.Sprintf("glossary, %s: %q is already defined above, as %s; a subdomain only adds words: drop it here, use another word, or narrow the inherited one with an invariant that refines it", t.Term, ph, where))
				pr.Node = t.Term
				out = append(out, pr)
				break
			}
		}
	}
	// Terms on the path, for see, refines and D011.
	pathKeys := map[string]bool{}
	for _, t := range append(slices.Clone(ancTerms), doc.Glossary()...) {
		pathKeys[bp.PhraseKey(t.Term)] = true
		for _, f := range t.Forms {
			pathKeys[bp.PhraseKey(f)] = true
		}
	}
	if _, sub := doc.DomainParent(); sub && p.Complete() {
		for _, t := range doc.Glossary() {
			for _, s := range t.See {
				if !pathKeys[bp.PhraseKey(s)] {
					pr := bp.NewProblem("G016", t.Line, fmt.Sprintf("glossary, %s: see names %q, which is not a term of this glossary or of a domain above it", t.Term, s))
					pr.Node = t.Term
					out = append(out, pr)
				}
			}
		}
	}
	ancM, ownM := bp.NewMatcher(ancTerms), bp.NewMatcher(doc.Glossary())
	for _, v := range doc.Invariants() {
		for _, r := range v.Refines {
			if !pathKeys[bp.PhraseKey(r)] {
				pr := bp.NewProblem("D012", v.Line, fmt.Sprintf("invariant %s: refines %q, which is not a term on the path %s; name a term this domain inherits", v.ID, r, p.String()))
				pr.Node = v.ID
				out = append(out, pr)
			}
		}
		if len(anc) == 0 || len(v.Refines) > 0 {
			continue
		}
		inherited := ancM.Match(v.Statement)
		if len(inherited) > 0 && len(ownM.Match(v.Statement)) == 0 {
			var words []string
			for _, m := range inherited {
				w := ancTerms[m.Term].Term
				if !slices.Contains(words, w) {
					words = append(words, w)
				}
			}
			pr := bp.NewProblem("D011", v.Line, fmt.Sprintf("invariant %s speaks only of inherited terms (%s), so it narrows them; say which with refines %s", v.ID, strings.Join(words, ", "), words[0]))
			pr.Node = v.ID
			out = append(out, pr)
		}
	}
	// The parent is newer than the pin (D030).
	if up, ok := doc.DomainParent(); ok && ds != nil {
		if l := ds.Latest(doc.Team(), up.Domain); l > up.Version {
			out = append(out, bp.NewProblem("D030", doc.Frontmatter().KeyLine(bp.KeyParent), fmt.Sprintf("v%d of %s is available; this node extends v%d, and moves when its owner chooses", l, up.Domain, up.Version)))
		}
		// Siblings that define a term the same way (D031).
		if sib, ok := ds.(Siblings); ok {
			for _, s := range sib.Children(doc.Team(), up.Domain) {
				if s.DomainName() == name {
					continue
				}
				for _, t := range doc.Glossary() {
					for _, o := range s.Glossary() {
						if bp.PhraseKey(t.Term) == bp.PhraseKey(o.Term) && bp.PhraseKey(t.Definition) == bp.PhraseKey(o.Definition) {
							pr := bp.NewProblem("D031", t.Line, fmt.Sprintf("glossary, %s: %s defines it the same way; lift it to %s, their common parent", t.Term, s.DomainName(), up.Domain))
							pr.Node = t.Term
							out = append(out, pr)
						}
					}
				}
			}
		}
	}
	return out
}

// Siblings is what D031 needs beyond Domains: the live documents of the
// domain nodes whose parent is the given domain.
type Siblings interface {
	Children(team, domain string) []*bp.Document
}

// diamond reports named domain nodes that pin different versions of a
// common ancestor (D021).
func diamond(doc *bp.Document, c DomainContext) []bp.Problem {
	var out []bp.Problem
	line := doc.Frontmatter().KeyLine(bp.KeyDomain)
	for i := 0; i < len(c.Paths); i++ {
		for j := i + 1; j < len(c.Paths); j++ {
			a, b := c.Paths[i], c.Paths[j]
			for _, x := range a.Nodes {
				for _, y := range b.Nodes {
					if x.DomainName() == y.DomainName() && x.DomainVersion() != y.DomainVersion() {
						out = append(out, bp.NewProblem("D021", line, fmt.Sprintf("inconsistent ancestor versions of %s: %s, and %s", x.DomainName(), a.String(), b.String())))
					}
				}
			}
		}
	}
	return out
}

// qualifyProblems: when the named domain nodes define one word in two
// ways, an unqualified use is D020.
func qualifyProblems(doc *bp.Document, c DomainContext, g Glossary) []bp.Problem {
	if doc.IsDomain() || len(c.Paths) < 2 || g.State != GlossaryHeld {
		return nil
	}
	// Words with more than one meaning among the terms.
	meanings := map[string]map[string]string{} // phrase → definition → domain
	for _, t := range g.Terms {
		for _, ph := range append([]string{t.Term}, t.Forms...) {
			k := bp.PhraseKey(ph)
			if meanings[k] == nil {
				meanings[k] = map[string]string{}
			}
			meanings[k][bp.PhraseKey(t.Definition)] = t.Domain
		}
	}
	m := bp.NewMatcher(g.Terms)
	var out []bp.Problem
	for _, u := range doc.TermUses(m) {
		ds := meanings[bp.PhraseKey(u.Surface)]
		if len(ds) < 2 || u.Qualifier != "" {
			continue
		}
		var names []string
		for _, d := range ds {
			names = append(names, d+"#"+u.Surface)
		}
		slices.Sort(names)
		p := bp.NewProblem("D020", u.Line, fmt.Sprintf("%q means different things in the named domains; write %s", u.Surface, strings.Join(names, " or ")))
		p.Node = u.Node
		if u.Column > 0 {
			p.Column, p.EndLine, p.EndColumn = u.Column, u.EndLine, u.EndColumn
		}
		out = append(out, p)
	}
	return out
}
