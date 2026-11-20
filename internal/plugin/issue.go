package plugin

// Capabilities, issue metadata and export (spec 10.3, 10.5). The core
// never branches on a plugin's name or scheme: it asks a plugin only for
// what its describe answer declares, and gets a normalized answer back.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Capabilities is what a plugin declares it can do.
type Capabilities struct {
	Resolve bool       `json:"resolve,omitempty"`
	Issue   *IssueCaps `json:"issue,omitempty"`
}

// IssueCaps are the capabilities of an issue tracker plugin.
type IssueCaps struct {
	// Meta: the plugin answers issue_meta.
	Meta bool `json:"meta,omitempty"`
	// Hierarchy: results carry a parent chain.
	Hierarchy bool `json:"hierarchy,omitempty"`
	// Fields lists the normalized fields it fills (Fields below).
	Fields []string `json:"fields,omitempty"`
	// Export is what an export can do; nil when it cannot export.
	Export *ExportCaps `json:"export,omitempty"`
}

// ExportCaps says what an export writes to the issue.
type ExportCaps struct {
	Attachment bool `json:"attachment,omitempty"`
	Comment    bool `json:"comment,omitempty"`
}

// Fields are the normalized issue fields a plugin may fill.
var Fields = []string{"status", "type", "assignee", "labels", "sprint"}

// MaxParents caps the parent chain (spec 10.3): nearest first.
const MaxParents = 5

// Status categories, normalized so the core can sort and colour statuses
// without knowing a tracker's own names.
const (
	CategoryTodo       = "todo"
	CategoryInProgress = "in_progress"
	CategoryDone       = "done"
)

// CanMeta reports the issue_meta capability.
func (c Capabilities) CanMeta() bool { return c.Issue != nil && c.Issue.Meta }

// CanHierarchy reports parent chains in issue_meta results.
func (c Capabilities) CanHierarchy() bool { return c.CanMeta() && c.Issue.Hierarchy }

// CanExport reports the export capability.
func (c Capabilities) CanExport() bool {
	return c.Issue != nil && c.Issue.Export != nil && (c.Issue.Export.Attachment || c.Issue.Export.Comment)
}

// clean keeps the known fields only, so the page is never offered a
// feature on the strength of a field name kalka does not know.
func (c Capabilities) clean() Capabilities {
	if c.Issue == nil {
		return c
	}
	is := *c.Issue
	var fields []string
	for _, f := range Fields {
		for _, g := range is.Fields {
			if f == g {
				fields = append(fields, f)
				break
			}
		}
	}
	is.Fields = fields
	if is.Export != nil {
		e := *is.Export
		is.Export = &e
	}
	if !is.Meta {
		is.Hierarchy, is.Fields = false, nil
	}
	c.Issue = &is
	return c
}

// IssueStatus is a status with its normalized category.
type IssueStatus struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

// IssueParent is one level of an issue's parent chain.
type IssueParent struct {
	Issue string `json:"issue"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type,omitempty"`
	URL   string `json:"url,omitempty"`
}

// IssueMeta is a plugin's answer for one issue. It is shown to the
// local user only: never written into a blueprint, never sent to peers.
type IssueMeta struct {
	Issue    string        `json:"issue"`
	OK       bool          `json:"ok"`
	Error    string        `json:"error,omitempty"`
	Title    string        `json:"title,omitempty"`
	URL      string        `json:"url,omitempty"`
	Type     string        `json:"type,omitempty"`
	Status   *IssueStatus  `json:"status,omitempty"`
	Assignee string        `json:"assignee,omitempty"`
	Labels   []string      `json:"labels,omitempty"`
	Sprint   string        `json:"sprint,omitempty"`
	Parents  []IssueParent `json:"parents,omitempty"`
	Updated  string        `json:"updated,omitempty"`
}

// ExportDocument is the released blueprint an export attaches.
type ExportDocument struct {
	Title    string `json:"title"`
	Version  int64  `json:"version"`
	FileName string `json:"file_name"`
	Markdown string `json:"markdown"`
}

// ExportPrevious names what the last export of the same issue wrote, so
// the plugin replaces it instead of adding another (spec 10.3).
type ExportPrevious struct {
	AttachmentID string `json:"attachment_id,omitempty"`
	CommentID    string `json:"comment_id,omitempty"`
}

