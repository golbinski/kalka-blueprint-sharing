// Package userpath adds a folder to the user's PATH and takes it out
// again (spec 23.1). On Windows that is the Path value under
// HKCU\Environment; the installer calls it through `kalka path`, so the
// whole value is handled in Go, with no length limit.
package userpath

import (
	"errors"
	"path"
	"strings"
)

// ErrUnsupported is returned where kalka does not manage the PATH.
var ErrUnsupported = errors.New("kalka changes the PATH only on Windows; add the folder in your shell's profile")

// Add appends dir to the user's PATH unless it is there already, in any
// spelling. It reports whether the PATH changed.
func Add(dir string) (bool, error) { return add(dir) }

// Remove takes every entry that names dir out of the user's PATH. It
// reports whether the PATH changed.
func Remove(dir string) (bool, error) { return remove(dir) }

// appendEntry is value with dir appended, or value unchanged (false) when
// an entry already names dir.
func appendEntry(value, dir string, expand func(string) string) (string, bool) {
	want := normalize(dir, expand)
	for _, e := range strings.Split(value, ";") {
		if same(e, want, expand) {
			return value, false
		}
	}
	switch {
	case value == "":
		return dir, true
	case strings.HasSuffix(value, ";"):
		return value + dir, true
	default:
		return value + ";" + dir, true
	}
}

// dropEntry is value without the entries that name dir, or value
// unchanged (false) when none does. Every other entry is kept as written,
// empty ones included, except the separator a removed entry leaves at
// either end.
func dropEntry(value, dir string, expand func(string) string) (string, bool) {
	want := normalize(dir, expand)
	var kept []string
	dropped := false
	for _, e := range strings.Split(value, ";") {
		if same(e, want, expand) {
			dropped = true
			continue
		}
		kept = append(kept, e)
	}
	if !dropped {
		return value, false
	}
	out := strings.Join(kept, ";")
	// A removed first or last entry leaves its separator behind.
	if !strings.HasPrefix(value, ";") {
		out = strings.TrimLeft(out, ";")
	}
	if !strings.HasSuffix(value, ";") {
		out = strings.TrimRight(out, ";")
	}
	return out, true
}

func same(entry, want string, expand func(string) string) bool {
	e := normalize(entry, expand)
	return e != "" && strings.EqualFold(e, want)
}

// normalize is how an entry is compared: variables expanded, quotes and
// spaces trimmed, either slash, no "." or ".." segments, no trailing
// separator.
func normalize(entry string, expand func(string) string) string {
	e := strings.TrimSpace(entry)
	if expand != nil {
		e = expand(e)
	}
	e = strings.TrimSpace(strings.Trim(strings.TrimSpace(e), `"`))
	if e == "" {
		return ""
	}
	e = path.Clean(strings.ReplaceAll(e, `\`, "/"))
	return strings.TrimRight(e, "/")
}
