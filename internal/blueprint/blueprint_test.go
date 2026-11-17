package blueprint

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 11, 17, 40, 3, 0, time.UTC)

func mustParse(t *testing.T, s string) *Document {
	t.Helper()
	d, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// fixIDs rewrites every entry id in s to the id its content hashes to, so
// hand-written fixtures hold valid entries.
func fixIDs(t *testing.T, s string) string {
	t.Helper()
	for _, it := range mustParse(t, s).Items() {
		s = strings.Replace(s, "id:"+it.Entry.ID, "id:"+it.Entry.ComputeID(), 1)
	}
	return s
}

func codes(d *Document) map[string]int {
	m := map[string]int{}
	for _, p := range d.Problems() {
		m[p.Rule]++
	}
	return m
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"\uFEFFa\r\nb  \r\n":    "a\nb",
		"\n\n  \na\t \rb\n\n\n": "a\nb",
		"a\n\n\nb":              "a\n\n\nb",
		"":                      "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlugger(t *testing.T) {
	s := NewSlugger()
	in := []string{"Plan", "Plan", "Plan-1", "Plan", "What's new? (v2)", "Zażółć gęślą", "snake_case name", "  Spaces  "}
	want := []string{"plan", "plan-1", "plan-1-1", "plan-2", "whats-new-v2", "zażółć-gęślą", "snake_case-name", "--spaces--"}
	for i, h := range in {
		if got := s.Slug(h); got != want[i] {
			t.Errorf("Slug(%q) = %q, want %q", h, got, want[i])
		}
	}
}

func TestParseHeader(t *testing.T) {
	ok := []string{
		"### comment · bob@corp.com · 2026-11-11T17:40:03Z · §plan · rev:3fa9c1d2 · id:7c2e91ab04f300000000000000000000",
		"### handoff · alice@corp.com · 2026-11-12T10:02:11Z · § · rev:3fa9c1d2 · id:19d0c3e2b7aa00000000000000000000\r\n",
		"### my-kind · a@b · 2026-11-11T17:40:03Z · §import-pipeline#C2 · rev:3fa9c1d2 · id:7c2e91ab04f300000000000000000000  ",
	}
	for _, l := range ok {
		if _, err := ParseHeader(l); err != nil {
			t.Errorf("ParseHeader(%q): %v", l, err)
		}
	}
	bad := []string{
		"### comment - bob@corp.com - 2026-11-11T17:40:03Z - §plan - rev:3fa9c1d2 - id:7c2e91ab04f300000000000000000000",
		"### Comment · bob@corp.com · 2026-11-11T17:40:03Z · §plan · rev:3fa9c1d2 · id:7c2e91ab04f300000000000000000000",
		"### comment · bob · 2026-11-11T17:40:03Z · §plan · rev:3fa9c1d2 · id:7c2e91ab04f300000000000000000000",
		"### comment · bob@corp.com · 2026-11-11T17:40:03+01:00 · §plan · rev:3fa9c1d2 · id:7c2e91ab04f300000000000000000000",
		"### comment · bob@corp.com · 2026-11-11T17:40:03Z · plan · rev:3fa9c1d2 · id:7c2e91ab04f300000000000000000000",
		"### comment · bob@corp.com · 2026-11-11T17:40:03Z · §Plan · rev:3fa9c1d2 · id:7c2e91ab04f300000000000000000000",
		"### comment · bob@corp.com · 2026-11-11T17:40:03Z · §plan · rev:3fa9c1d · id:7c2e91ab04f300000000000000000000",
		"### comment · bob@corp.com · 2026-13-11T17:40:03Z · §plan · rev:3fa9c1d2 · id:7c2e91ab04f300000000000000000000",
	}
	for _, l := range bad {
		if _, err := ParseHeader(l); err == nil {
			t.Errorf("ParseHeader(%q) accepted a bad header", l)
		}
	}
}

func TestEntryRoundTrip(t *testing.T) {
	e, err := NewEntry(KindComment, "bob@corp.com", t0, "plan", "3fa9c1d2", "re: 7c2e91ab04f300000000000000000000\n> the retry path\nMissing retry.\n")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseHeader(e.Header())
	if err != nil {
		t.Fatal(err)
	}
	got.Body = e.Body
	if !got.Valid() || got.ID != e.ID {
		t.Fatalf("entry does not survive its own header: %+v", got)
	}
	if e.ReplyTo() != "7c2e91ab04f300000000000000000000" || e.Quote() != "the retry path" {
		t.Errorf("ReplyTo=%q Quote=%q", e.ReplyTo(), e.Quote())
	}
}

func TestPrepareBody(t *testing.T) {
	got, err := PrepareBody("\n# Big\nSub\n---\ntext\n```\n# not a heading\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "#### Big\n#### Sub\ntext\n```\n# not a heading\n```"
	if got != want {
		t.Errorf("PrepareBody = %q, want %q", got, want)
	}
	if _, err := PrepareBody(" \n\n"); err == nil {
		t.Error("empty body accepted")
	}
	if _, err := PrepareBody(strings.Repeat("x", MaxEntryBody+1)); err == nil {
		t.Error("oversized body accepted")
	}
}

func TestEntrySignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	id := "6f1c2a0e-8d1b-4c57-9a43-0e2b7c9d51aa"
	e, err := NewEntry(KindComment, "alice@corp.com", t0, "", "3fa9c1d2", "Looks right.")
	if err != nil {
		t.Fatal(err)
	}
	e.Sign(id, Signer{Fn: func(m []byte) []byte { return ed25519.Sign(priv, m) }})
	if !e.VerifySig(id, pub) {
		t.Fatal("signature does not verify")
	}
	if e.VerifySig("00000000-0000-4000-8000-000000000000", pub) {
		t.Fatal("signature verifies for another blueprint")
	}
	h, err := ParseHeader(e.Header())
	if err != nil || h.Sig != e.Sig {
		t.Fatalf("sig lost in header round trip: %v", err)
	}
}

const sample = `---
kalka_id: 6f1c2a0e-8d1b-4c57-9a43-0e2b7c9d51aa
kalka_owner: alice@corp.com
repo: org/doc-service   # opaque
---

# Import pipeline refactor

## Context
Some context.

## Plan
The plan.

` + "```" + `
## Review log
not a heading, inside a fence
` + "```" + `

## Review log

### comment · bob@corp.com · 2026-11-11T17:40:03Z · §plan · rev:3fa9c1d2 · id:7c2e91ab04f300000000000000000000
Doesn't handle the retry path.
`

func TestParseStructure(t *testing.T) {
	d := mustParse(t, sample)
	if d.KalkaID() != "6f1c2a0e-8d1b-4c57-9a43-0e2b7c9d51aa" || d.Owner() != "alice@corp.com" {
		t.Fatalf("id=%q owner=%q", d.KalkaID(), d.Owner())
	}
	if d.Title() != "Import pipeline refactor" {
		t.Errorf("title %q", d.Title())
	}
	var slugs []string
	for _, h := range d.Headings() {
		slugs = append(slugs, h.Slug)
	}
	if got := strings.Join(slugs, ","); got != "import-pipeline-refactor,context,plan" {
		t.Errorf("slugs %s", got)
	}
	if len(d.Items()) != 1 || d.Items()[0].Entry.Author != "bob@corp.com" {
		t.Fatalf("items %+v", d.Items())
	}
	if string(d.Bytes()) != sample {
		t.Error("unmodified document does not round trip")
	}
	if !strings.Contains(d.Body(), "not a heading, inside a fence") {
		t.Error("fenced pseudo-heading taken as the log")
	}
}

func TestAddEntryCreatesLog(t *testing.T) {
	d := mustParse(t, "# Title\r\n\r\n## Plan\r\nText\r\n")
	e, _ := NewEntry(KindComment, "bob@corp.com", t0, "plan", d.Rev(), "one\ntwo")
	if err := d.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	want := "# Title\r\n\r\n## Plan\r\nText\r\n\r\n## Review log\r\n\r\n" + e.Header() + "\r\none\r\ntwo\r\n"
	if got := string(d.Bytes()); got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	d2 := mustParse(t, want)
	if len(d2.Items()) != 1 || !d2.Items()[0].Entry.Valid() || d2.Rev() != d.Rev() {
		t.Fatal("added entry does not parse back")
	}
	if codes(d2)["B016"] != 0 {
		t.Error("new entry is outdated")
	}
}

func TestSetFrontmatterKeepsOtherLines(t *testing.T) {
	src := "---\n# a comment\nrepo: x   # keep\nnested:\n  owner: not-me\nlist: [1, 2]\n---\nbody\n"
	d := mustParse(t, src)
	if d.Owner() != "" {
		t.Fatalf("nested owner taken as top-level: %q", d.Owner())
	}
	d.SetFrontmatter("kalka_id", "6f1c2a0e-8d1b-4c57-9a43-0e2b7c9d51aa")
	d.SetFrontmatter("kalka_owner", "alice@corp.com")
	want := "---\nkalka_id: 6f1c2a0e-8d1b-4c57-9a43-0e2b7c9d51aa\nkalka_owner: alice@corp.com\n# a comment\nrepo: x   # keep\nnested:\n  owner: not-me\nlist: [1, 2]\n---\nbody\n"
	if got := string(d.Bytes()); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}

	none := mustParse(t, "body only\n")
	none.SetFrontmatter("kalka_owner", "alice@corp.com")
	if got := string(none.Bytes()); got != "---\nkalka_owner: alice@corp.com\n---\nbody only\n" {
		t.Fatalf("created frontmatter: %q", got)
	}
}

