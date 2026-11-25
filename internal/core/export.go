package core

// Exporting a released version to its issue, and archiving (spec 10.5).
// An export is one way, at a release: the plugin attaches the whole file
// and writes one comment with a summary, and kalka records a signed
// exported entry. That entry and kalka_issue are the only tracker data
// in the shared file, and both come from an explicit action of a user.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/plugin"
)

// ExportState says whether the local user can export the open document
// now, and what was exported last (spec 10.5).
type ExportState struct {
	Can bool `json:"can"`
	// Why is the reason when Can is false, for the disabled control.
	Why string `json:"why,omitempty"`
	// Version is the released version an export would send, and
	// Approval its approval state in words (approved v3, draft…).
	Version  int64  `json:"version,omitempty"`
	Approval string `json:"approval,omitempty"`
	// Last is the newest exported entry for the issue, with its time and
	// author.
	Last   *bp.Exported `json:"last,omitempty"`
	LastAt string       `json:"last_at,omitempty"`
	LastBy string       `json:"last_by,omitempty"`
}

// ExportResult reports an export.
type ExportResult struct {
	Path    string `json:"path"`
	Issue   string `json:"issue"`
	Version int64  `json:"version"`
	URL     string `json:"url,omitempty"`
	// Updated: the export replaced what an earlier one wrote.
	Updated bool `json:"updated,omitempty"`
	// Approved: the version exported is the approved one (spec 6.18);
	// Approval says the state in words.
	Approved bool   `json:"approved"`
	Approval string `json:"approval,omitempty"`
}

// exportCheck is what an export of doc at path would send, or why it
// cannot be done. It does not run the plugin.
type exportPlan struct {
	issue   bp.IssueRef
	version int64
	chain   merge.ReleaseChain
}

func (d *Node) exportPlan(path string, doc *bp.Document, caps *plugin.Capabilities) (exportPlan, error) {
	var p exportPlan
	r, ok := doc.Issue()
	switch {
	case !ok:
		return p, errors.New("this blueprint names no issue: add `kalka_issue: <scheme>:<key>` to its frontmatter")
	case doc.IsDomain():
		return p, errors.New("a domain document is not exported; its versions are kept in kalka")
	case doc.KalkaID() == "":
		return p, &Error{Kind: KindInPersonal, Msg: "only a released version is exported: publish it to a team first"}
	}
	p.issue = r
	ts := d.teamDocState(path, doc)
	if ts == nil || !ts.Held {
		return p, &Error{Kind: KindInPersonal, Msg: "only a released version is exported: publish it to a team first"}
	}
	h, err := d.heldAt(path)
	if err != nil {
		return p, err
	}
	p.chain = merge.Releases(entriesOf(doc), doc.KalkaID(), h.team())
	head, ok := p.chain.Head()
	switch {
	case !ok || ts.Disputed:
		return p, errors.New("this blueprint has no released version to export")
	case head.Release.Deleted || doc.Deleted():
		return p, errors.New("a deleted document is not exported")
	case ts.Unrel:
		return p, fmt.Errorf("the plan has edits that are not released: release them first; only version %d as released, or a later release, is exported", head.Release.Version)
	}
	p.version = head.Release.Version
	if !d.ownsAt(path, doc) && !ts.TeamOwner {
		return p, &Error{Kind: KindNotOwner, Msg: fmt.Sprintf("only the owner (%s) or a team owner can export this blueprint", doc.Owner())}
	}
	if caps == nil || !caps.CanExport() {
		return p, fmt.Errorf("no installed plugin can export %s: issues", r.Scheme)
	}
	return p, nil
}

// exportState is the document view's export state.
func (d *Node) exportState(path string, doc *bp.Document, iv *IssueView) *ExportState {
	if iv == nil {
		return nil
	}
	st := &ExportState{}
	if x, e, ok := bp.LastExport(doc, iv.Issue, func(e *bp.Entry) bool { return e.Valid() }); ok {
		st.Last, st.LastAt, st.LastBy = &x, e.Timestamp, e.Author
	}
	var caps *plugin.Capabilities
	if iv.Caps != nil {
		caps = &plugin.Capabilities{Issue: iv.Caps}
	}
	p, err := d.exportPlan(path, doc, caps)
	st.Version = p.version
	st.Approval = d.approvalAt(path, doc).Text()
	if err != nil {
		st.Why = err.Error()
		if iv.Pending && caps == nil {
			st.Why = "looking for a plugin that can export…"
		}
		return st
	}
	st.Can = true
	return st
}

