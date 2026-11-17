package merge

import (
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

type fakeTeam struct {
	keys     map[string][]ed25519.PublicKey
	anchors  map[string][]Anchor
	accepted map[string]Acceptance
}

func (f *fakeTeam) Term(ref string) (string, ed25519.PublicKey, bool) {
	for email, keys := range f.keys {
		for _, k := range keys {
			if termOf(k) == ref {
				return email, k, true
			}
		}
	}
	return "", nil, false
}
func (f *fakeTeam) Anchors(id string) []Anchor { return f.anchors[id] }

// termOf is the fake team's term reference of a key.
func termOf(k ed25519.PublicKey) string {
	sum := sha256.Sum256(k)
	return fmt.Sprintf("%x", sum[:])[:bp.TermLen]
}
func (f *fakeTeam) Accepted(id string) (Acceptance, bool) {
	a, ok := f.accepted[id]
	return a, ok
}

func tkey(s string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte(s))
	return ed25519.NewKeyFromSeed(seed[:])
}

func sgn(email string) bp.Signer {
	k := tkey(email)
	return bp.Signer{Fn: func(m []byte) []byte { return ed25519.Sign(k, m) }, Term: termOf(k.Public().(ed25519.PublicKey))}
}

var members = []string{"dana@example.org", "bob@example.org", "carol@example.org", "erin@example.org"}

func newFakeTeam() *fakeTeam {
	f := &fakeTeam{keys: map[string][]ed25519.PublicKey{}, anchors: map[string][]Anchor{}, accepted: map[string]Acceptance{}}
	for _, m := range members {
		f.keys[m] = []ed25519.PublicKey{tkey(m).Public().(ed25519.PublicKey)}
	}
	return f
}

var tt0 = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

const tkid = "6f1c2b9e-4d7a-4c3e-9b1a-2e5f8c7d6a40"

