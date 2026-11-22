package blueprint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
)

// GlossaryInfo is the first word of a glossary block's fence (spec 6.16).
const GlossaryInfo = "glossary"

// Limits of glossary blocks (spec 6.16).
const (
	MaxGlossaryTerms  = 500
	maxTermLen        = 60
	advisedDefinition = 400
)

// Term is one entry of a domain's glossary.
type Term struct {
	Term       string   `json:"term"`
	Definition string   `json:"definition"`
	Aliases    []string `json:"aliases,omitempty"`
	Forms      []string `json:"forms,omitempty"`
	Context    string   `json:"context,omitempty"`
	See        []string `json:"see,omitempty"`
	Line       int      `json:"line"`
	// Domain is the domain node that defines the term, set where a
	// glossary is put together from a domain path (spec 6.16).
	Domain string `json:"domain,omitempty"`
	// Refined are the invariants of domain nodes on the path that narrow
	// the term (refines).
	Refined []Refinement `json:"refined,omitempty"`
}

// Refinement is an invariant that narrows a term: its domain, id and
// statement.
type Refinement struct {
	Domain    string `json:"domain"`
	ID        string `json:"id"`
	Statement string `json:"statement"`
}

// Glossary returns the terms of the document's valid glossary blocks, in
// order: a domain document's, or a blueprint's own when it names no domain
// (spec 6.16); nil for a blueprint that names one.
func (d *Document) Glossary() []Term { return d.glossary }

// buildGlossary reads the glossary blocks (spec 6.16).
func (d *Document) buildGlossary(fences []fence) {
	seen := map[string]string{} // lower-cased phrase → the term it names
	n := 0
	for _, f := range fences {
		if f.word != GlossaryInfo {
			continue
		}
		n++
		what := fmt.Sprintf("glossary %d", n)
		if !d.IsDomain() && d.get(KeyDomain) != "" {
			d.problem("G008", f.line, what+": this blueprint is linked against the glossary of the domain its "+KeyDomain+": names; its own is shown as code")
			continue
		}
		p := &langParser{lang: glossaryLang, base: f.line}
		terms := p.glossary(f)
		// Across blocks and within one: a term, form or alias names one entry.
		var keep []Term
		for _, t := range terms {
			clash := ""
			for _, ph := range append(append([]string{t.Term}, t.Forms...), t.Aliases...) {
				k := PhraseKey(ph)
				if other, ok := seen[k]; ok && clash == "" {
					clash = fmt.Sprintf("%q also names %q; a term, form or alias names one entry", ph, other)
				}
			}
			if clash != "" {
				p.errors = append(p.errors, TraceError{Code: "G012", Node: t.Term, Line: t.Line, Message: clash})
				continue
			}
			for _, ph := range append(append([]string{t.Term}, t.Forms...), t.Aliases...) {
				seen[PhraseKey(ph)] = t.Term
			}
			keep = append(keep, t)
		}
		if len(p.errors) == 0 || len(keep) > 0 && onlyCode(p.errors, "G012") {
			d.glossary = append(d.glossary, keep...)
		}
		d.blockProblems(what, "", p.errors, p.warns)
	}
	if len(d.glossary) > MaxGlossaryTerms {
		d.problem("G014", 0, fmt.Sprintf("the glossary has %d terms; at most %d are read", len(d.glossary), MaxGlossaryTerms))
		d.glossary = d.glossary[:MaxGlossaryTerms]
	}
	// see names a term of the same glossary; a subdomain's may also name
	// its ancestors' terms, which the checker resolves.
	if _, sub := d.DomainParent(); sub {
		return
	}
	known := map[string]bool{}
	for _, t := range d.glossary {
		known[PhraseKey(t.Term)] = true
	}
	for _, t := range d.glossary {
		for _, s := range t.See {
			if !known[PhraseKey(s)] {
				p := NewProblem("G016", t.Line, fmt.Sprintf("glossary, %s: see names %q, which is not a term of this glossary", t.Term, s))
				p.Node = t.Term
				d.problems = append(d.problems, p)
			}
		}
	}
}

func onlyCode(es []TraceError, code string) bool {
	for _, e := range es {
		if e.Code != code {
			return false
		}
	}
	return true
}

