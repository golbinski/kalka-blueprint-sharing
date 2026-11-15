package merge

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

const kid = "6f1c2a0e-8d1b-4c57-9a43-0e2b7c9d51aa"

var t0 = time.Date(2026, 11, 11, 17, 40, 3, 0, time.UTC)

const body = "# Branch return routing\n\n## Context\nWhy.\n\n## Plan\nSteps  \nwith a hard break.\n"

// fm is frontmatter with unknown keys of every shape (spec 16, M0).
const fm = "---\n" +
	"kalka_id: " + kid + "\n" +
	"owner: dana@example.org\n" +
	"team: parcels\n" +
	"# a comment kalka must keep\n" +
	"repo: org/lending-service        # trailing comment\n" +
	"nested:\n  map: {a: 1, b: [x, y]}\n  owner: not-the-owner@example.org\n" +
	"list:\n  - one\n  - two: 2\n" +
	"multi: |\n  line one\n  ## Review log (not a heading here)\n" +
	"quoted: \"a: b\"\n" +
	"---\n"

var team = newFakeTeam()

func parse(t *testing.T, s string) *bp.Document {
	t.Helper()
	d, err := bp.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func rev(t *testing.T, s string) string { return parse(t, s).Rev() }

// published returns src with a signed release v1 by its owner, as the
// team holds a document after publish.
func published(t *testing.T, src string) string {
	t.Helper()
	d := parse(t, src)
	return string(release(t, d, d.Owner(), 1, false, t0).Bytes())
}

// withEntries returns src with entries appended through the library.
func withEntries(t *testing.T, src string, es ...*bp.Entry) string {
	t.Helper()
	d := parse(t, src)
	for _, e := range es {
		if err := d.AddEntry(e); err != nil {
			t.Fatal(err)
		}
	}
	return string(d.Bytes())
}

// comment is an entry signed by its author's team key.
func comment(t *testing.T, author string, minute int, anchor, text, r string) *bp.Entry {
	t.Helper()
	e, err := bp.NewEntry(bp.KindComment, author, t0.Add(time.Duration(minute)*time.Minute), anchor, r, text)
	if err != nil {
		t.Fatal(err)
	}
	e.Sign(kid, sgn(author))
	return e
}

// ids lists the valid entries other than releases.
func ids(d *bp.Document) []string {
	var out []string
	for _, it := range d.Items() {
		if it.Entry.Valid() && it.Entry.Kind != bp.KindRelease {
			out = append(out, it.Entry.ID)
		}
	}
	return out
}

func mustMerge(t *testing.T, local, in string) Result {
	t.Helper()
	return mustTeam(t, parse(t, local), parse(t, in), team)
}

func TestReviewRoundTrip(t *testing.T) {
	base := published(t, fm+body)
	r0 := rev(t, base)
	c1 := comment(t, "bob@example.org", 1, "plan", "Retry path?", r0)
	c2 := comment(t, "bob@example.org", 2, "context", "Why now?", r0)
	bobs := withEntries(t, base, c1, c2)

	// Dana merges Bob's replica: same plan, both entries arrive.
	r := mustMerge(t, base, bobs)
	if r.Outcome != Updated || r.PlanFromIncoming {
		t.Fatalf("outcome %v planFromIncoming %v", r.Outcome, r.PlanFromIncoming)
	}
	if got := ids(r.Doc); !slices.Equal(got, []string{c1.ID, c2.ID}) {
		t.Fatalf("ids %v", got)
	}
	if r.Doc.Body() != parse(t, base).Body() {
		t.Fatal("body changed")
	}
	// Frontmatter with unknown keys survives byte for byte.
	if got := strings.Join(r.Doc.Frontmatter().Lines, ""); got != fm {
		t.Fatalf("frontmatter changed:\n%s", got)
	}
	// Bob merges Dana's copy back: nothing changes, files are equal.
	back := mustMerge(t, bobs, string(r.Doc.Bytes()))
	if back.Outcome != Unchanged {
		t.Fatalf("Bob's side: %v", back.Outcome)
	}
	if bp.Normalize(string(r.Doc.Bytes())) != bp.Normalize(bobs) {
		t.Fatal("files differ after replication")
	}
}

func TestNewBlueprint(t *testing.T) {
	if r := Docs(nil, parse(t, published(t, fm+body)), team); r.Outcome != New {
		t.Fatalf("outcome %v %s", r.Outcome, r.Detail)
	}
}

func TestMalformed(t *testing.T) {
	for name, src := range map[string]string{
		"no id":    "---\nowner: dana@example.org\nteam: parcels\n---\n" + body,
		"no owner": "---\nkalka_id: " + kid + "\nteam: parcels\n---\n" + body,
		"bad id":   "---\nkalka_id: 123\nowner: dana@example.org\nteam: parcels\n---\n" + body,
	} {
		if r := Docs(nil, parse(t, src), team); r.Reason != Malformed {
			t.Errorf("%s: %v %s", name, r.Outcome, r.Reason)
		}
	}
	if r := Merge(nil, []byte{0xff}, team); r.Reason != Malformed {
		t.Errorf("binary input: %v", r.Reason)
	}
}

func TestInvalidEntries(t *testing.T) {
	base := published(t, fm+body)
	r0 := rev(t, base)
	good := comment(t, "bob@example.org", 1, "plan", "good", r0)
	src := withEntries(t, base, good)
	tampered := strings.Replace(src, "\ngood\n", "\ntampered\n", 1)

	// An incoming invalid entry is dropped with a note.
	r := mustMerge(t, base, tampered)
	if len(ids(r.Doc)) != 0 || len(r.Notes) == 0 {
		t.Fatalf("invalid incoming entry kept: %v notes %v", ids(r.Doc), r.Notes)
	}
	// A local invalid entry is kept as unparsed text.
	other := comment(t, "carol@example.org", 2, "plan", "other", r0)
	r = mustMerge(t, tampered, withEntries(t, base, other))
	if !strings.Contains(string(r.Doc.Bytes()), "tampered") {
		t.Fatal("local invalid entry lost")
	}
}

func TestUnparsedMovesWithEntry(t *testing.T) {
	base := published(t, fm+body)
	r0 := rev(t, base)
	late := comment(t, "bob@example.org", 5, "plan", "late", r0)
	early := comment(t, "carol@example.org", 1, "plan", "early", r0)
	local := withEntries(t, base, late) + "\n### Notes by an agent\nkeep me\n"
	r := mustMerge(t, local, withEntries(t, base, early))
	out := string(r.Doc.Bytes())
	iEarly, iLate, iNote := strings.Index(out, "early"), strings.Index(out, "late"), strings.Index(out, "keep me")
	if !(iEarly < iLate && iLate < iNote) {
		t.Fatalf("order early=%d late=%d note=%d\n%s", iEarly, iLate, iNote, out)
	}
}

// variant returns src with CRLF endings and/or trailing whitespace stripped
// (spec 16.1).
func variant(src string, crlf, strip bool) string {
	if strip {
		lines := strings.Split(src, "\n")
		for i, l := range lines {
			lines[i] = strings.TrimRight(l, " \t")
		}
		src = strings.Join(lines, "\n")
	}
	if crlf {
		src = strings.ReplaceAll(src, "\n", "\r\n")
	}
	return src
}

func TestEditorReformatIsSamePlan(t *testing.T) {
	base := published(t, fm+body)
	c := comment(t, "bob@example.org", 1, "plan", "x", rev(t, base))
	reformatted := variant(withEntries(t, base, c), true, true)
	r := mustMerge(t, base, reformatted)
	if r.PlanFromIncoming || len(ids(r.Doc)) != 1 {
		t.Fatal("reformatted file not treated as the same plan")
	}
}

// TestProperties checks spec 7.2: merge is idempotent, commutative on the
// entry set, and never drops a valid signed entry, across random entry
// subsets and line-ending variants.
func TestProperties(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	base := published(t, fm+body)
	r0 := rev(t, base)
	authors := []string{"bob@example.org", "carol@example.org", "erin@example.org"}
	anchors := []string{"", "plan", "context", "gone"}
	var pool []*bp.Entry
	for i := range 40 {
		// Few distinct minutes, so equal timestamps exercise the id tie-break.
		pool = append(pool, comment(t, authors[i%3], rng.IntN(6), anchors[rng.IntN(4)], fmt.Sprintf("note %d\n\n```\n### not a header\n```", i), r0))
	}
	pick := func() []*bp.Entry {
		var out []*bp.Entry
		for _, e := range pool {
			if rng.IntN(3) == 0 {
				out = append(out, e)
			}
		}
		rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return out
	}
	for round := range 300 {
		a, b := pick(), pick()
		L := variant(withEntries(t, base, a...), rng.IntN(2) == 0, false)
		I := variant(withEntries(t, base, b...), rng.IntN(2) == 0, rng.IntN(2) == 0)

		ab := mustMerge(t, L, I)
		ba := mustMerge(t, I, L)
		want := map[string]bool{}
		for _, e := range append(append([]*bp.Entry{}, a...), b...) {
			want[e.ID] = true
		}
		got := ids(ab.Doc)
		if len(got) != len(want) {
			t.Fatalf("round %d: %d entries, want %d", round, len(got), len(want))
		}
		for _, id := range got {
			if !want[id] {
				t.Fatalf("round %d: unexpected entry %s", round, id)
			}
		}
		if !slices.Equal(got, ids(ba.Doc)) {
			t.Fatalf("round %d: not commutative", round)
		}
		if again := mustMerge(t, string(ab.Doc.Bytes()), I); again.Outcome != Unchanged {
			t.Fatalf("round %d: not idempotent (%v)", round, again.Outcome)
		}
		if ab.Doc.EOL() != parse(t, L).EOL() {
			t.Fatalf("round %d: line endings not preserved", round)
		}
		if lfm := parse(t, L).Frontmatter().Lines; !slices.Equal(ab.Doc.Frontmatter().Lines, lfm) {
			t.Fatalf("round %d: frontmatter bytes changed", round)
		}
	}
}