func planDoc(t *testing.T, owner, body string) *bp.Document {
	t.Helper()
	d, err := bp.Parse([]byte("---\nkalka_id: " + tkid + "\nkalka_owner: " + owner + "\nkalka_team: parcels\n---\n# Redelivery\n\n## Scope\n" + body + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// release adds a signed release of d's plan by author.
func release(t *testing.T, d *bp.Document, author string, v int64, deleted bool, at time.Time) *bp.Document {
	t.Helper()
	d = reparse(t, d) // work on a copy
	e, err := bp.NewRelease(d, author, at, v, deleted, nil, "", sgn(author))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	return reparse(t, d)
}

func tcomment(t *testing.T, d *bp.Document, author, text string, at time.Time, signed bool) (*bp.Document, *bp.Entry) {
	t.Helper()
	d = reparse(t, d) // work on a copy
	var s bp.Signer
	if signed {
		s = sgn(author)
	}
	e, err := bp.Append(d, bp.KindComment, author, at, "scope", "", "", text, s)
	if err != nil {
		t.Fatal(err)
	}
	return reparse(t, d), e
}

func reparse(t *testing.T, d *bp.Document) *bp.Document {
	t.Helper()
	out, err := bp.Parse(d.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// withPlan returns a copy of d with a new body and owner, keeping the log.
func withPlan(t *testing.T, d *bp.Document, owner, body string) *bp.Document {
	t.Helper()
	p := planDoc(t, owner, body)
	out, err := bp.Compose(p, "\n", d.Lead(), d.Items())
	if err != nil {
		t.Fatal(err)
	}
	return reparse(t, out)
}

func mustTeam(t *testing.T, local, in *bp.Document, team Team) Result {
	t.Helper()
	r := Docs(local, in, team)
	if r.Outcome == Rejected {
		t.Fatalf("rejected: %s %s", r.Reason, r.Detail)
	}
	return r
}

// v1 published by dana; v2 released by dana is relayed by carol (not the
// owner): it is taken.
func TestRelayOfReleaseAccepted(t *testing.T) {
	team := newFakeTeam()
	v1 := release(t, planDoc(t, "dana@example.org", "First."), "dana@example.org", 1, false, tt0)
	v2 := release(t, withPlan(t, v1, "dana@example.org", "Second."), "dana@example.org", 2, false, tt0.Add(time.Hour))
	r := mustTeam(t, v1, v2, team)
	if !r.PlanFromIncoming || r.Doc.PlanHash() != v2.PlanHash() {
		t.Fatalf("relayed release not taken: %+v", r.Notes)
	}
	// The other direction: the newer copy keeps its plan.
	if r := mustTeam(t, v2, v1, team); r.PlanFromIncoming || r.Outcome != Unchanged {
		t.Fatalf("version went down: %v", r.Outcome)
	}
}

func TestRelayOfUnreleasedEditRefused(t *testing.T) {
	team := newFakeTeam()
	v1 := release(t, planDoc(t, "dana@example.org", "First."), "dana@example.org", 1, false, tt0)
	draft := withPlan(t, v1, "dana@example.org", "Draft, not released.")
	draft, c := tcomment(t, draft, "bob@example.org", "Looks fine.", tt0.Add(time.Minute), true)
	r := mustTeam(t, v1, draft, team)
	if r.PlanFromIncoming || r.Doc.PlanHash() != v1.PlanHash() {
		t.Fatal("unreleased edit taken")
	}
	if !hasEntry(r.Doc, c.ID) {
		t.Fatal("entries of the incoming copy were not merged")
	}
}

// A release by someone other than the owner is not applied.
func TestReleaseByNonOwnerRejected(t *testing.T) {
	team := newFakeTeam()
	v1 := release(t, planDoc(t, "dana@example.org", "First."), "dana@example.org", 1, false, tt0)
	forged := release(t, withPlan(t, v1, "bob@example.org", "Bob's version."), "bob@example.org", 2, false, tt0.Add(time.Hour))
	r := mustTeam(t, v1, forged, team)
	if r.PlanFromIncoming {
		t.Fatal("non-owner release applied")
	}
	ch := Releases(entries(forged), tkid, team)
	if head, _ := ch.Head(); head.Release.Version != 1 || !strings.Contains(ch.Void[lastID(forged)], "only dana@example.org") {
		t.Fatalf("chain %+v", ch)
	}
}

// A handoff in a team document is a release naming the new owner: the
// next version is the new owner's.
func TestOwnershipFollowsReleases(t *testing.T) {
	team := newFakeTeam()
	v1 := release(t, planDoc(t, "dana@example.org", "First."), "dana@example.org", 1, false, tt0)
	v2 := release(t, withPlan(t, v1, "erin@example.org", "First."), "dana@example.org", 2, false, tt0.Add(time.Hour))
	v3 := release(t, withPlan(t, v2, "erin@example.org", "Erin's."), "erin@example.org", 3, false, tt0.Add(2*time.Hour))
	if r := mustTeam(t, v1, v3, team); !r.PlanFromIncoming {
		t.Fatal("new owner's release refused")
	}
	// dana, no longer owner, cannot release v3 or v4.
	late := release(t, withPlan(t, v2, "dana@example.org", "Dana again."), "dana@example.org", 3, false, tt0.Add(3*time.Hour))
	if r := mustTeam(t, v2, late, team); r.PlanFromIncoming {
		t.Fatal("former owner released")
	}
}

// A reassignment moves ownership above its cut-off; the old owner's
// releases above it are void. If the reassignment is discarded (no
// override), the new owner's releases become void.
func TestReassignmentAndCutOff(t *testing.T) {
	team := newFakeTeam()
	v1 := release(t, planDoc(t, "dana@example.org", "First."), "dana@example.org", 1, false, tt0)
	v2 := release(t, withPlan(t, v1, "dana@example.org", "Second."), "dana@example.org", 2, false, tt0.Add(time.Hour))
	// dana is removed; bob saw v1 only: cut-off 1.
	team.anchors[tkid] = []Anchor{{"bob@example.org", 1}}
	ch := Releases(entries(v2), tkid, team)
	if head, _ := ch.Head(); head.Release.Version != 1 {
		t.Fatalf("release above the cut-off counts: v%d", head.Release.Version)
	}
	bob2 := release(t, withPlan(t, v1, "bob@example.org", "Bob's."), "bob@example.org", 2, false, tt0.Add(2*time.Hour))
	r := mustTeam(t, v2, bob2, team)
	if !r.PlanFromIncoming {
		t.Fatalf("reassigned owner's release refused: %v", Releases(entries(r.Doc), tkid, team).Void)
	}
	// The reassignment is discarded: bob's v2 is void, dana's v2 counts again.
	delete(team.anchors, tkid)
	merged := r.Doc
	ch = Releases(entries(merged), tkid, team)
	if head, _ := ch.Head(); head.Release.Plan != v2.PlanHash() || head.Entry.Author != "dana@example.org" {
		t.Fatalf("after discard: %+v", head)
	}
	if r := mustTeam(t, merged, v2, team); !r.PlanFromIncoming {
		t.Fatal("dana's plan did not come back")
	}
}

// A tombstone beats a stale copy in both directions, and keeps only its
// releases.
func TestTombstoneBeatsStaleCopy(t *testing.T) {
	team := newFakeTeam()
	v1 := release(t, planDoc(t, "dana@example.org", "First."), "dana@example.org", 1, false, tt0)
	stale, c := tcomment(t, v1, "bob@example.org", "A late remark.", tt0.Add(time.Hour), true)
	stubPlan := mustStub(t, v1)
	tomb := release(t, stubPlan, "dana@example.org", 2, true, tt0.Add(2*time.Hour))

	for _, dir := range []string{"tomb<-stale", "stale<-tomb"} {
		var r Result
		if dir == "tomb<-stale" {
			r = mustTeam(t, tomb, stale, team)
		} else {
			r = mustTeam(t, stale, tomb, team)
		}
		if !r.Doc.Deleted() || hasEntry(r.Doc, c.ID) {
			t.Fatalf("%s: deleted=%v, comment kept=%v\n%s", dir, r.Doc.Deleted(), hasEntry(r.Doc, c.ID), r.Doc.Bytes())
		}
		for _, it := range r.Doc.Items() {
			if it.Entry.Kind != bp.KindRelease {
				t.Fatalf("%s: non-release entry in stub", dir)
			}
		}
	}
	// Nothing above a tombstone.
	v3 := release(t, withPlan(t, tomb, "dana@example.org", "Back."), "dana@example.org", 3, false, tt0.Add(3*time.Hour))
	if r := mustTeam(t, tomb, v3, team); r.PlanFromIncoming || !r.Doc.Deleted() {
		t.Fatal("release above a tombstone")
	}
}

func mustStub(t *testing.T, d *bp.Document) *bp.Document {
	s, err := bp.Parse([]byte("---\nkalka_id: " + tkid + "\nkalka_owner: " + d.Owner() + "\nkalka_team: parcels\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := bp.Compose(s, "\n", nil, d.Items())
	if err != nil {
		t.Fatal(err)
	}
	return reparse(t, out)
}

// Entries: only those signed by a team key travel; a stripped signature
// is restored from a signed copy.
func TestEntryFilterAndRestoredSignature(t *testing.T) {
	team := newFakeTeam()
	v1 := release(t, planDoc(t, "dana@example.org", "First."), "dana@example.org", 1, false, tt0)
	signed, c := tcomment(t, v1, "bob@example.org", "Signed remark.", tt0.Add(time.Minute), true)
	unsigned, u := tcomment(t, v1, "erin@example.org", "Typed by hand.", tt0.Add(2*time.Minute), false)
	stranger, s := tcomment(t, v1, "mallory@example.org", "Not a member.", tt0.Add(3*time.Minute), true)

	if r := mustTeam(t, v1, unsigned, team); hasEntry(r.Doc, u.ID) {
		t.Fatal("unsigned incoming entry taken")
	}
	if r := mustTeam(t, v1, stranger, team); hasEntry(r.Doc, s.ID) {
		t.Fatal("entry by a non-member taken")
	}
	// The local unsigned entry stays in the local file.
	if r := mustTeam(t, unsigned, signed, team); !hasEntry(r.Doc, u.ID) || !hasEntry(r.Doc, c.ID) {
		t.Fatal("local entry lost")
	}
	// A relay stripped bob's signature; the signed copy wins either way.
	stripped := strings.Replace(string(signed.Bytes()), " · sig:"+c.Sig, "", 1)
	sd, err := bp.Parse([]byte(stripped))
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]*bp.Document{{sd, signed}, {signed, sd}} {
		r := mustTeam(t, pair[0], pair[1], team)
		if got := find(r.Doc, c.ID); got == nil || got.Sig != c.Sig {
			t.Fatal("signature not restored")
		}
	}
}

func TestNewDocumentNeedsRelease(t *testing.T) {
	team := newFakeTeam()
	draft := planDoc(t, "dana@example.org", "Never released.")
	if r := Docs(nil, draft, team); r.Outcome != Rejected {
		t.Fatal("unreleased document accepted")
	}
	v1 := release(t, planDoc(t, "dana@example.org", "First."), "dana@example.org", 1, false, tt0)
	v1, _ = tcomment(t, v1, "erin@example.org", "Unsigned.", tt0.Add(time.Minute), false)
	r := Docs(nil, v1, team)
	if r.Outcome != New || r.Doc.PlanHash() != v1.PlanHash() || len(r.Doc.Items()) != 1 {
		t.Fatalf("new: %v items=%d", r.Outcome, len(r.Doc.Items()))
	}
}

// Properties over random histories of one document: merging is
// idempotent, the entries and plan do not depend on the direction, and
// the released version never goes down.
func TestTeamMergeProperties(t *testing.T) {
	team := newFakeTeam()
	rng := rand.New(rand.NewSource(7))
	for round := 0; round < 150; round++ {
		base := release(t, planDoc(t, "dana@example.org", "First."), "dana@example.org", 1, false, tt0)
		copies := []*bp.Document{base, base, base}
		version, owner := int64(1), "dana@example.org"
		at := tt0
		for step := 0; step < 12; step++ {
			at = at.Add(time.Minute)
			i := rng.Intn(len(copies))
			switch rng.Intn(4) {
			case 0: // the owner releases on its copy (only if it holds the head)
				ch := Releases(entries(copies[i]), tkid, team)
				if head, _ := ch.Head(); head.Release.Version == version {
					next := owner
					if rng.Intn(3) == 0 {
						next = members[rng.Intn(len(members))]
					}
					version++
					copies[i] = release(t, withPlan(t, copies[i], next, fmt.Sprintf("Version %d.", version)), owner, version, false, at)
					owner = next
				}
			case 1:
				a := members[rng.Intn(len(members))]
				copies[i], _ = tcomment(t, copies[i], a, fmt.Sprintf("Remark %d.", step), at, rng.Intn(4) > 0)
			default:
				j := rng.Intn(len(copies))
				before := headVersion(copies[i], team)
				r := mustTeam(t, copies[i], copies[j], team)
				if after := headVersion(r.Doc, team); after < before {
					t.Fatalf("round %d: version went down %d -> %d", round, before, after)
				}
				copies[i] = r.Doc
			}
		}
		a, b := copies[0], copies[1]
		ab, ba := mustTeam(t, a, b, team).Doc, mustTeam(t, b, a, team).Doc
		if ab.PlanHash() != ba.PlanHash() || signedIDs(ab, team) != signedIDs(ba, team) {
			t.Fatalf("round %d: direction matters", round)
		}
		if again := mustTeam(t, ab, b, team); again.Outcome != Unchanged {
			t.Fatalf("round %d: not idempotent", round)
		}
	}
}

func headVersion(d *bp.Document, team Team) int64 {
	if h, ok := Releases(entries(d), tkid, team).Head(); ok {
		return h.Release.Version
	}
	return 0
}

// signedIDs lists the entries that travel: those verified by the team.
func signedIDs(d *bp.Document, team Team) string {
	var ids []string
	for _, e := range entries(d) {
		if VerifiedBy(team, tkid, e) {
			ids = append(ids, e.ID)
		}
	}
	return strings.Join(ids, ",")
}

func entries(d *bp.Document) []*bp.Entry {
	var out []*bp.Entry
	for _, it := range d.Items() {
		out = append(out, it.Entry)
	}
	return out
}

func lastID(d *bp.Document) string { it := d.Items(); return it[len(it)-1].Entry.ID }

func find(d *bp.Document, id string) *bp.Entry {
	for _, it := range d.Items() {
		if it.Entry.ID == id {
			return it.Entry
		}
	}
	return nil
}

func hasEntry(d *bp.Document, id string) bool { return find(d, id) != nil }

// A member forges a competing version 1 naming themselves: the document
// is disputed, not taken over, whatever the ids. A reassignment from
// version 0 settles it.
func TestForgedFirstReleaseFreezes(t *testing.T) {
	team := newFakeTeam()
	v1 := release(t, planDoc(t, "dana@example.org", "First."), "dana@example.org", 1, false, tt0)
	forged := release(t, withPlan(t, v1, "bob@example.org", "Bob's now."), "bob@example.org", 1, false, tt0.Add(-time.Hour))
	ch := Releases(entries(forged), tkid, team)
	if _, ok := ch.Head(); ok || !ch.Disputed {
		t.Fatalf("forged v1 not disputed: %+v", ch)
	}
	r := mustTeam(t, v1, forged, team)
	if r.PlanFromIncoming || r.Doc.PlanHash() != v1.PlanHash() {
		t.Fatal("forger's plan taken")
	}
	team.anchors[tkid] = []Anchor{{"dana@example.org", 0}}
	ch = Releases(entries(r.Doc), tkid, team)
	if head, ok := ch.Head(); !ok || head.Entry.Author != "dana@example.org" || ch.Disputed {
		t.Fatalf("reassignment did not settle it: %+v", ch)
	}
}

// A document reassigned twice keeps a valid chain through both cut-offs:
// every reassignment is an anchor, not only the last (spec 20.4).
func TestReassignedTwice(t *testing.T) {
	team := newFakeTeam()
	v1 := release(t, planDoc(t, "dana@example.org", "First."), "dana@example.org", 1, false, tt0)
	// bob takes over at cut-off 1, releases 2 and 3; carol at cut-off 3.
	b2 := release(t, withPlan(t, v1, "bob@example.org", "Bob's."), "bob@example.org", 2, false, tt0.Add(time.Hour))
	b3 := release(t, withPlan(t, b2, "bob@example.org", "Bob's again."), "bob@example.org", 3, false, tt0.Add(2*time.Hour))
	c4 := release(t, withPlan(t, b3, "carol@example.org", "Carol's."), "carol@example.org", 4, false, tt0.Add(3*time.Hour))
	team.anchors[tkid] = []Anchor{{"bob@example.org", 1}, {"carol@example.org", 3}}
	ch := Releases(entries(c4), tkid, team)
	if len(ch.Releases) != 4 || ch.Pending != nil || ch.Disputed || len(ch.Void) != 0 {
		t.Fatalf("chain through two anchors: %d releases, void %v", len(ch.Releases), ch.Void)
	}
	if ch.NextSigner() != "carol@example.org" {
		t.Fatalf("next signer %q", ch.NextSigner())
	}
	// Before carol releases, the document is pending for her.
	ch = Releases(entries(b3), tkid, team)
	if ch.Pending == nil || ch.Pending.Owner != "carol@example.org" || ch.NextSigner() != "carol@example.org" {
		t.Fatalf("pending %+v", ch.Pending)
	}
	// A gap below an anchor does not void what stands above it.
	team.anchors[tkid] = []Anchor{{"carol@example.org", 3}}
	ch = Releases(entries(c4), tkid, team)
	if head, _ := ch.Head(); head.Release.Version != 4 || len(ch.Releases) != 2 {
		t.Fatalf("above the gap: %+v void %v", head.Release, ch.Void)
	}
}

// One key signs two different releases of one version: disputed, and a
// team owner's reassignment ends it.
func TestDisputeEndedByReassignment(t *testing.T) {
	team := newFakeTeam()
	v1 := release(t, planDoc(t, "dana@example.org", "First."), "dana@example.org", 1, false, tt0)
	a2 := release(t, withPlan(t, v1, "dana@example.org", "Laptop copy."), "dana@example.org", 2, false, tt0.Add(time.Hour))
	b2 := release(t, withPlan(t, v1, "dana@example.org", "Desktop copy."), "dana@example.org", 2, false, tt0.Add(time.Hour))
	both := mustTeam(t, a2, b2, team).Doc
	ch := Releases(entries(both), tkid, team)
	if !ch.Disputed || len(ch.Releases) != 1 || ch.NextSigner() != "" {
		t.Fatalf("not disputed: %+v", ch)
	}
	team.anchors[tkid] = []Anchor{{"bob@example.org", 1}}
	ch = Releases(entries(both), tkid, team)
	if ch.Disputed || ch.Pending == nil || ch.NextSigner() != "bob@example.org" {
		t.Fatalf("after the reassignment: %+v", ch)
	}
	bob2 := release(t, withPlan(t, both, "bob@example.org", "Settled."), "bob@example.org", 2, false, tt0.Add(2*time.Hour))
	ch = Releases(entries(bob2), tkid, team)
	if head, _ := ch.Head(); head.Entry.Author != "bob@example.org" || head.Release.Version != 2 || ch.Disputed {
		t.Fatalf("settled: %+v %v", head, ch.Void)
	}
}

// One entry naming a huge version, or an anchor with a huge cut-off, is
// read in no time: the chain visits only the versions something names.
// A version nothing names breaks the chain until the next anchor.
func TestHugeVersionsAndGaps(t *testing.T) {
	team := newFakeTeam()
	v1 := release(t, planDoc(t, "dana@example.org", "First."), "dana@example.org", 1, false, tt0)
	huge := release(t, withPlan(t, v1, "dana@example.org", "Far."), "dana@example.org", 999999999999999999, false, tt0.Add(time.Hour))
	team.anchors[tkid] = []Anchor{{"bob@example.org", 1 << 62}}
	start := time.Now()
	ch := Releases(entries(huge), tkid, team)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("chain took %v", d)
	}
	if head, _ := ch.Head(); head.Release.Version != 1 || len(ch.Releases) != 1 {
		t.Fatalf("head v%d, %d releases", head.Release.Version, len(ch.Releases))
	}
	if why := ch.Void[huge.Items()[len(huge.Items())-1].Entry.ID]; why != "version 2 is missing or void" {
		t.Fatalf("void %q", why)
	}
	// A gap below an anchor: the anchor restarts the chain above it.
	delete(team.anchors, tkid)
	v3 := release(t, withPlan(t, v1, "bob@example.org", "Bob's."), "bob@example.org", 3, false, tt0.Add(2*time.Hour))
	if ch := Releases(entries(v3), tkid, team); len(ch.Releases) != 1 {
		t.Fatalf("v3 above a gap counts: %+v", ch.Releases)
	}
	team.anchors[tkid] = []Anchor{{"bob@example.org", 2}}
	if ch := Releases(entries(v3), tkid, team); len(ch.Releases) != 2 || ch.Releases[1].Release.Version != 3 {
		t.Fatalf("v3 at the anchor refused: %+v", ch.Void)
	}
}