// glossary reads one block: term lines with their attributes.
func (p *langParser) glossary(f fence) []Term {
	if !p.version(f.info) {
		return nil
	}
	items := p.parse(f.source)
	if len(items) == 0 && len(p.errors) == 0 {
		p.fail("S031", "", f.line, "the block has no terms; write term copy: <what it means here>")
		return nil
	}
	var out []Term
	for _, it := range items {
		t := Term{Term: it.Name, Definition: it.Text, Line: it.Line,
			Forms: it.values("form"), Aliases: it.values("alias"), See: it.values("see"), Context: it.one("context")}
		if utf8.RuneCountInString(t.Term) > maxTermLen {
			p.fail("G013", t.Term, it.Line, "the term is longer than %d characters; a term is a word or a short phrase", maxTermLen)
			continue
		}
		if utf8.RuneCountInString(t.Definition) > advisedDefinition {
			p.warn("G015", t.Term, it.Line, "the definition is longer than %d characters; keep it to what a reader needs on hover", advisedDefinition)
		}
		out = append(out, t)
	}
	return out
}

// PhraseKey is a phrase as matching compares it: lower case, single
// spaces.
func PhraseKey(s string) string {
	return strings.Join(strings.Fields(lowerRunes(s)), " ")
}

// lowerRunes lower-cases one character at a time (simple case mapping),
// as the page's matcher does.
func lowerRunes(s string) string {
	return strings.Map(unicode.ToLower, s)
}

// isWordRune: letters, digits and combining marks make up words; any
// other character is a boundary.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}

type phrase struct {
	runes []rune // lower case; a space stands for any run of white space
	term  int
	alias bool
}

// Matcher finds a glossary's terms in text (spec 6.16).
type Matcher struct {
	terms   []Term
	phrases []phrase
}

// NewMatcher prepares terms for matching.
func NewMatcher(terms []Term) *Matcher {
	m := &Matcher{terms: terms}
	for i, t := range terms {
		for _, ph := range append([]string{t.Term}, t.Forms...) {
			if k := PhraseKey(ph); k != "" {
				m.phrases = append(m.phrases, phrase{[]rune(k), i, false})
			}
		}
		for _, ph := range t.Aliases {
			if k := PhraseKey(ph); k != "" {
				m.phrases = append(m.phrases, phrase{[]rune(k), i, true})
			}
		}
	}
	return m
}

// Terms are the matcher's terms.
func (m *Matcher) Terms() []Term { return m.terms }

// TermMatch is one match in a text: byte offsets, the term's index and
// whether an alias matched.
type TermMatch struct {
	Start, End int
	Term       int
	Alias      bool
}

// Match finds the terms in s: case-insensitive, whole words, the longest
// match at each position, never overlapping.
func (m *Matcher) Match(s string) []TermMatch {
	if m == nil || len(m.phrases) == 0 {
		return nil
	}
	var out []TermMatch
	prev := rune(-1)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if isWordRune(r) && (prev < 0 || !isWordRune(prev)) {
			best, bi := -1, -1
			for k, ph := range m.phrases {
				if end := matchAt(s, i, ph.runes); end > best {
					best, bi = end, k
				}
			}
			if bi >= 0 {
				out = append(out, TermMatch{Start: i, End: best, Term: m.phrases[bi].term, Alias: m.phrases[bi].alias})
				last, _ := utf8.DecodeLastRuneInString(s[:best])
				prev, i = last, best
				continue
			}
		}
		prev = r
		i += size
	}
	return out
}

