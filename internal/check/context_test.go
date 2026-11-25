package check

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// The focused agent context (spec 6.18): only what the focus's work
// needs, how current it is, and what is missing.

// libraryCopy copies the library samples into a temporary folder, without
// the files named.
func libraryCopy(t *testing.T, without ...string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(samplesDir, "library")
	es, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if slices.Contains(without, e.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func parseFile(t *testing.T, path string) *bp.Document {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err := bp.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func contextIn(t *testing.T, dir, file string, full bool) AgentContext {
	t.Helper()
	f := NewFolder(dir)
	return Context(file, parseFile(t, filepath.Join(dir, file)), Options{Domains: f.Tree(), Docs: f}, full)
}

func termRefs(c AgentContext) []string {
	var out []string
	for _, t := range c.Terms {
		out = append(out, t.Ref)
	}
	return out
}

func invariantLine(c AgentContext, ref string) *InvariantLine {
	for i := range c.Invariants.Plan {
		if c.Invariants.Plan[i].Ref == ref {
			return &c.Invariants.Plan[i]
		}
	}
	for _, d := range c.Invariants.Domains {
		for i := range d.Invariants {
			if d.Invariants[i].Ref == ref {
				return &d.Invariants[i]
			}
		}
	}
	return nil
}

func TestFocusedContextOfTheSample(t *testing.T) {
	c := contextIn(t, filepath.Join(samplesDir, "library"), "return-creates-hold.md", false)
	md := c.Markdown()
	// The goals the focus realizes, and the others by id only.
	if len(c.Goals) != 1 {
		t.Fatalf("goal chain %+v", c.Goals)
	}
	var ids []string
	for _, g := range c.Goals[0].Goals {
		ids = append(ids, g.ID)
	}
	if !slices.Equal(ids, []string{"G1", "G2"}) || !slices.Equal(c.Goals[0].LeftOut, []string{"G3"}) || strings.Contains(md, "Members can see their queue position") {
		t.Errorf("goals %v, left out %v", ids, c.Goals[0].LeftOut)
	}
	// The eight terms used, each with its narrowings; the unused one by name.
	want := []string{"library#member", "library#copy", "library#loan", "library#reservation", "library#return", "library.reservations#queue", "library.reservations#queue head", "library.reservations#hold"}
	if got := termRefs(c); !slices.Equal(got, want) {
		t.Errorf("terms %v", got)
	}
	if !slices.Equal(c.TermsLeftOut, []string{"branch"}) || strings.Contains(md, "One location of the library") || !strings.Contains(md, "Terms left out: branch.") {
		t.Errorf("left out %v", c.TermsLeftOut)
	}
	if !strings.Contains(md, "  - library.reservations#R2 refines loan: A loan of a copy") {
		t.Errorf("the narrowing of loan:\n%s", md)
	}
	// L1, R1, R2 and E1, one line each, R1 and E1 marked.
	for _, ref := range []string{"library#L1", "library.reservations#R1", "library.reservations#R2", "parent#E1"} {
		if invariantLine(c, ref) == nil {
			t.Errorf("no invariant %s", ref)
		}
	}
	if r1, e1 := invariantLine(c, "library.reservations#R1"), invariantLine(c, "parent#E1"); r1 == nil || !r1.Affects || !slices.Equal(r1.GuardedBy, []string{"C3"}) || e1 == nil || !e1.Affects || !slices.Equal(e1.GuardedBy, []string{"C4"}) {
		t.Errorf("marks %+v %+v", r1, e1)
	}
	if strings.Contains(md, "why ") || strings.Contains(md, "verified-by") {
		t.Errorf("why and verified-by are for --full:\n%s", md)
	}
	// Who else claims the parent's goals, and how many children are left.
	for _, s := range []string{
		"  - parent#G2: claimed by this document (AC2); also claimed by DEV-124 AC1 (approved v1).\n  - No other children of DEV-100.\n",
		"- Unclaimed goals of DEV-100: parent#G3.",
		"## Focus: DEV-123 Return creates a hold and notifies the queue head\n\nApproved v1, released v1 at 2026-11-20T09:30:00Z.",
		"### DEV-100 Holds and notification on return (parent)\n\nDraft, unreleased.",
	} {
		if !strings.Contains(md, s) {
			t.Errorf("lacks %q:\n%s", s, md)
		}
	}
	// The fixed order of the sections.
	at := -1
	for _, h := range []string{"# Agent context", "## Focus:", "## Gaps and boundaries", "## Goal chain", "## Invariants", "### Plan path", "### Domain path", "## Terms used"} {
		i := strings.Index(md, h)
		if i <= at {
			t.Fatalf("%q out of order:\n%s", h, md)
		}
		at = i
	}
	// The same files give the same bytes.
	again := contextIn(t, filepath.Join(samplesDir, "library"), "return-creates-hold.md", false)
	a, _ := json.Marshal(c)
	b, _ := json.Marshal(again)
	if string(a) != string(b) || md != again.Markdown() {
		t.Error("kalka context is not deterministic")
	}
}

// The authority note opens every context, the same bytes for every
// document, in place of the old header paragraph.
func TestAuthorityNote(t *testing.T) {
	note := "# Agent context\n\nDomain terms and invariants are binding at the pinned versions.\nAn approved blueprint describes an intended change as of its approval; the code may have moved since.\n\n## Focus: "
	for _, f := range []string{"return-creates-hold.md", "holds-on-return.md", "hold-blocks-lending.md", "team-lending-library.reservations-domain.md"} {
		md := contextIn(t, filepath.Join(samplesDir, "library"), f, false).Markdown()
		if !strings.HasPrefix(md, note) {
			t.Errorf("%s:\n%s", f, md)
		}
		if strings.Contains(md, "Read with the document. The plan path runs") {
			t.Errorf("%s keeps the old header", f)
		}
	}
}

// With the parent not held, the context says so, lists no siblings, and
// does not claim that no goal is unclaimed; so too with only the file.
func TestContextParentNotHere(t *testing.T) {
	dir := libraryCopy(t, "holds-on-return.md")
	c := contextIn(t, dir, "return-creates-hold.md", false)
	md := c.Markdown()
	if len(c.Gaps.Claims) != 0 || strings.Contains(md, "DEV-124") || strings.Contains(md, "## Goal chain") {
		t.Errorf("sibling lines without the parent:\n%s", md)
	}
	for _, s := range []string{"- Not here: the parent plan node 8d1c0f2a-4b7e-4c39-9a51-2e6f0b3d7c11 (kalka_parent).", "- Unclaimed goals of the parent: not looked for (the parent is not here)."} {
		if !strings.Contains(md, s) {
			t.Errorf("lacks %q:\n%s", s, md)
		}
	}
	// A single file, no folder.
	path := filepath.Join(samplesDir, "library", "return-creates-hold.md")
	f := NewFolder()
	f.Add(path)
	md = Context(path, parseFile(t, path), Options{Domains: f.Tree(), Docs: f}, false).Markdown()
	if !strings.Contains(md, "Unclaimed goals of the parent: not looked for") || strings.Contains(md, "Unclaimed goals of the parent: none") {
		t.Errorf("single file:\n%s", md)
	}
	// The parent given, its other children not looked for.
	f.Add(filepath.Join(samplesDir, "library", "holds-on-return.md"))
	md = Context(path, parseFile(t, path), Options{Domains: f.Tree(), Docs: f}, false).Markdown()
	for _, s := range []string{"Unclaimed goals of DEV-100: not looked for (only the files given were read).", "Other children of DEV-100: not looked for."} {
		if !strings.Contains(md, s) {
			t.Errorf("lacks %q:\n%s", s, md)
		}
	}
}

// One matcher selects the terms: an alias selects its term, and a
// multi-word term does not select the shorter term inside it.
func TestContextTermSelection(t *testing.T) {
	dir := libraryCopy(t)
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("desk-domain.md", "---\nkalka_kind: domain\nkalka_domain: desk\nkalka_version: 1\n---\n# Desk\n\n```glossary v2\nterm loan slip: The paper a member signs for a loan\n  alias chit\nterm shelf: Where returned copies wait\n```\n")
	write("lone.md", "---\nkalka_domain: team-lending/library.reservations@v1\n---\n# Lone\n\nThe queue head gets the copy.\n")
	write("slip.md", "---\nkalka_domain: team-lending/desk@v1\n---\n# Slip\n\nPrint a chit at the desk.\n")
	c := contextIn(t, dir, "lone.md", false)
	if got := termRefs(c); !slices.Contains(got, "library.reservations#queue head") || slices.Contains(got, "library.reservations#queue") {
		t.Errorf("queue head selects queue: %v", got)
	}
	c = contextIn(t, dir, "slip.md", false)
	if got := termRefs(c); !slices.Equal(got, []string{"desk#loan slip"}) || !slices.Equal(c.TermsLeftOut, []string{"shelf"}) {
		t.Errorf("alias: %v, left out %v", got, c.TermsLeftOut)
	}
	// The focus has no kalka_domain: no domain section, no terms.
	write("bare.md", "# Bare\n\nA queue head and a chit.\n\n```glossary v2\nterm chit: A note\n```\n")
	c = contextIn(t, dir, "bare.md", false)
	if md := c.Markdown(); len(c.Invariants.Domains) != 0 || len(c.Terms) != 0 || strings.Contains(md, "## Terms used") || strings.Contains(md, "Domain path") {
		t.Errorf("no kalka_domain:\n%s", md)
	}
	// A newer version that changes a term used is said, as R003 does.
	raw, _ := os.ReadFile(filepath.Join(dir, "team-lending-library.reservations-domain.md"))
	newer := strings.Replace(strings.Replace(string(raw), "kalka_version: 1", "kalka_version: 2", 1), "A copy set aside for the queue head after a return", "A copy kept for the queue head for seven days", 1)
	write("team-lending-library.reservations-domain@v2.md", newer)
	md := contextIn(t, dir, "return-creates-hold.md", false).Markdown()
	if !strings.Contains(md, "Newer version available: v2 of team-lending/library.reservations changes term hold, which the plan uses.") {
		t.Errorf("newer version:\n%s", md)
	}
}

// A three-level chain is printed up to the root, each level with the
// goals the level below realizes.
func TestContextGoalChain(t *testing.T) {
	const rootID = "2c7e4a19-5d3b-4f80-a6e1-9b0d8c3f7a25"
	c := plans(t, func(d string) {
		planDoc(t, d, "root.md", rootID, "", "kalka_issue: jira:DEV-1\n", "Root", goals("goal G1: Members wait less\ngoal G2: Staff work less\n"))
		planDoc(t, d, "epic.md", epicID, rootID, "kalka_issue: jira:DEV-10\n", "Epic", goals("goal G4: Holds are kept\n  realizes parent#G1\ngoal G5: Copies are counted\n"))
		planDoc(t, d, "leaf.md", leafID, epicID, "", "Leaf", goals("criterion AC1: A hold is kept\n  realizes parent#G4\n  then it waits\n"))
	})
	ctx := Context("leaf.md", parseFile(t, filepath.Join(c.dir, "leaf.md")), Options{Docs: c.f}, false)
	md := ctx.Markdown()
	for _, s := range []string{
		"### DEV-10 Epic (parent)",
		"- goal G4: Holds are kept (realizes parent#G1; AC1 realizes parent#G4)\n- Left out: G5.",
		"### DEV-1 Root (above DEV-10)",
		"- goal G1: Members wait less (DEV-10 G4 realizes parent#G1)\n- Left out: G2.",
		"- Unclaimed goals of DEV-10: parent#G5.",
	} {
		if !strings.Contains(md, s) {
			t.Errorf("lacks %q:\n%s", s, md)
		}
	}
	// --full prints every goal on the path.
	full := Context("leaf.md", parseFile(t, filepath.Join(c.dir, "leaf.md")), Options{Docs: c.f}, true).Markdown()
	if !strings.Contains(full, "- goal G5: Copies are counted") || !strings.Contains(full, "- goal G2: Staff work less") || strings.Contains(full, "Left out") {
		t.Errorf("full:\n%s", full)
	}
}

// Siblings are the team's: a Personal one is never listed. A change to a
// sibling's body leaves the context as it was; a change to its claims
// changes only its claimed-by line.
func TestContextSiblings(t *testing.T) {
	const sibID, personalID = "7b1e9c3a-2d4f-4a86-b0e5-c3d8f1a7e962", "4e8a1d7c-9b2f-4c35-a6d0-e7f9b2c4a813"
	team := "kalka_team: lending\n"
	var dir string
	c := plans(t, func(d string) {
		dir = d
		planDoc(t, d, "epic.md", epicID, "", team+"kalka_issue: jira:DEV-100\n", "Epic", goals(epicGoals))
		planDoc(t, d, "leaf.md", leafID, epicID, team+"kalka_issue: jira:DEV-123\n", "Leaf", goals(criterion+"criterion AC2: A held copy is refused\n  realizes parent#G2\n  then refused\n"))
		planDoc(t, d, "sib.md", sibID, epicID, team+"kalka_issue: jira:DEV-124\n", "Sibling", "Some prose.\n\n"+goals("criterion AC1: Refused at the desk\n  realizes parent#G2\n  then refused\n"))
		planDoc(t, d, "mine.md", personalID, epicID, "kalka_issue: jira:DEV-999\n", "Mine", goals("criterion AC1: Mine\n  realizes parent#G1\n  then mine\n"))
	})
	run := func() string {
		f := NewFolder(dir)
		return Context("leaf.md", parseFile(t, filepath.Join(c.dir, "leaf.md")), Options{Docs: f}, false).Markdown()
	}
	before := run()
	if strings.Contains(before, "DEV-999") || !strings.Contains(before, "  - parent#G2: claimed by this document (AC2); also claimed by DEV-124 AC1 (draft).\n  - No other children of DEV-100.") {
		t.Errorf("siblings:\n%s", before)
	}
	planDoc(t, dir, "sib.md", sibID, epicID, team+"kalka_issue: jira:DEV-124\n", "Sibling", "Other prose, rewritten.\n\n"+goals("criterion AC1: Refused at the desk\n  realizes parent#G2\n  then refused\n"))
	if after := run(); after != before {
		t.Errorf("a sibling's body changed the context:\n%s", after)
	}
	planDoc(t, dir, "sib.md", sibID, epicID, team+"kalka_issue: jira:DEV-124\n", "Sibling", "Other prose, rewritten.\n\n"+goals("criterion AC1: Refused at the desk\n  realizes parent#G2\n  realizes parent#G1\n  then refused\n"))
	after := run()
	var diff []string
	bl, al := strings.Split(before, "\n"), strings.Split(after, "\n")
	if len(bl) != len(al) {
		t.Fatalf("lines %d → %d:\n%s", len(bl), len(al), after)
	}
	for i := range bl {
		if bl[i] != al[i] {
			diff = append(diff, al[i])
		}
	}
	if !slices.Equal(diff, []string{"  - parent#G1: claimed by this document (AC1); also claimed by DEV-124 AC1 (draft)."}) {
		t.Errorf("a change of claims changed %q", diff)
	}
	// A focus in Personal gets no sibling lines.
	f := NewFolder(dir)
	md := Context("mine.md", parseFile(t, filepath.Join(dir, "mine.md")), Options{Docs: f}, false).Markdown()
	if strings.Contains(md, "Claims on") || !strings.Contains(md, "## Focus: DEV-999 Mine\n\nApproval not applicable, unreleased.") {
		t.Errorf("Personal focus:\n%s", md)
	}
}

// A plan node's own goals list the children that claim them; a domain
// document gets its ancestors' terms and invariants and no plan sections.
func TestContextPlanNodeAndDomain(t *testing.T) {
	lib := filepath.Join(samplesDir, "library")
	md := contextIn(t, lib, "holds-on-return.md", false).Markdown()
	for _, s := range []string{
		"- Claims on this document's goals:\n  - G1: claimed by DEV-123 AC1 (approved v1).\n  - G2: claimed by DEV-123 AC2 (approved v1); also claimed by DEV-124 AC1 (approved v1).\n  - No other children of this document.",
		"Open items:\n\n- goal G3 is unclaimed",
	} {
		if !strings.Contains(md, s) {
			t.Errorf("lacks %q:\n%s", s, md)
		}
	}
	c := contextIn(t, lib, "team-lending-library.reservations-domain.md", false)
	md = c.Markdown()
	if len(c.Goals) != 0 || len(c.Invariants.Plan) != 0 || len(c.Gaps.Claims) != 0 || c.Gaps.Unclaimed != nil {
		t.Errorf("plan sections for a domain document:\n%s", md)
	}
	for _, r := range termRefs(c) {
		if !strings.HasPrefix(r, "library#") {
			t.Errorf("its own term %s is repeated", r)
		}
	}
	if invariantLine(c, "library#L1") == nil || invariantLine(c, "library.reservations#R1") != nil || strings.Contains(md, "refines loan:") {
		t.Errorf("domain invariants:\n%s", md)
	}
	if !strings.Contains(md, "Domain node library.reservations@v1, approval not applicable.") || !strings.Contains(md, "### Domain path library.reservations@v1\n\nPinned: library@v2.") {
		t.Errorf("domain focus:\n%s", md)
	}
}

// --term prints one term with its narrowings; an unknown one is an error.
func TestTermContext(t *testing.T) {
	lib := filepath.Join(samplesDir, "library")
	f := NewFolder(lib)
	d := parseFile(t, filepath.Join(lib, "return-creates-hold.md"))
	o := Options{Domains: f.Tree(), Docs: f}
	tc, err := TermContext(d, o, "checkout")
	if err != nil || tc.Ref != "library#loan" || len(tc.Refined) != 1 || tc.Refined[0].ID != "R2" {
		t.Fatalf("%v %+v", err, tc)
	}
	if md := TermMarkdown(tc); !strings.Contains(md, "- library.reservations#R2 refines loan: A loan of a copy") || !strings.Contains(md, "- forms: loans") {
		t.Errorf("term:\n%s", md)
	}
	if tc, err := TermContext(d, o, "library.reservations#held"); err != nil || tc.Ref != "library.reservations#hold" {
		t.Errorf("qualified form: %v %+v", err, tc)
	}
	if _, err := TermContext(d, o, "fee"); err == nil {
		t.Error("an unknown term")
	}
}
