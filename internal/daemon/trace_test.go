package daemon

import (
	"context"
	"strings"
	"testing"

	bp "github.com/golbinski/kalka/internal/blueprint"
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

	// With a plugin: resolved refs verify; a warning makes a step stale.
	alice.d.opts.Resolver = func(_ context.Context, doc *bp.Document, refs []string) map[string]RefView {
		if doc.Title() != "Import pipeline refactor" || len(refs) != 2 {
			t.Errorf("resolver got %v", refs)
		}
		return map[string]RefView{
			"gh:src/upload.py#L1-L9": {OK: true, Title: "src/upload.py · L1–9", Snippet: &Snippet{StartLine: 1, Text: "<script>x</script>"}},
			"gh:src/notify.py":       {OK: true, Warnings: []RefWarning{{Text: "File changed in 3 commits since base_sha"}}},
		}
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
	if r := v.Refs["gh:src/upload.py#L1-L9"]; !r.OK || r.Snippet.Text != "<script>x</script>" || r.Ref == "" {
		t.Fatalf("resolved ref %+v", r)
	}
}
