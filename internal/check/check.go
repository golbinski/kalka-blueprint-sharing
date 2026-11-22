// Package check is the one checker of documents (spec 6.17): `kalka check`
// and the app's document view call it, so both report the same
// diagnostics from the same code.
package check

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/plugin"
	"github.com/golbinski/kalka/internal/rules"
)

// Domains finds domain versions (spec 6.12): Version is the document of
// one version, nil when it is not available; Latest the newest version
// available, 0 when no document of the domain is.
type Domains interface {
	Version(team, domain string, v int64) *bp.Document
	Latest(team, domain string) int64
}

// Plugins resolves references; *plugin.Host implements it.
type Plugins interface {
	Resolve(ctx context.Context, scheme string, frontmatter map[string]any, refs []plugin.Ref) []plugin.Result
}

// Options say what the checker may read. The zero value runs every rule
// with no domains and no plugins.
type Options struct {
	// Domains is where domain versions are read; nil means none is held.
	Domains Domains
	// Docs is where the plan graph is looked up; nil means it is not.
	Docs Docs
	// Plugins, when set, resolves reference targets (--with-plugins).
	Plugins Plugins
	Ctx     context.Context
}

// Result is what the checker found in one document.
type Result struct {
	Problems []bp.Problem
	// Context is the domain paths the document is read against.
	Context DomainContext
	// Glossary is what the document's terms are linked against.
	Glossary Glossary
}

// Document runs every rule on doc.
func Document(doc *bp.Document, o Options) Result {
	var r Result
	ps := append([]bp.Problem(nil), doc.Problems()...)
	ps = append(ps, bp.Hints(doc)...)
	r.Context = domainContext(doc, o.Domains)
	r.Glossary = GlossaryOf(doc, r.Context)
	ps = append(ps, domainRefProblems(doc, r.Context, r.Glossary, o.Domains)...)
	ps = append(ps, treeProblems(doc, r.Context, o.Domains)...)
	ps = append(ps, termProblems(doc, r.Glossary)...)
	ps = append(ps, qualifyProblems(doc, r.Context, r.Glossary)...)
	ps = append(ps, invariantProblems(doc, r.Context, o.Domains)...)
	ps = append(ps, planProblems(doc, o.Docs)...)
	if o.Plugins != nil {
		ps = append(ps, refProblems(doc, o)...)
	}
	for _, p := range ps {
		if rules.Known(p.Rule) {
			r.Problems = append(r.Problems, p)
		}
	}
	Sort(r.Problems)
	if r.Problems == nil {
		r.Problems = []bp.Problem{}
	}
	return r
}

// Sort orders diagnostics by line, column and rule, keeping the order of
// equal ones.
func Sort(ps []bp.Problem) {
	sort.SliceStable(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return a.Rule < b.Rule
	})
}

// domainRefProblems checks `kalka_domain:` of a blueprint (R001 to R003).
func domainRefProblems(doc *bp.Document, c DomainContext, g Glossary, ds Domains) []bp.Problem {
	if doc.IsDomain() {
		return nil
	}
	raw, has := doc.Frontmatter().Get(bp.KeyDomain)
	if !has || raw == "" {
		return nil
	}
	line := doc.Frontmatter().KeyLine(bp.KeyDomain)
	var out []bp.Problem
	for _, item := range bp.DomainList(raw) {
		if _, ok := bp.ParseDomainRef(item); !ok {
			out = append(out, bp.NewProblem("R002", line, fmt.Sprintf("%q is not <team>/<domain>@v<N>; write for example team-lending/library.reservations@v1", item)))
		}
	}
	for i, p := range c.Paths {
		ref := p.Ref
		var latest int64
		if ds != nil {
			latest = ds.Latest(ref.Team, ref.Domain)
		}
		node := c.node(i)
		switch {
		case node != nil:
		case latest == 0:
			out = append(out, bp.NewProblem("R001", line, fmt.Sprintf("no document of domain %s/%s is here, so the glossary and invariant rules were not run; point --domain at it", ref.Team, ref.Domain)))
		default:
			out = append(out, bp.NewProblem("R001", line, fmt.Sprintf("%s is not kept here (v%d is), so the glossary and invariant rules were not run; another version is never used instead", ref, latest)))
		}
		if latest > ref.Version {
			var newer *bp.Document
			if ds != nil {
				newer = ds.Version(ref.Team, ref.Domain, latest)
			}
			if what := touched(doc, node, newer, g); what != "" {
				out = append(out, bp.NewProblem("R003", line, fmt.Sprintf("v%d of %s/%s changes %s, which the plan uses; the plan is written against v%d", latest, ref.Team, ref.Domain, what, ref.Version)))
			}
		}
	}
	return out
}

