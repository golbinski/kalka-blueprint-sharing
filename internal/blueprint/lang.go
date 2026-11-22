package blueprint

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// The kalka language v2 (docs/language.md): one line-oriented syntax for
// every typed block. A block is header lines in column 0 (an item
// `<keyword> <name>: <text>`, a section `<keyword> <text>`, or a
// declaration `<keyword> <ref>`) and attribute lines indented by two
// spaces (`<keyword> <value>`). One line is one value; a list is a
// repeated keyword. The lexical rules are the S group, shared by all
// blocks; each block's own rules come after.

// LangVersion is the language version this reader implements.
const LangVersion = 2

// Types of a keyword's value.
type kwType int

const (
	kwText  kwType = iota // free text: colons and commas are ordinary
	kwRef                 // one reference, no spaces
	kwLabel               // a reference with an optional ": label"
	kwTerm                // the name of a term, which may hold spaces
	kwEnum                // one of a fixed set of words
)

// keyword is one row of the keyword table.
type keyword struct {
	name   string
	typ    kwType
	many   bool
	values []string // for kwEnum
}

// headerKind is what a column-0 keyword starts.
type headerKind int

const (
	hdrItem    headerKind = iota // <keyword> <name>: <text>, with attributes
	hdrSection                   // <keyword> <text>, no attributes
	hdrDecl                      // <keyword> <ref>, no attributes
)

// header is a column-0 keyword of one block.
type header struct {
	name string
	kind headerKind
	// spaced: the item's name may hold spaces and ends at the first ": "
	// (term); otherwise it is an id.
	spaced bool
	attrs  []keyword
	// declTyp is a declaration's value type.
	declTyp kwType
}

// blockLang is one block's part of the keyword table.
type blockLang struct {
	info    string
	headers []header
}

func (l *blockLang) header(name string) *header {
	for i := range l.headers {
		if l.headers[i].name == name {
			return &l.headers[i]
		}
	}
	return nil
}

func (l *blockLang) attrNames() []string {
	var out []string
	for _, h := range l.headers {
		for _, a := range h.attrs {
			if !slices.Contains(out, a.name) {
				out = append(out, a.name)
			}
		}
	}
	return out
}

func (l *blockLang) headerNames() []string {
	var out []string
	for _, h := range l.headers {
		out = append(out, h.name)
	}
	return out
}

func (h *header) attr(name string) *keyword {
	for i := range h.attrs {
		if h.attrs[i].name == name {
			return &h.attrs[i]
		}
	}
	return nil
}

// The keyword table (docs/language.md, 3).
var (
	glossaryLang = &blockLang{info: GlossaryInfo, headers: []header{
		{name: "term", kind: hdrItem, spaced: true, attrs: []keyword{
			{name: "form", typ: kwText, many: true},
			{name: "alias", typ: kwText, many: true},
			{name: "see", typ: kwTerm, many: true},
			{name: "context", typ: kwText},
		}},
	}}
	invariantsLang = &blockLang{info: InvariantsInfo, headers: []header{
		{name: "invariant", kind: hdrItem, attrs: []keyword{
			{name: "why", typ: kwText},
			{name: "verified-by", typ: kwText, many: true},
			{name: "refines", typ: kwTerm, many: true},
			{name: "status", typ: kwEnum, values: []string{StatusNew, StatusChanged, StatusRemoved}},
		}},
		{name: "affects", kind: hdrDecl, declTyp: kwRef},
	}}
	goalsLang = &blockLang{info: GoalsInfo, headers: []header{
		{name: "goal", kind: hdrItem, attrs: []keyword{
			{name: "realizes", typ: kwRef, many: true},
		}},
		{name: "criterion", kind: hdrItem, attrs: []keyword{
			{name: "realizes", typ: kwRef, many: true},
			{name: "given", typ: kwText, many: true},
			{name: "when", typ: kwText, many: true},
			{name: "then", typ: kwText, many: true},
		}},
	}}
	traceLang = &blockLang{info: TraceInfo, headers: []header{
		{name: "lane", kind: hdrSection},
		{name: "step", kind: hdrItem, attrs: []keyword{
			{name: "next", typ: kwLabel, many: true},
			{name: "back", typ: kwLabel, many: true},
			{name: "realizes", typ: kwRef, many: true},
			{name: "covers", typ: kwRef, many: true},
			{name: "ref", typ: kwRef},
			{name: "guards", typ: kwRef, many: true},
			{name: "why", typ: kwText},
			{name: "change", typ: kwEnum, values: []string{ChangeNew, ChangeModified, ChangeRemoved}},
		}},
	}}
)

