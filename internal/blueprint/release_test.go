package blueprint

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func testKey(s string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte(s))
	return ed25519.NewKeyFromSeed(seed[:])
}

func signerOf(k ed25519.PrivateKey) Signer {
	return func(m []byte) []byte { return ed25519.Sign(k, m) }
}

const relDoc = "---\nkalka_id: 4be1c0ffee01\nowner: dana@example.org\nteam: parcels\n---\n# Redelivery\n\n## Scope\nThe redelivery module.\n"

func TestEntrySignatureV2(t *testing.T) {
	k := testKey("dana")
	d := mustParse(t, relDoc)
	e, err := Append(d, KindComment, "dana@example.org", time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC), "scope", "", "", "Three attempts at most.", signerOf(k))
	if err != nil {
		t.Fatal(err)
	}
	pub := k.Public().(ed25519.PublicKey)
	if e.Verify(d.KalkaID(), pub) != SigOK {
		t.Fatal("v2 does not verify")
	}
	// The signature survives a round trip through the file.
	again := mustParse(t, string(d.Bytes()))
	got := again.Items()[0].Entry
	if got.Sig != e.Sig || got.Verify(d.KalkaID(), pub) != SigOK {
		t.Fatalf("after parse: %q", got.Header())
	}
	// Another document, another key, another text: no.
	if e.Verify("000000000000", pub) != SigBad {
		t.Fatal("verified for another kalka_id")
	}
	if e.Verify(d.KalkaID(), testKey("eve").Public().(ed25519.PublicKey)) != SigBad {
		t.Fatal("verified with another key")
	}
	// A moved signature: same id is impossible to keep cheaply, but even
	// with the id forced, the digest differs and v2 fails.
	moved := *e
	moved.Body = "Five attempts."
	moved.ID = moved.ComputeID()
	if moved.Verify(d.KalkaID(), pub) != SigBad {
		t.Fatal("moved signature verifies")
	}
	if (&Entry{Kind: KindComment}).Verify(d.KalkaID(), pub) != Unsigned {
		t.Fatal("unsigned")
	}
}

func TestNewHandoffSigned(t *testing.T) {
	k := testKey("dana")
	h, _ := NewHandoff("4be1c0ffee01", "dana@example.org", "erin@example.org", time.Now(), "0123abcd", signerOf(k))
	if h.Verify("4be1c0ffee01", k.Public().(ed25519.PublicKey)) != SigOK {
		t.Fatal("new handoff is not signed")
	}
}

func TestReleaseEntry(t *testing.T) {
	k := testKey("dana")
	d := mustParse(t, relDoc)
	e, err := NewRelease(d, "dana@example.org", time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC), 3, false,
		[]string{"7c2e91ab04f3"}, "Added the invariant on redelivery attempts.", signerOf(k))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	again := mustParse(t, string(d.Bytes()))
	got := again.Items()[0].Entry
	r, ok := got.Release()
	if !ok || r.Version != 3 || r.Plan != d.PlanHash() || r.Owner != "dana@example.org" || r.Deleted ||
		len(r.Incorporates) != 1 || r.Note != "Added the invariant on redelivery attempts." {
		t.Fatalf("release %+v %v\n%s", r, ok, d.Bytes())
	}
	if got.Verify(d.KalkaID(), k.Public().(ed25519.PublicKey)) != SigOK {
		t.Fatal("release signature")
	}
	// The plan hash ignores the review log and line endings.
	crlf := mustParse(t, strings.ReplaceAll(string(again.Bytes()), "\n", "\r\n"))
	if crlf.PlanHash() != d.PlanHash() || again.PlanHash() != d.PlanHash() {
		t.Fatal("plan hash depends on log or line endings")
	}
	if again.Team() != "parcels" || again.Deleted() {
		t.Fatal("team keys")
	}
	tomb, _ := NewRelease(d, "dana@example.org", time.Now(), 4, true, nil, "", signerOf(k))
	if r, _ := tomb.Release(); !r.Deleted || r.Version != 4 {
		t.Fatalf("tombstone %+v", r)
	}
	if _, err := Append(d, KindRelease, "dana@example.org", time.Now(), "", "", "", "v9 · plan:x", nil); err == nil {
		t.Fatal("hand-written release accepted")
	}
	if _, err := NewRelease(d, "dana@example.org", time.Now(), 1, false, nil, "", nil); err == nil {
		t.Fatal("unsigned release")
	}
	for _, body := range []string{"v0 · plan:" + strings.Repeat("a", 64) + " · owner:a@b.c", "v1 · plan:abc · owner:a@b.c", "v1 · plan:" + strings.Repeat("a", 64)} {
		bad, _ := NewEntry(KindRelease, "a@b.c", time.Now(), "", "0123abcd", body)
		if _, ok := bad.Release(); ok {
			t.Errorf("accepted %q", body)
		}
	}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