// Export sends the released version of a team blueprint to its issue,
// through the plugin for the issue's scheme, and records a signed
// exported entry. It either succeeds and writes the entry, or fails with
// the plugin's error and changes nothing.
func (d *Node) Export(ctx context.Context, path string) (ExportResult, error) {
	var res ExportResult
	f, err := d.Store.Load(d.resolve(path))
	if err != nil {
		return res, err
	}
	doc := f.Doc
	r, _ := doc.Issue()
	var caps *plugin.Capabilities
	if d.opts.Plugins != nil && r.Scheme != "" {
		if _, err := d.opts.Plugins.Find(r.Scheme); err == nil {
			if desc, err := d.opts.Plugins.Capabilities(ctx, r.Scheme); err == nil {
				caps = &desc.Capabilities
			} else {
				return res, fmt.Errorf("the %s: plugin failed: %v", r.Scheme, err)
			}
		}
	}
	p, err := d.exportPlan(f.Path, doc, caps)
	if err != nil {
		return res, err
	}
	issue := p.issue.String()
	prev, _, had := bp.LastExport(doc, issue, func(e *bp.Entry) bool { return e.Valid() })
	req := plugin.ExportRequest{
		Issue: p.issue.Payload,
		Document: &plugin.ExportDocument{
			Title: doc.Title(), Version: p.version,
			FileName: exportFileName(f.Path, p.version),
			Markdown: string(f.Raw),
		},
		SummaryMarkdown: exportSummary(doc, p, d.self.Email, d.now()),
	}
	if had && (prev.AttachmentID != "" || prev.CommentID != "") {
		req.Previous = &plugin.ExportPrevious{AttachmentID: prev.AttachmentID, CommentID: prev.CommentID}
	}
	out, err := d.opts.Plugins.Export(ctx, p.issue.Scheme, req)
	if err != nil {
		return res, fmt.Errorf("export to %s failed: %v", issue, err)
	}
	x := bp.Exported{Issue: issue, Version: p.version, URL: out.URL, AttachmentID: out.AttachmentID, CommentID: out.CommentID}
	if _, err := d.AddEntry(EntryRequest{Path: f.Path, Kind: bp.KindExported, Body: x.Body()}); err != nil {
		return res, fmt.Errorf("%s was exported to %s, but the exported entry could not be written (%v); export again to record it", d.rel(f.Path), issue, err)
	}
	d.logf("exported %s v%d to %s", f.Path, p.version, issue)
	ap := d.approvalAt(f.Path, doc)
	return ExportResult{Path: f.Path, Issue: issue, Version: p.version, URL: out.URL, Updated: req.Previous != nil,
		Approved: ap.State == bp.ApprovalApproved, Approval: ap.Text()}, nil
}

// exportFileName is the attachment's name: the file's name with the
// version, such as redelivery-v3.md.
func exportFileName(path string, v int64) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return fmt.Sprintf("%s-v%d.md", base, v)
}

// exportSummary is the comment's text: a line saying where it came from,
// the released plan, and the deviations a release incorporated.
func exportSummary(doc *bp.Document, p exportPlan, by string, at time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "*Exported from %s: %s, version %d, on %s by %s. The whole file is attached.*\n\n",
		name.Product, doc.Title(), p.version, at.UTC().Format("2006-01-02"), by)
	b.WriteString(strings.TrimSpace(doc.Body()))
	b.WriteString("\n")
	accepted := map[string]bool{}
	for _, l := range p.chain.Releases {
		for _, id := range l.Release.Incorporates {
			accepted[id] = true
		}
	}
	var devs []*bp.Entry
	for _, it := range doc.Items() {
		if e := it.Entry; e != nil && e.Kind == bp.KindDeviation && e.Valid() && accepted[e.ID] {
			devs = append(devs, e)
		}
	}
	if len(devs) > 0 {
		b.WriteString("\n## Accepted deviations\n\n")
		for _, e := range devs {
			where := "general"
			if e.Anchor != "" {
				where = "§" + e.Anchor
			}
			text := strings.Join(strings.Fields(displayBody(e)), " ")
			fmt.Fprintf(&b, "- **%s** · %s · %s: %s\n", where, e.Author, e.Timestamp[:10], text)
		}
	}
	return b.String()
}

// Archive archives or unarchives a blueprint or seed (spec 10.5), for
// plans abandoned or superseded: the owner writes or removes
// kalka_archived and, for a team document, releases the change, as for
// any plan edit. It changes only frontmatter, so it keeps an approval. A team owner acting on
// someone else's document takes it over first (7.4).
func (d *Node) Archive(path string, archived bool) error {
	for attempt := 0; ; attempt++ {
		f, err := d.Store.Load(d.resolve(path))
		if err != nil {
			return err
		}
		doc, err := bp.Parse(f.Raw)
		if err != nil {
			return err
		}
		if doc.IsDomain() {
			return errors.New("a domain document is not archived: blueprints name its versions")
		}
		if !d.ownsAt(f.Path, doc) {
			return &Error{Kind: KindNotOwner, Msg: fmt.Sprintf("only the owner (%s) can archive this blueprint; a team owner takes it over first", doc.Owner())}
		}
		// Archiving releases the file: other edits must not ride along.
		if doc.KalkaID() != "" {
			if _, ok := d.teamDirAt(f.Path); ok {
				if ts := d.teamDocState(f.Path, doc); ts != nil && ts.Held && ts.Unrel {
					return errors.New("the plan has edits that are not released: release them first, then archive")
				}
			}
		}
		if archived {
			err = bp.Archive(doc, d.now())
		} else {
			err = bp.Unarchive(doc)
		}
		if err != nil {
			return err
		}
		err = d.Store.Update(f, doc)
		if errors.Is(err, fsutil.ErrChanged) && attempt < 3 {
			continue
		}
		if err != nil {
			return err
		}
		note := map[bool]string{true: "archived", false: "unarchived"}[archived]
		d.logf("%s %s", note, f.Path)
		if doc.KalkaID() != "" {
			if _, ok := d.teamDirAt(f.Path); ok {
				if _, err := d.Release(f.Path, note, nil); err != nil {
					return fmt.Errorf("%s, but the release failed: %v", note, err)
				}
			}
		}
		return nil
	}
}

// TakeOverArchive is Archive by a team owner on someone else's document:
// a reassignment to the actor, then the archive (spec 7.4).
func (d *Node) TakeOverArchive(path string, archived bool) error {
	need, err := d.needsTakeOver(path)
	if err != nil {
		return err
	}
	if need {
		if _, err := d.Reassign(path, d.self.Email); err != nil {
			return err
		}
	}
	return d.Archive(path, archived)
}
