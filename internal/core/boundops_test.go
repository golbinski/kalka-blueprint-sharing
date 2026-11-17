package core

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/teamlog"
)

func opKey(s string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte(s))
	return ed25519.NewKeyFromSeed(seed[:])
}

// Only operations whose signer an ancestor binds are taken: a stranger's
// validly signed operations are not, nor ones whose ancestors are missing.
func TestBoundOps(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	alice, bob, eve := opKey("alice"), opKey("bob"), opKey("eve")
	create := teamlog.Create(alice, "parcels", "alice@library.example", at)
	team := create.Hash()
	sa := teamlog.Signer{Key: alice, Team: team}
	addBob := sa.AddMember([]string{team}, "bob@library.example", bob.Public().(ed25519.PublicKey), "", at)
	held := []teamlog.Op{create}

	byBob := teamlog.Signer{Key: bob, Team: team}.AddOwner([]string{addBob.Hash()}, "bob@library.example", at)
	byEve := teamlog.Signer{Key: eve, Team: team}.AddOwner([]string{team}, "eve@library.example", at)
	orphan := teamlog.Signer{Key: bob, Team: team}.AddOwner([]string{byEve.Hash()}, "carol@library.example", at)

	got := boundOps(held, []teamlog.Op{addBob, byBob, byEve})
	if len(got) != 2 || got[0].Hash() != addBob.Hash() || got[1].Hash() != byBob.Hash() {
		t.Fatalf("kept %d: %+v", len(got), got)
	}
	// Bob's operation arriving before the one that adds him waits for it.
	if got := boundOps(held, []teamlog.Op{byBob}); len(got) != 0 {
		t.Fatalf("taken without its ancestors: %+v", got)
	}
	// An operation built on a stranger's is not bound either.
	if got := boundOps(held, []teamlog.Op{orphan}); len(got) != 0 {
		t.Fatalf("orphan kept: %+v", got)
	}
}

// newerLine is a signed operation of rule version 3 by key.
func newerLine(key ed25519.PrivateKey, team string, prev []string) teamlog.Op {
	body := `{"args":{},"at":"2027-01-01T00:00:00Z","op":"rotate-key","prev":["` + strings.Join(prev, `","`) + `"],"signer":"` +
		teamlog.EncodeKey(key.Public().(ed25519.PublicKey)) + `","team":"` + team + `","v":3}`
	sum := sha256.Sum256([]byte(body))
	sig := base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, teamlog.SigMessage(hex.EncodeToString(sum[:]))))
	o, err := teamlog.Decode([]byte(strings.Replace(body, `,"signer":`, `,"sig":"`+sig+`","signer":`, 1)))
	if err != nil {
		panic(err)
	}
	return o
}

// An operation of a newer rule version by a bound member may bind a key
// this kalka cannot see bound: what is built on it is kept. A stranger's
// is not.
func TestBoundOpsNewerVersion(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	alice, fresh, eve := opKey("alice"), opKey("alice-new"), opKey("eve")
	create := teamlog.Create(alice, "parcels", "alice@library.example", at)
	team := create.Hash()
	held := []teamlog.Op{create}
	rotate := newerLine(alice, team, []string{team})
	byFresh := teamlog.Signer{Key: fresh, Team: team}.AddOwner([]string{rotate.Hash()}, "bob@library.example", at)
	if got := boundOps(held, []teamlog.Op{rotate, byFresh}); len(got) != 2 {
		t.Fatalf("kept %d of 2", len(got))
	}
	strange := newerLine(eve, team, []string{team})
	if got := boundOps(held, []teamlog.Op{strange}); len(got) != 0 {
		t.Fatalf("a stranger's newer operation kept")
	}
}
