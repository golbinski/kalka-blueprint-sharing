package teamlog

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func testInvite() (Invite, actor, actor) {
	owner, joiner := newActor("alice@example.org"), newActor("erin@example.org")
	iv := Invite{TeamName: "parcels", Team: strings.Repeat("c", 64), Owner: EncodeKey(owner.pub),
		Email: "erin@example.org", Secret: bytes.Repeat([]byte{7}, 32), Expires: t0.Add(DefaultInviteTTL),
		Rendezvous: "wss://rdv.example.org/kalka"}
	return iv, owner, joiner
}

func TestInviteToken(t *testing.T) {
	iv, owner, joiner := testInvite()
	tok := iv.Token()
	if strings.ContainsAny(tok, "\n =") {
		t.Fatalf("token not one line: %q", tok)
	}
	// Pasted in lower case and wrapped: still fine.
	got, err := ParseToken(strings.ToLower(tok[:30]) + "\n  " + tok[30:])
	if err != nil || got.Email != iv.Email || !got.Expires.Equal(iv.Expires) || got.ID() != iv.ID() {
		t.Fatalf("parse: %v %+v", err, got)
	}
	if len(iv.ID()) != 32 {
		t.Fatalf("id %q", iv.ID())
	}

	jk, ok := EncodeKey(joiner.pub), EncodeKey(owner.pub)
	proof := Proof(got.Secret, got.Team, ok, jk)
	if err := iv.Check(iv.Team, ok, jk, proof, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := iv.Check(iv.Team, ok, jk, proof, t0.Add(DefaultInviteTTL)); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("expired: %v", err)
	}
	if err := iv.Check(strings.Repeat("d", 64), ok, jk, proof, t0); !errors.Is(err, ErrInviteWrongTeam) {
		t.Fatalf("wrong team: %v", err)
	}
	// A proof made for another joiner key (a relayed proof) fails.
	other := EncodeKey(newActor("mallory@example.org").pub)
	if err := iv.Check(iv.Team, ok, other, proof, t0); !errors.Is(err, ErrInviteProof) {
		t.Fatalf("relayed proof: %v", err)
	}
	if err := iv.Check(iv.Team, ok, jk, Proof(bytes.Repeat([]byte{8}, 32), iv.Team, ok, jk), t0); !errors.Is(err, ErrInviteProof) {
		t.Fatalf("wrong secret: %v", err)
	}
}

func TestInviteTampered(t *testing.T) {
	iv, _, _ := testInvite()
	tok := []byte(iv.Token())
	for i := 0; i < len(tok); i += 7 {
		bad := append([]byte(nil), tok...)
		if bad[i] == 'A' {
			bad[i] = 'B'
		} else {
			bad[i] = 'A'
		}
		if _, err := ParseToken(string(bad)); err == nil {
			t.Fatalf("tampered at %d accepted", i)
		}
	}
	if _, err := ParseToken("hello"); err == nil {
		t.Fatal("junk accepted")
	}
}
