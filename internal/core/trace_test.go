package core

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/plugin"
)

const tracePlan = "# Import pipeline refactor\n\n## Plan\n\n```trace v2\n" + `lane business
step B1: Receive files
  next B2
step B2: Notify

lane code
step C1: Upload handler
  realizes B1
  ref gh:src/upload.py#L1-L9
  next C2
step C2: Skip large files
  ref https://example.com/x
  why the parser runs out of memory
  next C3
step C3: Notify in batches
  realizes B2
  ref gh:src/notify.py
` + "```\n"

func findEntry(v *DocView, anchor string) *EntryView {
	for i := range v.Entries {
		if v.Entries[i].Anchor == anchor {
			return &v.Entries[i]
		}
	}
	return nil
}

// A comment on a trace node round-trips through replication and becomes
// orphaned when the owner removes the node.
func TestNodeCommentRoundTrip(t *testing.T) {
	_, ns := teamOfN(t, "alice@library.example", "bob@library.example")
	alice, bob := ns[0], ns[1]
	alice.write("plan.md", tracePlan)
	waitIndexed(t, alice, 1)
	id := alice.share("plan.md", bob)
	bob.comment(bob.rel(id), "plan#C2", "Why 20 MB?")
	bob.replicate(alice)

	ctx := context.Background()
	name := alice.rel(id)
	v, err := alice.d.Document(ctx, name)
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

	// The owner renames the node and releases the revision.
	src := alice.read(name)
	src = strings.Replace(src, "step C2:", "step C9:", 1)
	src = strings.Replace(src, "next C2\n", "next C9\n", 1)
	alice.write(name, src)
	waitStored(t, alice, alice.path(name), "step C9:")
	alice.share(name, bob)
	v, _ = bob.d.Document(ctx, bob.rel(id))
	if e := findEntry(v, "plan#C2"); e == nil || !e.Orphaned {
		t.Fatalf("not orphaned: %+v", e)
	}
	found := false
	for _, p := range v.Problems {
		found = found || p.Rule == "B015"
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
	for deadline := time.After(5 * time.Second); ; {
		select {
		case e := <-events:
			if e.Type != "refs" {
				continue // presence and status events may come first
			}
		case <-deadline:
			t.Fatal("no refs event")
		}
		break
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

// A ref that does not resolve is unverified in any lane; a new step needs
// no ref, and a removed step takes no part.
func TestTraceViewChange(t *testing.T) {
	w := newWorld(t)
	alice := w.node("alice@library.example")
	alice.write("plan.md", "# Renewals\n\n## Plan\n\n```trace v2\n"+`lane business
step B1: A member renews a loan
  ref https://example.com/policy

lane code
step C1: Extend the due date
  realizes B1
  change new
  next C2
step C2: Old renewal form
  why replaced by the API
  ref gh:src/form.py
  change removed
`+"```\n")
	v, err := alice.d.Document(context.Background(), "plan.md")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range v.Traces[0].Divergences {
		got = append(got, d.Class+":"+d.Node+":"+d.Detail)
	}
	if want := "unverified:B1:not a plugin reference"; strings.Join(got, "|") != want {
		t.Fatalf("divergences %q, want %q", got, want)
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

func (f *fakePlugins) Installed() []string { return []string{"gh"} }
func (f *fakePlugins) Forget(string)       {}

func (f *fakePlugins) Capabilities(context.Context, string) (*plugin.Description, error) {
	return &plugin.Description{Protocol: plugin.Protocol, Name: "github", Capabilities: plugin.Capabilities{Resolve: true}}, nil
}

func (f *fakePlugins) IssueMeta(context.Context, string, []string) ([]plugin.IssueMeta, error) {
	return nil, errors.New("not declared")
}

func (f *fakePlugins) Export(context.Context, string, plugin.ExportRequest) (*plugin.ExportResult, error) {
	return nil, errors.New("not declared")
}