func TestProblems(t *testing.T) {
	src := fixIDs(t, sample) + "\n### comment · bob@corp.com · 2026-11-11T17:40:03Z · §gone · rev:3fa9c1d2 · id:00000000000000000000000000000000\nTampered.\n\n### not an entry\nloose text\n"
	c := codes(mustParse(t, src))
	for _, want := range []string{"B010", "B003", "B016"} {
		if c[want] == 0 {
			t.Errorf("missing problem %s in %v", want, c)
		}
	}
	if c := codes(mustParse(t, "---\nkalka_id: nope\n---\n")); c["F002"] == 0 || c["F003"] == 0 {
		t.Errorf("frontmatter problems not found: %v", c)
	}
	if c := codes(mustParse(t, "---\nkalka_owner: a@b\nno end\n")); c["F001"] == 0 {
		t.Errorf("unterminated frontmatter not reported: %v", c)
	}
	if _, err := Parse([]byte{0xff, 0xfe}); err != ErrNotUTF8 {
		t.Errorf("invalid UTF-8: %v", err)
	}
}

func TestSectionsAfterLog(t *testing.T) {
	src := "# T\n\n## Plan\nP\n\n## Review log\n\n## Appendix\nA\n"
	d := mustParse(t, src)
	if !d.HasSection("appendix") || d.Body() != "# T\n\n## Plan\nP\n\n## Appendix\nA" {
		t.Fatalf("body %q", d.Body())
	}
	e, _ := NewEntry(KindComment, "bob@corp.com", t0, "appendix", d.Rev(), "on the appendix")
	if err := d.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	d2 := mustParse(t, string(d.Bytes()))
	if d2.Rev() != d.Rev() || len(d2.Items()) != 1 || !d2.HasSection("appendix") {
		t.Fatalf("after adding:\n%s", d.Bytes())
	}
}

