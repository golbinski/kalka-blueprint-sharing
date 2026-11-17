package check

import (
	"fmt"
	"sort"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// Row is one line of the invariants matrix (spec 11.1, 12.4): one of the
// document's own invariants, or a domain invariant it affects or guards.
type Row struct {
	// Ref is the id, or <domain>#<id> for a domain invariant.
	Ref        string   `json:"ref"`
	ID         string   `json:"id"`
	Domain     string   `json:"domain,omitempty"`
	Statement  string   `json:"statement,omitempty"`
	Context    string   `json:"context,omitempty"`
	Why        string   `json:"why,omitempty"`
	VerifiedBy []string `json:"verified_by"`
	Status     string   `json:"status,omitempty"`
	// Section is the heading the invariant is under: its anchor is
	// §<section>#<id>. Empty for a domain invariant.
	Section string `json:"section,omitempty"`
	Line    int    `json:"line"`
	// Resolved is false for a domain invariant whose version is not here
	// or that the version does not have.
	Resolved  bool              `json:"resolved"`
	GuardedBy []bp.TraceRefNode `json:"guarded_by"`
	// Gaps name what the checks found missing: unguarded, unverified.
	Gaps []string `json:"gaps"`
	// ReferencedBy are, for a domain document, the held blueprints that
	// name this invariant.
	ReferencedBy []string `json:"referenced_by,omitempty"`
}

// Matrix is the invariants matrix of doc; dom is the domain version it
// references (Result.Domain), or nil.
func Matrix(doc, dom *bp.Document) []Row {
	hasTrace := false
	for _, t := range doc.Traces() {
		hasTrace = hasTrace || t.Valid()
	}
	rows := []Row{}
	for _, v := range doc.Invariants() {
		r := Row{Ref: v.ID, ID: v.ID, Statement: v.Statement, Context: v.Context, Why: v.Why, VerifiedBy: v.VerifiedBy, Status: v.Status, Section: v.Section, Line: v.Line, Resolved: true}
		r.GuardedBy = doc.GuardedBy(v.ID)
		if v.Live() {
			if hasTrace && len(r.GuardedBy) == 0 {
				r.Gaps = append(r.Gaps, "unguarded")
			}
			if len(v.VerifiedBy) == 0 {
				r.Gaps = append(r.Gaps, "unverified")
			}
		}
		rows = append(rows, r.norm())
	}
	affects := map[string]bool{}
	for _, a := range doc.Affects() {
		affects[a.Text] = true
	}
	for _, a := range doc.DomainRefs() {
		r := Row{Ref: a.Text, ID: a.Ref.ID, Domain: a.Ref.Domain, Line: a.Line}
		if dom != nil && dom.DomainName() == a.Ref.Domain {
			if v := dom.Invariant(a.Ref.ID); v != nil {
				r.Resolved = true
				r.Statement, r.Context, r.Why, r.VerifiedBy = v.Statement, v.Context, v.Why, v.VerifiedBy
			}
		}
		r.GuardedBy = doc.GuardedBy(a.Text)
		if hasTrace && affects[a.Text] && len(r.GuardedBy) == 0 {
			r.Gaps = append(r.Gaps, "unguarded")
		}
		rows = append(rows, r.norm())
	}
	return rows
}

func (r Row) norm() Row {
	if r.VerifiedBy == nil {
		r.VerifiedBy = []string{}
	}
	if r.GuardedBy == nil {
		r.GuardedBy = []bp.TraceRefNode{}
	}
	if r.Gaps == nil {
		r.Gaps = []string{}
	}
	return r
}

// ReferencedBy fills, for a domain document's matrix, the held blueprints
// naming each invariant. blueprints maps a path to its document.
func ReferencedBy(rows []Row, dom *bp.Document, blueprints map[string]*bp.Document) {
	name := dom.DomainName()
	if name == "" {
		return
	}
	for _, path := range sortedPaths(blueprints) {
		seen := map[string]bool{}
		for _, a := range blueprints[path].DomainRefs() {
			if a.Ref.Domain == name {
				seen[a.Ref.ID] = true
			}
		}
		for i := range rows {
			if rows[i].Domain == "" && seen[rows[i].ID] {
				rows[i].ReferencedBy = append(rows[i].ReferencedBy, path)
			}
		}
	}
}

// Affected is one held blueprint a domain release would concern.
type Affected struct {
	Path     string       `json:"path"`
	Title    string       `json:"title"`
	Problems []bp.Problem `json:"problems"`
}

// Impact is what releasing dom's current text would mean for held
// blueprints of the same domain (spec 6.12, 11.1): I040 for an invariant
// they reference that the text no longer has, G030 for an alias the text
// introduces that they use. ds gives each blueprint's referenced version,
// to tell new aliases from old ones.
func Impact(dom *bp.Document, blueprints map[string]*bp.Document, ds Domains) []Affected {
	name := dom.DomainName()
	out := []Affected{}
	if name == "" {
		return out
	}
	m := bp.NewMatcher(dom.Glossary())
	next := dom.DomainVersion() + 1
	for _, path := range sortedPaths(blueprints) {
		doc := blueprints[path]
		ref, ok := bp.ParseDomainRef(fmValue(doc, bp.KeyDomain))
		if !ok || ref.Domain != name || doc.IsDomain() {
			continue
		}
		var ps []bp.Problem
		for _, a := range doc.DomainRefs() {
			if a.Ref.Domain == name && dom.Invariant(a.Ref.ID) == nil {
				ps = append(ps, bp.NewProblem("I040", 0, fmt.Sprintf("references %s, which v%d no longer has", a.Text, next)))
			}
		}
		var old map[string]bool // phrases that are aliases in the version it references
		if ds != nil {
			if v := ds.Version(ref.Team, ref.Domain, ref.Version); v != nil {
				old = map[string]bool{}
				for _, t := range v.Glossary() {
					for _, a := range t.Aliases {
						old[bp.PhraseKey(a)] = true
					}
				}
			}
		}
		seen := map[string]bool{}
		for _, u := range doc.TermUses(m) {
			k := bp.PhraseKey(u.Surface)
			if !u.Alias || old[k] || seen[k] {
				continue
			}
			seen[k] = true
			ps = append(ps, bp.NewProblem("G030", 0, fmt.Sprintf("uses %q, which v%d makes an alias of %q", u.Surface, next, m.Terms()[u.Term].Term)))
		}
		if len(ps) > 0 {
			out = append(out, Affected{Path: path, Title: doc.Title(), Problems: ps})
		}
	}
	return out
}

func sortedPaths(m map[string]*bp.Document) []string {
	ps := make([]string, 0, len(m))
	for p := range m {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	return ps
}