// matchAt is the end of ph matched at s[i:], or -1.
func matchAt(s string, i int, ph []rune) int {
	j := i
	for _, pr := range ph {
		if j >= len(s) {
			return -1
		}
		if pr == ' ' {
			n := 0
			for j < len(s) {
				r, size := utf8.DecodeRuneInString(s[j:])
				if !unicode.IsSpace(r) {
					break
				}
				j += size
				n++
			}
			if n == 0 {
				return -1
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(s[j:])
		if unicode.ToLower(r) != pr {
			return -1
		}
		j += size
	}
	if j < len(s) {
		if r, _ := utf8.DecodeRuneInString(s[j:]); isWordRune(r) {
			return -1
		}
	}
	return j
}

// textRun is a maximal run of prose text in one inline element (spec
// 6.16), with where each piece came from.
type textRun struct {
	text   string
	pieces []runPiece
	src    *textSource
}

type runPiece struct{ at, src int } // offset in the run, offset in src

// textSource is a parsed part of the file: its text with LF endings, the
// start of each line, and the file line of its first line minus one.
type textSource struct {
	lf     string
	starts []int
	base   int
}

// pos is the file line and column (characters, from 1) of an offset.
func (s *textSource) pos(off int) (int, int) {
	i := sort.Search(len(s.starts), func(k int) bool { return s.starts[k] > off }) - 1
	if i < 0 {
		i = 0
	}
	return s.base + i + 1, utf8.RuneCountInString(s.lf[s.starts[i]:off]) + 1
}

// srcOffset maps an offset in the run to one in its source.
func (r *textRun) srcOffset(at int) int {
	k := sort.Search(len(r.pieces), func(i int) bool { return r.pieces[i].at > at }) - 1
	if k < 0 {
		k = 0
	}
	return r.pieces[k].src + at - r.pieces[k].at
}

// collectRuns gathers the prose runs of a parsed part: paragraphs,
// headings, list items, link text, emphasis. Code, raw HTML, autolinks
// and images are left out; keep filters by source offset.
func collectRuns(root ast.Node, src *textSource, keep func(off int) bool) []textRun {
	var out []textRun
	b := []byte(src.lf)
	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		switch n.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock, *ast.HTMLBlock, *ast.CodeSpan, *ast.RawHTML, *ast.AutoLink, *ast.Image:
			return
		}
		var cur *textRun
		flush := func() {
			if cur != nil && len(cur.pieces) > 0 && keep(cur.pieces[0].src) {
				out = append(out, *cur)
			}
			cur = nil
		}
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			t, ok := c.(*ast.Text)
			if !ok {
				flush()
				walk(c)
				continue
			}
			if cur == nil {
				cur = &textRun{src: src}
			}
			seg := t.Segment
			cur.pieces = append(cur.pieces, runPiece{len(cur.text), seg.Start})
			cur.text += string(seg.Value(b))
			if t.SoftLineBreak() || t.HardLineBreak() {
				cur.pieces = append(cur.pieces, runPiece{len(cur.text), seg.Stop})
				cur.text += "\n"
			}
		}
		flush()
	}
	walk(root)
	return out
}

// TermUse is one use of a glossary term in a document.
type TermUse struct {
	Term    int    `json:"term"`
	Alias   bool   `json:"alias,omitempty"`
	Surface string `json:"surface"`
	// Qualifier is the domain written right before the word,
	// library.reservations in library.reservations#hold, or "".
	Qualifier string `json:"qualifier,omitempty"`
	// Where: body, trace (a step's text), invariant (a statement) or goal
	// (a goal's or criterion's text and steps).
	Where     string `json:"where"`
	Node      string `json:"node,omitempty"`
	Line      int    `json:"line"`
	Column    int    `json:"column,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	EndColumn int    `json:"end_column,omitempty"`
}

var qualifierRe = regexp.MustCompile(`([a-z0-9][a-z0-9-]*(?:\.[a-z0-9][a-z0-9-]*)*)#$`)

// qualifier is the domain name written right before offset at in s.
func qualifier(s string, at int) string {
	if m := qualifierRe.FindStringSubmatch(s[:at]); m != nil {
		return m[1]
	}
	return ""
}

// TermUses finds the terms in the document's prose, trace step texts,
// invariant statements and goals, in that order.
func (d *Document) TermUses(m *Matcher) []TermUse {
	var out []TermUse
	for i := range d.runs {
		r := &d.runs[i]
		for _, x := range m.Match(r.text) {
			l, c := r.src.pos(r.srcOffset(x.Start))
			el, ec := r.src.pos(r.srcOffset(x.End))
			out = append(out, TermUse{Term: x.Term, Alias: x.Alias, Surface: r.text[x.Start:x.End], Qualifier: qualifier(r.text, x.Start), Where: "body", Line: l, Column: c, EndLine: el, EndColumn: ec})
		}
	}
	add := func(where, node, text string, line int) {
		for _, x := range m.Match(text) {
			out = append(out, TermUse{Term: x.Term, Alias: x.Alias, Surface: text[x.Start:x.End], Qualifier: qualifier(text, x.Start), Where: where, Node: node, Line: line})
		}
	}
	for _, t := range d.traces {
		for _, n := range t.Nodes {
			add("trace", n.ID, n.Text, n.Line)
		}
	}
	for _, inv := range d.invariants {
		add("invariant", inv.ID, inv.Statement, inv.Line)
	}
	for _, g := range d.goals {
		add("goal", g.ID, g.Text, g.Line)
		for _, s := range g.Steps {
			add("goal", g.ID, s.Text, s.Line)
		}
	}
	return out
}
