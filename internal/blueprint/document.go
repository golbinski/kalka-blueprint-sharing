// Package blueprint parses and writes blueprint files (spec section 6).
//
// A Document keeps the file as spans of raw lines: frontmatter, body before
// the review log, the log, and any sections after the log. An unmodified
// document writes back exactly the bytes it was parsed from. A modified one
// keeps the raw bytes of every span it did not change and renders the review
// log in canonical order.
package blueprint

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/rules"
)

// MaxFile is the largest blueprint accepted (spec 6.9).
const MaxFile = 2 << 20

// LogTitle is the heading text of the review log section.
const LogTitle = "Review log"

// ErrTooLarge and ErrNotUTF8 make a file unreadable as a blueprint.
var (
	ErrTooLarge = fmt.Errorf("file is larger than %d bytes", MaxFile)
	ErrNotUTF8  = errors.New("file is not valid UTF-8")
)

// Severity of a Problem.
type Severity int

const (
	Info Severity = iota
	Warning
	Error
)

func (s Severity) String() string {
	return [...]string{"info", "warning", "error"}[s]
}

// MarshalText makes severities readable in JSON.
func (s Severity) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Problem is a diagnostic (spec 6.17): a rule of the registry, where it
// applies and what to write instead. Lines and columns are 1-based (columns
// in characters), 0 when unknown.
type Problem struct {
	Line      int      `json:"line,omitempty"`
	Column    int      `json:"column,omitempty"`
	EndLine   int      `json:"end_line,omitempty"`
	EndColumn int      `json:"end_column,omitempty"`
	Rule      string   `json:"rule"`
	Severity  Severity `json:"severity"`
	Message   string   `json:"message"`
	// Node is the trace node or invariant the diagnostic concerns.
	Node string `json:"node,omitempty"`
	// Blocks marks a structural error: the file cannot be published.
	Blocks bool `json:"blocks,omitempty"`
	Fix    *Fix `json:"fix,omitempty"`
}

// Fix is a suggested replacement; kalka never applies it.
type Fix struct {
	Text      string `json:"text"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"end_line"`
	EndColumn int    `json:"end_column"`
}

// NewProblem is a diagnostic of a registered rule, with its severity.
func NewProblem(rule string, line int, msg string) Problem {
	r := rules.Must(rule)
	return Problem{Rule: rule, Severity: severityOf(r.Severity), Line: line, Message: msg, Blocks: r.Blocks}
}

func severityOf(s rules.Severity) Severity {
	switch s {
	case rules.Error:
		return Error
	case rules.Warning:
		return Warning
	}
	return Info
}

// UnmarshalText reads a severity back from JSON.
func (s *Severity) UnmarshalText(b []byte) error {
	switch string(b) {
	case "error":
		*s = Error
	case "warning":
		*s = Warning
	default:
		*s = Info
	}
	return nil
}

// Heading is a heading of the body.
type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Slug  string `json:"slug"`
	Line  int    `json:"line"` // 1-based
	// Nested is true for a heading inside a block quote or list item.
	Nested bool `json:"nested,omitempty"`
}

// Item is one entry of the review log with the unparsed content that
// follows it. Unparsed content belongs to the entry before it (spec 6.4).
type Item struct {
	Entry    *Entry
	Line     int      // 1-based line of the header, 0 when not from a file
	Trailing []string // raw lines of unparsed content after the entry
}

// Document is a parsed blueprint.
type Document struct {
	raw      string
	eol      string
	fm       *Frontmatter
	pre      []string // raw lines between frontmatter and log heading
	hasLog   bool
	logHead  string   // raw log heading line
	lead     []string // raw lines between log heading and first entry
	items    []*Item
	tail     []string // raw lines after the log section
	headings []Heading
	problems []Problem
	modified bool
	preLine  int // 1-based file line of pre[0]
	tailLine int // 1-based file line of tail[0]
	traces   []Trace
	// Typed blocks other than trace (spec 6.15, 6.16).
	glossary   []Term
	invariants []Invariant
	affects    []Affect
	// runs is the body's prose, where glossary terms are matched.
	runs []textRun
}

