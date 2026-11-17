package check

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// States of a document's glossary (spec 6.16, 13).
const (
	GlossaryHeld    = "held"     // linked against Terms
	GlossaryNotKept = "not-kept" // the referenced version is not here
	GlossaryNone    = "none"     // the document names no domain and has no glossary
)

// Glossary is the glossary a document is linked against.
type Glossary struct {
	// Ref is the domain version, such as parcels/delivery@v3; for a domain
	// document its own name and version.
	Ref   string    `json:"ref,omitempty"`
	State string    `json:"state"`
	Terms []bp.Term `json:"terms"`
}

// GlossaryOf is the glossary of the domain version doc references (dom,
// from Document's Result), or for a domain document its own.
func GlossaryOf(doc, dom *bp.Document) Glossary {
	if doc.IsDomain() {
		g := Glossary{State: GlossaryNone, Terms: doc.Glossary()}
		if len(g.Terms) > 0 {
			g.State = GlossaryHeld
			g.Ref = fmt.Sprintf("%s@v%d", doc.DomainName(), doc.DomainVersion())
		}
		return g.norm()
	}
	ref, ok := bp.ParseDomainRef(fmValue(doc, bp.KeyDomain))
	if !ok {
		return Glossary{State: GlossaryNone}.norm()
	}
	if dom == nil {
		return Glossary{Ref: ref.String(), State: GlossaryNotKept}.norm()
	}
	return Glossary{Ref: ref.String(), State: GlossaryHeld, Terms: dom.Glossary()}.norm()
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
		}
		p := bp.NewProblem("G020", u.Line, fmt.Sprintf("%s%q is an alias in the %s glossary; write %q", where, u.Surface, g.Ref, term))
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
// version no longer has. A version not held is R001, once, elsewhere.
func invariantProblems(doc, dom *bp.Document, ds Domains) []bp.Problem {
	refs := doc.DomainRefs()
	if len(refs) == 0 {
		return nil
	}
	ref, hasRef := bp.ParseDomainRef(fmValue(doc, bp.KeyDomain))
	if doc.IsDomain() {
		hasRef = false
	}
	var newer *bp.Document
	if hasRef && dom != nil && ds != nil {
		if l := ds.Latest(ref.Team, ref.Domain); l > ref.Version {
			newer = ds.Version(ref.Team, ref.Domain, l)
		}
	}
	var out []bp.Problem
	add := func(rule string, a bp.Affect, format string, args ...any) {
		p := bp.NewProblem(rule, a.Line, a.Text+": "+fmt.Sprintf(format, args...))
		p.Node = a.Text
		out = append(out, p)
	}
	for _, a := range refs {
		switch {
		case !hasRef:
			add("I020", a, "the document references no domain version; add kalka_domain: <team>/%s@v<N>", a.Ref.Domain)
		case a.Ref.Domain != ref.Domain:
			add("I020", a, "the document references domain %s, not %s; an invariant of another domain cannot be named", ref.Domain, a.Ref.Domain)
		case dom == nil:
			// R001 says the version is not here.
		case dom.Invariant(a.Ref.ID) == nil:
			add("I020", a, "%s has no invariant %s", ref, a.Ref.ID)
		case newer != nil && newer.Invariant(a.Ref.ID) == nil:
			add("I040", a, "v%d of %s/%s no longer has %s; the plan relies on something the domain no longer promises", newer.DomainVersion(), ref.Team, ref.Domain, a.Ref.ID)
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
