package blueprint

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/golbinski/kalka/internal/name"
)

// Frontmatter keys of domain documents (spec 6.2, 6.12).
const (
	KeyKind    = name.Product + "_kind"
	KeyDomain  = name.Product + "_domain"
	KeyVersion = name.Product + "_version"

	KindDomain = "domain"
)

var (
	domainNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	domainRefRe  = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*)/([a-z0-9][a-z0-9-]*)@v([1-9][0-9]{0,17})$`)
	snapshotRe   = regexp.MustCompile(`(?i)@v[1-9][0-9]*\.md$`)
)

// IsDomain reports `kalka_kind: domain`.
func (d *Document) IsDomain() bool { return d.get(KeyKind) == KindDomain }

// DomainName is a domain document's `kalka_domain:`, or "" when the document is
// not a domain document or the name is not valid.
func (d *Document) DomainName() string {
	if !d.IsDomain() {
		return ""
	}
	if v := d.get(KeyDomain); domainNameRe.MatchString(v) {
		return v
	}
	return ""
}

// DomainVersion is a domain document's `kalka_version:`, or 0.
func (d *Document) DomainVersion() int64 {
	v, err := strconv.ParseInt(d.get(KeyVersion), 10, 64)
	if err != nil || v < 1 {
		return 0
	}
	return v
}

// DomainRef is a reference to one version of a team's domain (spec 6.12).
type DomainRef struct {
	Team    string
	Domain  string
	Version int64
}

func (r DomainRef) String() string { return fmt.Sprintf("%s/%s@v%d", r.Team, r.Domain, r.Version) }

// ParseDomainRef parses `<team>/<domain>@v<N>`.
func ParseDomainRef(s string) (DomainRef, bool) {
	m := domainRefRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return DomainRef{}, false
	}
	v, err := strconv.ParseInt(m[3], 10, 64)
	if err != nil {
		return DomainRef{}, false
	}
	return DomainRef{Team: m[1], Domain: m[2], Version: v}, true
}

// DomainRef is the `kalka_domain:` reference of a document that is not itself a
// domain document.
func (d *Document) DomainRef() (DomainRef, bool) {
	if d.IsDomain() {
		return DomainRef{}, false
	}
	return ParseDomainRef(d.get(KeyDomain))
}

// IsSnapshotName reports a domain snapshot's file name, `*@v<N>.md`
// (spec 6.12, 14).
func IsSnapshotName(name string) bool { return snapshotRe.MatchString(name) }

// SnapshotPath is where version v of the live domain document at path is
// kept: `<name>@v<N>.md` beside it.
func SnapshotPath(live string, v int64) string {
	base := strings.TrimSuffix(live, filepath.Ext(live))
	return fmt.Sprintf("%s@v%d.md", base, v)
}

// domainProblems checks the frontmatter of a domain document.
func (d *Document) domainProblems() []Problem {
	if !d.IsDomain() {
		return nil
	}
	var out []Problem
	if v := d.get(KeyDomain); !domainNameRe.MatchString(v) {
		out = append(out, NewProblem("F010", d.fm.KeyLine(KeyDomain), fmt.Sprintf("a domain document needs `kalka_domain:` with a name of lower-case letters, digits and dashes (have %q)", v)))
	}
	if v := d.get(KeyVersion); v != "" && d.DomainVersion() == 0 {
		out = append(out, NewProblem("F011", d.fm.KeyLine(KeyVersion), fmt.Sprintf("`kalka_version:` must be a positive number (have %q)", v)))
	}
	return out
}