// ExportRequest is the export op's input.
type ExportRequest struct {
	Issue           string          `json:"issue,omitempty"`
	Document        *ExportDocument `json:"document,omitempty"`
	SummaryMarkdown string          `json:"summary_markdown,omitempty"`
	Previous        *ExportPrevious `json:"previous,omitempty"`
}

// ExportResult is the export op's answer.
type ExportResult struct {
	OK           bool   `json:"ok"`
	Error        string `json:"error,omitempty"`
	URL          string `json:"url,omitempty"`
	AttachmentID string `json:"attachment_id,omitempty"`
	CommentID    string `json:"comment_id,omitempty"`
}

// idRe is what a tracker's attachment or comment id may look like when
// it is written into the review log (spec 6.5, `exported`).
var idRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// ValidID reports an attachment or comment id kalka will record.
func ValidID(s string) bool { return idRe.MatchString(s) }

type descEntry struct {
	size  int64
	mod   time.Time
	desc  *Description
	err   error
	until time.Time
}

// descFailTTL: a plugin that failed to describe itself is asked again
// after this long, or as soon as its file changes.
var descFailTTL = time.Minute

// Capabilities answers describe for a scheme's plugin. The answer is
// kept while the executable is unchanged, so asking is cheap.
func (h *Host) Capabilities(ctx context.Context, scheme string) (*Description, error) {
	path, err := h.Find(scheme)
	if err != nil {
		return nil, err
	}
	return h.describeCached(ctx, path)
}

func (h *Host) describeCached(ctx context.Context, path string) (*Description, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	h.descMu.Lock()
	e, ok := h.descs[path]
	h.descMu.Unlock()
	if ok && e.size == st.Size() && e.mod.Equal(st.ModTime()) && (e.err == nil || time.Now().Before(e.until)) {
		return e.desc, e.err
	}
	d, err := h.Describe(ctx, path)
	if err != nil {
		h.logf("%s: describe: %v", filepath.Base(path), err)
	}
	h.descMu.Lock()
	if h.descs == nil {
		h.descs = map[string]descEntry{}
	}
	h.descs[path] = descEntry{size: st.Size(), mod: st.ModTime(), desc: d, err: err, until: time.Now().Add(descFailTTL)}
	h.descMu.Unlock()
	return d, err
}

