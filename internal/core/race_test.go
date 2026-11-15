package core

// The race of spec 7.3, enumerated (T3): a former owner releases version
// n+1 concurrently with a reassignment at cut-off n. Every delivery order
// of the operation and the two releases ends with the same chain on every
// peer; after one settling round, the same plan and owner too.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/teamlog"
)

var raceT0 = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

func raceKey(email string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("race " + email))
	return ed25519.NewKeyFromSeed(seed[:])
}

func raceSigner(email string) bp.Signer {
	k := raceKey(email)
	return func(m []byte) []byte { return ed25519.Sign(k, m) }
}

// raceTeam is a team log: marek and zofia are team owners, ewa and olga
// members.
func raceTeam(t *testing.T) (string, []teamlog.Op) {
	t.Helper()
	create := teamlog.Create(raceKey("marek@parcels.example"), "parcels", "marek@parcels.example", raceT0)
	team := create.Hash()
	ops := []teamlog.Op{create}
	sg := teamlog.Signer{Key: raceKey("marek@parcels.example"), Team: team}
	for _, e := range []string{"zofia@parcels.example", "ewa@parcels.example", "olga@parcels.example"} {
		ops = append(ops, sg.AddMember(teamlog.Heads(ops), e, raceKey(e).Public().(ed25519.PublicKey), "", raceT0))
	}
	ops = append(ops, sg.AddOwner(teamlog.Heads(ops), "zofia@parcels.example", raceT0))
	return team, ops
}

const raceID = "6f1c2b9e-4d7a-4c3e-9b1a-2e5f8c7d6a41"

