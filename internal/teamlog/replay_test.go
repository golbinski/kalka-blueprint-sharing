package teamlog

import (
	"crypto/ed25519"
	"crypto/sha256"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

type actor struct {
	email string
	key   ed25519.PrivateKey
	pub   ed25519.PublicKey
}

func newActor(email string) actor {
	seed := sha256.Sum256([]byte(email))
	k := ed25519.NewKeyFromSeed(seed[:])
	return actor{email, k, k.Public().(ed25519.PublicKey)}
}

// team is a test log: alice creates "parcels".
type team struct {
	t    *testing.T
	id   string
	ops  []Op
	byEm map[string]actor
}

func newTeam(t *testing.T) *team {
	a := newActor("alice@example.org")
	c := Create(a.key, "parcels", a.email, t0)
	return &team{t: t, id: c.Hash(), ops: []Op{c}, byEm: map[string]actor{a.email: a}}
}

func (tm *team) actor(email string) actor {
	if a, ok := tm.byEm[email]; ok {
		return a
	}
	a := newActor(email)
	tm.byEm[email] = a
	return a
}

func (tm *team) signer(email string) Signer { return Signer{Key: tm.actor(email).key, Team: tm.id} }

func (tm *team) heads() []string { return Heads(tm.ops) }

// add appends ops and returns the first one's hash.
func (tm *team) add(ops ...Op) string {
	tm.ops = append(tm.ops, ops...)
	return ops[0].Hash()
}

func (tm *team) member(by, email string, prev []string) string {
	return tm.add(tm.signer(by).AddMember(prev, email, tm.actor(email).pub, "", t0))
}

func (tm *team) owner(by, email string, prev []string) string {
	return tm.add(tm.signer(by).AddOwner(prev, email, t0))
}

func (tm *team) state() *State {
	s, bad := Replay(tm.id, tm.ops)
	if len(bad) > 0 {
		tm.t.Fatalf("rejected %+v", bad)
	}
	return s
}

func (tm *team) reason(s *State, h string) string {
	for _, d := range s.Discarded {
		if d.Hash == h {
			return d.Reason
		}
	}
	return ""
}

// setup: alice creates, adds bob and carol as owners, dave as a member.
func setup(t *testing.T) *team {
	tm := newTeam(t)
	tm.member("alice@example.org", "bob@example.org", tm.heads())
	tm.owner("alice@example.org", "bob@example.org", tm.heads())
	tm.member("alice@example.org", "carol@example.org", tm.heads())
	tm.owner("alice@example.org", "carol@example.org", tm.heads())
	tm.member("alice@example.org", "dave@example.org", tm.heads())
	return tm
}

func owners(s *State) string { return strings.Join(s.Owners, ",") }

func TestBasicRoster(t *testing.T) {
	tm := setup(t)
	s := tm.state()
	if s.Name != "parcels" || len(s.Members) != 4 || owners(s) != "alice@example.org,bob@example.org,carol@example.org" {
		t.Fatalf("state %+v owners %s", s.Members, owners(s))
	}
	if len(s.Discarded) != 0 || len(s.Pending) != 0 {
		t.Fatalf("discarded %+v pending %+v", s.Discarded, s.Pending)
	}
}

func TestNonOwnerRejected(t *testing.T) {
	tm := setup(t)
	h := tm.member("dave@example.org", "erin@example.org", tm.heads())
	s := tm.state()
	if s.Effective[h] || tm.reason(s, h) != "signer is not an owner" {
		t.Fatalf("%q", tm.reason(s, h))
	}
	// A stranger's key, too.
	h = tm.member("mallory@example.org", "erin@example.org", tm.heads())
	if s := tm.state(); s.Effective[h] {
		t.Fatal("stranger accepted")
	}
}

func TestRemovedSignerRejected(t *testing.T) {
	tm := setup(t)
	tm.add(tm.signer("alice@example.org").Remove(tm.heads(), "carol@example.org", nil, "", t0)...)
	after := tm.member("carol@example.org", "erin@example.org", tm.heads())
	s := tm.state()
	if s.Effective[after] || len(s.Members) != 3 {
		t.Fatalf("after removal: %v %q", s.Effective[after], tm.reason(s, after))
	}
}

// Rule 2: an operation by the removed owner, concurrent with the removal
// (back-dated), does not count.
func TestBackdatedOperationLoses(t *testing.T) {
	tm := setup(t)
	base := tm.heads()
	tm.add(tm.signer("alice@example.org").Remove(base, "carol@example.org", nil, "", t0)...)
	// carol signs from the old heads, as if before her removal
	carolsAdd := tm.member("carol@example.org", "erin@example.org", base)
	s := tm.state()
	if s.Effective[carolsAdd] || !strings.Contains(tm.reason(s, carolsAdd), "removed by") {
		t.Fatalf("back-dated add: %q", tm.reason(s, carolsAdd))
	}
	// An honest owner who merged carol's op and builds on it is not
	// poisoned by it.
	bobsAdd := tm.member("bob@example.org", "frank@example.org", tm.heads())
	if s := tm.state(); !s.Effective[bobsAdd] {
		t.Fatalf("honest op after merge: %q", tm.reason(s, bobsAdd))
	}
}

// Rule 3: in a mutual removal the senior owner stays.
func TestMutualRemoval(t *testing.T) {
	for _, order := range []bool{false, true} {
		tm := setup(t)
		base := tm.heads()
		bobRemovesCarol := tm.signer("bob@example.org").Remove(base, "carol@example.org", nil, "", t0)
		carolRemovesBob := tm.signer("carol@example.org").Remove(base, "bob@example.org", nil, "", t0)
		if order {
			tm.add(carolRemovesBob...)
			tm.add(bobRemovesCarol...)
		} else {
			tm.add(bobRemovesCarol...)
			tm.add(carolRemovesBob...)
		}
		s := tm.state()
		if owners(s) != "alice@example.org,bob@example.org" {
			t.Fatalf("owners %s, discarded %+v", owners(s), s.Discarded)
		}
	}
}

// Grinding: whatever hash carol's counter-removal has, bob stays.
func TestGrindingGainsNothing(t *testing.T) {
	for i := 0; i < 40; i++ {
		tm := setup(t)
		base := tm.heads()
		tm.add(tm.signer("bob@example.org").Remove(base, "carol@example.org", nil, "", t0)...)
		tm.add(tm.signer("carol@example.org").Remove(base, "bob@example.org", nil, "", t0.Add(time.Duration(i)*time.Second))...)
		if s := tm.state(); owners(s) != "alice@example.org,bob@example.org" {
			t.Fatalf("variant %d: owners %s", i, owners(s))
		}
	}
}

func TestRingOfThree(t *testing.T) {
	tm := setup(t)
	base := tm.heads()
	tm.add(tm.signer("alice@example.org").Remove(base, "bob@example.org", nil, "", t0)...)
	tm.add(tm.signer("bob@example.org").Remove(base, "carol@example.org", nil, "", t0)...)
	tm.add(tm.signer("carol@example.org").Remove(base, "alice@example.org", nil, "", t0)...)
	s := tm.state()
	// alice (most senior) removes bob; bob's removal of carol is blocked;
	// carol's removal of alice ranks below alice's and alice is removed
	// too. carol remains.
	if owners(s) != "carol@example.org" {
		t.Fatalf("owners %s %+v", owners(s), s.Discarded)
	}
}

func TestLastOwner(t *testing.T) {
	tm := newTeam(t)
	tm.member("alice@example.org", "bob@example.org", tm.heads())
	tm.owner("alice@example.org", "bob@example.org", tm.heads())
	base := tm.heads()
	tm.add(tm.signer("alice@example.org").Remove(base, "bob@example.org", nil, "", t0)...)
	tm.add(tm.signer("bob@example.org").Remove(base, "alice@example.org", nil, "", t0)...)
	if s := tm.state(); owners(s) != "alice@example.org" {
		t.Fatalf("owners %s", owners(s))
	}
	// Removing yourself is refused, so the last owner cannot leave.
	h := tm.add(tm.signer("alice@example.org").RemoveOwner(tm.heads(), "alice@example.org", t0))
	if s := tm.state(); s.Effective[h] {
		t.Fatal("self-removal accepted")
	}
}

// A member added by an owner who is concurrently being removed is not a
// member (19.4).
func TestAddByConcurrentlyRemovedOwner(t *testing.T) {
	tm := setup(t)
	base := tm.heads()
	tm.add(tm.signer("alice@example.org").Remove(base, "bob@example.org", nil, "", t0)...)
	h := tm.member("bob@example.org", "erin@example.org", base)
	s := tm.state()
	if s.Effective[h] || s.Members["erin@example.org"].Email != "" {
		t.Fatal("erin is a member")
	}
}

var planA, planB = strings.Repeat("a", 64), strings.Repeat("b", 64)

func TestReassignment(t *testing.T) {
	tm := setup(t)
	// A plain member cannot reassign.
	if h := tm.add(tm.signer("dave@example.org").Reassign(tm.heads(), "c0ffee000000", "bob@example.org", "dave@example.org", 2, planB, t0)); tm.state().Effective[h] {
		t.Fatal("a plain member reassigned")
	}
	ops := tm.signer("bob@example.org").Remove(tm.heads(), "dave@example.org",
		[]Owned{{"a1b2c3d4e5f6", 3, planA}, {"0f9e8d7c6b5a", 0, ""}}, "bob@example.org", t0)
	tm.add(ops...)
	s := tm.state()
	if got := s.Reassigned["a1b2c3d4e5f6"]; len(got) != 1 || got[0].Owner != "bob@example.org" || got[0].From != "dave@example.org" || got[0].After != 3 || got[0].Plan != planA {
		t.Fatalf("reassigned %+v", s.Reassigned)
	}
	// Rule 9: a present member's document can be reassigned, from is not
	// judged; the new owner must be a member.
	h := tm.add(tm.signer("bob@example.org").Reassign(tm.heads(), "c0ffee000000", "carol@example.org", "bob@example.org", 1, planA, t0))
	if s := tm.state(); !s.Effective[h] {
		t.Fatalf("present member's document: %q", tm.reason(s, h))
	}
	h = tm.add(tm.signer("bob@example.org").Reassign(tm.heads(), "c0ffee000000", "carol@example.org", "zed@example.org", 2, planB, t0))
	if s := tm.state(); s.Effective[h] {
		t.Fatal("reassigned to a non-member")
	}
}

// Rule 8: every effective reassignment is an anchor; a cut-off below one
// the signer saw is stale; of two with one cut-off, the later counts.
func TestReassignmentAnchors(t *testing.T) {
	tm := setup(t)
	alice, bob := tm.signer("alice@example.org"), tm.signer("bob@example.org")
	const doc = "a1b2c3d4e5f6"
	h1 := tm.add(alice.Reassign(tm.heads(), doc, "carol@example.org", "dave@example.org", 2, planA, t0))
	h2 := tm.add(bob.Reassign(tm.heads(), doc, "dave@example.org", "carol@example.org", 5, planB, t0))
	stale := tm.add(alice.Reassign(tm.heads(), doc, "carol@example.org", "bob@example.org", 4, planA, t0))
	s := tm.state()
	if s.Effective[stale] || !strings.Contains(tm.reason(s, stale), "stale cut-off") {
		t.Fatalf("stale cut-off: %q", tm.reason(s, stale))
	}
	as := s.Reassigned[doc]
	if len(as) != 2 || as[0].Op != h1 || as[0].After != 2 || as[1].Op != h2 || as[1].After != 5 {
		t.Fatalf("anchors %+v", as)
	}
	// Two concurrent with one cut-off: both count, one anchor, the later
	// in visiting order.
	base := tm.heads()
	c1 := tm.add(alice.Reassign(base, doc, "carol@example.org", "dave@example.org", 7, planA, t0))
	c2 := tm.add(bob.Reassign(base, doc, "carol@example.org", "bob@example.org", 7, planB, t0))
	s = tm.state()
	if !s.Effective[c1] || !s.Effective[c2] {
		t.Fatal("concurrent same cut-off discarded")
	}
	as = s.Reassigned[doc]
	if len(as) != 3 || as[2].After != 7 {
		t.Fatalf("anchors %+v", as)
	}
	// A concurrent lower cut-off, not seen: it counts, as an anchor.
	low := tm.add(alice.Reassign(base, doc, "carol@example.org", "dave@example.org", 6, planA, t0))
	if s := tm.state(); !s.Effective[low] || len(s.Reassigned[doc]) != 4 {
		t.Fatalf("concurrent lower cut-off: %q %+v", tm.reason(s, low), s.Reassigned[doc])
	}
}

// The reassignments of a removal fall with the remover's term when the
// remover loses a mutual removal (rule 2).
func TestReassignmentFallsWithRemoval(t *testing.T) {
	tm := setup(t)
	base := tm.heads()
	// carol removes bob and takes his document; concurrently, bob (senior
	// to carol) removes carol.
	carolOps := tm.signer("carol@example.org").Remove(base, "bob@example.org", []Owned{{"a1b2c3d4e5f6", 2, planA}}, "carol@example.org", t0)
	tm.add(carolOps...)
	tm.add(tm.signer("bob@example.org").Remove(base, "carol@example.org", nil, "", t0)...)
	s := tm.state()
	if _, ok := s.Reassigned["a1b2c3d4e5f6"]; ok || s.Effective[carolOps[1].Hash()] {
		t.Fatalf("reassignment survived: %+v", s.Reassigned)
	}
	if owners(s) != "alice@example.org,bob@example.org" {
		t.Fatalf("owners %s", owners(s))
	}
}

func TestReAdding(t *testing.T) {
	tm := setup(t)
	tm.add(tm.signer("alice@example.org").Remove(tm.heads(), "carol@example.org", nil, "", t0)...)
	if s := tm.state(); s.Members["carol@example.org"].Email != "" {
		t.Fatal("still a member")
	}
	tm.member("alice@example.org", "carol@example.org", tm.heads())
	tm.owner("alice@example.org", "carol@example.org", tm.heads())
	h := tm.member("carol@example.org", "erin@example.org", tm.heads())
	s := tm.state()
	if !s.Effective[h] || !s.Members["carol@example.org"].Owner {
		t.Fatalf("re-added owner cannot act: %q", tm.reason(s, h))
	}
	// Her new term ranks after bob: she was made owner again later.
	if owners(s) != "alice@example.org,bob@example.org,carol@example.org" {
		t.Fatalf("owners %s", owners(s))
	}
	if len(s.Keys["carol@example.org"]) != 2 {
		t.Fatalf("keys %v", s.Keys)
	}
}

func TestTwoKeysForOneEmail(t *testing.T) {
	tm := setup(t)
	base := tm.heads()
	k1, k2 := newActor("erin-laptop"), newActor("erin-desktop")
	h1 := tm.add(tm.signer("alice@example.org").AddMember(base, "erin@example.org", k1.pub, "", t0))
	h2 := tm.add(tm.signer("bob@example.org").AddMember(base, "erin@example.org", k2.pub, "", t0))
	s := tm.state()
	if s.Effective[h1] == s.Effective[h2] {
		t.Fatalf("both or neither: %v %v", s.Effective[h1], s.Effective[h2])
	}
	if s.Members["erin@example.org"].Email == "" {
		t.Fatal("erin missing")
	}
}

func TestDuplicateInvite(t *testing.T) {
	tm := setup(t)
	id := strings.Repeat("ab", 16)
	h1 := tm.add(tm.signer("alice@example.org").AddMember(tm.heads(), "erin@example.org", newActor("erin@example.org").pub, id, t0))
	h2 := tm.add(tm.signer("alice@example.org").AddMember(tm.heads(), "frank@example.org", newActor("frank@example.org").pub, id, t0))
	s := tm.state()
	if !s.Effective[h1] || s.Effective[h2] || s.Invites[id] != h1 {
		t.Fatalf("invite reuse: %v %v %q", s.Effective[h1], s.Effective[h2], tm.reason(s, h2))
	}
}

func TestPendingAndBadSignature(t *testing.T) {
	tm := setup(t)
	missing := strings.Repeat("0", 64)
	h := tm.member("alice@example.org", "erin@example.org", []string{missing})
	s := tm.state()
	if s.Effective[h] || len(s.Pending) != 1 || s.Pending[0].Hash != h {
		t.Fatalf("pending %+v", s.Pending)
	}
	forged := tm.signer("alice@example.org").AddMember(tm.heads(), "mallory@example.org", newActor("m").pub, "", t0)
	forged.Args.Email = "zed@example.org"
	_, bad := Replay(tm.id, append(tm.ops, forged))
	if len(bad) != 1 {
		t.Fatalf("forged op accepted")
	}
}

// Replay is a function of the set: input order does not matter.
func TestReplayIgnoresInputOrder(t *testing.T) {
	tm := setup(t)
	base := tm.heads()
	tm.add(tm.signer("bob@example.org").Remove(base, "carol@example.org", nil, "", t0)...)
	tm.add(tm.signer("carol@example.org").Remove(base, "bob@example.org", nil, "", t0)...)
	tm.member("carol@example.org", "erin@example.org", base)
	want := tm.state()
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 50; i++ {
		ops := append([]Op(nil), tm.ops...)
		rng.Shuffle(len(ops), func(a, b int) { ops[a], ops[b] = ops[b], ops[a] })
		got, _ := Replay(tm.id, ops)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("order %d differs", i)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	tm := setup(t)
	tm.add(tm.signer("alice@example.org").Remove(tm.heads(), "dave@example.org", []Owned{{"a1b2c3d4e5f6", 2, planA}, {"0f9e8d7c6b5a", 0, ""}}, "alice@example.org", t0)...)
	for _, o := range tm.ops {
		line := o.Encode()
		d, err := Decode(line)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if d.Hash() != o.Hash() || !d.VerifySig() || string(d.Encode()) != string(line) {
			t.Fatalf("round trip %s", line)
		}
	}
	// The canonical form is fixed: keys sorted, no spaces.
	c := string(tm.ops[0].Encode())
	if !strings.HasPrefix(c, `{"args":{"email":"alice@example.org","name":"parcels"},"at":"2026-11-20T09:00:00Z","op":"create-team","prev":[],"sig":"`) ||
		!strings.HasSuffix(c, `","team":"","v":2}`) {
		t.Fatalf("canonical %s", c)
	}
}

func TestDecodeRefuses(t *testing.T) {
	good := string(newTeam(t).ops[0].Encode())
	for name, line := range map[string]string{
		"unknown field": strings.Replace(good, `"v":2}`, `"v":2,"x":2}`, 1),
		"unknown kind":  strings.Replace(good, `create-team`, `make-team`, 1),
		"extra arg":     strings.Replace(good, `"name":"parcels"`, `"name":"parcels","pubkey":"x"`, 1),
		"bad name":      strings.Replace(good, `"parcels"`, `"Parcels!"`, 1),
		"float":         strings.Replace(good, `"v":2}`, `"v":2.5}`, 1),
		"not json":      good[:20],
		"trailing":      good + " {}",
	} {
		if _, err := Decode([]byte(line)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, v := range []string{"1", "3"} {
		other := strings.Replace(good, `"v":2}`, `"v":`+v+`}`, 1)
		if _, err := Decode([]byte(other)); !IsUnsupported(err) {
			t.Errorf("v%s: %v", v, err)
		}
	}
	// assign-domain is gone in rule version 2.
	dom := strings.Replace(strings.Replace(good, `create-team`, `assign-domain`, 1), `"name":"parcels"`, `"domain":"lockers"`, 1)
	if _, err := Decode([]byte(dom)); err == nil {
		t.Error("assign-domain accepted")
	}
	// The cut-off and its plan hash go together.
	tm := setup(t)
	sg := tm.signer("alice@example.org")
	for name, o := range map[string]Op{
		"plan without release": sg.Reassign(tm.heads(), "a1b2c3d4e5f6", "bob@example.org", "carol@example.org", 0, planA, t0),
		"release without plan": sg.Reassign(tm.heads(), "a1b2c3d4e5f6", "bob@example.org", "carol@example.org", 3, "", t0),
		"short plan":           sg.Reassign(tm.heads(), "a1b2c3d4e5f6", "bob@example.org", "carol@example.org", 3, "abc", t0),
	} {
		if _, err := Decode(o.Encode()); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	unsorted := Op{V: RuleVersion, Team: strings.Repeat("a", 64), Kind: AddOwner, Args: Args{Email: "a@b.c"},
		Prev: []string{strings.Repeat("b", 64), strings.Repeat("a", 64)}, Signer: EncodeKey(newActor("x").pub)}
	if _, err := Decode(unsorted.Encode()); err == nil {
		t.Error("unsorted prev accepted")
	}
}

func FuzzDecode(f *testing.F) {
	tm := setup(&testing.T{})
	for _, o := range tm.ops {
		f.Add(o.Encode())
	}
	f.Fuzz(func(t *testing.T, line []byte) {
		o, err := Decode(line)
		if err != nil {
			return
		}
		// Whatever decodes re-encodes to something that decodes to the
		// same operation.
		again, err := Decode(o.Encode())
		if err != nil || again.Hash() != o.Hash() {
			t.Fatalf("unstable: %s", line)
		}
	})
}

func sortedKeys(m map[string]Member) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