var (
	h12Re = regexp.MustCompile(`^ {0,3}#{1,2}(?:[ \t]|$)`)
	// logHeadRe is the line that starts the review log: at the start of
	// the line, outside a code fence.
	logHeadRe = regexp.MustCompile(`(?i)^## +review log *$`)
	h3Re      = regexp.MustCompile(`^ {0,3}###(?:[ \t]|$)`)
)

// Parse reads a blueprint. It fails only for input that cannot be a
// blueprint at all (too large, not UTF-8); everything else is reported as a
// Problem and the document stays usable.
func Parse(data []byte) (*Document, error) {
	if len(data) > MaxFile {
		return nil, ErrTooLarge
	}
	if !utf8.Valid(data) {
		return nil, ErrNotUTF8
	}
	d := &Document{raw: string(data)}
	s := strings.TrimPrefix(d.raw, "\uFEFF")
	d.eol = dominantEOL(s)
	lines := splitLines(s)

	fmLines, rest, ok := splitFrontmatter(lines)
	if !ok {
		d.problem("F001", 1, "frontmatter opened with --- is never closed; it is read as body")
	}
	if fmLines != nil {
		d.fm = &Frontmatter{Lines: fmLines}
	}
	base := len(fmLines) // index of rest[0] among all lines
	d.preLine = base + 1

	lf, starts := lfText(rest)
	root := goldmark.DefaultParser().Parse(text.NewReader([]byte(lf)))
	lineOf := func(n ast.Node) int {
		return sort.Search(len(starts), func(i int) bool { return starts[i] > n.Pos() }) - 1
	}

	// The review log starts at a line rule, not at what a Markdown parser
	// takes for a heading: what is hashed must not depend on a parser's
	// edge cases (spec 6.4).
	logStart := -1
	var fence fenceState
	for i, l := range rest {
		if fence.step(l) || fence.open {
			continue
		}
		if !logHeadRe.MatchString(content(l)) {
			continue
		}
		if logStart < 0 {
			logStart = i
		} else {
			d.problem("B002", base+i+1, "a second \"## Review log\" heading is ordinary body text; only the first is the review log")
		}
		fence = fenceState{}
	}

	logEnd := len(rest)
	if logStart >= 0 {
		d.hasLog = true
		d.logHead = rest[logStart]
		var fence fenceState
		for i := logStart + 1; i < len(rest); i++ {
			if isEntryHeader(rest[i]) {
				fence = fenceState{}
				continue
			}
			if fence.step(rest[i]) || fence.open {
				continue
			}
			if h12Re.MatchString(content(rest[i])) {
				logEnd = i
				break
			}
		}
		d.pre = rest[:logStart]
		d.tail = rest[logEnd:]
		d.tailLine = base + logEnd + 1
		d.parseLog(rest[logStart+1:logEnd], base+logStart+1)
	} else {
		d.pre = rest
	}

	// Body headings: those before the log from the full parse, and those
	// after it from a parse of the tail alone.
	sl := NewSlugger()
	ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		ln := lineOf(h)
		if logStart >= 0 && ln >= logStart {
			return ast.WalkStop, nil
		}
		t := strings.TrimSpace(inlineText(h, []byte(lf)))
		d.headings = append(d.headings, Heading{Level: h.Level, Text: t, Slug: sl.Slug(t), Line: base + ln + 1, Nested: h.Parent() != root})
		return ast.WalkSkipChildren, nil
	})
	lineAt := func(starts []int) func(int) int {
		return func(off int) int { return sort.Search(len(starts), func(i int) bool { return starts[i] > off }) - 1 }
	}
	fences := findFences(root, []byte(lf), lineAt(starts), base, func(l int) bool { return logStart < 0 || l < logStart })
	src := &textSource{lf: lf, starts: starts, base: base}
	d.runs = collectRuns(root, src, func(off int) bool { return logStart < 0 || lineAt(starts)(off) < logStart })
	if len(d.tail) > 0 {
		tlf, tstarts := lfText(d.tail)
		troot := goldmark.DefaultParser().Parse(text.NewReader([]byte(tlf)))
		ast.Walk(troot, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			h, ok := n.(*ast.Heading)
			if !entering || !ok {
				return ast.WalkContinue, nil
			}
			ln := sort.Search(len(tstarts), func(i int) bool { return tstarts[i] > h.Pos() }) - 1
			t := strings.TrimSpace(inlineText(h, []byte(tlf)))
			d.headings = append(d.headings, Heading{Level: h.Level, Text: t, Slug: sl.Slug(t), Line: base + logEnd + ln + 1, Nested: h.Parent() != troot})
			return ast.WalkSkipChildren, nil
		})
		fences = append(fences, findFences(troot, []byte(tlf), lineAt(tstarts), base+logEnd, func(int) bool { return true })...)
		d.runs = append(d.runs, collectRuns(troot, &textSource{lf: tlf, starts: tstarts, base: base + logEnd}, func(int) bool { return true })...)
	}
	d.buildTraces(fences)
	d.buildGlossary(fences)
	d.buildInvariants(fences)

	if !d.hasLog && !logAppendable(d.pre) {
		d.problem("B001", 0, "the body ends inside an unclosed code fence or HTML block, so a review log cannot be added after it")
	}
	d.checkFrontmatter()
	d.problems = append(d.problems, d.domainProblems()...)
	d.checkEntries()
	return d, nil
}

