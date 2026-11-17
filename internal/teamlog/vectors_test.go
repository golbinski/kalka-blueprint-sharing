package teamlog

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"
	"time"
)

var updateVectors = flag.Bool("update-vectors", false, "rewrite testdata/teamlog/vectors.json")

// vector is one operation as another implementation must reproduce it:
// its key seed, the canonical line, the hash and the signature.
type vector struct {
	Name   string `json:"name"`
	Seed   string `json:"key_seed"` // hex of the 32-byte ed25519 seed
	Line   string `json:"line"`     // the canonical line, signature included
	Signed string `json:"signed"`   // the line without sig: what is hashed
	Hash   string `json:"hash"`
	Sig    string `json:"sig"`
}

func vectorKey(name string) (ed25519.PrivateKey, string) {
	seed := sha256.Sum256([]byte("kalka test vector " + name))
	return ed25519.NewKeyFromSeed(seed[:]), hex.EncodeToString(seed[:])
}

// The canonical encoding, hash and signature of one operation of each
// kind are fixed in testdata/teamlog/vectors.json (docs/teamlog.md). A
// change to any byte is a change of the format.
func TestVectors(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	alice, aliceSeed := vectorKey("alice")
	bob, _ := vectorKey("bob")
	create := Create(alice, "parcels", "alice@library.example", at)
	team := create.Hash()
	sa := Signer{Key: alice, Team: team}
	add := sa.AddMember([]string{team}, "zoë+desk@library.example", bob.Public().(ed25519.PublicKey), strings.Repeat("0f", 16), at)
	prev := []string{add.Hash()}
	doc := "6f1c2b9e-4d7a-4c3e-9b1a-2e5f8c7d6a40"
	digest := strings.Repeat("ab", 32)
	ops := []struct {
		name, seed string
		op         Op
	}{
		{"create-team", aliceSeed, create},
		{"add-member", aliceSeed, add},
		{"add-owner", aliceSeed, sa.AddOwner(prev, "zoë+desk@library.example", at)},
		{"remove-owner", aliceSeed, sa.RemoveOwner(prev, "zoë+desk@library.example", at)},
		{"remove-member", aliceSeed, sa.Remove(prev, "zoë+desk@library.example", nil, "alice@library.example", at)[0]},
		{"reassign-document", aliceSeed, sa.Reassign(prev, doc, "zoë+desk@library.example", "alice@library.example", 3, at)},
		{"accept-transfer", aliceSeed, sa.Accept(prev, doc, "alice@library.example", digest, at)},
		{"decline-transfer", aliceSeed, sa.Decline(prev, doc, digest, at)},
	}
	var got []vector
	for _, x := range ops {
		line := string(x.op.Encode())
		if _, err := Decode([]byte(line)); err != nil {
			t.Fatalf("%s: %v", x.name, err)
		}
		got = append(got, vector{Name: x.name, Seed: x.seed, Line: line, Signed: string(withoutSig([]byte(line))), Hash: x.op.Hash(), Sig: x.op.Sig})
	}
	b, _ := json.MarshalIndent(got, "", "  ")
	b = append(b, '\n')
	path := "../../testdata/teamlog/vectors.json"
	if *updateVectors {
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update-vectors to create)", err)
	}
	if string(want) != string(b) {
		t.Fatalf("the vectors changed: the encoding, hash or signature of an operation is different\n%s", b)
	}
	// Each vector checks on its own: the hash of the signed bytes, the
	// signature over the context and the hash.
	var vs []vector
	if err := json.Unmarshal(want, &vs); err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		sum := sha256.Sum256([]byte(v.Signed))
		o, err := Decode([]byte(v.Line))
		if err != nil || hex.EncodeToString(sum[:]) != v.Hash || o.Hash() != v.Hash || !o.VerifySig() {
			t.Errorf("%s: %v", v.Name, err)
		}
	}
}
