// Package update finds, downloads, verifies and installs new releases of
// the desktop app from GitHub (spec 24). It knows nothing of the daemon:
// internal/daemon decides when to call it.
package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a semantic version; Pre is the part after the hyphen.
type Version struct {
	Major, Minor, Patch int
	Pre                 string
}

// Parse reads "1.2.3", "v1.2.3" or "1.2.3-rc1". Build metadata ("+…") is
// dropped.
func Parse(s string) (Version, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, _, _ = strings.Cut(s, "+")
	num, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(num, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("version %q: want major.minor.patch", s)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return Version{}, fmt.Errorf("version %q: %q is not a number", s, p)
		}
		n[i] = v
	}
	return Version{n[0], n[1], n[2], pre}, nil
}

func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

// Compare orders versions: a pre-release is below its release, and
// pre-release identifiers compare as semver says (numbers by value, below
// words).
func (v Version) Compare(o Version) int {
	for _, p := range [][2]int{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if p[0] != p[1] {
			return cmp(p[0], p[1])
		}
	}
	switch {
	case v.Pre == o.Pre:
		return 0
	case v.Pre == "":
		return 1
	case o.Pre == "":
		return -1
	}
	a, b := strings.Split(v.Pre, "."), strings.Split(o.Pre, ".")
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			continue
		}
		x, errX := strconv.Atoi(a[i])
		y, errY := strconv.Atoi(b[i])
		switch {
		case errX == nil && errY == nil:
			return cmp(x, y)
		case errX == nil:
			return -1
		case errY == nil:
			return 1
		}
		return cmp(strings.Compare(a[i], b[i]), 0)
	}
	return cmp(len(a), len(b))
}

func cmp(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Compatible is the caret policy: from 1.0.0 on, the same major version;
// below it, the same minor (a 0.x minor may break, as semver allows).
func Compatible(cur, cand Version) bool {
	if cur.Major != cand.Major {
		return false
	}
	return cur.Major > 0 || cur.Minor == cand.Minor
}