// langAttr is one attribute line.
type langAttr struct {
	Keyword string
	Value   string // the reference, or the text
	Label   string // after ": " in a labelled reference
	Line    int
}

// langItem is a header line with its attributes. For a section and a
// declaration, Text is the value and there are no attributes.
type langItem struct {
	Keyword string
	Name    string
	Text    string
	Line    int
	// Section is the text of the section header above it (a trace's lane),
	// "" before any.
	Section string
	Attrs   []langAttr
}

func (it *langItem) all(kw string) []langAttr {
	var out []langAttr
	for _, a := range it.Attrs {
		if a.Keyword == kw {
			out = append(out, a)
		}
	}
	return out
}

func (it *langItem) one(kw string) string {
	for _, a := range it.Attrs {
		if a.Keyword == kw {
			return a.Value
		}
	}
	return ""
}

// lines is the line of each value of kw, the first when one repeats.
func (it *langItem) lines(kw string) map[string]int {
	out := map[string]int{}
	for _, a := range it.Attrs {
		if _, ok := out[a.Value]; a.Keyword == kw && !ok {
			out[a.Value] = a.Line
		}
	}
	return out
}

func (it *langItem) values(kw string) []string {
	var out []string
	for _, a := range it.Attrs {
		if a.Keyword == kw {
			out = append(out, a.Value)
		}
	}
	return out
}

var (
	langIDRe     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	langVerRe    = regexp.MustCompile(`^v[1-9][0-9]*$`)
	keywordishRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*:?$`)
)

// langParser reads one block; lines are file lines.
type langParser struct {
	lang   *blockLang
	base   int // file line of the opening fence
	errors []TraceError
	warns  []TraceError
}

func (p *langParser) fail(code, node string, line int, format string, a ...any) {
	p.errors = append(p.errors, TraceError{Code: code, Node: node, Line: line, Message: fmt.Sprintf(format, a...)})
}

func (p *langParser) warn(code, node string, line int, format string, a ...any) {
	p.warns = append(p.warns, TraceError{Code: code, Node: node, Line: line, Message: fmt.Sprintf(format, a...)})
}

// fix records an error with a suggested replacement of the whole line.
// fix reports an error whose fix replaces the whole line with repl. A line
// gets one fix: each repl is built on the line as the fixes before it left
// it, so the last one carries them all, over the span of the line as
// written, and the earlier ones lose theirs.
func (p *langParser) fix(code string, line int, raw, repl string, format string, a ...any) {
	f := &Fix{Text: repl, Line: line, Column: 1, EndLine: line, EndColumn: utf8.RuneCountInString(raw) + 1}
	for i := len(p.errors) - 1; i >= 0 && p.errors[i].Line == line; i-- {
		if prev := p.errors[i].Fix; prev != nil {
			f.EndColumn = prev.EndColumn
			p.errors[i].Fix = nil
		}
	}
	p.errors = append(p.errors, TraceError{Code: code, Line: line, Message: fmt.Sprintf(format, a...), Fix: f})
}

// version reads the fence: true when the block is read as v2.
func (p *langParser) version(info string) bool {
	words := strings.Fields(info)
	name := p.lang.info
	switch {
	case len(words) < 2:
		p.warn("S013", "", p.base, "the fence names no version and is read as v2; write ```%s v2", name)
	case words[1] == "v2":
	case words[1] == "v1":
		p.fail("S012", "", p.base, "%s v1 is the retired YAML form, which this kalka no longer reads; rewrite the block in the kalka language v2 and fence it ```%s v2 (kalka reference export shows the form)", name, name)
		return false
	case langVerRe.MatchString(words[1]):
		p.fail("S012", "", p.base, "%s %s: needs a newer kalka", name, words[1])
		return false
	default:
		p.fail("S012", "", p.base, "%q after %s is not a version; write ```%s v2", words[1], name, name)
		return false
	}
	return true
}

