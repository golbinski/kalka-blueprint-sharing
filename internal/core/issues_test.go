package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/plugin"
)

// fakeTracker is an issue tracker plugin for jira: issues, with the
// state an export changes, so idempotency can be checked.
type fakeTracker struct {
	mu        sync.Mutex
	caps      plugin.Capabilities
	titles    map[string]string // issue key -> title; others are not found
	offline   bool
	metaCalls int
	exportErr string

	comments    map[string]string // live comment id -> text
	attachments map[string]string // live attachment id -> file name
	next        int
	exports     []plugin.ExportRequest
}

func newTracker(hierarchy bool) *fakeTracker {
	return &fakeTracker{
		caps: plugin.Capabilities{Resolve: true, Issue: &plugin.IssueCaps{Meta: true, Hierarchy: hierarchy,
			Fields: []string{"status", "type"}, Export: &plugin.ExportCaps{Attachment: true, Comment: true}}},
		titles:      map[string]string{"DEV-123": "Redelivery after a failed attempt"},
		comments:    map[string]string{},
		attachments: map[string]string{},
	}
}

func (f *fakeTracker) Find(scheme string) (string, error) {
	if scheme != "jira" {
		return "", plugin.ErrNotFound
	}
	return "/x/kalka-plugin-jira", nil
}
func (f *fakeTracker) Installed() []string { return []string{"jira"} }
func (f *fakeTracker) Capabilities(context.Context, string) (*plugin.Description, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &plugin.Description{Protocol: plugin.Protocol, Name: "jira", Schemes: []string{"jira"}, Capabilities: f.caps}, nil
}
func (f *fakeTracker) Resolve(_ context.Context, _ string, _ map[string]any, refs []plugin.Ref) []plugin.Result {
	out := make([]plugin.Result, len(refs))
	for i, r := range refs {
		out[i] = plugin.Result{Ref: r.Ref, Error: "not resolved"}
	}
	return out
}
func (f *fakeTracker) IssueMeta(_ context.Context, _ string, issues []string) ([]plugin.IssueMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.metaCalls++
	if f.offline {
		return nil, errors.New("kalka-plugin-jira failed: dial tcp: no route to host")
	}
	out := make([]plugin.IssueMeta, len(issues))
	for i, is := range issues {
		title, ok := f.titles[is]
		if !ok {
			out[i] = plugin.IssueMeta{Issue: is, Error: "not found or no permission"}
			continue
		}
		m := plugin.IssueMeta{Issue: is, OK: true, Title: title, URL: "https://parcels.example/browse/" + is, Type: "Story",
			Status: &plugin.IssueStatus{Name: "In Progress", Category: plugin.CategoryInProgress}}
		if f.caps.CanHierarchy() {
			m.Parents = []plugin.IssueParent{{Issue: "DEV-90", Title: "Failed-delivery handling", Type: "Epic"}}
		}
		out[i] = m
	}
	return out, nil
}
func (f *fakeTracker) Export(_ context.Context, _ string, req plugin.ExportRequest) (*plugin.ExportResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exports = append(f.exports, req)
	if f.exportErr != "" {
		return nil, errors.New(f.exportErr)
	}
	f.next++
	att := fmt.Sprintf("1004%d", f.next)
	f.attachments[att] = req.Document.FileName
	comment := ""
	if req.Previous != nil {
		delete(f.attachments, req.Previous.AttachmentID)
		if _, live := f.comments[req.Previous.CommentID]; live {
			comment = req.Previous.CommentID
		}
	}
	if comment == "" {
		comment = fmt.Sprintf("2031%d", f.next)
	}
	f.comments[comment] = req.SummaryMarkdown
	return &plugin.ExportResult{OK: true, URL: "https://parcels.example/browse/" + req.Issue + "?focusedCommentId=" + comment, AttachmentID: att, CommentID: comment}, nil
}

// usePlugins gives a running test node plugins.
func (n *node) usePlugins(p Plugins) {
	n.d.opts.Plugins = p
	n.d.refs.plugins = p
	n.d.issues = newIssueCache(p, n.d.now, n.cfgDir, nil)
}

