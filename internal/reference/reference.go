// Package reference embeds BLUEPRINT-REFERENCE.md (spec 6.13): what a
// blueprint useful to agents usually contains. It is guidance written to
// be used unchanged as an agent skill file, never a requirement.
package reference

import (
	_ "embed"
	"strings"
)

// Version is the reference version documents opt into with
// `kalka_reference: <Version>` in their frontmatter.
const Version = "kalka-blueprint@v1"

// FileName is the name the reference is exported under.
const FileName = "BLUEPRINT-REFERENCE.md"

//go:embed BLUEPRINT-REFERENCE.md
var text string

// Bytes returns the embedded reference.
func Bytes() []byte { return []byte(text) }

// declared is the version the embedded file states in its frontmatter;
// a test keeps it equal to Version.
func declared() string {
	for _, line := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(line, "kalka_reference: "); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