// gherkin are the words of familiar formats a writer may reach for, with
// what v2 writes instead.
var driftWords = map[string]string{
	"scenario":   "criterion AC1: <what is accepted>",
	"feature":    "a heading in the prose, and goal G1: <the outcome>",
	"background": "given lines under each criterion",
	"examples":   "one criterion per example",
	"rule":       "invariant I1: <what must hold>",
	"lanes":      "lane business, then lane code, each a line of its own",
	"nodes":      "step B1: <what happens>, under its lane",
	"invariants": "invariant I1: <what must hold>",
	"goals":      "goal G1: <the outcome>",
}

// parse lexes src. Lines that do not lex are reported and skipped; the
// items that do are returned in order.
func (p *langParser) parse(src string) []langItem {
	var out []langItem
	var cur *langItem  // the item attributes attach to
	var curHdr *header // its header
	skipping := false  // attributes of an ignored or broken header
	section := ""
	lines := strings.Split(strings.TrimSuffix(src, "\n"), "\n")
lines:
	for i, raw := range lines {
		line := p.base + 1 + i
		raw = strings.TrimSuffix(raw, "\r")
		l := strings.TrimRight(raw, " ")
		if strings.TrimSpace(l) == "" {
			continue
		}
		if strings.Contains(l, "\t") {
			repl := strings.ReplaceAll(l, "\t", "  ")
			p.fix("S001", line, l, repl, "a tab; indent with two spaces and separate words with spaces")
			l = repl
		}
		indent := len(l) - len(strings.TrimLeft(l, " "))
		body := l[indent:]
		if strings.HasPrefix(body, "#") || strings.HasPrefix(body, "//") {
			p.fail("S011", "", line, "a block has no comments; say it in the prose around the block")
			continue
		}
		if strings.HasPrefix(body, "- ") || body == "-" {
			p.drift(line, l, "a YAML list item; v2 has no dashes: write a header line such as %s with its attributes indented below it", p.example())
			if indent == 0 {
				cur, curHdr, skipping = nil, nil, true
			}
			continue
		}
		word, rest, _ := strings.Cut(body, " ")
		colon := strings.HasSuffix(word, ":")
		kw := strings.TrimSuffix(word, ":")
		lower := strings.ToLower(kw)
		switch indent {
		case 0:
			if p.gherkin(line, l, kw, rest, cur) {
				if lower != "given" && lower != "when" && lower != "then" && lower != "and" && lower != "but" {
					cur, curHdr, skipping = nil, nil, true
				}
				continue
			}
			if d, ok := driftWords[lower]; ok && p.lang.header(kw) == nil && (colon || kw != lower || strings.TrimSpace(rest) == "") {
				p.drift(line, l, "%s is the retired YAML form; in v2 write %s", kw, d)
				cur, curHdr, skipping = nil, nil, true
				continue
			}
			if strings.HasPrefix(lower, "x-") {
				cur, curHdr, skipping = nil, nil, true
				continue
			}
			h := p.lang.header(kw)
			if h == nil && kw != lower && p.lang.header(lower) != nil {
				p.fix("S002", line, l, lower+l[len(kw):], "keywords are lower case; write %s", lower)
				h = p.lang.header(lower)
				kw = lower
			}
			if h == nil {
				if p.lang.header(lower) == nil {
					for _, hh := range p.lang.headers {
						if hh.attr(lower) != nil {
							p.fail("S008", "", line, "%s is an attribute; indent it by two spaces under the %s it belongs to", lower, hh.name)
							cur, curHdr, skipping = nil, nil, true
							continue lines
						}
					}
				}
				p.unknown(line, kw, p.lang.headerNames())
				cur, curHdr, skipping = nil, nil, true
				continue
			}
			if colon {
				p.fix("S003", line, l, strings.Replace(l, word, kw, 1), "no colon after the keyword %s; write %s %s", kw, kw, strings.TrimSpace(rest))
			}
			it, ok := p.headerLine(h, line, strings.TrimSpace(rest))
			skipping = !ok
			cur, curHdr = nil, nil
			if !ok {
				continue
			}
			if h.kind == hdrSection {
				section = it.Text
			}
			it.Section = section
			out = append(out, it)
			if h.kind == hdrItem {
				cur, curHdr = &out[len(out)-1], h
			}
		case 2:
			if p.gherkin(line, l, kw, rest, cur) {
				continue
			}
			if strings.HasPrefix(lower, "x-") {
				continue
			}
			if skipping {
				continue
			}
			if cur == nil {
				p.fail("S006", "", line, "an attribute with no item above it; attributes are indented under a header line such as %s", p.example())
				continue
			}
			a := curHdr.attr(kw)
			if a == nil && kw != lower && curHdr.attr(lower) != nil {
				p.fix("S002", line, l, "  "+lower+l[2+len(kw):], "keywords are lower case; write %s", lower)
				a, kw = curHdr.attr(lower), lower
			}
			if a == nil {
				if p.lang.header(lower) != nil {
					p.fail("S008", "", line, "%s starts a line of its own; write it in column 0, not indented", lower)
					continue
				}
				var known []string
				for _, k := range curHdr.attrs {
					known = append(known, k.name)
				}
				p.unknown(line, kw, known)
				continue
			}
			if colon {
				p.fix("S003", line, l, strings.Replace(l, word, kw, 1), "no colon after the keyword %s; write %s %s", kw, kw, strings.TrimSpace(rest))
			}
			p.attrLine(cur, a, line, strings.TrimSpace(rest))
		default:
			p.fail("S004", "", line, "indented by %d spaces; a header starts in column 0 and its attributes are indented by exactly two, with no third level", indent)
		}
	}
	return out
}

