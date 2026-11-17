// Package check is the one checker of documents (spec 6.17): `kalka check`
// and the app's document view call it, so both report the same
// diagnostics from the same code.
package check

import (
	"context"
	"fmt"
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

// Options say what the checker may read. The zero value runs the newest
// rule set with no domains and no plugins.
type Options struct {
	// Set is the rule set (rules.ParseSet); 0 means the newest.
	Set int
	// Domains is where domain versions are read; nil means none is held.
	Domains Domains
	// Plugins, when set, resolves reference targets (--with-plugins).
	Plugins Plugins
	Ctx     context.Context
}

// Result is what the checker found in one document.
type Result struct {
	Problems []bp.Problem
	// Domain is the referenced domain version, when available.
	Domain *bp.Document
	// Glossary is what the document's terms are linked against.
	Glossary Glossary
}

// Document runs every rule of the set on doc.
func Document(doc *bp.Document, o Options) Result {
	set := o.Set
	if set == 0 {
		set = rules.Newest
	}
	var r Result
	ps := append([]bp.Problem(nil), doc.Problems()...)
	ps = append(ps, bp.Hints(doc)...)
	var dps []bp.Problem
	r.Domain, dps = domainRef(doc, o.Domains)
	ps = append(ps, dps...)
	r.Glossary = GlossaryOf(doc, r.Domain)
	ps = append(ps, termProblems(doc, r.Glossary)...)
	ps = append(ps, invariantProblems(doc, r.Domain, o.Domains)...)
	if o.Plugins != nil {
		ps = append(ps, refProblems(doc, o)...)
	}
	for _, p := range ps {
		if rules.InSet(p.Rule, set) {
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

// domainRef checks `kalka_domain:` of a blueprint (R001 to R003) and
// returns the version it names when it is available.
func domainRef(doc *bp.Document, ds Domains) (*bp.Document, []bp.Problem) {
	if doc.IsDomain() {
		return nil, nil
	}
	raw, has := doc.Frontmatter().Get(bp.KeyDomain)
	if !has || raw == "" {
		return nil, nil
	}
	line := doc.Frontmatter().KeyLine(bp.KeyDomain)
	ref, ok := bp.ParseDomainRef(raw)
	if !ok {
		return nil, []bp.Problem{bp.NewProblem("R002", line, fmt.Sprintf("%q is not <team>/<domain>@v<N>; write for example parcels/delivery@v7", raw))}
	}
	var latest int64
	var dom *bp.Document
	if ds != nil {
		latest = ds.Latest(ref.Team, ref.Domain)
		dom = ds.Version(ref.Team, ref.Domain, ref.Version)
	}
	var out []bp.Problem
	switch {
	case dom != nil:
	case latest == 0:
		out = append(out, bp.NewProblem("R001", line, fmt.Sprintf("no document of domain %s/%s is here, so the glossary and invariant rules were not run; point --domain at it", ref.Team, ref.Domain)))
	default:
		out = append(out, bp.NewProblem("R001", line, fmt.Sprintf("%s is not kept here (v%d is), so the glossary and invariant rules were not run; another version is never used instead", ref, latest)))
	}
	if latest > ref.Version {
		out = append(out, bp.NewProblem("R003", line, fmt.Sprintf("v%d of %s/%s is available; the plan is written against v%d", latest, ref.Team, ref.Domain, ref.Version)))
	}
	return dom, out
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
