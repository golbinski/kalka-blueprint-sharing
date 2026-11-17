package blueprint

import (
	"strings"
	"testing"
	"time"
)

func TestSections(t *testing.T) {
	src := "---\nkalka_owner: a@b.co\n---\nIntro line.\n\n# Title\n\nText\n\nSetext\n------\n\n> ## Quoted\n\n```\n# not a heading\n```\n\n## Review log\n\n### comment · a@b.co · 2026-11-12T10:00:00Z · §title · rev:00000000 · id:00000000000000000000000000000000\nx\n\n## After\n\ntail\n"
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	ss := d.Sections()
	if len(ss) != 4 {
		t.Fatalf("%d sections: %+v", len(ss), ss)
	}
	if ss[0].Heading != nil || ss[0].Markdown != "Intro line.\n\n" || ss[0].Line != 4 {
		t.Fatalf("lead %+v", ss[0])
	}
	if ss[1].Heading.Slug != "title" || !strings.HasPrefix(ss[1].Markdown, "# Title\n") {
		t.Fatalf("title %+v", ss[1])
	}
	s := ss[2]
	if s.Heading.Slug != "setext" || !strings.HasPrefix(s.Markdown, "Setext\n------\n") || len(s.Slugs) != 2 || s.Slugs[1] != "quoted" || !strings.Contains(s.Markdown, "# not a heading") {
		t.Fatalf("setext %+v", s)
	}
	if strings.Contains(s.Markdown, "Review log") || ss[3].Heading.Slug != "after" || ss[3].Markdown != "## After\n\ntail\n" {
		t.Fatalf("log leaked or tail wrong: %+v", ss[2:])
	}
}

func TestAppend(t *testing.T) {
	d, _ := Parse([]byte("# T\n\n## Plan\nP\n"))
	at := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	if _, err := Append(d, KindComment, "a@b.co", at, "nope", "", "", "x", Signer{}); err == nil {
		t.Fatal("unknown anchor accepted")
	}
	e1, err := Append(d, KindComment, "a@b.co", at, "§plan", "", "P\nand more", "Why?", Signer{})
	if err != nil {
		t.Fatal(err)
	}
	if e1.Quote() != "P\nand more" || e1.Anchor != "plan" {
		t.Fatalf("quote %q anchor %q", e1.Quote(), e1.Anchor)
	}
	e2, err := Append(d, KindDeviation, "b@b.co", at.Add(time.Second), "plan", e1.ID, "", "Because", Signer{})
	if err != nil || e2.ReplyTo() != e1.ID {
		t.Fatal(e2, err)
	}
	if _, err := Append(d, KindComment, "a@b.co", at, "", "00000000000000000000000000000000", "", "x", Signer{}); err == nil {
		t.Fatal("unknown reply target accepted")
	}
	r, err := Parse(d.Bytes())
	if err != nil || r.HasErrors() || len(r.Items()) != 2 {
		t.Fatalf("%v %+v", err, r.Problems())
	}
}
