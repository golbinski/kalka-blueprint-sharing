package blueprint

import (
	"strings"
)

// toLF converts CRLF and lone CR to LF (spec 6.6 step 2).
func toLF(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// withEOL converts any line endings in s to eol ("\n" or "\r\n").
func withEOL(s, eol string) string {
	s = toLF(s)
	if eol == "\r\n" {
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

// Normalize applies spec 6.6 to one unit (body, frontmatter or entry body):
// BOM stripped, line endings to LF, trailing spaces and tabs stripped from
// every line, leading and trailing blank lines removed. Used for comparison
// and hashing only.
func Normalize(s string) string {
	s = strings.TrimPrefix(s, "\uFEFF")
	s = toLF(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	start, end := 0, len(lines)
	for start < end && lines[start] == "" {
		start++
	}
	for end > start && lines[end-1] == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}

// dominantEOL reports "\r\n" when CRLF endings outnumber LF-only endings.
func dominantEOL(s string) string {
	crlf := strings.Count(s, "\r\n")
	lf := strings.Count(s, "\n") - crlf
	if crlf > lf {
		return "\r\n"
	}
	return "\n"
}

// splitLines splits s into lines that keep their terminators ("\n", "\r\n"
// or a lone "\r"). The last line has no terminator if s does not end in one.
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n':
			out = append(out, s[start:i+1])
			start = i + 1
		case '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				out = append(out, s[start:i+2])
				i++
			} else {
				out = append(out, s[start:i+1])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// content returns a line without its terminator.
func content(line string) string {
	return strings.TrimRight(line, "\r\n")
}

// trimBlankLines removes leading and trailing lines that are blank after
// trimming whitespace, keeping the bytes of the remaining lines.
func trimBlankLines(lines []string) []string {
	start, end := 0, len(lines)
	for start < end && isBlank(lines[start]) {
		start++
	}
	for end > start && isBlank(lines[end-1]) {
		end--
	}
	return lines[start:end]
}

// trimTrailingBlank removes trailing blank lines only.
func trimTrailingBlank(lines []string) []string {
	end := len(lines)
	for end > 0 && isBlank(lines[end-1]) {
		end--
	}
	return lines[:end]
}

// isBlank reports whether a line holds only spaces and tabs (spec 6.6).
func isBlank(line string) bool { return strings.Trim(content(line), " \t") == "" }

// IsBlank is isBlank for other packages.
func IsBlank(line string) bool { return isBlank(line) }

// ensureEOL makes sure the last line of s ends with a line terminator.
func ensureEOL(s, eol string) string {
	if s == "" || strings.HasSuffix(s, "\n") || strings.HasSuffix(s, "\r") {
		return s
	}
	return s + eol
}
