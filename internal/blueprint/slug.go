package blueprint

import (
	"strconv"
	"strings"
	"unicode"
)

// Slugger assigns GitHub-style heading anchors in document order (spec 6.5).
//
// The rule follows github-slugger: lower-case; keep letters, marks, digits,
// connector punctuation (underscore), spaces and hyphens; each space becomes
// a hyphen; a repeated slug gets -1, -2, ... The spec's short description
// omits underscore and marks; GitHub keeps them, and matching GitHub means
// anchors also work as links when the file is viewed there.
type Slugger struct {
	seen map[string]int
}

// NewSlugger returns a slugger with no headings seen.
func NewSlugger() *Slugger { return &Slugger{seen: map[string]int{}} }

// Slug returns the unique slug for heading text.
func (s *Slugger) Slug(text string) string {
	base := BaseSlug(text)
	slug := base
	for {
		if _, taken := s.seen[slug]; !taken {
			break
		}
		s.seen[base]++
		slug = base + "-" + strconv.Itoa(s.seen[base])
	}
	s.seen[slug] = 0
	return slug
}

// BaseSlug is the slug of text before de-duplication.
func BaseSlug(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-', r == '_':
			b.WriteRune(r)
		case unicode.IsLetter(r), unicode.IsDigit(r), unicode.Is(unicode.M, r), unicode.Is(unicode.Pc, r):
			b.WriteRune(r)
		}
	}
	return b.String()
}
