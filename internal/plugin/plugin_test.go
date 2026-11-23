package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as the plugins: copied as kalka-plugin-<mode>,
// it behaves according to <mode>.
func TestMain(m *testing.M) {
	base := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if mode, ok := strings.CutPrefix(base, Prefix); ok {
		fake(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fake(mode string) {
	raw, _ := io.ReadAll(os.Stdin)
	var req struct {
		Op       string
		Context  struct{ Frontmatter map[string]any }
		Refs     []Ref
		Issues   []string
		Issue    string
		Document *ExportDocument
		Previous *ExportPrevious
	}
	_ = json.Unmarshal(raw, &req)
	if len(os.Args) > 1 {
		fmt.Println(`{"protocol":2,"results":[]}`) // arguments are never passed
		return
	}
	if settingsFake(mode, req.Op, raw) {
		return
	}
	switch mode {
	case "crash":
		fmt.Fprintln(os.Stderr, "boom: segfault in parser")
		os.Exit(2)
	case "sleep":
		time.Sleep(8 * time.Second)
	case "garbage":
		fmt.Println("hello, I am not JSON")
	case "flood":
		os.Stdout.Write([]byte(strings.Repeat("x", maxStdout+10)))
	}
	if mode == "tracker" || mode == "flat" || mode == "old" {
		tracker(mode, req.Op, req.Issues, req.Issue, req.Document, req.Previous)
		return
	}
	if req.Op == "describe" {
		fmt.Println(`{"protocol":2,"name":"fake","version":"0.1","schemes":["ok"],"capabilities":{"resolve":true}}`)
		return
	}
	var res []map[string]any
	for i, r := range req.Refs {
		if mode == "partial" && i == 1 {
			continue
		}
		res = append(res, map[string]any{
			"ref": r.Ref, "ok": true, "title": "T " + r.Payload + strings.Repeat("!", 2000),
			"url":      map[bool]string{true: "javascript:alert(1)", false: "https://example.com/" + r.Payload}[strings.Contains(r.Payload, "evil")],
			"status":   fmt.Sprint(req.Context.Frontmatter["repo"]),
			"snippet":  map[string]any{"start_line": 3, "text": "<b>x</b>\n"},
			"warnings": []map[string]string{{"text": "changed"}},
		})
	}
	out, _ := json.Marshal(map[string]any{"protocol": 2, "results": res})
	os.Stdout.Write(out)
}

// tracker is a fake issue tracker plugin: "tracker" declares everything,
// "flat" metadata without parents, labels or export, "old" protocol 1.
func tracker(mode, op string, issues []string, issue string, doc *ExportDocument, prev *ExportPrevious) {
	var out any
	switch op {
	case "describe":
		caps := map[string]any{"resolve": true, "issue": map[string]any{"meta": true, "hierarchy": true,
			"fields": []string{"status", "type", "assignee", "labels", "sprint", "story_points"},
			"export": map[string]bool{"attachment": true, "comment": true}}}
		if mode == "flat" {
			caps = map[string]any{"issue": map[string]any{"meta": true, "fields": []string{"status"}}}
		}
		proto := 2
		if mode == "old" {
			proto = 1
		}
		out = map[string]any{"protocol": proto, "name": "tracker", "version": "0.1", "schemes": []string{mode}, "capabilities": caps}
	case "issue_meta":
		if len(issues) > 0 && issues[0] == "DEV-401" {
			out = map[string]any{"error": "Jira rejected the credentials"}
			break
		}
		var res []map[string]any
		for _, is := range issues {
			if is == "DEV-404" {
				res = append(res, map[string]any{"issue": is, "ok": false, "error": "not found or no permission"})
				continue
			}
			var parents []map[string]any
			for i := 0; i < 7; i++ {
				parents = append(parents, map[string]any{"issue": fmt.Sprintf("DEV-%d", 90-i), "title": "Parent", "type": "Epic", "url": "https://t.example/DEV"})
			}
			res = append(res, map[string]any{"issue": is, "ok": true, "title": "Redelivery after a failed attempt",
				"url": "https://t.example/browse/" + is, "type": "Story", "status": map[string]string{"name": "In Progress", "category": "doing"},
				"assignee": "Ewa Nowak", "labels": []string{"delivery"}, "sprint": "Sprint 41", "parents": parents})
		}
		out = map[string]any{"results": res}
	case "export":
		if issue == "DEV-500" {
			out = map[string]any{"ok": false, "error": "Jira rejected the credentials"}
			break
		}
		comment := "20311"
		if prev != nil && prev.CommentID != "" {
			comment = prev.CommentID
		}
		att := "bad id with spaces"
		if doc != nil && doc.FileName == "redelivery-v3.md" {
			att = "10046"
		}
		out = map[string]any{"ok": true, "url": "https://t.example/browse/" + issue, "attachment_id": att, "comment_id": comment}
	}
	b, _ := json.Marshal(out)
	os.Stdout.Write(b)
}

func install(t *testing.T, dir string, modes ...string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	for _, m := range modes {
		if err := os.WriteFile(filepath.Join(dir, Prefix+m+ext), bin, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func refs(payloads ...string) []Ref {
	var out []Ref
	for _, p := range payloads {
		out = append(out, Ref{Ref: "x:" + p, Payload: p})
	}
	return out
}

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	install(t, dir, "ok", "crash", "sleep", "garbage", "partial", "flood")
	var log strings.Builder
	h := &Host{Dir: dir, Log: &log}
	ctx := context.Background()
	fm := map[string]any{"repo": "org/doc-service"}

	got := h.Resolve(ctx, "ok", fm, refs("a.py", "evil.py"))
	if len(got) != 2 || !got[0].OK || got[0].Status != "org/doc-service" || got[0].URL != "https://example.com/a.py" || got[0].Snippet.Text != "<b>x</b>\n" {
		t.Fatalf("ok: %+v", got)
	}
	// Plugin output is capped and a non-web URL is dropped.
	if got[1].URL != "" || len(got[0].Title) > maxText+4 || !strings.HasSuffix(got[0].Title, "…") {
		t.Fatalf("not cleaned: %q %d", got[1].URL, len(got[0].Title))
	}

	old := Timeout
	Timeout = 2 * time.Second
	defer func() { Timeout = old }()
	for mode, want := range map[string]string{
		"missing": "no plugin installed for missing:",
		"crash":   "exited with an error",
		"sleep":   "timed out",
		"garbage": "not the expected JSON",
		"flood":   "more output than allowed",
	} {
		start := time.Now()
		got := h.Resolve(ctx, mode, fm, refs("a.py", "b.py"))
		if len(got) != 2 || got[0].OK || got[1].OK || !strings.Contains(got[0].Error, want) || got[1].Ref != "x:b.py" {
			t.Errorf("%s: %+v", mode, got)
		}
		if time.Since(start) > 4*time.Second {
			t.Errorf("%s took %v", mode, time.Since(start))
		}
	}
	if !strings.Contains(log.String(), "boom: segfault") {
		t.Errorf("stderr not logged: %q", log.String())
	}
	// A ref the plugin skipped gets its own error; the others resolve.
	got = h.Resolve(ctx, "partial", fm, refs("a.py", "b.py", "c.py"))
	if !got[0].OK || got[1].OK || !strings.Contains(got[1].Error, "no result") || !got[2].OK {
		t.Fatalf("partial: %+v", got)
	}
}

func TestFindAndDescribe(t *testing.T) {
	dir := t.TempDir()
	install(t, dir, "ok")
	h := &Host{Dir: dir}
	p, err := h.Find("ok")
	if err != nil || filepath.Dir(p) != dir {
		t.Fatal(p, err)
	}
	for _, bad := range []string{"../ok", "OK", "", "o k"} {
		if _, err := h.Find(bad); err == nil {
			t.Errorf("found %q", bad)
		}
	}
	d, err := h.Describe(context.Background(), p)
	if err != nil || d.Name != "fake" || d.Schemes[0] != "ok" {
		t.Fatal(d, err)
	}
	// Plugins on PATH are found after the config dir.
	other := t.TempDir()
	install(t, other, "onpath")
	t.Setenv("PATH", other+string(os.PathListSeparator)+os.Getenv("PATH"))
	if p, err := h.Find("onpath"); err != nil || filepath.Dir(p) != other {
		t.Fatal(p, err)
	}
	// Beside kalka itself, before PATH.
	beside := t.TempDir()
	install(t, beside, "onpath")
	h.ExeDir = beside
	if p, err := h.Find("onpath"); err != nil || filepath.Dir(p) != beside {
		t.Fatal(p, err)
	}
}

func TestCapabilities(t *testing.T) {
	dir := t.TempDir()
	install(t, dir, "tracker", "flat", "old", "ok")
	h := &Host{Dir: dir}
	ctx := context.Background()
	d, err := h.Capabilities(ctx, "tracker")
	if err != nil || !d.Capabilities.CanMeta() || !d.Capabilities.CanHierarchy() || !d.Capabilities.CanExport() {
		t.Fatal(d, err)
	}
	// A field kalka does not know is dropped.
	if got := strings.Join(d.Capabilities.Issue.Fields, ","); got != "status,type,assignee,labels,sprint" {
		t.Fatalf("fields: %s", got)
	}
	f, err := h.Capabilities(ctx, "flat")
	if err != nil || !f.Capabilities.CanMeta() || f.Capabilities.CanHierarchy() || f.Capabilities.CanExport() {
		t.Fatal(f, err)
	}
	o, err := h.Capabilities(ctx, "ok")
	if err != nil || !o.Capabilities.Resolve || o.Capabilities.CanMeta() {
		t.Fatal(o, err)
	}
	if _, err := h.Capabilities(ctx, "old"); err == nil || !strings.Contains(err.Error(), "protocol 1") {
		t.Fatalf("protocol 1 accepted: %v", err)
	}
	if got := strings.Join(h.Installed(), ","); !strings.Contains(got, "flat,ok,old,tracker") {
		t.Fatalf("installed: %s", got)
	}
	// Asking for what is not declared does not run the op.
	if _, err := h.IssueMeta(ctx, "ok", []string{"DEV-1"}); err == nil {
		t.Fatal("issue_meta without the capability")
	}
	if _, err := h.Export(ctx, "flat", ExportRequest{Issue: "DEV-1"}); err == nil {
		t.Fatal("export without the capability")
	}
}

func TestIssueMeta(t *testing.T) {
	dir := t.TempDir()
	install(t, dir, "tracker", "flat")
	h := &Host{Dir: dir}
	ctx := context.Background()
	got, err := h.IssueMeta(ctx, "tracker", []string{"DEV-123", "DEV-404", "DEV-123"})
	if err != nil || len(got) != 3 {
		t.Fatal(got, err)
	}
	m := got[0]
	if !m.OK || m.Title == "" || m.Status.Name != "In Progress" || m.Assignee != "Ewa Nowak" || m.Sprint != "Sprint 41" {
		t.Fatalf("meta: %+v", m)
	}
	// An unknown category is normalized; the chain is capped, nearest first.
	if m.Status.Category != CategoryTodo || len(m.Parents) != MaxParents || m.Parents[0].Issue != "DEV-90" {
		t.Fatalf("not normalized: %+v", m)
	}
	if got[1].OK || got[1].Error != "not found or no permission" || got[1].Title != "" {
		t.Fatalf("failed issue: %+v", got[1])
	}
	// A failure of the whole batch is an error, not per-issue answers.
	if _, err := h.IssueMeta(ctx, "tracker", []string{"DEV-401"}); err == nil || err.Error() != "Jira rejected the credentials" {
		t.Fatalf("batch error: %v", err)
	}
	// Without hierarchy no parents; undeclared fields are dropped.
	flat, err := h.IssueMeta(ctx, "flat", []string{"DEV-123"})
	if err != nil || flat[0].Parents != nil || flat[0].Assignee != "" || flat[0].Labels != nil || flat[0].Status == nil {
		t.Fatalf("flat: %+v %v", flat, err)
	}
}

func TestExport(t *testing.T) {
	dir := t.TempDir()
	install(t, dir, "tracker")
	h := &Host{Dir: dir}
	ctx := context.Background()
	req := ExportRequest{Issue: "DEV-123", Document: &ExportDocument{Title: "Redelivery", Version: 3, FileName: "redelivery-v3.md", Markdown: "# Redelivery\n"}, SummaryMarkdown: "plan"}
	res, err := h.Export(ctx, "tracker", req)
	if err != nil || res.AttachmentID != "10046" || res.CommentID != "20311" || res.URL != "https://t.example/browse/DEV-123" {
		t.Fatal(res, err)
	}
	req.Previous = &ExportPrevious{AttachmentID: "10046", CommentID: "30000"}
	if res, err = h.Export(ctx, "tracker", req); err != nil || res.CommentID != "30000" {
		t.Fatal(res, err)
	}
	// The plugin's error comes back as it is.
	if _, err := h.Export(ctx, "tracker", ExportRequest{Issue: "DEV-500"}); err == nil || err.Error() != "Jira rejected the credentials" {
		t.Fatal(err)
	}
	// An id that cannot go into the review log fails the export.
	req.Document.FileName = "other.md"
	if _, err := h.Export(ctx, "tracker", req); err == nil || !strings.Contains(err.Error(), "attachment id") {
		t.Fatal(err)
	}
}
