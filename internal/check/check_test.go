package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

func domainFile(t *testing.T, path, team string, v int, text string) {
	t.Helper()
	fm := "---\nkalka_kind: domain\nkalka_domain: loans\n"
	if team != "" {
		fm += "kalka_team: " + team + "\n"
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(fm+"kalka_version: "+string(rune('0'+v))+"\n---\n# Loans\n\n"+text+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A version is its snapshot first, then a live document of that version;
// a document naming no team serves any team (spec 12.4, 18.2).
func TestFolderVersions(t *testing.T) {
	dir := t.TempDir()
	domainFile(t, filepath.Join(dir, "lending-loans-domain.md"), "lending", 2, "live two")
	domainFile(t, filepath.Join(dir, "lending-loans-domain@v1.md"), "lending", 1, "kept one")
	domainFile(t, filepath.Join(dir, "lending-loans-domain@v2.md"), "lending", 2, "kept two")
	f := NewFolder(dir)
	if got := f.Latest("lending", "loans"); got != 2 {
		t.Fatalf("latest %d", got)
	}
	if d := f.Version("lending", "loans", 2); d == nil || !strings.Contains(d.Body(), "kept two") {
		t.Fatal("v2 is not the snapshot")
	}
	if f.Version("lending", "loans", 3) != nil || f.Version("desk", "loans", 1) != nil {
		t.Fatal("found what is not there")
	}

	repo := t.TempDir()
	domainFile(t, filepath.Join(repo, "docs", "loans.md"), "", 4, "repository copy")
	g := NewFolder(filepath.Join(repo, "docs", "loans.md"))
	if d := g.Version("anyone", "loans", 4); d == nil {
		t.Fatal("a document with no team does not serve")
	}
}

func TestDomainRules(t *testing.T) {
	dir := t.TempDir()
	domainFile(t, filepath.Join(dir, "lending-loans-domain.md"), "lending", 2, "two")
	f := NewFolder(dir)
	rules := func(fm string, ds Domains) string {
		d, err := bp.Parse([]byte("---\n" + fm + "\n---\n# Holds\n"))
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, p := range Document(d, Options{Domains: ds}).Problems {
			if p.Rule[0] == 'R' {
				ids = append(ids, p.Rule)
			}
		}
		return strings.Join(ids, " ")
	}
	for _, c := range []struct {
		fm   string
		ds   Domains
		want string
	}{
		{"kalka_domain: lending/loans@v2", f, ""},
		{"kalka_domain: lending/loans@v1", f, "R001 R003"},
		{"kalka_domain: lending/loans@v2", nil, "R001"},
		{"kalka_domain: loans", f, "R002"},
		{"kalka_title: no domain", f, ""},
	} {
		if got := rules(c.fm, c.ds); got != c.want {
			t.Errorf("%s: %q, want %q", c.fm, got, c.want)
		}
	}
}