// issueOf opens the document until its issue view is no longer pending.
func issueOf(t *testing.T, n *node, path string) *DocView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		v, err := n.d.Document(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		if v.Issue == nil || !v.Issue.Pending {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("issue still pending: %+v", v.Issue)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

const issueDoc = "---\nkalka_issue: jira:DEV-123\n---\n# Redelivery after a failed attempt\n\n## Plan\n\nA parcel gets a second attempt the next day.\n"

func TestIssueMetaCacheAndOffline(t *testing.T) {
	w := newWorld(t)
	n := w.node("ewa@parcels.example")
	tr := newTracker(true)
	n.usePlugins(tr)
	os.MkdirAll(filepath.Join(n.folder, "personal"), 0o755)
	n.write("personal/redelivery.md", issueDoc)
	path := n.path("personal/redelivery.md")

	v := issueOf(t, n, path)
	m := v.Issue.Meta
	if m == nil || m.Title != "Redelivery after a failed attempt" || m.Status.Category != plugin.CategoryInProgress || len(m.Parents) != 1 || v.Issue.Stale || v.Issue.AsOf == "" {
		t.Fatalf("issue: %+v", v.Issue)
	}
	if v.Issue.Caps == nil || !v.Issue.Caps.Hierarchy {
		t.Fatalf("caps: %+v", v.Issue.Caps)
	}
	// Cached: opening again does not ask the plugin.
	calls := tr.metaCalls
	issueOf(t, n, path)
	if tr.metaCalls != calls {
		t.Fatalf("asked again within the TTL: %d calls", tr.metaCalls)
	}

	// Offline after the TTL: the last answer is shown, with its time.
	old := IssueTTL
	IssueTTL = time.Nanosecond
	t.Cleanup(func() { IssueTTL = old })
	tr.mu.Lock()
	tr.offline = true
	tr.mu.Unlock()
	v = issueOf(t, n, path)
	if v.Issue.Meta == nil || v.Issue.Meta.Title != m.Title || !v.Issue.Stale || !strings.Contains(v.Issue.Error, "no route to host") || v.Issue.AsOf != issueOf(t, n, path).Issue.AsOf {
		t.Fatalf("offline: %+v", v.Issue)
	}
	// It survives a restart, from the configuration folder.
	c := newIssueCache(tr, n.d.now, n.cfgDir, nil)
	got := c.lookup([]bp.IssueRef{{Scheme: "jira", Payload: "DEV-123"}}, nil)["jira:DEV-123"]
	if got.Meta == nil || got.Meta.Title != m.Title {
		t.Fatalf("after restart: %+v", got)
	}

	// The tracker's own "not found" is an answer, not a stale value.
	tr.mu.Lock()
	tr.offline = false
	tr.mu.Unlock()
	n.write("personal/redelivery.md", strings.Replace(issueDoc, "DEV-123", "DEV-404", 1))
	v = issueOf(t, n, path)
	if v.Issue.Meta != nil || v.Issue.Error != "not found or no permission" || v.Issue.Stale {
		t.Fatalf("not found: %+v", v.Issue)
	}
}

// Capability negotiation: what the plugin does not declare is not
// offered and not asked for.
func TestIssueCapabilities(t *testing.T) {
	w := newWorld(t)
	n := w.node("ewa@parcels.example")
	tr := newTracker(false)
	tr.caps.Issue.Export = nil
	n.usePlugins(tr)
	os.MkdirAll(filepath.Join(n.folder, "personal"), 0o755)
	n.write("personal/redelivery.md", issueDoc)
	v := issueOf(t, n, n.path("personal/redelivery.md"))
	if v.Issue.Meta == nil || v.Issue.Meta.Parents != nil || v.Issue.Caps.Hierarchy || v.Issue.Caps.Export != nil {
		t.Fatalf("issue: %+v", v.Issue)
	}
	if v.Export == nil || v.Export.Can {
		t.Fatalf("export offered: %+v", v.Export)
	}
	sc, _ := n.d.ResolveScope(ScopePersonal)
	var s IssuesView
	for i := 0; i < 100; i++ {
		if s = n.d.IssuesIn(sc); s.Caps.Meta {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !s.Caps.Meta || s.Caps.Hierarchy || s.Caps.Export {
		t.Fatalf("summary: %+v", s.Caps)
	}
	// With no plugin at all, only the raw key.
	tr.mu.Lock()
	tr.caps = plugin.Capabilities{Resolve: true}
	tr.mu.Unlock()
	n.usePlugins(tr)
	v = issueOf(t, n, n.path("personal/redelivery.md"))
	if v.Issue.Issue != "jira:DEV-123" || v.Issue.Meta != nil || v.Issue.Caps != nil || tr.metaCalls != 1 {
		t.Fatalf("no issue capability: %+v, %d calls", v.Issue, tr.metaCalls)
	}
}

// What a plugin tells about an issue never reaches the file, the folder
// or a peer.
func TestIssueMetaStaysLocal(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	installTaps(depot, courier)
	tr := newTracker(true)
	tr.titles["DEV-123"] = "Secret title from the tracker"
	depot.usePlugins(tr)
	depot.write("redelivery.md", issueDoc)
	waitIndexed(t, depot, 1)
	id := depot.share("redelivery.md", courier)
	v := issueOf(t, depot, depot.path("redelivery.md"))
	if v.Issue.Meta == nil {
		t.Fatalf("no meta: %+v", v.Issue)
	}
	tp := recordAll(depot)
	depot.write("redelivery.md", strings.Replace(depot.read("redelivery.md"), "next day.", "next day, before noon.", 1))
	if _, err := depot.d.Release(depot.path("redelivery.md"), "", nil); err != nil {
		t.Fatal(err)
	}
	depot.replicate(courier)
	sent, _ := tp.snapshot()
	stopRecording(depot)
	if len(sent) == 0 {
		t.Fatal("nothing recorded")
	}
	for _, secret := range []string{"Secret title from the tracker", "Failed-delivery handling", "In Progress"} {
		if s := leaks(sent, nil, secret); s != "" {
			t.Fatalf("%q went to a peer", s)
		}
		for _, n := range ns {
			filepath.WalkDir(n.folder, func(p string, e os.DirEntry, err error) error {
				if err == nil && !e.IsDir() {
					if b, _ := os.ReadFile(p); strings.Contains(string(b), secret) {
						t.Errorf("%q is in %s", secret, p)
					}
				}
				return nil
			})
		}
	}
	// The courier, without the plugin, sees the key only.
	if _, doc := courier.teamFile(id); doc == nil || !strings.Contains(string(doc.Bytes()), "before noon") {
		t.Fatal("the release did not arrive")
	}
	f, _ := courier.teamFile(id)
	cv := issueOf(t, courier, f)
	if cv.Issue == nil || cv.Issue.Issue != "jira:DEV-123" || cv.Issue.Meta != nil || !strings.Contains(cv.Issue.Error, "no plugin installed for jira:") {
		t.Fatalf("courier: %+v", cv.Issue)
	}
}

func TestExport(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example", "clerk@parcels.example")
	depot, courier, clerk := ns[0], ns[1], ns[2] // depot owns the team, courier the blueprint
	tr := newTracker(true)
	for _, n := range ns {
		n.usePlugins(tr)
	}
	ctx := context.Background()

	// In Personal there is no released version.
	os.MkdirAll(filepath.Join(courier.folder, "personal"), 0o755)
	courier.write("personal/draft.md", issueDoc)
	if _, err := courier.d.Export(ctx, courier.path("personal/draft.md")); KindOf(err) != KindInPersonal {
		t.Fatalf("export from Personal: %v", err)
	}
	os.Remove(courier.path("personal/draft.md"))

	courier.write("redelivery.md", issueDoc)
	waitIndexed(t, courier, 1)
	id := courier.share("redelivery.md", depot, clerk)
	path := courier.path("redelivery.md")

	// Unreleased edits are never exported.
	courier.write("redelivery.md", strings.Replace(courier.read("redelivery.md"), "next day.", "next day, before noon.", 1))
	if _, err := courier.d.Export(ctx, path); err == nil || !strings.Contains(err.Error(), "not released") {
		t.Fatalf("unreleased export: %v", err)
	}
	if v := issueOf(t, courier, path); v.Export == nil || v.Export.Can || !strings.Contains(v.Export.Why, "not released") {
		t.Fatalf("export state: %+v", v.Export)
	}
	dev, err := courier.d.AddEntry(EntryRequest{Path: path, Kind: bp.KindDeviation, Anchor: "plan", Body: "Second attempts start at 8:00, not 9:00."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := courier.d.Release(path, "before noon", []string{dev.ID}); err != nil {
		t.Fatal(err)
	}
	courier.replicate(depot)
	courier.replicate(clerk)

	// A member who owns neither the blueprint nor the team cannot.
	cf, _ := clerk.teamFile(id)
	if _, err := clerk.d.Export(ctx, cf); KindOf(err) != KindNotOwner {
		t.Fatalf("clerk export: %v", err)
	}

	// A failed export changes nothing.
	before := courier.read("redelivery.md")
	tr.exportErr = "Jira rejected the credentials"
	if _, err := courier.d.Export(ctx, path); err == nil || !strings.Contains(err.Error(), "Jira rejected the credentials") {
		t.Fatalf("failed export: %v", err)
	}
	if courier.read("redelivery.md") != before {
		t.Fatal("a failed export changed the file")
	}
	tr.exportErr = ""

	// The owner exports version 2.
	res, err := courier.d.Export(ctx, path)
	if err != nil || res.Version != 2 || res.Issue != "jira:DEV-123" || res.Updated {
		t.Fatalf("export: %+v %v", res, err)
	}
	req := tr.exports[len(tr.exports)-1]
	if req.Issue != "DEV-123" || req.Previous != nil || req.Document.FileName != "redelivery-v2.md" || req.Document.Version != 2 ||
		!strings.Contains(req.Document.Markdown, "kalka_issue: jira:DEV-123") ||
		!strings.Contains(req.SummaryMarkdown, "before noon") || !strings.Contains(req.SummaryMarkdown, "Exported from kalka") ||
		!strings.Contains(req.SummaryMarkdown, "## Accepted deviations") || !strings.Contains(req.SummaryMarkdown, "8:00, not 9:00") {
		t.Fatalf("request: %+v", req)
	}
	first, e, ok := bp.LastExport(courier.doc("redelivery.md"), "jira:DEV-123", nil)
	if !ok || first.Version != 2 || first.CommentID == "" || first.AttachmentID == "" || e.Sig == "" || e.Author != courier.email {
		t.Fatalf("exported entry: %+v %+v", first, e)
	}

	// A team owner may export someone else's blueprint; the same comment
	// is updated and the attachment replaced.
	courier.replicate(depot)
	df, _ := depot.teamFile(id)
	res, err = depot.d.Export(ctx, df)
	if err != nil || !res.Updated {
		t.Fatalf("team owner export: %+v %v", res, err)
	}
	req = tr.exports[len(tr.exports)-1]
	second, _, _ := bp.LastExport(depot.teamDoc(id), "jira:DEV-123", nil)
	if req.Previous == nil || req.Previous.CommentID != first.CommentID || second.CommentID != first.CommentID || second.AttachmentID == first.AttachmentID {
		t.Fatalf("not idempotent: %+v -> %+v", first, second)
	}
	if len(tr.comments) != 1 || len(tr.attachments) != 1 {
		t.Fatalf("duplicates on the issue: %v %v", tr.comments, tr.attachments)
	}

	// Someone deleted the comment on the issue: the next export writes a
	// new one and records it.
	tr.mu.Lock()
	delete(tr.comments, second.CommentID)
	tr.mu.Unlock()
	depot.replicate(courier)
	if _, err := courier.d.Export(ctx, path); err != nil {
		t.Fatal(err)
	}
	third, _, _ := bp.LastExport(courier.doc("redelivery.md"), "jira:DEV-123", nil)
	if third.CommentID == second.CommentID || len(tr.comments) != 1 || tr.comments[third.CommentID] == "" {
		t.Fatalf("deleted comment: %+v, %v", third, tr.comments)
	}

	// The exported entries replicate like any entry.
	courier.replicate(clerk)
	n := 0
	for _, it := range clerk.teamDoc(id).Items() {
		if _, ok := it.Entry.Exported(); ok {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("clerk holds %d exported entries", n)
	}
}

func TestArchive(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	courier.write("redelivery.md", issueDoc)
	waitIndexed(t, courier, 1)
	id := courier.share("redelivery.md", depot)
	path := courier.path("redelivery.md")

	if err := depot.d.Archive(func() string { f, _ := depot.teamFile(id); return f }(), true); KindOf(err) != KindNotOwner {
		t.Fatalf("a member archived: %v", err)
	}
	// Edits not released yet would ride along with the archive: refused.
	orig := courier.read("redelivery.md")
	courier.write("redelivery.md", strings.Replace(orig, "the next day", "the same evening", 1))
	if err := courier.d.Archive(path, true); err == nil || !strings.Contains(err.Error(), "not released") {
		t.Fatalf("archived over unreleased edits: %v", err)
	}
	courier.write("redelivery.md", orig)
	if err := courier.d.Archive(path, true); err != nil {
		t.Fatal(err)
	}
	doc := courier.doc("redelivery.md")
	if _, ok := doc.Archived(); !ok || courier.head(id).Release.Version != 2 {
		t.Fatalf("not archived and released: %s", doc.Bytes())
	}
	sc, _ := courier.d.ResolveScope("")
	archived := false
	for _, in := range courier.d.BlueprintsIn(sc) {
		if in.Path == path {
			archived = in.Archived
		}
	}
	if !archived {
		t.Fatal("the list row is not archived")
	}
	if err := courier.d.Archive(path, true); err == nil {
		t.Fatal("archived twice")
	}
	// It keeps replicating.
	courier.replicate(depot)
	if _, ok := depot.teamDoc(id).Archived(); !ok {
		t.Fatal("the archive did not replicate")
	}
	// A team owner takes it over to unarchive it.
	df, _ := depot.teamFile(id)
	if err := depot.d.TakeOverArchive(df, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := depot.teamDoc(id).Archived(); ok || !bp.SameEmail(depot.teamDoc(id).Owner(), depot.email) {
		t.Fatalf("not unarchived by the team owner: %s", depot.teamDoc(id).Bytes())
	}
	// A domain document is not archived.
	depot.write("delivery.md", "---\nkalka_kind: domain\nkalka_domain: delivery\n---\n# Delivery\n")
	if err := depot.d.Archive(depot.path("delivery.md"), true); err == nil {
		t.Fatal("a domain document was archived")
	}
}

// teamDoc is n's copy of the team document id.
func (n *node) teamDoc(id string) *bp.Document {
	n.w.t.Helper()
	_, d := n.teamFile(id)
	return d
}
