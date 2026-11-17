// Package addr holds the one canonical form of an email address, used
// wherever kalka compares or writes one: in team log operations (spec
// 19.1), in entry headers and frontmatter (6.2, 6.5).
//
// The form is narrow on purpose: ASCII letters are lowered and every
// other byte is kept. Unicode case mapping differs between libraries and
// versions (Go's ToLower and EqualFold disagree on ſ and K, for one), and
// a replay rule cannot depend on that.
package addr

// Canonical returns s with ASCII letters lowered.
func Canonical(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; 'A' <= c && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if 'A' <= b[j] && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

// IsCanonical reports whether s is already in canonical form.
func IsCanonical(s string) bool { return Canonical(s) == s }

// Same reports whether a and b name one address.
func Same(a, b string) bool { return Canonical(a) == Canonical(b) }
