package blueprint

import (
	"encoding/json"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/golbinski/kalka/internal/name"
)

// Frontmatter is the YAML block between --- fences at the top of a file.
//
// kalka reads and writes only two top-level keys and does it line by line:
// the block is never parsed into a structure and re-serialized, so every
// other key, comment and blank line keeps its bytes (spec 6.2). Map parses
// a copy, read-only, for plugins.
type Frontmatter struct {
	// Lines are the raw lines, terminators included, from the opening fence
	// to the closing fence inclusive.
	Lines []string
}

var fmKeyRe = regexp.MustCompile(`^([A-Za-z0-9_-]+)[ \t]*:(?:[ \t]+(.*?))?[ \t]*$`)

// splitFrontmatter returns the frontmatter lines (nil if none) and the rest.
// ok is false when an opening fence has no closing fence.
func splitFrontmatter(lines []string) (fm, rest []string, ok bool) {
	if len(lines) == 0 || strings.TrimRight(content(lines[0]), " \t") != "---" {
		return nil, lines, true
	}
	for i := 1; i < len(lines); i++ {
		c := strings.TrimRight(content(lines[i]), " \t")
		if c == "---" || c == "..." {
			return lines[:i+1], lines[i+1:], true
		}
	}
	return nil, lines, false
}

// Inner returns the raw text between the fences.
func (f *Frontmatter) Inner() string {
	if f == nil || len(f.Lines) < 2 {
		return ""
	}
	return strings.Join(f.Lines[1:len(f.Lines)-1], "")
}

// Get returns the value of a top-level key with surrounding quotes and a
// trailing comment removed.
func (f *Frontmatter) Get(key string) (string, bool) {
	if f == nil {
		return "", false
	}
	i := f.find(key)
	if i < 0 {
		return "", false
	}
	m := fmKeyRe.FindStringSubmatch(content(f.Lines[i]))
	return scalar(m[2]), true
}

// Set replaces the line holding key, or inserts it after the opening fence.
// Only that line changes.
func (f *Frontmatter) Set(key, value, eol string) {
	line := key + ": " + value
	if i := f.find(key); i >= 0 {
		f.Lines[i] = line + lineEnding(f.Lines[i], eol)
		return
	}
	// Insert after the last kalka-managed key, or after the opening fence,
	// so kalka's keys stay together at the top in the order they were added.
	at := 1
	for _, k := range managedKeys {
		if i := f.find(k); i >= at {
			at = i + 1
		}
	}
	f.Lines[at-1] = ensureEOL(f.Lines[at-1], eol)
	f.Lines = append(f.Lines[:at], append([]string{line + eol}, f.Lines[at:]...)...)
}

// Delete removes the line holding key, if any.
func (f *Frontmatter) Delete(key string) {
	if i := f.find(key); i >= 0 {
		f.Lines = append(f.Lines[:i], f.Lines[i+1:]...)
	}
}

var managedKeys = []string{name.FrontmatterID, name.FrontmatterOwner}

func (f *Frontmatter) find(key string) int {
	for i := 1; i < len(f.Lines)-1; i++ {
		m := fmKeyRe.FindStringSubmatch(content(f.Lines[i]))
		if m != nil && m[1] == key {
			return i
		}
	}
	return -1
}

func lineEnding(line, fallback string) string {
	switch {
	case strings.HasSuffix(line, "\r\n"):
		return "\r\n"
	case strings.HasSuffix(line, "\n"):
		return "\n"
	case strings.HasSuffix(line, "\r"):
		return "\r"
	}
	return fallback
}

// scalar extracts a plain or quoted YAML scalar from the text after "key:".
func scalar(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
		if end := strings.IndexByte(v[1:], v[0]); end >= 0 {
			return v[1 : end+1]
		}
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// newFrontmatter creates an empty block.
func newFrontmatter(eol string) *Frontmatter {
	return &Frontmatter{Lines: []string{"---" + eol, "---" + eol}}
}

// Map returns the frontmatter as an opaque map for plugins (spec 6.2:
// pass-through keys are handed to plugins as they are). Invalid YAML gives
// an empty map; keys whose values cannot be expressed as JSON are left out.
func (f *Frontmatter) Map() map[string]any {
	out := map[string]any{}
	if f == nil {
		return out
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(f.Inner()), &m); err != nil {
		return out
	}
	for k, v := range m {
		if _, err := json.Marshal(v); err == nil {
			out[k] = v
		}
	}
	return out
}
