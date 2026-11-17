package blueprint

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Section is a piece of the body for display: the
// UI renders each section's Markdown on its own and places the entries
// anchored to any of its Slugs next to it.
type Section struct {
	// Heading is nil for text before the first heading.
	Heading *Heading `json:"heading,omitempty"`
	// Slugs are the heading's slug and those of headings nested inside the
	// section (in block quotes or lists), which do not start sections.
	Slugs []string `json:"slugs"`
	// Markdown is the section source, heading included, with LF endings.
	Markdown string `json:"markdown"`
	Line     int    `json:"line"`
}

// Sections splits the body at its top-level headings. The review log is not
// part of any section.
func (d *Document) Sections() []Section {
	var out []Section
	split := func(lines []string, first int) {
		if len(lines) == 0 {
			return
		}
		last := first + len(lines) // one past the part's last line
		var cur *Section
		flush := func(upto int) {
			if cur == nil {
				return
			}
			from := cur.Line - first
			cur.Markdown = toLF(strings.Join(lines[from:upto-first], ""))
			if cur.Heading != nil || strings.TrimSpace(cur.Markdown) != "" {
				out = append(out, *cur)
			}
			cur = nil
		}
		cur = &Section{Line: first, Slugs: []string{}}
		for i := range d.headings {
			h := &d.headings[i]
			if h.Line < first || h.Line >= last {
				continue
			}
			if h.Nested {
				cur.Slugs = append(cur.Slugs, h.Slug)
				continue
			}
			flush(h.Line)
			cur = &Section{Heading: h, Slugs: []string{h.Slug}, Line: h.Line}
		}
		flush(last)
	}
	split(d.pre, d.preLine)
	split(d.tail, d.tailLine)
	return out
}

// ErrNoAnchor means an entry names a heading the body does not have.
var ErrNoAnchor = errors.New("no heading with that slug")

// Append adds an entry written by the local user (`kalka comment`, the web
// UI): the anchor must name a heading, re must name an entry, and a quote
// becomes a leading block quote (spec 6.5). The caller writes d.Bytes().
func Append(d *Document, kind, author string, at time.Time, anchor, re, quote, body string, sign Signer) (*Entry, error) {
	if kind == KindRelease {
		return nil, errors.New("release entries are created by releasing the document, not written by hand")
	}
	anchor = strings.TrimPrefix(strings.TrimSpace(anchor), "§")
	if anchor != "" {
		slug, node, hasNode := strings.Cut(anchor, "#")
		if !d.HasSection(slug) {
			var s []string
			for _, h := range d.headings {
				s = append(s, h.Slug)
			}
			return nil, fmt.Errorf("%w %q; headings are: %s", ErrNoAnchor, slug, strings.Join(s, ", "))
		}
		if hasNode && !d.HasNode(slug, node) {
			return nil, fmt.Errorf("%w: no trace node %q under §%s", ErrNoAnchor, node, slug)
		}
	}
	text := body
	if q := strings.TrimSpace(toLF(quote)); q != "" {
		var b strings.Builder
		for _, l := range strings.Split(q, "\n") {
			b.WriteString(strings.TrimRight("> "+l, " ") + "\n")
		}
		text = b.String() + "\n" + body
	}
	if re = strings.TrimSpace(re); re != "" {
		found := false
		for _, it := range d.items {
			if it.Entry != nil && it.Entry.ID == re {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("no entry with id %q", re)
		}
		text = "re: " + re + "\n" + text
	}
	e, err := NewEntry(kind, author, at, anchor, d.Rev(), text)
	if err != nil {
		return nil, err
	}
	if sign.Ok() && d.KalkaID() != "" {
		e.Sign(d.KalkaID(), sign)
	}
	if err := d.AddEntry(e); err != nil {
		return nil, err
	}
	if n := len(d.Bytes()); n > MaxFile {
		return nil, fmt.Errorf("the file would grow to %d bytes, over the %d byte limit", n, MaxFile)
	}
	return e, nil
}