// example is a header line of the block, for messages.
func (p *langParser) example() string {
	switch p.lang.info {
	case GlossaryInfo:
		return "term copy: <definition>"
	case InvariantsInfo:
		return "invariant I1: <what must hold>"
	case GoalsInfo:
		return "goal G1: <the outcome>"
	}
	return "step B1: <what happens>"
}

func (p *langParser) drift(line int, raw, format string, a ...any) {
	p.errors = append(p.errors, TraceError{Code: "S010", Line: line, Message: fmt.Sprintf(format, a...)})
	_ = raw
}

// gherkin reports Gherkin's capitalised steps and its and/but; it returns
// true when the line was one of them.
func (p *langParser) gherkin(line int, raw, kw, rest string, cur *langItem) bool {
	lower := strings.ToLower(kw)
	switch lower {
	case "given", "when", "then", "and", "but":
		if p.lang.info != GoalsInfo && (kw != lower || lower == "and" || lower == "but") {
			p.drift(line, raw, "%s is Gherkin; given, when and then are written under a criterion of a goals block", kw)
			return true
		}
	}
	switch lower {
	case "given", "when", "then":
		if kw == lower {
			return false // the v2 attribute, or one misplaced (S008)
		}
		repl := "  " + lower + " " + strings.TrimSpace(rest)
		p.fix("S010", line, raw, repl, "%s is Gherkin; in v2 the step is an attribute of a criterion, lower case and indented by two spaces: %s", kw, strings.TrimSpace(repl))
		return true
	case "and", "but":
		prev := "then"
		if cur != nil {
			for _, a := range slices.Backward(cur.Attrs) {
				if _, ok := stepOrder[a.Keyword]; ok {
					prev = a.Keyword
					break
				}
			}
		}
		repl := "  " + prev + " " + strings.TrimSpace(rest)
		p.fix("S010", line, raw, repl, "there is no %s; repeat the keyword: %s", lower, strings.TrimSpace(repl))
		return true
	}
	return false
}

// unknown warns about a keyword that is not in the table (S020).
func (p *langParser) unknown(line int, kw string, known []string) {
	if !keywordishRe.MatchString(kw) {
		p.warn("S020", "", line, "%q does not start with a keyword, so the line is ignored; a line starts with one of %s", kw, strings.Join(known, ", "))
		return
	}
	msg := fmt.Sprintf("unknown keyword %s is ignored", kw)
	if s := nearest(kw, known); s != "" {
		msg += fmt.Sprintf("; did you mean %s?", s)
	} else {
		msg += fmt.Sprintf("; the keywords here are %s, and one of your own starts with x-", strings.Join(known, ", "))
	}
	p.warn("S020", "", line, "%s", msg)
}

