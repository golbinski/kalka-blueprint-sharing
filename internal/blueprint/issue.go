package blueprint

// The issue a blueprint addresses, archiving, and the exported entry
// (spec 6.2, 6.5, 10.5). Only two things about a tracker are ever in the
// file: the kalka_issue key the author writes, and the exported entries
// an export by the user writes. Everything a plugin tells about an issue
// (title, status, parents) stays on the reader's machine.

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golbinski/kalka/internal/name"
)

// Frontmatter keys.
const (
	KeyIssue    = name.Product + "_issue"
	KeyArchived = name.Product + "_archived"
)

// KindExported records an export of a released version (spec 10.5).
const KindExported = "exported"

// IssueSchemes are the issue schemes of the plugins shipped with kalka.
// Another scheme is information in kalka check (F021), never an error:
// any plugin that declares the issue capabilities serves it.
var IssueSchemes = []string{"jira"}

// maxIssue caps a kalka_issue value.
const maxIssue = 200

// IssueRef is a `kalka_issue:` value, `<scheme>:<payload>`.
type IssueRef struct {
	Scheme  string `json:"scheme"`
	Payload string `json:"payload"`
}

func (r IssueRef) String() string { return r.Scheme + ":" + r.Payload }

// ParseIssue reads `<scheme>:<payload>`: a plugin scheme (10.1) and a
// payload of up to 200 characters with no spaces.
func ParseIssue(s string) (IssueRef, bool) {
	s = strings.TrimSpace(s)
	if len(s) > maxIssue {
		return IssueRef{}, false
	}
	scheme, payload, ok := PluginRef(s)
	if !ok || payload == "" || strings.ContainsAny(payload, "#\"'`") {
		return IssueRef{}, false
	}
	return IssueRef{Scheme: scheme, Payload: payload}, true
}

// Issue is the issue the blueprint addresses, when kalka_issue is valid.
func (d *Document) Issue() (IssueRef, bool) {
	v := d.get(KeyIssue)
	if v == "" {
		return IssueRef{}, false
	}
	return ParseIssue(v)
}

// Archived reports a valid `kalka_archived:` and when it was archived.
func (d *Document) Archived() (time.Time, bool) {
	v := d.get(KeyArchived)
	if v == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Archive writes `kalka_archived:` with the time; Unarchive removes it.
func Archive(d *Document, at time.Time) error {
	if _, ok := d.Archived(); ok {
		return fmt.Errorf("this blueprint is already archived")
	}
	d.SetFrontmatter(KeyArchived, at.UTC().Format(TimeLayout))
	return nil
}

// Unarchive removes `kalka_archived:`, also a malformed one.
func Unarchive(d *Document) error {
	if d.get(KeyArchived) == "" {
		return fmt.Errorf("this blueprint is not archived")
	}
	d.DeleteFrontmatter(KeyArchived)
	return nil
}

// issueProblems checks kalka_issue and kalka_archived. Neither needs a
// plugin, so kalka check stays deterministic (spec 6.17).
func (d *Document) issueProblems() []Problem {
	var out []Problem
	if v := d.get(KeyIssue); v != "" {
		if r, ok := ParseIssue(v); !ok {
			out = append(out, NewProblem("F020", d.fm.KeyLine(KeyIssue), fmt.Sprintf("`kalka_issue:` must be `<scheme>:<issue>`, such as jira:DEV-123 (have %q)", v)))
		} else if !knownIssueScheme(r.Scheme) {
			out = append(out, NewProblem("F021", d.fm.KeyLine(KeyIssue), fmt.Sprintf("no plugin shipped with kalka handles %s: issues (it knows %s:); one you install may", r.Scheme, strings.Join(IssueSchemes, ":, "))))
		}
	}
	if v := d.get(KeyArchived); v != "" {
		if _, ok := d.Archived(); !ok {
			out = append(out, NewProblem("F022", d.fm.KeyLine(KeyArchived), fmt.Sprintf("`kalka_archived:` must be a UTC time such as 2026-11-20T10:00:00Z (have %q); kalka writes it when you archive", v)))
		}
	}
	return out
}

func knownIssueScheme(s string) bool {
	for _, k := range IssueSchemes {
		if s == k {
			return true
		}
	}
	return false
}

// Exported is an exported entry's body (spec 10.5):
//
//	exported: jira:DEV-123 · v3
//	url: https://example.atlassian.net/browse/DEV-123
//	attachment: 10046
//	comment: 20311
type Exported struct {
	Issue        string `json:"issue"`
	Version      int64  `json:"version"`
	URL          string `json:"url,omitempty"`
	AttachmentID string `json:"attachment_id,omitempty"`
	CommentID    string `json:"comment_id,omitempty"`
}

var (
	exportedHeadRe = regexp.MustCompile(`^exported: (\S+) · v([1-9][0-9]{0,17})$`)
	exportIDRe     = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)
)

// Body is the entry text of an exported entry.
func (x Exported) Body() string {
	var b strings.Builder
	fmt.Fprintf(&b, "exported: %s · v%d\n", x.Issue, x.Version)
	if x.URL != "" {
		fmt.Fprintf(&b, "url: %s\n", x.URL)
	}
	if x.AttachmentID != "" {
		fmt.Fprintf(&b, "attachment: %s\n", x.AttachmentID)
	}
	if x.CommentID != "" {
		fmt.Fprintf(&b, "comment: %s\n", x.CommentID)
	}
	return b.String()
}

// Exported reads an exported entry. Lines it does not know are ignored,
// so a later kalka can add some; a malformed known line makes the entry
// generic.
func (e *Entry) Exported() (Exported, bool) {
	if e == nil || e.Kind != KindExported {
		return Exported{}, false
	}
	lines := strings.Split(strings.TrimSpace(displayText(e)), "\n")
	m := exportedHeadRe.FindStringSubmatch(strings.TrimSpace(lines[0]))
	if m == nil {
		return Exported{}, false
	}
	if _, ok := ParseIssue(m[1]); !ok {
		return Exported{}, false
	}
	v, _ := strconv.ParseInt(m[2], 10, 64)
	x := Exported{Issue: m[1], Version: v}
	for _, l := range lines[1:] {
		k, val, ok := strings.Cut(strings.TrimSpace(l), ": ")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch k {
		case "url":
			if u, err := url.Parse(val); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return Exported{}, false
			}
			x.URL = val
		case "attachment":
			if !exportIDRe.MatchString(val) {
				return Exported{}, false
			}
			x.AttachmentID = val
		case "comment":
			if !exportIDRe.MatchString(val) {
				return Exported{}, false
			}
			x.CommentID = val
		}
	}
	return x, true
}

// LastExport is the newest exported entry for issue, by timestamp then
// id (the order entries are kept in, 20.4), among those ok accepts.
func LastExport(d *Document, issue string, ok func(*Entry) bool) (Exported, *Entry, bool) {
	var best *Entry
	var bx Exported
	for _, it := range d.Items() {
		e := it.Entry
		x, isX := e.Exported()
		if !isX || x.Issue != issue || (ok != nil && !ok(e)) {
			continue
		}
		if best == nil || e.Timestamp > best.Timestamp || (e.Timestamp == best.Timestamp && e.ID > best.ID) {
			best, bx = e, x
		}
	}
	return bx, best, best != nil
}
