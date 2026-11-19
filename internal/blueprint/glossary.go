package blueprint

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"go.yaml.in/yaml/v3"
)

// GlossaryInfo is the first word of a glossary block's fence (spec 6.16).
const GlossaryInfo = "glossary"

// Limits of glossary blocks (spec 6.16).
const (
	MaxGlossaryTerms  = 500
	maxTermLen        = 60
	advisedDefinition = 400
)

var glossaryKeys = []string{"term", "definition", "aliases", "forms", "context", "see"}

var glossaryCodes = blockCodes{parse: "G001", feature: "G002", dupKey: "G003", shape: "G004", version: "G005", noVersion: "G006", unknownKey: "G007"}

// Term is one entry of a domain's glossary.
type Term struct {
	Term       string   `json:"term"`
	Definition string   `json:"definition"`
	Aliases    []string `json:"aliases,omitempty"`
	Forms      []string `json:"forms,omitempty"`
	Context    string   `json:"context,omitempty"`
	See        []string `json:"see,omitempty"`
	Line       int      `json:"line"`
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
		p := &blockParser{codes: glossaryCodes, base: f.line}
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
		d.blockProblems(what, p.errors, p.warns)
	}
	if len(d.glossary) > MaxGlossaryTerms {
		d.problem("G014", 0, fmt.Sprintf("the glossary has %d terms; at most %d are read", len(d.glossary), MaxGlossaryTerms))
		d.glossary = d.glossary[:MaxGlossaryTerms]
	}
	// see names a term of the same glossary.
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

// glossary reads one block: a list of entries.
func (p *blockParser) glossary(f fence) []Term {
	if !p.version(f.info, GlossaryInfo) {
		return nil
	}
	root := p.read(f.source)
	if root == nil {
		if len(p.errors) == 0 {
			p.fail("G004", "", 1, "the block is a list of entries, each with term and definition")
		}
		return nil
	}
	if root.Kind != yaml.SequenceNode || len(root.Content) == 0 {
		p.fail("G004", "", root.Line, "the block is a list of entries, each starting \"- term: …\"")
		return nil
	}
	var out []Term
	for _, e := range root.Content {
		if e.Kind != yaml.MappingNode || e.Style&yaml.FlowStyle != 0 {
			p.fail("G004", "", e.Line, "an entry is a block mapping, one key per line, starting \"- term: …\"")
			continue
		}
		t := Term{Line: p.base + e.Line}
		var ok [6]bool
		t.Term, ok[0] = p.text(e, "term", "")
		node := t.Term
		p.unknownKeys(e, glossaryKeys, node)
		t.Definition, ok[1] = p.text(e, "definition", node)
		t.Context, ok[2] = p.text(e, "context", node)
		t.Aliases, ok[3] = p.texts(e, "aliases", node)
		t.Forms, ok[4] = p.texts(e, "forms", node)
		t.See, ok[5] = p.texts(e, "see", node)
		if ok != [6]bool{true, true, true, true, true, true} {
			continue
		}
		switch {
		case t.Term == "":
			p.fail("G010", "", e.Line, "the entry has no term; add term: with the word as the team uses it")
			continue
		case utf8.RuneCountInString(t.Term) > maxTermLen:
			p.fail("G013", node, e.Line, "the term is longer than %d characters; a term is a word or a short phrase", maxTermLen)
			continue
		case t.Definition == "":
			p.fail("G011", node, e.Line, "the term has no definition; say in a sentence what it means here")
			continue
		}
		if utf8.RuneCountInString(t.Definition) > advisedDefinition {
			p.warn("G015", node, e.Line, "the definition is longer than %d characters; keep it to what a reader needs on hover", advisedDefinition)
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
	// Where: body, trace (a node's text) or invariant (a statement).
	Where     string `json:"where"`
	Node      string `json:"node,omitempty"`
	Line      int    `json:"line"`
	Column    int    `json:"column,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	EndColumn int    `json:"end_column,omitempty"`
}

// TermUses finds the terms in the document's prose, trace node texts and
// invariant statements, in that order.
func (d *Document) TermUses(m *Matcher) []TermUse {
	var out []TermUse
	for i := range d.runs {
		r := &d.runs[i]
		for _, x := range m.Match(r.text) {
			l, c := r.src.pos(r.srcOffset(x.Start))
			el, ec := r.src.pos(r.srcOffset(x.End))
			out = append(out, TermUse{Term: x.Term, Alias: x.Alias, Surface: r.text[x.Start:x.End], Where: "body", Line: l, Column: c, EndLine: el, EndColumn: ec})
		}
	}
	for _, t := range d.traces {
		for _, n := range t.Nodes {
			for _, x := range m.Match(n.Text) {
				out = append(out, TermUse{Term: x.Term, Alias: x.Alias, Surface: n.Text[x.Start:x.End], Where: "trace", Node: n.ID, Line: n.Line})
			}
		}
	}
	for _, inv := range d.invariants {
		for _, x := range m.Match(inv.Statement) {
			out = append(out, TermUse{Term: x.Term, Alias: x.Alias, Surface: inv.Statement[x.Start:x.End], Where: "invariant", Node: inv.ID, Line: inv.Line})
		}
	}
	return out
}