// lfText joins lines with LF endings and returns the start offset of each.
func lfText(lines []string) (string, []int) {
	var b strings.Builder
	starts := make([]int, len(lines))
	for i, l := range lines {
		starts[i] = b.Len()
		b.WriteString(content(l))
		b.WriteByte('\n')
	}
	return b.String(), starts
}

// inlineText is the plain text of a heading, as GitHub slugs it.
func inlineText(n ast.Node, src []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch t := c.(type) {
			case *ast.Text:
				b.Write(t.Value(src))
				if t.SoftLineBreak() || t.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(t.Value)
			case *ast.AutoLink:
				b.Write(t.Label(src))
			case *ast.RawHTML:
				// GitHub drops tags from heading text.
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}

// parseLog splits the log section into entries and unparsed content.
// first is the 1-based line number of lines[0].
func (d *Document) parseLog(lines []string, first int) {
	// A strict entry header always starts an entry, even inside a code
	// fence: an entry that leaves a fence open must not swallow the entries
	// after it. Other level-3 headings count only outside fences.
	var starts []int
	var fence fenceState
	for i, l := range lines {
		if isEntryHeader(l) {
			starts = append(starts, i)
			fence = fenceState{}
			continue
		}
		if fence.step(l) || fence.open {
			continue
		}
		if h3Re.MatchString(content(l)) {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		d.lead = lines
		d.noteUnparsed(lines, first)
		return
	}
	d.lead = lines[:starts[0]]
	d.noteUnparsed(d.lead, first)
	for k, a := range starts {
		b := len(lines)
		if k+1 < len(starts) {
			b = starts[k+1]
		}
		chunk := lines[a:b]
		e, err := ParseHeader(chunk[0])
		if err != nil {
			d.problem("B003", first+a+1, "not an entry header, kept as unparsed content: "+err.Error())
			if n := len(d.items); n > 0 {
				d.items[n-1].Trailing = append(d.items[n-1].Trailing, chunk...)
			} else {
				d.lead = append(d.lead, chunk...)
			}
			continue
		}
		e.Body = rawBody(chunk[1:])
		d.items = append(d.items, &Item{Entry: e, Line: first + a})
	}
}

func isEntryHeader(line string) bool {
	if !strings.HasPrefix(line, "### ") {
		return false
	}
	_, err := ParseHeader(line)
	return err == nil
}

// leavesFenceOpen reports whether lines end inside a code fence.
func leavesFenceOpen(lines []string) (bool, fenceState) {
	var f fenceState
	for _, l := range lines {
		f.step(l)
	}
	return f.open, f
}

func (d *Document) noteUnparsed(lines []string, first int) {
	for i, l := range lines {
		if !isBlank(l) {
			d.problem("B003", first+i, "content before the first entry is kept as unparsed content")
			return
		}
	}
}

// rawBody returns body lines as LF text without outer blank lines.
func rawBody(lines []string) string {
	out := make([]string, 0, len(lines))
	for _, l := range trimBlankLines(lines) {
		out = append(out, content(l))
	}
	return strings.Join(out, "\n")
}

var uuidV4Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func (d *Document) checkFrontmatter() {
	id, hasID := d.fm.Get(name.FrontmatterID)
	owner, hasOwner := d.fm.Get(name.FrontmatterOwner)
	switch {
	case !hasID:
		d.problem("F005", 0, "no "+name.FrontmatterID+": a local blueprint, given an id on first send")
	case !uuidV4Re.MatchString(strings.ToLower(id)):
		d.problem("F002", d.fm.KeyLine(name.FrontmatterID), fmt.Sprintf("%s %q is not a UUID v4", name.FrontmatterID, id))
	}
	if hasID && !hasOwner {
		d.problem("F003", d.fm.KeyLine(name.FrontmatterID), "shared blueprint has no owner")
	}
	if hasOwner && !emailRe.MatchString(owner) {
		d.problem("F004", d.fm.KeyLine(name.FrontmatterOwner), fmt.Sprintf("owner %q is not an email address", owner))
	}
}

func (d *Document) checkEntries() {
	rev := d.Rev()
	seen := map[string]bool{}
	for _, it := range d.items {
		e := it.Entry
		if !e.Valid() {
			// A warning, not an error: one edited entry must not take the
			// whole document out of the replica (spec 6.6).
			d.problem("B010", it.Line, fmt.Sprintf("entry %s was edited: its id no longer matches its content (want %s); peers do not take it; if the team holds the original, the next sync restores it", e.ID, e.ComputeID()))
			continue
		}
		if seen[e.ID] {
			d.problem("B011", it.Line, "entry "+e.ID+" appears more than once")
		}
		seen[e.ID] = true
		if len(e.Body) > MaxEntryBody {
			d.problem("B012", it.Line, fmt.Sprintf("entry body is larger than %d bytes", MaxEntryBody))
		}
		if open, _ := leavesFenceOpen(strings.Split(e.Body, "\n")); open {
			d.problem("B013", it.Line, "entry body opens a code fence and never closes it")
		}
		if hasShallowHeading(e.Body) {
			d.problem("B014", it.Line, "headings inside an entry must be level 4 or deeper")
		}
		if e.Anchor != "" && !d.HasAnchor(e.Anchor) {
			what := "no heading"
			if e.Node() != "" && d.HasSection(e.Section()) {
				what = "no trace node under that heading"
			}
			d.problem("B015", it.Line, "anchor §"+e.Anchor+" matches "+what)
		}
		if e.Rev != rev {
			d.problem("B016", it.Line, "entry was written against rev "+e.Rev+", the body is now "+rev)
		}
	}
}

func hasShallowHeading(body string) bool {
	var fence fenceState
	for _, l := range strings.Split(body, "\n") {
		if fence.step(l) || fence.open {
			continue
		}
		if m := atxRe.FindStringSubmatch(l); m != nil && len(m[1]) < 4 {
			return true
		}
	}
	return false
}

func (d *Document) problem(rule string, line int, msg string) {
	d.problems = append(d.problems, NewProblem(rule, line, msg))
}

// Problems returns what Parse found, in file order of discovery.
func (d *Document) Problems() []Problem { return d.problems }

// HasErrors reports whether any problem has Error severity.
//
// Only structural errors count (rules marked Blocks, spec 6.17): the file
// cannot be signed or merged safely. Other errors never block.
func (d *Document) HasErrors() bool {
	for _, p := range d.problems {
		if p.Blocks {
			return true
		}
	}
	return false
}

// KalkaID returns the blueprint identity, "" for a local blueprint.
func (d *Document) KalkaID() string { v, _ := d.fm.Get(name.FrontmatterID); return strings.ToLower(v) }

// Owner returns the owner email, "" if absent.
func (d *Document) Owner() string { v, _ := d.fm.Get(name.FrontmatterOwner); return v }

// EOL is the line ending used for lines kalka writes.
func (d *Document) EOL() string { return d.eol }

// Frontmatter returns the frontmatter block, nil if absent.
func (d *Document) Frontmatter() *Frontmatter { return d.fm }

// Headings returns the body headings in document order.
func (d *Document) Headings() []Heading { return d.headings }

// Title is the text of the first level-1 heading, "" if none.
func (d *Document) Title() string {
	for _, h := range d.headings {
		if h.Level == 1 {
			return h.Text
		}
	}
	return ""
}

// HasSection reports whether slug names a body heading.
func (d *Document) HasSection(slug string) bool {
	for _, h := range d.headings {
		if h.Slug == slug {
			return true
		}
	}
	return false
}

// Items returns the review log entries in file order.
func (d *Document) Items() []*Item { return d.items }

// Lead returns the raw lines between the log heading and the first entry.
func (d *Document) Lead() []string { return d.lead }

// Body is the normalized body (spec 6.3, 6.6): everything after the
// frontmatter except the review log. Text before and after the log is
// joined by one blank line.
func (d *Document) Body() string {
	pre := Normalize(strings.Join(d.pre, ""))
	tail := Normalize(strings.Join(d.tail, ""))
	switch {
	case tail == "":
		return pre
	case pre == "":
		return tail
	}
	return pre + "\n\n" + tail
}

// Rev is the short hash of the normalized body (spec 6.5).
func (d *Document) Rev() string {
	sum := sha256.Sum256([]byte(d.Body()))
	return hex.EncodeToString(sum[:])[:8]
}

// Plan is the normalized frontmatter and body: what only the owner may
// change. Two documents have the same plan when their Plan strings match.
func (d *Document) Plan() string {
	return Normalize(d.fm.Inner()) + "\n\x00\n" + d.Body()
}

// SetFrontmatter sets one top-level frontmatter key, creating the block
// if the file has none.
func (d *Document) SetFrontmatter(key, value string) {
	if d.fm == nil {
		d.fm = newFrontmatter(d.eol)
	} else {
		d.fm = &Frontmatter{Lines: append([]string(nil), d.fm.Lines...)}
	}
	d.fm.Set(key, value, d.eol)
	d.modified = true
}

// DeleteFrontmatter removes one top-level frontmatter key.
func (d *Document) DeleteFrontmatter(key string) {
	if d.fm == nil || d.fm.find(key) < 0 {
		return
	}
	d.fm = &Frontmatter{Lines: append([]string(nil), d.fm.Lines...)}
	d.fm.Delete(key)
	d.modified = true
}

// ErrOpenBlock means the body ends inside an unclosed code fence or HTML
// block, so a review log added after it would not be read as a heading.
var ErrOpenBlock = errors.New("the body ends inside an unclosed code fence or HTML block; close it before adding entries")

// logAppendable reports whether a review log heading written after pre is
// read as a top-level heading by a CommonMark parser.
func logAppendable(pre []string) bool {
	lf, _ := lfText(trimBlankLines(pre))
	src := []byte(lf + "\n## " + LogTitle + "\n")
	root := goldmark.DefaultParser().Parse(text.NewReader(src))
	for n := root.FirstChild(); n != nil; n = n.NextSibling() {
		if h, ok := n.(*ast.Heading); ok && h.Level == 2 && strings.EqualFold(strings.TrimSpace(inlineText(h, src)), LogTitle) {
			return true
		}
	}
	return false
}

// AddEntry appends an entry to the log, creating the log if absent.
func (d *Document) AddEntry(e *Entry) error {
	if !d.hasLog && len(d.items) == 0 && !logAppendable(d.pre) {
		return ErrOpenBlock
	}
	d.items = append(d.items, &Item{Entry: e})
	d.modified = true
	return nil
}

// Modified reports whether Bytes will differ from the parsed input.
func (d *Document) Modified() bool { return d.modified }

// Bytes renders the document. An unmodified document returns its input.
func (d *Document) Bytes() []byte {
	if !d.modified {
		return []byte(d.raw)
	}
	var b crSafeBuilder
	if d.fm != nil {
		fm := strings.Join(d.fm.Lines, "")
		if len(d.pre) > 0 || len(d.items) > 0 || d.hasLog {
			fm = ensureEOL(fm, d.eol)
		}
		b.write(fm)
	}
	if !d.hasLog && len(d.items) == 0 && len(d.lead) == 0 {
		b.write(strings.Join(d.pre, ""))
		return []byte(b.String())
	}
	if d.hasLog {
		b.write(strings.Join(d.pre, ""))
	} else if pre := trimTrailingBlank(d.pre); len(pre) > 0 {
		b.write(ensureEOL(strings.Join(pre, ""), d.eol))
		b.write(d.eol)
	}
	head := d.logHead
	if head == "" {
		head = "## " + LogTitle
	}
	b.write(ensureEOL(head, d.eol))
	var blocks []string
	if lead := trimBlankLines(d.lead); len(lead) > 0 {
		blocks = append(blocks, ensureEOL(strings.Join(lead, ""), d.eol))
	}
	for _, it := range d.items {
		e := it.Entry
		blk := e.Header() + d.eol
		if e.Body != "" {
			blk += withEOL(e.Body, d.eol) + d.eol
		}
		blocks = append(blocks, blk)
		if tr := trimBlankLines(it.Trailing); len(tr) > 0 {
			blocks = append(blocks, ensureEOL(strings.Join(tr, ""), d.eol))
		}
	}
	if n := len(blocks); n > 0 && len(d.tail) > 0 {
		// Sections after the log must not end up inside an open fence.
		if open, f := leavesFenceOpen(splitLines(blocks[n-1])); open {
			blocks[n-1] += f.closer() + d.eol
		}
	}
	for _, blk := range blocks {
		b.write(d.eol)
		b.write(blk)
	}
	if len(d.tail) > 0 {
		b.write(d.eol)
		b.write(strings.Join(d.tail, ""))
	}
	return []byte(b.String())
}

// crSafeBuilder joins raw and generated text. Raw text may end in a lone
// CR line ending; text starting with LF written right after it would merge
// with it into one CRLF and lose a line break, so a line ending is
// inserted first.
type crSafeBuilder struct{ strings.Builder }

func (b *crSafeBuilder) write(s string) {
	if s == "" {
		return
	}
	if cur := b.String(); strings.HasSuffix(cur, "\r") && s[0] == '\n' {
		b.WriteString("\n")
	}
	b.WriteString(s)
}

// Compose builds a new document with plan's frontmatter and body, the given
// log lead and items, and line ending eol. Raw lines taken from plan are
// converted to eol when plan uses a different ending. The result is parsed
// again from its bytes, so its line numbers and problems are current.
func Compose(plan *Document, eol string, lead []string, items []*Item) (*Document, error) {
	conv := func(lines []string) []string {
		if plan.eol == eol {
			return lines
		}
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = withEOL(l, eol)
		}
		return out
	}
	d := &Document{
		eol:      eol,
		pre:      conv(plan.pre),
		hasLog:   plan.hasLog,
		logHead:  withEOL(plan.logHead, eol),
		lead:     toEOL(lead, eol),
		tail:     conv(plan.tail),
		headings: plan.headings,
		modified: true,
	}
	if plan.fm != nil {
		d.fm = &Frontmatter{Lines: conv(plan.fm.Lines)}
	}
	for _, it := range items {
		d.items = append(d.items, &Item{Entry: it.Entry, Trailing: toEOL(it.Trailing, eol)})
	}
	return Parse(d.Bytes())
}

// toEOL converts log lines, which kalka re-renders, to eol.
func toEOL(lines []string, eol string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = withEOL(l, eol)
	}
	return out
}

// Reparse returns the document parsed again from its rendered bytes.
func (d *Document) Reparse() (*Document, error) { return Parse(d.Bytes()) }
