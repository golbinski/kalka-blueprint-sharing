package merge

import (
	"crypto/ed25519"
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

const body = "# Import pipeline refactor\n\n## Context\nWhy.\n\n## Plan\nSteps  \nwith a hard break.\n"

// fm is frontmatter with unknown keys of every shape (spec 16, M0).
const fm = "---\n" +
	"kalka_id: " + kid + "\n" +
	"owner: alice@corp.com\n" +
	"# a comment kalka must keep\n" +
	"repo: org/doc-service        # trailing comment\n" +
	"nested:\n  map: {a: 1, b: [x, y]}\n  owner: not-the-owner@corp.com\n" +
	"list:\n  - one\n  - two: 2\n" +
	"multi: |\n  line one\n  ## Review log (not a heading here)\n" +
	"quoted: \"a: b\"\n" +
	"---\n"

// identity is a test user with a key.
type identity struct {
	email string
	pub   ed25519.PublicKey
	priv  ed25519.PrivateKey
}

func newID(email string) identity {
	pub, priv, _ := ed25519.GenerateKey(nil)
	return identity{email, pub, priv}
}

func (id identity) sign(m []byte) []byte { return ed25519.Sign(id.priv, m) }

func keysOf(ids ...identity) Keys {
	return func(email string) (ed25519.PublicKey, bool) {
		for _, id := range ids {
			if bp.SameEmail(id.email, email) {
				return id.pub, true
			}
		}
		return nil, false
	}
}

var alice, bob, carol, mallory = newID("alice@corp.com"), newID("bob@corp.com"), newID("carol@corp.com"), newID("mallory@corp.com")
var allKeys = keysOf(alice, bob, carol, mallory)

func parse(t *testing.T, s string) *bp.Document {
	t.Helper()
	d, err := bp.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func rev(t *testing.T, s string) string { return parse(t, s).Rev() }

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

func comment(t *testing.T, author string, minute int, anchor, text, r string) *bp.Entry {
	t.Helper()
	e, err := bp.NewEntry(bp.KindComment, author, t0.Add(time.Duration(minute)*time.Minute), anchor, r, text)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func ids(d *bp.Document) []string {
	var out []string
	for _, it := range d.Items() {
		if it.Entry.Valid() {
			out = append(out, it.Entry.ID)
		}
	}
	return out
}

func mustMerge(t *testing.T, local, in, sender string) Result {
	t.Helper()
	r := Docs(parse(t, local), parse(t, in), sender, allKeys)
	if r.Outcome == Rejected {
		t.Fatalf("rejected: %s: %s", r.Reason, r.Detail)
	}
	return r
}

func TestReviewRoundTrip(t *testing.T) {
	base := fm + body
	r0 := rev(t, base)
	c1 := comment(t, "bob@corp.com", 1, "plan", "Retry path?", r0)
	c2 := comment(t, "bob@corp.com", 2, "context", "Why now?", r0)
	bobs := withEntries(t, base, c1, c2)

	// Alice merges Bob's file: same plan, both entries arrive.
	r := mustMerge(t, base, bobs, "bob@corp.com")
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
	// Bob merges Alice's reply: nothing changes, files are equal.
	back := mustMerge(t, bobs, string(r.Doc.Bytes()), "alice@corp.com")
	if back.Outcome != Unchanged {
		t.Fatalf("Bob's side: %v", back.Outcome)
	}
	if bp.Normalize(string(r.Doc.Bytes())) != bp.Normalize(bobs) {
		t.Fatal("files differ after sync")
	}
}

func TestNewBlueprint(t *testing.T) {
	r := Docs(nil, parse(t, fm+body), "alice@corp.com", allKeys)
	if r.Outcome != New {
		t.Fatalf("outcome %v", r.Outcome)
	}
}

func TestMalformed(t *testing.T) {
	for name, src := range map[string]string{
		"no id":    "---\nowner: alice@corp.com\n---\n" + body,
		"no owner": "---\nkalka_id: " + kid + "\n---\n" + body,
		"bad id":   "---\nkalka_id: 123\nowner: alice@corp.com\n---\n" + body,
	} {
		if r := Docs(nil, parse(t, src), "alice@corp.com", allKeys); r.Reason != Malformed {
			t.Errorf("%s: %v %s", name, r.Outcome, r.Reason)
		}
	}
	if r := Merge(nil, []byte{0xff}, "alice@corp.com", allKeys); r.Reason != Malformed {
		t.Errorf("binary input: %v", r.Reason)
	}
}

func TestOwnerRevision(t *testing.T) {
	base := fm + body
	r0 := rev(t, base)
	onCtx := comment(t, "bob@corp.com", 1, "context", "on context", r0)
	onPlan := comment(t, "bob@corp.com", 2, "plan", "on plan", r0)
	bobs := withEntries(t, base, onCtx, onPlan)

	revised := fm + "# Import pipeline refactor\n\n## Plan\nNew steps.\n" // Context removed
	r := mustMerge(t, bobs, revised, "alice@corp.com")
	if !r.PlanFromIncoming || r.Outcome != Updated {
		t.Fatalf("owner revision not taken: %v", r.Outcome)
	}
	if len(ids(r.Doc)) != 2 {
		t.Fatal("entries lost")
	}
	var orphaned, outdated int
	for _, p := range r.Doc.Problems() {
		switch p.Code {
		case "orphaned":
			orphaned++
		case "outdated":
			outdated++
		}
	}
	if orphaned != 1 || outdated != 2 {
		t.Fatalf("orphaned %d outdated %d", orphaned, outdated)
	}
}

func TestNonOwnerBodyEditRejected(t *testing.T) {
	base := fm + body
	edited := strings.Replace(base, "Why.", "Because I say so.", 1)
	r := Docs(parse(t, base), parse(t, edited), "bob@corp.com", allKeys)
	if r.Outcome != Rejected || r.Reason != NotOwner {
		t.Fatalf("%v %s", r.Outcome, r.Reason)
	}
	// Convergence (spec 7.5): Bob merges Alice's file, which she sent as the owner.
	conv := mustMerge(t, edited, base, "alice@corp.com")
	if !conv.PlanFromIncoming || conv.Doc.Body() != parse(t, base).Body() {
		t.Fatal("convergence did not take the owner's plan")
	}
}

func TestNonOwnerFrontmatterEditRejected(t *testing.T) {
	base := fm + body
	edited := strings.Replace(base, "repo: org/doc-service", "repo: org/other", 1)
	if r := Docs(parse(t, base), parse(t, edited), "bob@corp.com", allKeys); r.Reason != NotOwner {
		t.Fatalf("frontmatter edit by non-owner: %v %s", r.Outcome, r.Reason)
	}
}

// handoff returns src handed from one identity to another, as the
// sender's client builds it (spec 7.4).
func handoff(t *testing.T, src string, from identity, to string, minute int, signer identity) string {
	t.Helper()
	d := parse(t, src)
	e, err := bp.NewHandoff(kid, from.email, to, t0.Add(time.Duration(minute)*time.Minute), d.Rev(), signer.sign)
	if err != nil {
		t.Fatal(err)
	}
	d.SetFrontmatter("owner", to)
	if err := d.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	return string(d.Bytes())
}

func TestHandoffChain(t *testing.T) {
	base := fm + body
	toBob := handoff(t, base, alice, "bob@corp.com", 10, alice)
	bobRevised := strings.Replace(toBob, "Why.", "Bob's reasons.", 1)

	// Carol holds the pre-handoff copy and receives Bob's revision.
	r := mustMerge(t, base, bobRevised, "bob@corp.com")
	if !r.PlanFromIncoming || r.Doc.Owner() != "bob@corp.com" {
		t.Fatalf("handoff not honoured: owner %s", r.Doc.Owner())
	}

	// Two steps: Alice → Bob → Carol, received by a peer still on Alice.
	toCarol := handoff(t, toBob, bob, "carol@corp.com", 20, bob)
	if r := mustMerge(t, base, toCarol, "carol@corp.com"); r.Doc.Owner() != "carol@corp.com" {
		t.Fatal("two-step chain not followed")
	}

	// Without Alice's key the chain cannot be verified.
	r = Docs(parse(t, base), parse(t, bobRevised), "bob@corp.com", keysOf(bob))
	if r.Reason != NotOwner || !slices.Equal(r.UnknownKeys, []string{"alice@corp.com"}) {
		t.Fatalf("missing key: %v %s %v", r.Outcome, r.Reason, r.UnknownKeys)
	}
}

func TestForgedHandoffRejected(t *testing.T) {
	base := fm + body
	// Mallory writes a handoff "by Alice" signed with Mallory's own key.
	forged := handoff(t, base, alice, "mallory@corp.com", 10, mallory)
	forged = strings.Replace(forged, "Why.", "Mallory's plan.", 1)
	if r := Docs(parse(t, base), parse(t, forged), "mallory@corp.com", allKeys); r.Reason != NotOwner {
		t.Fatalf("forged signature accepted: %v", r.Outcome)
	}
	// A correctly signed handoff by someone who is not the owner is no link.
	byBob := handoff(t, base, bob, "mallory@corp.com", 10, bob)
	if r := Docs(parse(t, base), parse(t, byBob), "mallory@corp.com", allKeys); r.Reason != NotOwner {
		t.Fatalf("handoff by non-owner accepted: %v", r.Outcome)
	}
}

func TestInvalidEntries(t *testing.T) {
	base := fm + body
	r0 := rev(t, base)
	good := comment(t, "bob@corp.com", 1, "plan", "good", r0)
	src := withEntries(t, base, good)
	tampered := strings.Replace(src, "\ngood\n", "\ntampered\n", 1)

	// Incoming invalid entry is dropped with a note.
	r := mustMerge(t, base, tampered, "bob@corp.com")
	if len(r.Doc.Items()) != 0 || len(r.Notes) == 0 {
		t.Fatalf("invalid incoming entry kept: %v notes %v", len(r.Doc.Items()), r.Notes)
	}
	// Local invalid entry is kept as unparsed text.
	other := comment(t, "carol@corp.com", 2, "plan", "other", r0)
	r = mustMerge(t, tampered, withEntries(t, base, other), "carol@corp.com")
	if !strings.Contains(string(r.Doc.Bytes()), "tampered") {
		t.Fatal("local invalid entry lost")
	}
}

func TestUnparsedMovesWithEntry(t *testing.T) {
	base := fm + body
	r0 := rev(t, base)
	late := comment(t, "bob@corp.com", 5, "plan", "late", r0)
	early := comment(t, "carol@corp.com", 1, "plan", "early", r0)
	local := withEntries(t, base, late) + "\n### Notes by an agent\nkeep me\n"
	r := mustMerge(t, local, withEntries(t, base, early), "carol@corp.com")
	out := string(r.Doc.Bytes())
	iEarly, iLate, iNote := strings.Index(out, "early"), strings.Index(out, "late"), strings.Index(out, "keep me")
	if !(iEarly < iLate && iLate < iNote) {
		t.Fatalf("order early=%d late=%d note=%d\n%s", iEarly, iLate, iNote, out)
	}
}

// variant returns src with CRLF endings and/or trailing whitespace stripped
// (spec 16.1 scenario 9).
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
	base := fm + body
	c := comment(t, "bob@corp.com", 1, "plan", "x", rev(t, base))
	reformatted := variant(withEntries(t, base, c), true, true)
	r := mustMerge(t, base, reformatted, "bob@corp.com")
	if r.PlanFromIncoming || len(ids(r.Doc)) != 1 {
		t.Fatal("reformatted file not treated as the same plan")
	}
}

// TestProperties checks spec 7.2: merge is idempotent, commutative on the
// entry set, and never drops a valid entry, across random entry subsets and
// line-ending variants.
func TestProperties(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	base := fm + body
	r0 := rev(t, base)
	authors := []string{"bob@corp.com", "carol@corp.com", "dave@corp.com"}
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
		sender := authors[rng.IntN(3)]

		ab := mustMerge(t, L, I, sender)
		ba := mustMerge(t, I, L, sender)
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
		if again := mustMerge(t, string(ab.Doc.Bytes()), I, sender); again.Outcome != Unchanged {
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
