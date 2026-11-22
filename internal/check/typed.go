package check

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// States of a document's glossary (spec 6.16, 13).
const (
	GlossaryHeld    = "held"     // linked against Terms
	GlossaryNotKept = "not-kept" // the referenced version is not here
	GlossaryNone    = "none"     // no glossary: none of its own, no domain named
)

// Glossary is the glossary a document is linked against.
type Glossary struct {
	// Ref is the domain version, such as parcels/delivery@v3; for a domain
	// document its own name and version; "" for a blueprint's own.
	Ref   string    `json:"ref,omitempty"`
	State string    `json:"state"`
	Terms []bp.Term `json:"terms"`
	// Own: the terms are the document's own blocks, at its own lines.
	Own bool `json:"own,omitempty"`
}

// GlossaryOf is the glossary doc is linked against (spec 6.16): the
// terms on the paths of the domain nodes it names, at the pinned versions;
// for a domain document, its own path; for a blueprint that names no
// domain, its own blocks.
func GlossaryOf(doc *bp.Document, c DomainContext) Glossary {
	if !doc.IsDomain() && len(bp.DomainList(fmValue(doc, bp.KeyDomain))) == 0 {
		g := Glossary{State: GlossaryNone, Terms: doc.Glossary()}
		if len(g.Terms) > 0 {
			g.State, g.Own = GlossaryHeld, true
		}
		return g.norm()
	}
	if doc.IsDomain() {
		g := Glossary{State: GlossaryNone, Own: true, Ref: fmt.Sprintf("%s@v%d", doc.DomainName(), doc.DomainVersion())}
		var terms []bp.Term
		if len(c.Paths) == 1 {
			terms = termsOf(c.Paths[0])
		} else {
			terms = refined(doc.Glossary(), []*bp.Document{doc})
		}
		g.Terms = terms
		if len(terms) > 0 {
			g.State = GlossaryHeld
		}
		return g.norm()
	}
	var refs []string
	for _, p := range c.Paths {
		refs = append(refs, p.Ref.String())
	}
	g := Glossary{Ref: strings.Join(refs, ", ")}
	if len(c.Paths) == 0 {
		return Glossary{State: GlossaryNone}.norm()
	}
	if !c.Complete() {
		g.State = GlossaryNotKept
		return g.norm()
	}
	g.State = GlossaryHeld
	seen := map[string]bool{}
	for _, p := range c.Paths {
		for _, t := range termsOf(p) {
			k := t.Domain + "#" + bp.PhraseKey(t.Term)
			if !seen[k] {
				seen[k] = true
				g.Terms = append(g.Terms, t)
			}
		}
	}
	return g.norm()
}

func (g Glossary) norm() Glossary {
	if g.Terms == nil {
		g.Terms = []bp.Term{}
	}
	return g
}

// termProblems: every use of an alias is G020, with the term as a fix
// where the position is known.
func termProblems(doc *bp.Document, g Glossary) []bp.Problem {
	if g.State != GlossaryHeld || len(g.Terms) == 0 {
		return nil
	}
	m := bp.NewMatcher(g.Terms)
	var out []bp.Problem
	for _, u := range doc.TermUses(m) {
		if !u.Alias {
			continue
		}
		term := g.Terms[u.Term].Term
		where := ""
		switch u.Where {
		case "trace":
			where = "node " + u.Node + ": "
		case "invariant":
			where = "invariant " + u.Node + ": "
		case "goal":
			where = u.Node + ": "
		}
		in := "the " + g.Ref + " glossary"
		if g.Ref == "" {
			in = "this blueprint's glossary"
		}
		p := bp.NewProblem("G020", u.Line, fmt.Sprintf("%s%q is an alias in %s; write %q", where, u.Surface, in, term))
		p.Node = u.Node
		if u.Column > 0 {
			p.Column, p.EndLine, p.EndColumn = u.Column, u.EndLine, u.EndColumn
			p.Fix = &bp.Fix{Text: matchCase(term, u.Surface), Line: u.Line, Column: u.Column, EndLine: u.EndLine, EndColumn: u.EndColumn}
		}
		out = append(out, p)
	}
	return out
}

// matchCase capitalizes the replacement's first letter when the text it
// replaces starts with a capital.
func matchCase(term, surface string) string {
	r, _ := utf8.DecodeRuneInString(surface)
	if !unicode.IsUpper(r) {
		return term
	}
	t, size := utf8.DecodeRuneInString(term)
	return string(unicode.ToUpper(t)) + term[size:]
}

// invariantProblems resolve the references to domain invariants (spec
// 6.15): I020 for one that does not resolve, I040 for one a newer held
// version of the pinned node no longer has. A version not held is R001 or
// D002, once, elsewhere.
func invariantProblems(doc *bp.Document, c DomainContext, ds Domains) []bp.Problem {
	refs := doc.DomainRefs()
	if len(refs) == 0 {
		return nil
	}
	var out []bp.Problem
	add := func(rule string, a bp.Affect, format string, args ...any) {
		p := bp.NewProblem(rule, a.Line, a.Text+": "+fmt.Sprintf(format, args...))
		p.Node = a.Text
		out = append(out, p)
	}
	var onPath []string
	for _, p := range c.Paths {
		for _, n := range p.Names() {
			if !slices.Contains(onPath, n) {
				onPath = append(onPath, n)
			}
		}
	}
	// newer versions of the pinned nodes, by name
	newer := map[string]*bp.Document{}
	team := map[string]string{}
	if !doc.IsDomain() && ds != nil {
		for _, p := range c.Paths {
			if l := ds.Latest(p.Ref.Team, p.Ref.Domain); l > p.Ref.Version {
				if v := ds.Version(p.Ref.Team, p.Ref.Domain, l); v != nil {
					newer[p.Ref.Domain] = v
					team[p.Ref.Domain] = p.Ref.Team
				}
			}
		}
	}
	for _, a := range refs {
		dom := c.ByName[a.Ref.Domain]
		switch {
		case len(c.Paths) == 0:
			add("I020", a, "the document references no domain version; add kalka_domain: <team>/%s@v<N>", a.Ref.Domain)
		case dom == nil && c.Complete():
			add("I020", a, "%s is not on the path from the named domain to the root (%s); name an invariant of one of those domains", a.Ref.Domain, strings.Join(onPath, ", "))
		case dom == nil:
			// R001 or D002 says a version is not here.
		case dom.Invariant(a.Ref.ID) == nil:
			add("I020", a, "%s@v%d has no invariant %s", a.Ref.Domain, dom.DomainVersion(), a.Ref.ID)
		case newer[a.Ref.Domain] != nil && newer[a.Ref.Domain].Invariant(a.Ref.ID) == nil:
			n := newer[a.Ref.Domain]
			add("I040", a, "v%d of %s/%s no longer has %s; the plan relies on something the domain no longer promises", n.DomainVersion(), team[a.Ref.Domain], a.Ref.Domain, a.Ref.ID)
		}
	}
	return out
}

// verifiedRefs are the plugin references in verified_by (R010, R011).
func verifiedRefs(doc *bp.Document) []refUse {
	var out []refUse
	for _, v := range doc.Invariants() {
		if !v.Live() {
			continue
		}
		for _, s := range v.VerifiedBy {
			if _, _, ok := bp.PluginRef(s); ok {
				out = append(out, refUse{s, "invariant " + v.ID, v.Line, v.ID})
			}
		}
	}
	return out
}

func fmValue(doc *bp.Document, key string) string {
	v, _ := doc.Frontmatter().Get(key)
	return v
}