func FuzzParse(f *testing.F) {
	f.Add(sample)
	f.Add("---\r\nkalka_owner: a@b\r\n---\r\n## Review log\r\n### x\r\n")
	f.Add("# T\n\n## Plan\n\n" + specTrace + "\n- x\n\n  ```trace v1\n  lanes: [a, b]\n  nodes:\n    - id: X\n      lane: a\n      text: \"t\"\n      back: [X]\n  ```\n")
	f.Fuzz(func(t *testing.T, s string) {
		d, err := Parse([]byte(s))
		if err != nil {
			return
		}
		if string(d.Bytes()) != s {
			t.Fatal("round trip changed bytes")
		}
		// Adding an entry must never change the plan or lose an entry.
		for _, p := range d.Problems() {
			if p.Rule == "B013" || p.Rule == "F001" || p.Rule == "B001" {
				return
			}
		}
		rev, plan, n := d.Rev(), d.Plan(), len(d.Items())
		e, err := NewEntry(KindComment, "a@b.c", t0, "", rev, "fuzz")
		if err != nil {
			t.Fatal(err)
		}
		if err := d.AddEntry(e); err != nil {
			t.Fatal(err)
		}
		d2, err := d.Reparse()
		if err == ErrTooLarge {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if d2.Plan() != plan {
			t.Fatalf("adding an entry changed the plan\nbefore %q\nafter  %q\nfile %q", plan, d2.Plan(), d.Bytes())
		}
		if len(d2.Items()) != n+1 {
			t.Fatalf("items %d -> %d\nfile %q", n, len(d2.Items()), d.Bytes())
		}
		found := false
		for _, it := range d2.Items() {
			found = found || (it.Entry.ID == e.ID && it.Entry.Valid())
		}
		if !found {
			t.Fatalf("new entry not valid after reparse\nfile %q", d.Bytes())
		}
	})
}

// A header may carry name:value fields a later kalka adds, between id and
// sig. They are kept as written and are part of the id input, so editing
// one invalidates the entry and a signature covers them.
func TestHeaderExtensionFields(t *testing.T) {
	e, err := NewEntry(KindComment, "bob@library.example", t0, "plan", "3fa9c1d2", "Missing retry.")
	if err != nil {
		t.Fatal(err)
	}
	plain := e.ID
	e.Ext = []Field{{"term", "4be1c0ffee01"}, {"x-tool", "review-bot"}}
	e.ID = e.ComputeID()
	if e.ID == plain || len(e.ID) != IDLen {
		t.Fatalf("id %s, without fields %s", e.ID, plain)
	}
	h := e.Header() + Sep + "sig:" + strings.Repeat("A", 86)
	got, err := ParseHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	got.Body = e.Body
	if v, ok := got.ExtValue("term"); !ok || v != "4be1c0ffee01" || len(got.Ext) != 2 || !got.Valid() || got.Sig == "" {
		t.Fatalf("parsed %+v", got)
	}
	got.Ext[1].Value = "other"
	if got.Valid() {
		t.Fatal("an edited field leaves the id valid")
	}
	for _, bad := range []string{
		e.Header() + Sep + "nocolon",
		e.Header() + Sep + "id:" + strings.Repeat("0", IDLen),
		e.Header() + Sep + "sig:" + strings.Repeat("A", 86) + Sep + "late:field",
		e.Header() + strings.Repeat(Sep+"a:b", MaxExt+1),
	} {
		if _, err := ParseHeader(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// A release line may end with name:value tokens a later kalka adds.
func TestReleaseExtensionTokens(t *testing.T) {
	plan := strings.Repeat("a", 64)
	for line, ok := range map[string]bool{
		"v2 · plan:" + plan + " · owner:dana@example.org":                         true,
		"v2 · plan:" + plan + " · owner:dana@example.org · key:abcd":              true,
		"v2 · plan:" + plan + " · owner:dana@example.org · deleted · moved:a/b-1": true,
		"v2 · plan:" + plan + " · owner:dana@example.org · deleted · key:abcd":    true,
		"v2 · plan:" + plan + " · owner:dana@example.org · Key:abcd":              false,
		"v2 · plan:" + plan + " · owner:dana@example.org · nonsense":              false,
	} {
		e := &Entry{Kind: KindRelease, Body: line}
		r, got := e.Release()
		if got != ok || ok && (r.Version != 2 || r.Owner != "dana@example.org") {
			t.Errorf("%q: %+v %v", line, r, got)
		}
	}
}

// The review log starts at the first line "## Review log" outside a code
// fence, whatever a Markdown parser makes of the lines around it.
func TestLogHeadingLineRule(t *testing.T) {
	entry := "### comment · a@b.co · 2026-11-12T10:00:00Z · § · rev:00000000 · id:" + strings.Repeat("0", IDLen) + "\nx\n"
	for name, tc := range map[string]struct {
		src string
		log bool
	}{
		"plain":          {"# T\n\n## Review log\n\n" + entry, true},
		"case, spaces":   {"# T\n\n##   review LOG  \n\n" + entry, true},
		"after html":     {"# T\n<div>\n## Review log\n" + entry, true},
		"in a fence":     {"# T\n\n```\n## Review log\n```\n", false},
		"indented":       {"# T\n\n ## Review log\n", false},
		"setext":         {"# T\n\nReview log\n----------\n", false},
		"quoted":         {"# T\n\n> ## Review log\n", false},
		"other text":     {"# T\n\n## Review logs\n", false},
		"closing hashes": {"# T\n\n## Review log ##\n", false},
	} {
		d := mustParse(t, tc.src)
		if d.hasLog != tc.log {
			t.Errorf("%s: log %v", name, d.hasLog)
		}
	}
}

func TestAnchorCharacters(t *testing.T) {
	for a, ok := range map[string]bool{"plan": true, "zażółć-gęślą": true, "snake_case": true, "step-1#C2": true, "Plan": false, "a b": false, "a.b": false, "a b": false, "a—b": false} {
		if got := validAnchor(a) == nil; got != ok {
			t.Errorf("%q: %v", a, got)
		}
	}
}
