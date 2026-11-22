package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// node writes a domain node of the team lending: its name, its parent pin
// ("" for the root), its version and its typed blocks.
func node(t *testing.T, dir, file, name, parent string, v int, body string) {
	t.Helper()
	fm := "---\nkalka_kind: domain\nkalka_domain: " + name + "\nkalka_team: lending\n"
	if parent != "" {
		fm += "kalka_parent: " + parent + "\n"
	}
	fm += "kalka_version: " + string(rune('0'+v)) + "\n---\n# " + name + "\n\n"
	if err := os.WriteFile(filepath.Join(dir, file), []byte(fm+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gloss(body string) string { return "```glossary v2\n" + body + "```\n\n" }
func invs(body string) string  { return "```invariants v2\n" + body + "```\n\n" }

const libraryRoot = "term member: A person registered with the library\n  alias patron\nterm loan: One copy lent to one member\n  form loans\nterm copy: One physical item of a title\n  form copies\n"

// library is a domain tree: library@v1 and v2, reservations on v2, fines
// on v1, and the given extra nodes.
func library(t *testing.T, extra func(dir string)) *Folder {
	dir := t.TempDir()
	libDir[t.Name()] = dir
	node(t, dir, "lending-library-domain@v1.md", "library", "", 1, gloss(libraryRoot)+invs("invariant L1: A member has at most ten loans\n"))
	node(t, dir, "lending-library-domain@v2.md", "library", "", 2, gloss(libraryRoot)+invs("invariant L1: A member has at most ten loans\n"))
	node(t, dir, "lending-library-domain.md", "library", "", 2, gloss(libraryRoot)+invs("invariant L1: A member has at most ten loans\n"))
	node(t, dir, "lending-library.reservations-domain.md", "library.reservations", "lending/library@v2", 1,
		gloss("term hold: A copy kept at the desk for the member first in the queue\nterm queue: The reservations of one title, in order\n  see member\n")+
			invs("invariant R1: A hold waits seven days\ninvariant R2: A loan of a copy whose title has a queue cannot be renewed\n  refines loan\n"))
	node(t, dir, "lending-library.fines-domain.md", "library.fines", "lending/library@v1", 1,
		gloss("term hold: A block on a member's card until a fine is paid\n"))
	if extra != nil {
		extra(dir)
	}
	return NewFolder(dir)
}

func checkNode(t *testing.T, f *Folder, file, prefixes string) (string, Result) {
	t.Helper()
	d := f.byPath(t, file)
	r := Document(d, Options{Domains: f.Tree(), Docs: f})
	return ruleList(r.Problems, prefixes), r
}

// libDir is the folder library last wrote, by test.
var libDir = map[string]string{}

// byPath parses a file of the folder.
func (f *Folder) byPath(t *testing.T, file string) *bp.Document {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(libDir[t.Name()], file))
	if err != nil {
		t.Fatal(err)
	}
	d, err := bp.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// A subdomain inherits its ancestors' terms and invariants, and its own
// rules run against them (spec 6.12).
func TestDomainTree(t *testing.T) {
	f := library(t, nil)
	got, r := checkNode(t, f, "lending-library.reservations-domain.md", "DGR")
	if got != "" {
		t.Errorf("reservations: %q", got)
	}
	// The glossary is the whole path: root terms first, then the node's.
	var names []string
	for _, x := range r.Glossary.Terms {
		names = append(names, x.Domain+"#"+x.Term)
	}
	if strings.Join(names, " ") != "library#member library#loan library#copy library.reservations#hold library.reservations#queue" {
		t.Errorf("terms %v", names)
	}
	// R2 narrows the inherited loan: shown on the term.
	for _, x := range r.Glossary.Terms {
		if x.Term == "loan" && (len(x.Refined) != 1 || x.Refined[0].ID != "R2" || x.Refined[0].Domain != "library.reservations") {
			t.Errorf("loan refined %+v", x.Refined)
		}
	}
	// fines pins library@v1 while v2 is out: parent newer, information.
	if got, _ := checkNode(t, f, "lending-library.fines-domain.md", "D"); got != "D030" {
		t.Errorf("fines: %q", got)
	}
}

func TestDomainTreeRules(t *testing.T) {
	for _, c := range []struct {
		name, file, dom, parent, body, want string
	}{
		{"redefines an inherited term", "a.md", "library.desk", "lending/library@v2", gloss("term loan: Something else\n"), "D010"},
		{"an inherited alias as a term", "a.md", "library.desk", "lending/library@v2", gloss("term patron: A sponsor\n"), "D010"},
		{"narrows without refines", "a.md", "library.desk", "lending/library@v2", invs("invariant K1: A loan lasts at most three weeks\n"), "D011"},
		{"own term, no refines needed", "a.md", "library.desk", "lending/library@v2", gloss("term desk: Where loans start\n") + invs("invariant K1: A loan starts at a desk\n"), ""},
		{"refines nothing on the path", "a.md", "library.desk", "lending/library@v2", invs("invariant K1: A loan lasts at most three weeks\n  refines fine\n"), "D012"},
		{"see an inherited term", "a.md", "library.desk", "lending/library@v2", gloss("term desk: Where a member is served\n  see member\n"), ""},
		{"see nothing", "a.md", "library.desk", "lending/library@v2", gloss("term desk: Where a member is served\n  see fine\n"), "G016"},
		{"not under its parent", "a.md", "desk", "lending/library@v2", gloss("term desk: x\n"), "D001"},
		{"parent version not here", "a.md", "library.desk", "lending/library@v7", gloss("term desk: x\n"), "D002"},
		{"same as a sibling", "a.md", "library.desk", "lending/library@v2", gloss("term queue: The reservations of one title, in order\n"), "D031"},
	} {
		f := library(t, func(dir string) { node(t, dir, c.file, c.dom, c.parent, 1, c.body) })
		if got, _ := checkNode(t, f, c.file, "DG"); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	// A cycle of pins.
	f := library(t, func(dir string) {
		node(t, dir, "x.md", "library.x", "lending/library.x.y@v1", 1, "")
		node(t, dir, "y.md", "library.x.y", "lending/library.x@v1", 1, "")
	})
	if got, _ := checkNode(t, f, "x.md", "D"); !strings.Contains(got, "D003") {
		t.Errorf("cycle: %q", got)
	}
}

func blueprintIn(t *testing.T, fm, body string) *bp.Document {
	t.Helper()
	d, err := bp.Parse([]byte("---\n" + fm + "\n---\n# Plan\n\n" + body))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// A plan on a subdomain is linked against the whole path; on two
// branches, a word they define differently is written qualified.
func TestPlanOnTheTree(t *testing.T) {
	f := library(t, nil)
	o := Options{Domains: f.Tree(), Docs: f}
	d := blueprintIn(t, "kalka_domain: lending/library.reservations@v1", "A patron's hold.\n\n"+invs("affects library#L1\naffects library.reservations#R1\n"))
	if got := ruleList(Document(d, o).Problems, "GIDR"); got != "G020" {
		t.Errorf("one branch: %q", got)
	}
	// An invariant of a domain off the path.
	d = blueprintIn(t, "kalka_domain: lending/library.reservations@v1", invs("affects library.fines#F1\n"))
	if got := ruleList(Document(d, o).Problems, "I"); got != "I020" {
		t.Errorf("off the path: %q", got)
	}
	// Two branches: hold means two things; library pinned at two versions.
	two := "kalka_domain: [lending/library.reservations@v1, lending/library.fines@v1]"
	d = blueprintIn(t, two, "A hold, a library.fines#hold and a member.\n")
	r := Document(d, o)
	if got := ruleList(r.Problems, "D"); got != "D021 D020" {
		t.Errorf("two branches: %q", got)
	}
	for _, p := range r.Problems {
		if p.Rule == "D020" && !strings.Contains(p.Message, "library.fines#hold or library.reservations#hold") {
			t.Errorf("D020: %s", p.Message)
		}
	}
}

// R003 is information only when a newer version changes what the plan
// uses (spec 6.12).
func TestNewerVersionTouched(t *testing.T) {
	dir := t.TempDir()
	base := gloss("term loan: One copy lent\nterm fine: Money owed\n") + invs("invariant I1: A loan lasts three weeks\n")
	node(t, dir, "lending-loans-domain@v1.md", "loans", "", 1, base)
	node(t, dir, "lending-loans-domain.md", "loans", "", 2, strings.Replace(base, "Money owed", "A charge for a late return", 1))
	f := NewFolder(dir)
	o := Options{Domains: f.Tree(), Docs: f}
	for _, c := range []struct{ body, want string }{
		{"A loan ends.\n", ""},
		{"A fine is charged.\n", "R003"},
	} {
		d := blueprintIn(t, "kalka_domain: lending/loans@v1", c.body)
		if got := ruleList(Document(d, o).Problems, "R"); got != c.want {
			t.Errorf("%q: %q, want %q", c.body, got, c.want)
		}
	}
}
