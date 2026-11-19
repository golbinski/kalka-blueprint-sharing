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

// writeDomain writes version v of lending/loans with a glossary and
// invariants, as the live file or as a snapshot.
func writeDomain(t *testing.T, dir string, v int, snapshot bool, glossary, invariants string) {
	t.Helper()
	name := "lending-loans-domain.md"
	if snapshot {
		name = "lending-loans-domain@v" + string(rune('0'+v)) + ".md"
	}
	domainFile(t, filepath.Join(dir, name), "lending", v, "```glossary v1\n"+glossary+"```\n\n## Rules\n\n```invariants v1\ninvariants:\n"+invariants+"```")
}

const (
	glossV1 = "- term: \"loan\"\n  definition: \"One item lent to one member\"\n  aliases: [\"borrowing\"]\n- term: \"member\"\n  definition: \"Someone with a card\"\n"
	glossV2 = glossV1 + "  aliases: [\"patron\"]\n"
	invV1   = "  - id: I1\n    statement: \"A loan is renewed at most twice\"\n  - id: I3\n    statement: \"A member has at most ten loans\"\n"
	invV2   = "  - id: I1\n    statement: \"A loan is renewed at most twice\"\n"
)

func lendingFolder(t *testing.T) *Folder {
	dir := t.TempDir()
	writeDomain(t, dir, 1, true, glossV1, invV1)
	writeDomain(t, dir, 2, true, glossV2, invV2)
	writeDomain(t, dir, 2, false, glossV2, invV2)
	return NewFolder(dir)
}

