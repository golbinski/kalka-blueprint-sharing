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
	// A domain name is a path in the domain tree: segments joined by dots
	// (spec 6.12).
	domainNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(?:\.[a-z0-9][a-z0-9-]*)*$`)
	domainRefRe  = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*)/([a-z0-9][a-z0-9-]*(?:\.[a-z0-9][a-z0-9-]*)*)@v([1-9][0-9]{0,17})$`)
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
	if v := d.get(KeyDomain); domainNameRe.MatchString(v) && v != ParentRef {
		return v
	}
	return ""
}

// ValidDomainName reports a domain name: dotted segments, not "parent".
func ValidDomainName(s string) bool { return domainNameRe.MatchString(s) && s != ParentRef }

// DomainParent is the parent version a domain node pins with
// kalka_parent: <team>/<domain>@v<N> (spec 6.12).
func (d *Document) DomainParent() (DomainRef, bool) {
	if !d.IsDomain() {
		return DomainRef{}, false
	}
	return ParseDomainRef(d.get(KeyParent))
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

// DomainRef is the `kalka_domain:` reference of a document that is not
// itself a domain document: the first when it names several.
func (d *Document) DomainRef() (DomainRef, bool) {
	refs := d.DomainRefsNamed()
	if len(refs) == 0 {
		return DomainRef{}, false
	}
	return refs[0], true
}

// DomainRefsNamed are the domain versions `kalka_domain:` names: one, or
// a plan node's several, written as a list [a, b] (spec 6.12). Malformed
// items are left out.
func (d *Document) DomainRefsNamed() []DomainRef {
	if d.IsDomain() {
		return nil
	}
	var out []DomainRef
	for _, s := range DomainList(d.get(KeyDomain)) {
		if r, ok := ParseDomainRef(s); ok {
			out = append(out, r)
		}
	}
	return out
}

// DomainList splits a kalka_domain value: one item, or [a, b].
func DomainList(v string) []string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
		var out []string
		for _, s := range strings.Split(v[1:len(v)-1], ",") {
			if s = strings.Trim(strings.TrimSpace(s), `"'`); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	if v == "" {
		return nil
	}
	return []string{v}
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
	v := d.get(KeyDomain)
	switch {
	case v == ParentRef:
		out = append(out, NewProblem("F012", d.fm.KeyLine(KeyDomain), "parent is a reserved name (parent#<id> names the parent plan node); choose another domain name"))
	case !domainNameRe.MatchString(v):
		out = append(out, NewProblem("F010", d.fm.KeyLine(KeyDomain), fmt.Sprintf("a domain document needs `kalka_domain:` with a name of lower-case letters, digits and dashes, a subdomain's segments joined by dots, such as library.reservations (have %q)", v)))
	}
	if raw := d.get(KeyParent); raw != "" {
		if pr, ok := ParseDomainRef(raw); !ok {
			out = append(out, NewProblem("F013", d.fm.KeyLine(KeyParent), fmt.Sprintf("a domain node names its parent as <team>/<domain>@v<N>, such as team-lending/library@v2 (have %q)", raw)))
		} else if domainNameRe.MatchString(v) && !strings.HasPrefix(v, pr.Domain+".") {
			out = append(out, NewProblem("D001", d.fm.KeyLine(KeyDomain), fmt.Sprintf("a subdomain's name starts with its parent's: %s is not under %s; name it %s.<segment>", v, pr.Domain, pr.Domain)))
		}
	}
	if v := d.get(KeyVersion); v != "" && d.DomainVersion() == 0 {
		out = append(out, NewProblem("F011", d.fm.KeyLine(KeyVersion), fmt.Sprintf("`kalka_version:` must be a positive number (have %q)", v)))
	}
	return out
}
