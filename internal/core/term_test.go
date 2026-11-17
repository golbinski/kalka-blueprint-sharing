package core

import (
	"crypto/ed25519"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/teamlog"
)

// An entry is signed under a term of one team: it verifies there and in
// no other team, even one with the same name and the same member key. A
// signature without a term verifies in no team.
func TestEntryTermPinsTeam(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	key := raceKey("ewa@parcels.example")
	pub := key.Public().(ed25519.PublicKey)
	teamOf := func(at time.Time) logTeam {
		c := teamlog.Create(key, "parcels", "ewa@parcels.example", at)
		st, _ := teamlog.Replay(c.Hash(), []teamlog.Op{c})
		return logTeam{st: st}
	}
	a, b := teamOf(at), teamOf(at.Add(time.Hour))
	if a.st.Team == b.st.Team {
		t.Fatal("one team")
	}
	e, err := bp.NewEntry(bp.KindComment, "ewa@parcels.example", at, "", "3fa9c1d2", "Lockers first.")
	if err != nil {
		t.Fatal(err)
	}
	sign := func(term string) *bp.Entry {
		c := *e
		c.Sign(raceID, bp.Signer{Fn: func(m []byte) []byte { return ed25519.Sign(key, m) }, Term: term})
		return &c
	}
	inA := sign(TermIn(a.st, "ewa@parcels.example", pub))
	if inA.Term == "" || !merge.VerifiedBy(a, raceID, inA) || merge.VerifiedBy(b, raceID, inA) {
		t.Fatalf("term %q: in a %v, in b %v", inA.Term, merge.VerifiedBy(a, raceID, inA), merge.VerifiedBy(b, raceID, inA))
	}
	if bare := sign(""); merge.VerifiedBy(a, raceID, bare) {
		t.Fatal("a signature without a term verified")
	}
	// The term is signed: moving the signature to another term fails.
	moved := *inA
	moved.Term = TermIn(b.st, "ewa@parcels.example", pub)
	if merge.VerifiedBy(b, raceID, &moved) {
		t.Fatal("a signature moved to another term verified")
	}
}