// Installed lists the installed plugins by scheme, as Find would pick
// them: <config>/plugins first, then kalka's own folder, then PATH.
func (h *Host) Installed() []string {
	seen := map[string]bool{}
	dirs := []string{h.Dir, h.ExeDir}
	dirs = append(dirs, filepath.SplitList(os.Getenv("PATH"))...)
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			n := e.Name()
			if runtime.GOOS == "windows" {
				ext := strings.ToLower(filepath.Ext(n))
				if ext != ".exe" && ext != ".cmd" {
					continue
				}
				n = strings.TrimSuffix(n, filepath.Ext(n))
			}
			scheme, ok := strings.CutPrefix(n, Prefix)
			if !ok || !schemeRe.MatchString(scheme) || seen[scheme] {
				continue
			}
			if _, err := h.Find(scheme); err == nil {
				seen[scheme] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// errNoCapability is returned when the plugin did not declare what is asked.
var errNoCapability = errors.New("the plugin does not declare this capability")

// IssueMeta asks the scheme's plugin about issues, in one batch. Plugin
// failures come back as an error; per-issue failures as results with ok
// false. Every asked issue gets a result, in order.
func (h *Host) IssueMeta(ctx context.Context, scheme string, issues []string) ([]IssueMeta, error) {
	d, err := h.Capabilities(ctx, scheme)
	if err != nil {
		return nil, err
	}
	if !d.Capabilities.CanMeta() {
		return nil, errNoCapability
	}
	path, err := h.Find(scheme)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Results []IssueMeta `json:"results"`
		// Error fails the whole batch (no credentials, tracker down), so
		// the reader keeps the last answers instead of losing them.
		Error string `json:"error"`
	}
	if err := h.call(ctx, path, Timeout, request{Protocol: Protocol, Op: "issue_meta", Issues: issues}, &resp); err != nil {
		h.logf("%s: %v", filepath.Base(path), err)
		return nil, fmt.Errorf("%s failed: %v", filepath.Base(path), err)
	}
	if resp.Error != "" {
		return nil, errors.New(text(resp.Error, maxText))
	}
	byIssue := map[string]IssueMeta{}
	for _, r := range resp.Results {
		if _, dup := byIssue[r.Issue]; !dup {
			byIssue[r.Issue] = cleanMeta(r, d.Capabilities)
		}
	}
	out := make([]IssueMeta, len(issues))
	for i, is := range issues {
		r, ok := byIssue[is]
		if !ok {
			r = IssueMeta{Issue: is, Error: filepath.Base(path) + " returned no result for this issue"}
		}
		out[i] = r
	}
	return out, nil
}

// Export asks the scheme's plugin to export a released blueprint to an
// issue. It fails unless the plugin declares export and answers ok with
// ids kalka can record.
func (h *Host) Export(ctx context.Context, scheme string, req ExportRequest) (*ExportResult, error) {
	d, err := h.Capabilities(ctx, scheme)
	if err != nil {
		return nil, err
	}
	if !d.Capabilities.CanExport() {
		return nil, errNoCapability
	}
	path, err := h.Find(scheme)
	if err != nil {
		return nil, err
	}
	var res ExportResult
	if err := h.call(ctx, path, ExportTimeout, request{Protocol: Protocol, Op: "export", ExportRequest: &req}, &res); err != nil {
		h.logf("%s: %v", filepath.Base(path), err)
		return nil, fmt.Errorf("%s failed: %v", filepath.Base(path), err)
	}
	if !res.OK {
		msg := text(res.Error, maxText)
		if msg == "" {
			msg = "the export failed"
		}
		return nil, errors.New(msg)
	}
	res.URL = httpURL(res.URL)
	if res.AttachmentID != "" && !ValidID(res.AttachmentID) {
		return nil, fmt.Errorf("%s returned an attachment id kalka cannot record", filepath.Base(path))
	}
	if res.CommentID != "" && !ValidID(res.CommentID) {
		return nil, fmt.Errorf("%s returned a comment id kalka cannot record", filepath.Base(path))
	}
	return &res, nil
}

// cleanMeta caps text, drops URLs that are not http(s), keeps only the
// fields the plugin declared, and normalizes the status category.
func cleanMeta(r IssueMeta, c Capabilities) IssueMeta {
	r.Issue = text(r.Issue, 200)
	r.Error = text(r.Error, maxText)
	r.Title = text(r.Title, maxText)
	r.URL = httpURL(r.URL)
	r.Updated = text(r.Updated, 64)
	declared := map[string]bool{}
	if c.Issue != nil {
		for _, f := range c.Issue.Fields {
			declared[f] = true
		}
	}
	if !declared["type"] {
		r.Type = ""
	}
	r.Type = text(r.Type, 64)
	if !declared["assignee"] {
		r.Assignee = ""
	}
	r.Assignee = text(r.Assignee, 200)
	if !declared["sprint"] {
		r.Sprint = ""
	}
	r.Sprint = text(r.Sprint, 200)
	if !declared["labels"] {
		r.Labels = nil
	}
	if len(r.Labels) > 50 {
		r.Labels = r.Labels[:50]
	}
	for i := range r.Labels {
		r.Labels[i] = text(r.Labels[i], 100)
	}
	if !declared["status"] {
		r.Status = nil
	}
	if r.Status != nil {
		r.Status.Name = text(r.Status.Name, 100)
		switch r.Status.Category {
		case CategoryTodo, CategoryInProgress, CategoryDone:
		default:
			r.Status.Category = CategoryTodo
		}
	}
	if !c.CanHierarchy() {
		r.Parents = nil
	}
	if len(r.Parents) > MaxParents {
		r.Parents = r.Parents[:MaxParents]
	}
	for i := range r.Parents {
		p := &r.Parents[i]
		p.Issue, p.Title, p.Type, p.URL = text(p.Issue, 200), text(p.Title, maxText), text(p.Type, 64), httpURL(p.URL)
	}
	if !r.OK && r.Error == "" {
		r.Error = "not found"
	}
	if !r.OK {
		r = IssueMeta{Issue: r.Issue, Error: r.Error}
	}
	return r
}

func httpURL(s string) string {
	if u, err := url.Parse(s); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return s
}