func plan(t *testing.T, version, body string) *bp.Document {
	d, err := bp.Parse([]byte("---\nkalka_domain: lending/loans@" + version + "\n---\n# Renewals\n\n" + body))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func ruleList(ps []bp.Problem, prefixes string) string {
	var ids []string
	for _, p := range ps {
		if strings.ContainsRune(prefixes, rune(p.Rule[0])) {
			ids = append(ids, p.Rule)
		}
	}
	return strings.Join(ids, " ")
}

const guardsTrace = "```trace v1\nlanes: [business, code]\nnodes:\n  - id: B1\n    lane: business\n    text: \"A borrowing is renewed\"\n    guards: [\"loans#I1\", \"loans#I3\"]\n```\n"

func TestDomainInvariantsAndTerms(t *testing.T) {
	f := lendingFolder(t)
	body := "A member renews a Borrowing.\n\n" + guardsTrace
	r := Document(plan(t, "v1", body), Options{Domains: f})
	if got := ruleList(r.Problems, "GIR"); got != "R003 G020 G020 I040" {
		t.Errorf("v1: %q", got)
	}
	var fix *bp.Fix
	for _, p := range r.Problems {
		if p.Rule == "G020" && p.Fix != nil {
			fix = p.Fix
		}
	}
	if fix == nil || fix.Text != "Loan" || fix.Line != 6 || fix.Column != 19 || fix.EndColumn != 28 {
		t.Errorf("fix %+v", fix)
	}
	if r.Glossary.State != GlossaryHeld || r.Glossary.Ref != "lending/loans@v1" || len(r.Glossary.Terms) != 2 {
		t.Errorf("glossary %+v", r.Glossary)
	}

	// v2 has no I3: an error against v2 itself.
	r = Document(plan(t, "v2", body), Options{Domains: f})
	if got := ruleList(r.Problems, "GIR"); got != "G020 G020 I020" {
		t.Errorf("v2: %q", got)
	}
	// Not kept here: one R001, nothing else checked.
	r = Document(plan(t, "v7", body), Options{Domains: f})
	if got := ruleList(r.Problems, "GIR"); got != "R001" || r.Glossary.State != GlossaryNotKept {
		t.Errorf("v7: %q %s", got, r.Glossary.State)
	}
	// No domain reference at all: the guards cannot resolve.
	d, _ := bp.Parse([]byte("# Renewals\n\n" + guardsTrace))
	if got := ruleList(Document(d, Options{Domains: f}).Problems, "GIR"); got != "I020 I020" {
		t.Errorf("no reference: %q", got)
	}
}

// A blueprint that names no domain is linked and checked against its own
// glossary, as a domain document is against its (spec 6.16).
func TestOwnGlossary(t *testing.T) {
	src := "# Shelving\n\n```glossary v1\n- term: \"returns cart\"\n  definition: \"The trolley returned books wait on\"\n  aliases: [\"trolley\"]\n```\n\nEach book on the Trolley is labelled.\n"
	d, err := bp.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	r := Document(d, Options{})
	if r.Glossary.State != GlossaryHeld || !r.Glossary.Own || r.Glossary.Ref != "" || len(r.Glossary.Terms) != 1 {
		t.Fatalf("glossary %+v", r.Glossary)
	}
	var alias *bp.Problem
	for i, p := range r.Problems {
		if p.Rule == "G008" {
			t.Errorf("G008 in a blueprint with no domain: %s", p.Message)
		}
		if p.Rule == "G020" {
			alias = &r.Problems[i]
		}
	}
	if alias == nil || !strings.Contains(alias.Message, "this blueprint's glossary") || alias.Fix == nil || alias.Fix.Text != "Returns cart" {
		t.Errorf("alias %+v", alias)
	}
	// Naming a domain, the blueprint's own block is code and links nothing.
	d, _ = bp.Parse([]byte("---\nkalka_domain: lending/loans@v1\n---\n" + src))
	r = Document(d, Options{Domains: lendingFolder(t)})
	if r.Glossary.Own || !strings.Contains(ruleList(r.Problems, "G"), "G008") || strings.Contains(ruleList(r.Problems, "G"), "G020") {
		t.Errorf("with a domain: %+v %q", r.Glossary, ruleList(r.Problems, "G"))
	}
}

func TestMatrixAndImpact(t *testing.T) {
	f := lendingFolder(t)
	p := plan(t, "v1", "## Plan\n\n```invariants v1\naffects: [\"loans#I3\"]\ninvariants:\n  - id: N1\n    statement: \"A renewal is logged\"\n```\n\nThe member returns the borrowing.\n\n"+guardsTrace)
	r := Document(p, Options{Domains: f})
	rows := Matrix(p, r.Domain)
	var got []string
	for _, row := range rows {
		got = append(got, row.Ref+"/"+strings.Join(row.Gaps, ",")+"/"+itoa(len(row.GuardedBy))+"/"+row.Statement)
	}
	want := "N1/unguarded,unverified/0/A renewal is logged loans#I3//1/A member has at most ten loans loans#I1//1/A loan is renewed at most twice"
	if strings.Join(got, " ") != want {
		t.Errorf("matrix %v", got)
	}

	// The owner of loans, releasing v3 from a text without I1 and with
	// "returning" as a new alias of loan.
	dom := f.Version("lending", "loans", 2)
	next, _ := bp.Parse([]byte(strings.Replace(strings.Replace(string(dom.Bytes()), "aliases: [\"borrowing\"]", "aliases: [\"borrowing\", \"returns\"]", 1), "  - id: I1\n    statement: \"A loan is renewed at most twice\"\n", "  - id: I2\n    statement: \"x\"\n", 1)))
	imp := Impact(next, map[string]*bp.Document{"renewals.md": p}, f)
	if len(imp) != 1 || ruleList(imp[0].Problems, "GI") != "I040 I040 G030" {
		t.Fatalf("impact %+v", imp)
	}
	rows = Matrix(dom, nil)
	ReferencedBy(rows, dom, map[string]*bp.Document{"renewals.md": p})
	if len(rows) != 1 || len(rows[0].ReferencedBy) != 1 {
		t.Errorf("referenced by %+v", rows)
	}
}

func itoa(n int) string { return string(rune(0x30 + n)) }