// touched says which terms and invariants of node the document uses that
// newer changes, or "" for none; when either version is not available it
// cannot tell and says "the domain".
func touched(doc, node, newer *bp.Document, g Glossary) string {
	if node == nil || newer == nil {
		return "the domain"
	}
	var out []string
	name := node.DomainName()
	nt := map[string]bp.Term{}
	for _, t := range newer.Glossary() {
		nt[bp.PhraseKey(t.Term)] = t
	}
	if g.State == GlossaryHeld {
		m := bp.NewMatcher(g.Terms)
		for _, u := range doc.TermUses(m) {
			t := g.Terms[u.Term]
			if t.Domain != name {
				continue
			}
			n, ok := nt[bp.PhraseKey(t.Term)]
			if !ok || !sameTerm(t, n) {
				if w := "term " + t.Term; !slices.Contains(out, w) {
					out = append(out, w)
				}
			}
		}
	}
	for _, a := range doc.DomainRefs() {
		if a.Ref.Domain != name {
			continue
		}
		o, n := node.Invariant(a.Ref.ID), newer.Invariant(a.Ref.ID)
		if o != nil && (n == nil || bp.PhraseKey(o.Statement) != bp.PhraseKey(n.Statement)) {
			if w := "invariant " + a.Ref.ID; !slices.Contains(out, w) {
				out = append(out, w)
			}
		}
	}
	return strings.Join(out, ", ")
}

func sameTerm(a, b bp.Term) bool {
	return bp.PhraseKey(a.Definition) == bp.PhraseKey(b.Definition) && slices.Equal(a.Forms, b.Forms) && slices.Equal(a.Aliases, b.Aliases)
}

// refProblems resolves the reference targets of the typed blocks with the
// plugins (R010, R011).
func refProblems(doc *bp.Document, o Options) []bp.Problem {
	var uses []refUse
	for _, t := range doc.Traces() {
		for _, n := range t.Nodes {
			if n.Ref != "" && n.Live() {
				uses = append(uses, refUse{n.Ref, "node " + n.ID, n.Line, n.ID})
			}
		}
	}
	uses = append(uses, verifiedRefs(doc)...)
	if len(uses) == 0 {
		return nil
	}
	ctx := o.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	byScheme := map[string][]plugin.Ref{}
	var schemes []string
	seen := map[string]bool{}
	for _, u := range uses {
		scheme, payload, ok := strings.Cut(u.ref, ":")
		if !ok || seen[u.ref] {
			continue
		}
		seen[u.ref] = true
		if byScheme[scheme] == nil {
			schemes = append(schemes, scheme)
		}
		byScheme[scheme] = append(byScheme[scheme], plugin.Ref{Ref: u.ref, Payload: payload})
	}
	sort.Strings(schemes)
	res := map[string]plugin.Result{}
	fm := doc.Frontmatter().Map()
	for _, s := range schemes {
		for _, r := range o.Plugins.Resolve(ctx, s, fm, byScheme[s]) {
			res[r.Ref] = r
		}
	}
	var out []bp.Problem
	for _, u := range uses {
		r, ok := res[u.ref]
		if !ok {
			continue
		}
		var p bp.Problem
		switch {
		case !r.OK:
			msg := r.Error
			if msg == "" {
				msg = "the plugin could not resolve it"
			}
			p = bp.NewProblem("R010", u.line, fmt.Sprintf("%s: %s: %s", u.what, u.ref, msg))
		case len(r.Warnings) > 0:
			p = bp.NewProblem("R011", u.line, fmt.Sprintf("%s: %s: %s", u.what, u.ref, r.Warnings[0].Text))
		default:
			continue
		}
		p.Node = u.node
		out = append(out, p)
	}
	return out
}

// refUse is one reference target in a typed block.
type refUse struct {
	ref, what string
	line      int
	node      string
}