// headerLine reads what follows a header keyword.
func (p *langParser) headerLine(h *header, line int, rest string) (langItem, bool) {
	it := langItem{Keyword: h.name, Line: line}
	switch h.kind {
	case hdrSection:
		if rest == "" {
			p.fail("S005", "", line, "%s needs a name: %s business", h.name, h.name)
			return it, false
		}
		it.Text = rest
		return it, true
	case hdrDecl:
		if rest == "" {
			p.fail("S007", "", line, "%s needs a value: %s library#L1", h.name, h.name)
			return it, false
		}
		if strings.ContainsAny(rest, " ") {
			p.fail("S009", "", line, "%s names one reference with no spaces, such as %s library#L1; write one per line", h.name, h.name)
			return it, false
		}
		it.Text = rest
		return it, true
	}
	name, text, ok := strings.Cut(rest, ": ")
	if !ok && strings.HasSuffix(rest, ":") {
		name, text, ok = strings.TrimSuffix(rest, ":"), "", true
	}
	name, text = strings.TrimSpace(name), strings.TrimSpace(text)
	switch {
	case !ok || name == "":
		p.fail("S005", "", line, "a %s line is %s <name>: <text>, such as %s", h.name, h.name, p.example())
		return it, false
	case !h.spaced && (utf8.RuneCountInString(name) > maxNodeID || !langIDRe.MatchString(name)):
		p.fail("S030", name, line, "%q is not an id: an id starts with a letter, uses only letters, digits, - and _, and has at most %d characters", name, maxNodeID)
		return it, false
	case text == "":
		p.fail("S005", name, line, "%s %s has no text after the colon; say it in one line", h.name, name)
		return it, false
	}
	it.Name, it.Text = name, text
	return it, true
}

// attrLine reads one attribute's value into it.
func (p *langParser) attrLine(it *langItem, k *keyword, line int, rest string) {
	if rest == "" {
		p.fail("S007", it.Name, line, "%s has no value; write %s <value> or remove the line", k.name, k.name)
		return
	}
	if !k.many && slices.ContainsFunc(it.Attrs, func(a langAttr) bool { return a.Keyword == k.name }) {
		p.fail("S021", it.Name, line, "%s is written once per %s; keep one line", k.name, it.Keyword)
		return
	}
	a := langAttr{Keyword: k.name, Value: rest, Line: line}
	switch k.typ {
	case kwRef:
		if strings.Contains(rest, " ") {
			p.fail("S009", it.Name, line, "%s names one reference with no spaces; write one per line, %s %s", k.name, k.name, strings.Fields(rest)[0])
			return
		}
	case kwLabel:
		ref, label, hasLabel := strings.Cut(rest, ": ")
		ref, label = strings.TrimSpace(ref), strings.TrimSpace(label)
		if !hasLabel && strings.HasSuffix(ref, ":") {
			ref, hasLabel = strings.TrimSuffix(ref, ":"), true
		}
		if strings.Contains(ref, " ") || ref == "" {
			p.fail("S009", it.Name, line, "%s names one id with no spaces, optionally followed by \": \" and a label: %s C3: queue empty", k.name, k.name)
			return
		}
		if hasLabel && label == "" {
			p.fail("S007", it.Name, line, "the label after %s %s is empty; write a label or drop the colon", k.name, ref)
			return
		}
		a.Value, a.Label = ref, label
	case kwEnum:
		if !slices.Contains(k.values, rest) {
			p.fail("S022", it.Name, line, "%s is %s, not %q", k.name, orList(k.values), rest)
			return
		}
	}
	it.Attrs = append(it.Attrs, a)
}

func orList(vs []string) string {
	if len(vs) < 2 {
		return strings.Join(vs, "")
	}
	return strings.Join(vs[:len(vs)-1], ", ") + " or " + vs[len(vs)-1]
}
