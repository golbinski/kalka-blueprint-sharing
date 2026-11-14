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
	keys map[string][]ed25519.PublicKey
	over map[string][2]any // kalka_id -> {owner, after}
}

func (f *fakeTeam) Keys(email string) []ed25519.PublicKey { return f.keys[strings.ToLower(email)] }
func (f *fakeTeam) Override(id string) (string, int64, bool) {
	o, ok := f.over[id]
	if !ok {
		return "", 0, false
	}
	return o[0].(string), o[1].(int64), true
}

func tkey(s string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte(s))
	return ed25519.NewKeyFromSeed(seed[:])
}

func sgn(email string) bp.Signer {
	k := tkey(email)
	return func(m []byte) []byte { return ed25519.Sign(k, m) }
}

var members = []string{"dana@example.org", "bob@example.org", "carol@example.org", "erin@example.org"}

func newFakeTeam() *fakeTeam {
	f := &fakeTeam{keys: map[string][]ed25519.PublicKey{}, over: map[string][2]any{}}
	for _, m := range members {
		f.keys[m] = []ed25519.PublicKey{tkey(m).Public().(ed25519.PublicKey)}
	}
	return f
}

var tt0 = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

const tkid = "6f1c2b9e-4d7a-4c3e-9b1a-2e5f8c7d6a40"

func planDoc(t *testing.T, owner, body string) *bp.Document {
	t.Helper()
	d, err := bp.Parse([]byte("---\nkalka_id: " + tkid + "\nowner: " + owner + "\nteam: parcels\n---\n# Redelivery\n\n## Scope\n" + body + "\n"))
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

func noKeys(string) (ed25519.PublicKey, bool) { return nil, false }

func mustTeam(t *testing.T, local, in *bp.Document, team Team) Result {
	t.Helper()
	r := DocsTeam(local, in, "carol@example.org", noKeys, team)
	if r.Outcome == Rejected {
		t.Fatalf("rejected: %s %s", r.Reason, r.Detail)
	}
	return r
}

// v1 published by dana; v2 released by dana is relayed by carol (not the
// owner): it is taken (D4).
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

// A release by someone other than the owner (a domain release by a
// non-assigned member, a blueprint release by a non-owner) is not applied.
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
	team.over[tkid] = [2]any{"bob@example.org", int64(1)}
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
	delete(team.over, tkid)
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
	s, err := bp.Parse([]byte("---\nkalka_id: " + tkid + "\nowner: " + d.Owner() + "\nteam: parcels\ndeleted: true\n---\n"))
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
	// Private documents: the pinned key decides the same way.
	keys := func(e string) (ed25519.PublicKey, bool) {
		return tkey(e).Public().(ed25519.PublicKey), true
	}
	r := Docs(sd, signed, "carol@example.org", keys)
	if got := find(r.Doc, c.ID); got == nil || got.Sig != c.Sig {
		t.Fatal("classic merge: signature not restored")
	}
}

func TestNewDocumentNeedsRelease(t *testing.T) {
	team := newFakeTeam()
	draft := planDoc(t, "dana@example.org", "Never released.")
	if r := DocsTeam(nil, draft, "dana@example.org", noKeys, team); r.Outcome != Rejected {
		t.Fatal("unreleased document accepted")
	}
	v1 := release(t, planDoc(t, "dana@example.org", "First."), "dana@example.org", 1, false, tt0)
	v1, _ = tcomment(t, v1, "erin@example.org", "Unsigned.", tt0.Add(time.Minute), false)
	r := DocsTeam(nil, v1, "carol@example.org", noKeys, team)
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
	team.over[tkid] = [2]any{"dana@example.org", int64(0)}
	ch = Releases(entries(r.Doc), tkid, team)
	if head, ok := ch.Head(); !ok || head.Entry.Author != "dana@example.org" || ch.Disputed {
		t.Fatalf("reassignment did not settle it: %+v", ch)
	}
}

type domainTeam struct {
	*fakeTeam
	owner string
	after int64
}

func (d domainTeam) DomainOwner() (string, int64, bool) { return d.owner, d.after, true }

// Spec 6.12, 19.6: a domain document's releases are the assigned domain
// owner's, for every version above the cut-off; unassigned, nobody's.
func TestDomainReleases(t *testing.T) {
	ft := newFakeTeam()
	v1 := release(t, planDoc(t, "dana@example.org", "Glossary."), "dana@example.org", 1, false, tt0)
	if ch := Releases(entries(v1), tkid, domainTeam{ft, "", 0}); len(ch.Releases) != 0 || !strings.Contains(ch.Void[lastID(v1)], "no domain owner") {
		t.Fatalf("unassigned domain released: %+v", ch)
	}
	team := domainTeam{ft, "dana@example.org", 0}
	if ch := Releases(entries(v1), tkid, team); len(ch.Releases) != 1 {
		t.Fatalf("assigned owner's v1 refused: %v", ch.Void)
	}
	// dana hands the plan's owner line to bob: bob still may not release,
	// the assignment decides.
	v2 := release(t, withPlan(t, v1, "bob@example.org", "Glossary."), "dana@example.org", 2, false, tt0.Add(time.Hour))
	v3 := release(t, withPlan(t, v2, "bob@example.org", "Bob's glossary."), "bob@example.org", 3, false, tt0.Add(2*time.Hour))
	if ch := Releases(entries(v3), tkid, team); len(ch.Releases) != 2 || !strings.Contains(ch.Void[lastID(v3)], "only dana@example.org") {
		t.Fatalf("non-assigned release: %+v", ch.Void)
	}
	// A new assignment to carol with cut-off 2 voids dana's v3.
	dana3 := release(t, withPlan(t, v2, "dana@example.org", "Dana's third."), "dana@example.org", 3, false, tt0.Add(3*time.Hour))
	carol := domainTeam{ft, "carol@example.org", 2}
	if ch := Releases(entries(dana3), tkid, carol); len(ch.Releases) != 2 {
		t.Fatalf("old domain owner released above the cut-off: %d", len(ch.Releases))
	}
	carol3 := release(t, withPlan(t, v2, "carol@example.org", "Carol's third."), "carol@example.org", 3, false, tt0.Add(4*time.Hour))
	if r := mustTeam(t, dana3, carol3, carol); !r.PlanFromIncoming {
		t.Fatal("new domain owner's release refused")
	}
}
