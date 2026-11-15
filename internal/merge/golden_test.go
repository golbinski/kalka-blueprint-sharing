package merge

import (
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

var (
	update      = flag.Bool("update", false, "rewrite golden outputs")
	fixFixtures = flag.Bool("fix-fixtures", false, "replace rev:REV and id:ID placeholders in golden inputs")
)

const goldenDir = "../../testdata/golden"

// TestGolden runs every fixture through check and a team round trip:
// Alice publishes (adding kalka_id and owner if missing), Bob receives,
// comments, and both merge each other's replica. It compares the check report and Alice's final
// file with stored outputs and asserts the sync invariants.
func TestGolden(t *testing.T) {
	inputs, _ := filepath.Glob(filepath.Join(goldenDir, "*.md"))
	n := 0
	for _, in := range inputs {
		if strings.Contains(filepath.Base(in), ".out.") {
			continue
		}
		n++
		t.Run(strings.TrimSuffix(filepath.Base(in), ".md"), func(t *testing.T) { golden(t, in) })
	}
	if n < 7 {
		t.Fatalf("found %d golden inputs, want at least 7", n)
	}
}

var placeholderRe = regexp.MustCompile(`(?m)^(### .*)rev:REV · id:ID$`)

func golden(t *testing.T, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if *fixFixtures && strings.Contains(src, "rev:REV") {
		src = fillPlaceholders(t, src)
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	base := strings.TrimSuffix(path, ".md")
	orig := parse(t, src)
	if string(orig.Bytes()) != src {
		t.Fatal("unmodified parse does not round trip")
	}
	compare(t, base+".out.check.json", checkReport(orig))

	// Alice: publish to the team (a missing kalka_id and owner are added,
	// and a signed release v1).
	alices := parse(t, src)
	if alices.KalkaID() == "" {
		alices.SetFrontmatter("kalka_id", "9c3b2a7d-8e0f-4c9b-8a1d-5b7c8d9e0f1a")
	}
	if alices.Owner() == "" {
		alices.SetFrontmatter("owner", "alice@example.org")
	}
	alices.SetFrontmatter("team", "parcels")
	gt := newFakeTeam()
	for _, m := range []string{alices.Owner(), "bob@example.org"} {
		gt.keys[strings.ToLower(m)] = []ed25519.PublicKey{tkey(strings.ToLower(m)).Public().(ed25519.PublicKey)}
	}
	alices = reparse(t, alices)
	sent := string(release(t, alices, alices.Owner(), 1, false, time.Date(2026, 11, 12, 11, 0, 0, 0, time.UTC)).Bytes())

	// Bob: receives the new document and comments on the first section.
	r := Merge(nil, []byte(sent), gt)
	if r.Outcome != New {
		t.Fatalf("Bob: %v %s %s", r.Outcome, r.Reason, r.Detail)
	}
	bobs := r.Doc
	anchor := ""
	if hs := bobs.Headings(); len(hs) > 1 {
		anchor = hs[1].Slug
	}
	e, err := bp.Append(bobs, bp.KindComment, "bob@example.org", time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC), anchor, "", "", "Golden comment.", sgn("bob@example.org"))
	if err != nil {
		t.Fatal(err)
	}
	bobs = reparse(t, bobs)

	// Alice merges Bob's replica; Bob merges hers.
	toAlice := mustTeam(t, parse(t, sent), bobs, gt)
	final := string(toAlice.Doc.Bytes())
	compare(t, base+".out.synced.md", final)
	toBob := mustTeam(t, bobs, parse(t, final), gt)
	// Both hold the same replica; unsigned entries of the fixture stay
	// in Alice's file only (20.4).
	ra, err1 := Replica(toAlice.Doc, gt)
	rb, err2 := Replica(toBob.Doc, gt)
	if err1 != nil || err2 != nil || bp.Normalize(string(ra.Bytes())) != bp.Normalize(string(rb.Bytes())) {
		t.Error("Alice and Bob end with different replicas", err1, err2)
	}
	if again := mustTeam(t, parse(t, final), bobs, gt); again.Outcome != Unchanged {
		t.Errorf("repeating the round changed the file: %v", again.Outcome)
	}

	// The plan and every frontmatter line kalka does not manage survive.
	if toAlice.Doc.Body() != orig.Body() {
		t.Error("body changed")
	}
	if got, want := unmanaged(toAlice.Doc), unmanaged(orig); got != want {
		t.Errorf("frontmatter changed:\n got %q\nwant %q", got, want)
	}
	found := false
	for _, it := range toAlice.Doc.Items() {
		found = found || it.Entry.ID == e.ID
	}
	if !found {
		t.Error("Bob's comment did not arrive")
	}
}

func fillPlaceholders(t *testing.T, src string) string {
	rev := parse(t, strings.ReplaceAll(src, "rev:REV · id:ID", "rev:00000000 · id:000000000000")).Rev()
	src = strings.ReplaceAll(src, "rev:REV", "rev:"+rev)
	for strings.Contains(src, "id:ID\n") {
		src = strings.Replace(src, "id:ID\n", "id:000000000000\n", 1)
		for _, it := range parse(t, src).Items() {
			if it.Entry.ID == "000000000000" {
				src = strings.Replace(src, "id:000000000000\n", "id:"+it.Entry.ComputeID()+"\n", 1)
				break
			}
		}
	}
	return src
}

func unmanaged(d *bp.Document) string {
	var keep []string
	if f := d.Frontmatter(); f != nil {
		for _, l := range f.Lines[1 : len(f.Lines)-1] {
			if !strings.HasPrefix(l, "kalka_id:") && !strings.HasPrefix(l, "owner:") && !strings.HasPrefix(l, "team:") {
				keep = append(keep, l)
			}
		}
	}
	return strings.Join(keep, "")
}

type report struct {
	KalkaID  string       `json:"kalka_id"`
	Owner    string       `json:"owner"`
	Title    string       `json:"title"`
	Rev      string       `json:"rev"`
	Headings []bp.Heading `json:"headings"`
	Entries  []entryInfo  `json:"entries"`
	Problems []bp.Problem `json:"problems"`
}

type entryInfo struct {
	Line     int    `json:"line"`
	Kind     string `json:"kind"`
	Author   string `json:"author"`
	Anchor   string `json:"anchor"`
	ID       string `json:"id"`
	Valid    bool   `json:"valid"`
	Trailing int    `json:"unparsed_lines_after"`
}

func checkReport(d *bp.Document) string {
	r := report{KalkaID: d.KalkaID(), Owner: d.Owner(), Title: d.Title(), Rev: d.Rev(), Headings: d.Headings(), Problems: d.Problems()}
	for _, it := range d.Items() {
		r.Entries = append(r.Entries, entryInfo{it.Line, it.Entry.Kind, it.Entry.Author, it.Entry.Anchor, it.Entry.ID, it.Entry.Valid(), len(it.Trailing)})
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b) + "\n"
}

func compare(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs; run with -update and review the diff\n--- got ---\n%s", filepath.Base(path), got)
	}
}