// raceRelease is doc with body text and owner, released as version v by
// author.
func raceRelease(t *testing.T, base *bp.Document, owner, text, author string, v int64) *bp.Document {
	t.Helper()
	raw := "---\nkalka_id: " + raceID + "\nowner: " + owner + "\nteam: parcels\n---\n# Depot routing\n\n## Plan\n\n" + text + "\n"
	d, err := bp.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if base != nil {
		for _, e := range entriesOf(base) {
			if err := d.AddEntry(e); err != nil {
				t.Fatal(err)
			}
		}
	}
	e, err := bp.NewRelease(d, author, raceT0.Add(time.Duration(v)*time.Hour), v, false, nil, "", raceSigner(author))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	d, err = d.Reparse()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// racePeer is one peer: the log operations and the document it holds.
type racePeer struct {
	team string
	ops  []teamlog.Op
	doc  *bp.Document
}

func (p *racePeer) state(t *testing.T) logTeam {
	st, bad := teamlog.Replay(p.team, p.ops)
	if len(bad) > 0 {
		t.Fatalf("bad ops: %v", bad)
	}
	return logTeam{st: st}
}

// deliver gives the peer one item: an operation or a document.
func (p *racePeer) deliver(t *testing.T, item any) {
	switch x := item.(type) {
	case teamlog.Op:
		p.ops = append(p.ops, x)
	case *bp.Document:
		res := merge.Docs(p.doc, x, p.state(t))
		if res.Outcome == merge.Updated || res.Outcome == merge.New {
			p.doc = res.Doc
		}
	}
}

// summary is what must agree: the chain, and the plan and owner held.
func (p *racePeer) chain(t *testing.T) string {
	ch := merge.Releases(entriesOf(p.doc), raceID, p.state(t))
	var b strings.Builder
	for _, l := range ch.Releases {
		fmt.Fprintf(&b, "v%d:%s:%s ", l.Release.Version, l.Entry.Author, l.Release.Plan[:8])
	}
	next, _ := nextReleaser(ch, p.doc.Owner())
	fmt.Fprintf(&b, "disputed=%v next=%s", ch.Disputed, next)
	return b.String()
}

func (p *racePeer) held() string { return p.doc.Owner() + " " + p.doc.PlanHash()[:8] }

func permutations(n int) [][]int {
	if n == 0 {
		return [][]int{{}}
	}
	var out [][]int
	for _, rest := range permutations(n - 1) {
		for i := 0; i <= len(rest); i++ {
			p := append(append(append([]int{}, rest[:i]...), n-1), rest[i:]...)
			out = append(out, p)
		}
	}
	return out
}

// runRace delivers items to a fresh peer in every order and checks that
// all end alike. docs are re-delivered once at the end: a settling round,
// as replication offers again a document whose plan differs.
func runRace(t *testing.T, team string, base []teamlog.Op, start *bp.Document, items []any, docs []*bp.Document) (chain, held string) {
	t.Helper()
	for _, order := range permutations(len(items)) {
		p := &racePeer{team: team, ops: append([]teamlog.Op(nil), base...), doc: start}
		for _, i := range order {
			p.deliver(t, items[i])
		}
		c := p.chain(t)
		for _, d := range docs {
			p.deliver(t, d)
		}
		if c2 := p.chain(t); c2 != c {
			t.Fatalf("order %v: the settling round changed the chain:\n  %s\n  %s", order, c, c2)
		}
		h := p.held()
		if chain == "" {
			chain, held = c, h
			continue
		}
		if c != chain || h != held {
			t.Fatalf("order %v differs:\n  %s | %s\n  %s | %s", order, c, h, chain, held)
		}
	}
	return chain, held
}

func TestReassignmentRaceEveryOrder(t *testing.T) {
	team, base := raceTeam(t)
	// Ewa owns the plan at version 5.
	var v *bp.Document
	for i := int64(1); i <= 5; i++ {
		v = raceRelease(t, v, "ewa@parcels.example", fmt.Sprintf("Route by postcode, take %d.", i), "ewa@parcels.example", i)
	}
	ch := merge.Releases(entriesOf(v), raceID, logTeam{st: mustReplay(t, team, base)})
	head, _ := ch.Head()
	// Marek reassigns to Olga with after 5; Ewa, not knowing, releases 6.
	marek := teamlog.Signer{Key: raceKey("marek@parcels.example"), Team: team}
	op := marek.Reassign(teamlog.Heads(base), raceID, "ewa@parcels.example", "olga@parcels.example", 5, head.Release.Plan, raceT0)
	ewa6 := raceRelease(t, v, "ewa@parcels.example", "Route by van size.", "ewa@parcels.example", 6)
	olga6 := raceRelease(t, v, "olga@parcels.example", "Route by depot load.", "olga@parcels.example", 6)

	chain, held := runRace(t, team, base, v, []any{op, ewa6, olga6}, []*bp.Document{ewa6, olga6})
	if !strings.Contains(chain, "v6:olga@parcels.example:"+olga6.PlanHash()[:8]) || !strings.HasSuffix(chain, "next=olga@parcels.example") {
		t.Fatalf("chain %s", chain)
	}
	if held != "olga@parcels.example "+olga6.PlanHash()[:8] {
		t.Fatalf("held %s", held)
	}
}

// Two team owners reassign one document concurrently, at one cut-off:
// every order converges on the same anchor and chain.
func TestConcurrentReassignmentsConverge(t *testing.T) {
	team, base := raceTeam(t)
	v1 := raceRelease(t, nil, "ewa@parcels.example", "Route by postcode.", "ewa@parcels.example", 1)
	plan := merge.Releases(entriesOf(v1), raceID, logTeam{st: mustReplay(t, team, base)}).Releases[0].Release.Plan
	heads := teamlog.Heads(base)
	byMarek := teamlog.Signer{Key: raceKey("marek@parcels.example"), Team: team}.Reassign(heads, raceID, "ewa@parcels.example", "olga@parcels.example", 1, plan, raceT0)
	byZofia := teamlog.Signer{Key: raceKey("zofia@parcels.example"), Team: team}.Reassign(heads, raceID, "ewa@parcels.example", "zofia@parcels.example", 1, plan, raceT0)
	olga2 := raceRelease(t, v1, "olga@parcels.example", "Route by load.", "olga@parcels.example", 2)
	zofia2 := raceRelease(t, v1, "zofia@parcels.example", "Route by van.", "zofia@parcels.example", 2)

	chain, held := runRace(t, team, base, v1, []any{byMarek, byZofia, olga2, zofia2}, []*bp.Document{olga2, zofia2})
	st := mustReplay(t, team, append(append([]teamlog.Op(nil), base...), byMarek, byZofia))
	as := st.Reassigned[raceID]
	if len(as) != 1 {
		t.Fatalf("anchors %+v", as)
	}
	winner := as[0].Owner
	if !strings.Contains(chain, "v2:"+winner) || !strings.HasPrefix(held, winner) {
		t.Fatalf("winner %s, chain %s, held %s", winner, chain, held)
	}
}

func mustReplay(t *testing.T, team string, ops []teamlog.Op) *teamlog.State {
	t.Helper()
	st, bad := teamlog.Replay(team, ops)
	if len(bad) > 0 {
		t.Fatalf("bad ops: %v", bad)
	}
	return st
}
