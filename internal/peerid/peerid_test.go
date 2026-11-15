package peerid

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func TestDerivation(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	key := ed25519.NewKeyFromSeed(seed)
	pub := key.Public().(ed25519.PublicKey)
	sum := sha256.Sum256(append([]byte("kalka-key-v1|"), pub...))
	want := "kalka-k" + hex.EncodeToString(sum[:])[:32]
	if got := Key(pub); got != want || !IsKey(got) {
		t.Fatalf("Key = %q, want %q", got, want)
	}
	if IsKey("kalka-" + hex.EncodeToString(make([]byte, 16))) {
		t.Fatal("IsKey accepts an id without the key prefix")
	}
	if IsKey("kalka-kzz") || IsKey("kalka-k"+hex.EncodeToString(make([]byte, 15))+"zz") {
		t.Fatal("IsKey accepts junk")
	}
	if Display("1a2b3c4d5e") != "1a2b 3c4d 5e" {
		t.Fatal(Display("1a2b3c4d5e"))
	}
}

func TestProof(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(nil)
	_, other, _ := ed25519.GenerateKey(nil)
	now := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	id, proof := Proof(key, now)
	if err := CheckProof(id, proof, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	otherID, otherProof := Proof(other, now)
	cases := []struct {
		id, proof string
		at        time.Time
		want      error
	}{
		{id, "", now, ErrNoProof},
		{id, "x.y", now, ErrBadProof},
		{id, otherProof, now, ErrWrongKey}, // someone else's proof for this id
		{otherID, proof, now, ErrWrongKey}, // this proof for someone else's id
		{id, proof, now.Add(10 * time.Minute), ErrStale},
		{id, proof, now.Add(-10 * time.Minute), ErrStale},
	}
	for i, c := range cases {
		if err := CheckProof(c.id, c.proof, c.at); !errors.Is(err, c.want) {
			t.Errorf("case %d: %v, want %v", i, err, c.want)
		}
	}
	// A signature made by another key over this id does not verify.
	forged := proof[:len(proof)-10] + "AAAAAAAAAA"
	if err := CheckProof(id, forged, now); !errors.Is(err, ErrBadProof) {
		t.Errorf("forged: %v", err)
	}
}
