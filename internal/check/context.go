package check

import (
	"fmt"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// AgentContext is the flattened context of a document for an agent (spec
// 6.18, `kalka context`): the path in the plan tree from the root, with
// each level's goals and invariants, and the path in the domain tree, with
// terms and their narrowings, at the pinned versions. Siblings are never
// included.
type AgentContext struct {
	Document string        `json:"document"`
	Title    string        `json:"title"`
	Plan     []PlanLevel   `json:"plan"`
	Domains  []DomainLevel `json:"domains"`
	Problems []string      `json:"problems,omitempty"`
	Missing  []string      `json:"missing,omitempty"`
}

// PlanLevel is one plan node on the path, root first; the last is the
// document itself.
type PlanLevel struct {
	KalkaID    string         `json:"kalka_id,omitempty"`
	Title      string         `json:"title"`
	Issue      string         `json:"issue,omitempty"`
	Self       bool           `json:"self,omitempty"`
	Goals      []bp.Goal      `json:"goals"`
	Invariants []bp.Invariant `json:"invariants"`
}

// DomainLevel is one domain node at its pinned version, root first.
type DomainLevel struct {
	Domain     string         `json:"domain"`
	Version    int64          `json:"version"`
	Terms      []bp.Term      `json:"terms"`
	Invariants []bp.Invariant `json:"invariants"`
}

// Context builds the agent context of doc (file is how it is named).
func Context(file string, doc *bp.Document, o Options) AgentContext {
	c := AgentContext{Document: file, Title: doc.Title(), Plan: []PlanLevel{}, Domains: []DomainLevel{}}
	path, missing, cycle := PlanPath(doc, o.Docs)
	if missing != "" {
		c.Missing = append(c.Missing, "plan node "+missing)
	}
	if cycle {
		c.Problems = append(c.Problems, "kalka_parent leads back to the document; the plan path stops there")
	}
	level := func(d *bp.Document, self bool) PlanLevel {
		l := PlanLevel{KalkaID: d.KalkaID(), Title: d.Title(), Self: self, Goals: d.Goals(), Invariants: d.Invariants()}
		if r, ok := d.Issue(); ok {
			l.Issue = r.String()
		}
		if l.Goals == nil {
			l.Goals = []bp.Goal{}
		}
		if l.Invariants == nil {
			l.Invariants = []bp.Invariant{}
		}
		return l
	}
	for _, p := range path {
		c.Plan = append(c.Plan, level(p, false))
	}
	c.Plan = append(c.Plan, level(doc, true))

	dc := domainContext(doc, o.Domains)
	seen := map[string]bool{}
	for _, p := range dc.Paths {
		if p.Missing != nil {
			c.Missing = append(c.Missing, "domain "+p.Missing.String())
		}
		if p.Cycle {
			c.Problems = append(c.Problems, "the domain pins of "+p.Ref.Domain+" lead back; the domain path stops there")
		}
		terms := termsOf(p)
		for _, n := range p.Nodes {
			k := fmt.Sprintf("%s@v%d", n.DomainName(), n.DomainVersion())
			if seen[k] {
				continue
			}
			seen[k] = true
			l := DomainLevel{Domain: n.DomainName(), Version: n.DomainVersion(), Terms: []bp.Term{}, Invariants: n.Invariants()}
			for _, t := range terms {
				if t.Domain == n.DomainName() {
					l.Terms = append(l.Terms, t)
				}
			}
			if l.Invariants == nil {
				l.Invariants = []bp.Invariant{}
			}
			c.Domains = append(c.Domains, l)
		}
	}
	return c
}

// Markdown is the context as text an agent reads.
func (c AgentContext) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Context for %s\n\n", c.Title)
	b.WriteString("Read with the document. The plan path runs from the root plan node to the document; the domain path from the root domain to the pinned node, at the pinned versions. Goals and invariants are inherited downwards: a document may add, never loosen or remove. References: `parent#<id>` is the parent plan node, `<domain>#<id>` a domain node.\n\n")
	if len(c.Missing) > 0 {
		fmt.Fprintf(&b, "Not available here: %s.\n\n", strings.Join(c.Missing, ", "))
	}
	for _, p := range c.Problems {
		fmt.Fprintf(&b, "Note: %s.\n\n", p)
	}
	b.WriteString("## Plan path\n\n")
	for i, l := range c.Plan {
		title := l.Title
		if l.Issue != "" {
			title = l.Issue + " " + title
		}
		if l.Self {
			title += " (this document)"
		}
		fmt.Fprintf(&b, "### %d. %s\n\n", i+1, title)
		if len(l.Goals) == 0 && len(l.Invariants) == 0 {
			b.WriteString("No goals or invariants.\n\n")
			continue
		}
		for _, g := range l.Goals {
			fmt.Fprintf(&b, "- %s %s: %s", g.Kind, g.ID, g.Text)
			if len(g.Realizes) > 0 {
				fmt.Fprintf(&b, " (realizes %s)", strings.Join(g.Realizes, ", "))
			}
			b.WriteString("\n")
			for _, s := range g.Steps {
				fmt.Fprintf(&b, "  - %s %s\n", s.Keyword, s.Text)
			}
		}
		for _, v := range l.Invariants {
			fmt.Fprintf(&b, "- invariant %s: %s", v.ID, v.Statement)
			if v.Why != "" {
				fmt.Fprintf(&b, " (why: %s)", v.Why)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if len(c.Domains) > 0 {
		b.WriteString("## Domain path\n\n")
		for _, l := range c.Domains {
			fmt.Fprintf(&b, "### %s@v%d\n\n", l.Domain, l.Version)
			for _, t := range l.Terms {
				fmt.Fprintf(&b, "- term %s: %s", t.Term, t.Definition)
				if len(t.Aliases) > 0 {
					fmt.Fprintf(&b, " (not: %s)", strings.Join(t.Aliases, ", "))
				}
				b.WriteString("\n")
				for _, r := range t.Refined {
					fmt.Fprintf(&b, "  - narrowed by %s#%s: %s\n", r.Domain, r.ID, r.Statement)
				}
			}
			for _, v := range l.Invariants {
				fmt.Fprintf(&b, "- invariant %s#%s: %s", l.Domain, v.ID, v.Statement)
				if len(v.Refines) > 0 {
					fmt.Fprintf(&b, " (refines %s)", strings.Join(v.Refines, ", "))
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
