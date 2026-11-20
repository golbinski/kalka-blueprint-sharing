package blueprint

import (
	"strings"
	"testing"
	"time"
)

func TestIssueKey(t *testing.T) {
	for in, want := range map[string]string{
		"jira:DEV-123":                     "jira:DEV-123",
		" jira:DEV-123 ":                   "jira:DEV-123",
		"tracker:ABC_2-9":                  "tracker:ABC_2-9",
		"DEV-123":                          "",
		"https://x/DEV-1":                  "",
		"jira:":                            "",
		"jira:DEV 123":                     "",
		"Jira:DEV-1":                       "",
		"jira:DEV-1#frag":                  "",
		"jira:" + strings.Repeat("A", 200): "",
	} {
		r, ok := ParseIssue(in)
		if got := map[bool]string{true: r.String(), false: ""}[ok]; got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestIssueRules(t *testing.T) {
	rules := func(fm string) string {
		d, err := Parse([]byte("---\n" + fm + "\n---\n# Redelivery\n"))
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, p := range d.Problems() {
			if strings.HasPrefix(p.Rule, "F02") {
				out = append(out, p.Rule+":"+p.Severity.String())
			}
		}
		return strings.Join(out, ",")
	}
	for fm, want := range map[string]string{
		"kalka_issue: jira:DEV-123":            "",
		"kalka_issue: DEV-123":                 "F020:warning",
		"kalka_issue: tracker:DEV-123":         "F021:info",
		"kalka_archived: yes":                  "F022:warning",
		"kalka_archived: 2026-11-20T10:00:00Z": "",
	} {
		if got := rules(fm); got != want {
			t.Errorf("%s: %q, want %q", fm, got, want)
		}
	}
}

func TestArchive(t *testing.T) {
	d, _ := Parse([]byte("---\nrepo: org/x # keep\nkalka_issue: jira:DEV-1\n---\n# Redelivery\n"))
	at := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	if err := Archive(d, at); err != nil {
		t.Fatal(err)
	}
	if got, ok := d.Archived(); !ok || !got.Equal(at) {
		t.Fatal(got, ok)
	}
	if err := Archive(d, at); err == nil {
		t.Fatal("archived twice")
	}
	if err := Unarchive(d); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Archived(); ok || !strings.Contains(string(d.Bytes()), "repo: org/x # keep\nkalka_issue: jira:DEV-1\n") {
		t.Fatalf("%s", d.Bytes())
	}
	if err := Unarchive(d); err == nil {
		t.Fatal("unarchived twice")
	}
}

func TestExportedEntry(t *testing.T) {
	x := Exported{Issue: "jira:DEV-123", Version: 3, URL: "https://t.example/browse/DEV-123", AttachmentID: "10046", CommentID: "20311"}
	d, _ := Parse([]byte("---\nkalka_issue: jira:DEV-123\n---\n# Redelivery\n"))
	at := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	if _, err := Append(d, KindExported, "ewa@parcels.example", at, "", "", "", x.Body(), Signer{}); err != nil {
		t.Fatal(err)
	}
	x2 := x
	x2.Version, x2.AttachmentID = 4, "10050"
	if _, err := Append(d, KindExported, "ewa@parcels.example", at.Add(time.Hour), "", "", "", x2.Body(), Signer{}); err != nil {
		t.Fatal(err)
	}
	back, err := Parse(d.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	got, e, ok := LastExport(back, "jira:DEV-123", nil)
	if !ok || got != x2 || e == nil {
		t.Fatalf("%+v %v", got, ok)
	}
	if _, _, ok := LastExport(back, "jira:DEV-9", nil); ok {
		t.Fatal("found an export of another issue")
	}
	for _, bad := range []string{"exported: DEV-1 · v3", "exported: jira:DEV-1 · v0", "exported: jira:DEV-1 · v3\nurl: javascript:x", "exported: jira:DEV-1 · v3\ncomment: a b"} {
		e := &Entry{Kind: KindExported, Body: bad}
		if _, ok := e.Exported(); ok {
			t.Errorf("read %q", bad)
		}
	}
}
