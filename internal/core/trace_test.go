package core

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/plugin"
)

const tracePlan = "# Import pipeline refactor\n\n## Plan\n\n```trace\n" + `lanes: [business, code]
nodes:
  - {id: B1, lane: business, text: Receive files, next: [B2]}
  - {id: B2, lane: business, text: Notify}
  - {id: C1, lane: code, text: Upload handler, realizes: [B1], ref: "gh:src/upload.py#L1-L9", next: [C2]}
  - {id: C2, lane: code, text: Skip large files, ref: "https://example.com/x", next: [C3]}
  - {id: C3, lane: code, text: Notify in batches, realizes: [B2], ref: "gh:src/notify.py"}
` + "```\n"

func findEntry(v *DocView, anchor string) *EntryView {
	for i := range v.Entries {
		if v.Entries[i].Anchor == anchor {
			return &v.Entries[i]
		}
	}
	return nil
}

// A comment on a trace node round-trips through a sync and becomes
// orphaned when the owner removes the node (M7 acceptance).
func TestNodeCommentRoundTrip(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.node("alice@corp.com"), w.node("bob@corp.com")
	alice.write("plan.md", tracePlan)
	alice.mustSync("plan.md", bob, IntentSync)
	bob.comment("plan.md", "plan#C2", "Why 20 MB?")
	bob.mustSync("plan.md", alice, IntentSync)
	equalFiles(t, "plan.md", alice, bob)

	ctx := context.Background()
	v, err := alice.d.Document(ctx, "plan.md")
	if err != nil {
		t.Fatal(err)
	}
	e := findEntry(v, "plan#C2")
	if e == nil || e.Node != "C2" || e.Section != "plan" || e.Orphaned {
		t.Fatalf("entry %+v", e)
	}
	if len(v.Traces) != 1 || v.Traces[0].Section != "plan" {
		t.Fatalf("traces %+v", v.Traces)
	}

	// The owner renames the node and sends the revision.
	src := alice.read("plan.md")
	src = strings.Replace(src, "{id: C2,", "{id: C9,", 1)
	src = strings.Replace(src, "next: [C2]", "next: [C9]", 1)
	alice.write("plan.md", src)
	alice.mustSync("plan.md", bob, IntentSync)
	v, _ = bob.d.Document(ctx, "plan.md")
	if e := findEntry(v, "plan#C2"); e == nil || !e.Orphaned {
		t.Fatalf("not orphaned: %+v", e)
	}
	found := false
	for _, p := range v.Problems {
		found = found || p.Code == "orphaned"
	}
	if !found {
		t.Fatalf("problems %+v", v.Problems)
	}
}

// Unverified and Stale depend on the reader's plugins; the library's graph
// divergences come first.
func TestTraceViewReferences(t *testing.T) {
	w := newWorld(t)
	alice := w.node("alice@corp.com")
	alice.write("plan.md", tracePlan)
	ctx := context.Background()

	// No plugins: every code step is unverified, each with its reason.
	v, err := alice.d.Document(ctx, "plan.md")
	if err != nil {
		t.Fatal(err)
	}
	tv := v.Traces[0]
	var got []string
	for _, d := range tv.Divergences {
		got = append(got, d.Class+":"+d.Node+":"+d.Detail)
	}
	want := []string{
		"implementation-only:C2:",
		"unverified:C1:no plugin installed for gh:",
		"unverified:C2:not a plugin reference",
		"unverified:C3:no plugin installed for gh:",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") || tv.Graph != 1 {
		t.Fatalf("divergences\n got %q\nwant %q (graph %d)", got, want, tv.Graph)
	}
	if r := v.Refs["gh:src/upload.py#L1-L9"]; r.Scheme != "gh" || r.Payload != "src/upload.py#L1-L9" || r.OK {
		t.Fatalf("ref %+v", r)
	}

	// With a plugin: the first look shows the refs resolving, in the
	// background; the UI hears about it, and the next look has results.
	fp := &fakePlugins{t: t}
	alice.d.refs = newRefCache(fp, w.now)
	events, stop := alice.d.bus.Subscribe()
	defer stop()
	v, _ = alice.d.Document(ctx, "plan.md")
	if r := v.Refs["gh:src/notify.py"]; !r.Pending || r.Plugin != "kalka-plugin-gh" {
		t.Fatalf("not pending: %+v", r)
	}
	select {
	case e := <-events:
		if e.Type != "refs" {
			t.Fatalf("event %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no refs event")
	}
	v, _ = alice.d.Document(ctx, "plan.md")
	got = nil
	for _, d := range v.Traces[0].Divergences {
		got = append(got, d.Class+":"+d.Node+":"+d.Detail)
	}
	want = []string{
		"implementation-only:C2:",
		"unverified:C2:not a plugin reference",
		"stale:C3:File changed in 3 commits since base_sha",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("with plugin\n got %q\nwant %q", got, want)
	}
	if r := v.Refs["gh:src/upload.py#L1-L9"]; !r.OK || r.Snippet.Text != "<script>x</script>" || r.Ref == "" || r.Pending {
		t.Fatalf("resolved ref %+v", r)
	}
	// Cached: a second look calls no plugin.
	if fp.calls != 1 {
		t.Fatalf("%d plugin calls", fp.calls)
	}
	alice.d.Document(ctx, "plan.md")
	time.Sleep(50 * time.Millisecond)
	if fp.calls != 1 {
		t.Fatalf("cache missed: %d calls", fp.calls)
	}
}

type fakePlugins struct {
	t     *testing.T
	mu    sync.Mutex
	calls int
}

func (f *fakePlugins) Find(scheme string) (string, error) {
	if scheme != "gh" {
		return "", plugin.ErrNotFound
	}
	return "/x/kalka-plugin-gh", nil
}

func (f *fakePlugins) Resolve(_ context.Context, scheme string, fm map[string]any, refs []plugin.Ref) []plugin.Result {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if scheme != "gh" || len(refs) != 2 || fm == nil {
		f.t.Errorf("resolve %s %v %v", scheme, refs, fm)
	}
	var out []plugin.Result
	for _, r := range refs {
		switch r.Ref {
		case "gh:src/upload.py#L1-L9":
			out = append(out, plugin.Result{Ref: r.Ref, OK: true, Title: "src/upload.py · L1–9", Snippet: &plugin.Snippet{StartLine: 1, Text: "<script>x</script>"}})
		default:
			out = append(out, plugin.Result{Ref: r.Ref, OK: true, Warnings: []plugin.Warning{{Text: "File changed in 3 commits since base_sha"}}})
		}
	}
	return out
}
