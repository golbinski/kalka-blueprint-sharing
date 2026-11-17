package teamlog

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/name"
)

func testInvite() (Invite, actor, actor) {
	owner, joiner := newActor("alice@example.org"), newActor("erin@example.org")
	iv := Invite{TeamName: "parcels", Team: strings.Repeat("c", 64), Owner: EncodeKey(owner.pub),
		Inviter: "alice@example.org", Email: "erin@example.org", Secret: bytes.Repeat([]byte{7}, 32), Expires: t0.Add(DefaultInviteTTL),
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
	if err != nil || got.Email != iv.Email || !got.Expires.Equal(iv.Expires) || got.ID() != iv.ID() || got.Inviter != "alice@example.org" {
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

func TestParseJoin(t *testing.T) {
	iv, _, _ := testInvite()
	tok := iv.Token()
	link := iv.Link()
	if link != "kalka://join/"+tok {
		t.Fatalf("link %q", link)
	}
	for _, in := range []string{
		link,
		tok,
		strings.ToLower(link),
		strings.ToLower(tok),
		"KALKA://JOIN/" + strings.ToLower(tok),
		"Kalka://Join/" + tok,
		"  \n\t" + link + " \r\n",
		"<" + link + ">",
		" < " + tok + " > ",
		link + "/",
		link[:40] + "\n  " + link[40:], // wrapped by a mail client
	} {
		got, err := ParseJoin(in)
		if err != nil || got.ID() != iv.ID() || got.Inviter != iv.Inviter {
			t.Fatalf("ParseJoin(%q): %v", in, err)
		}
		if n, _ := NormalizeJoin(in); n != tok {
			t.Fatalf("NormalizeJoin(%q) = %q", in, n)
		}
	}
	damaged := []byte(tok)
	if i := len(damaged) / 2; damaged[i] == 'A' { // still base32: only the checksum can tell
		damaged[i] = 'B'
	} else {
		damaged[i] = 'A'
	}
	for name, in := range map[string]string{
		"another scheme":   "https://join/" + tok,
		"another scheme 2": "kalkax://join/" + tok,
		"another path":     "kalka://open/" + tok,
		"no path":          "kalka://" + tok,
		"extra parts":      link + "/more",
		"two slashes":      link + "//",
		"a query":          link + "?team=x",
		"a fragment":       link + "#x",
		"credentials":      "kalka://user@join/" + tok,
		"empty link":       "kalka://join/",
		"empty":            "  ",
		"brackets only":    "<>",
		"over 4 KiB":       link + strings.Repeat(" ", MaxJoinInput),
		"damaged checksum": string(damaged),
		"script":           "kalka://join/<script>",
		"javascript":       "javascript:alert(1)",
	} {
		if _, err := ParseJoin(in); !errors.Is(err, ErrInviteToken) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
}

func TestInviteNeedsInviter(t *testing.T) {
	iv, _, _ := testInvite()
	iv.Inviter = ""
	if _, err := ParseToken(iv.Token()); !errors.Is(err, ErrInviteToken) {
		t.Fatalf("an invite without an inviter: %v", err)
	}
}

// A token with a field this kalka does not know, and a matching checksum,
// says it needs a newer kalka.
func TestInviteFromNewerKalka(t *testing.T) {
	msg := []byte(name.InviteContext + "\n" + `{"later":1}`)
	sum := sha256.Sum256(msg)
	tok := tokenEnc.EncodeToString(append(msg, sum[:4]...))
	_, err := ParseToken(tok)
	if !errors.Is(err, ErrInviteToken) || !strings.Contains(err.Error(), "newer kalka") {
		t.Fatalf("%v", err)
	}
}
