package identity

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

func TestKeyCreatedOncePrivate(t *testing.T) {
	dir := t.TempDir()
	k1, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !k1.Equal(k2) {
		t.Fatal("key changed between loads")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(dir, KeyFile))
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", info.Mode().Perm())
		}
	}
}

func TestCodeSymmetric(t *testing.T) {
	a, _, _ := ed25519.GenerateKey(nil)
	b, _, _ := ed25519.GenerateKey(nil)
	c := Code(a, b)
	if c != Code(b, a) {
		t.Fatal("code differs by side")
	}
	if !regexp.MustCompile(`^[A-Z2-7]{4}-[A-Z2-7]{4}$`).MatchString(c) {
		t.Fatalf("code %q", c)
	}
	d, _, _ := ed25519.GenerateKey(nil)
	if Code(a, d) == c {
		t.Fatal("different pair, same code")
	}
}

func TestPinning(t *testing.T) {
	dir := t.TempDir()
	p, _ := OpenPeers(dir, nil)
	k1, _, _ := ed25519.GenerateKey(nil)
	k2, _, _ := ed25519.GenerateKey(nil)
	if err := p.Pin("Bob@corp.com", k1); err != nil {
		t.Fatal(err)
	}
	if err := p.Pin("bob@corp.com", k1); err != nil {
		t.Fatal(err)
	}
	if e, _ := p.Get("bob@corp.com"); e.Verified {
		t.Fatal("first contact must be unverified")
	}
	if err := p.SetVerified("bob@corp.com", k1); err != nil {
		t.Fatal(err)
	}
	var kc *KeyChangedError
	if err := p.Pin("bob@corp.com", k2); !errors.As(err, &kc) {
		t.Fatalf("want KeyChangedError, got %v", err)
	}
	// The block persists, even for the old key, until forgotten.
	p2, _ := OpenPeers(dir, nil)
	if err := p2.Pin("bob@corp.com", k1); !errors.As(err, &kc) {
		t.Fatalf("after reload: %v", err)
	}
	if k, _ := p2.Key("bob@corp.com"); !k.Equal(k1) {
		t.Fatal("pinned key replaced")
	}
	if ok, err := p2.Forget("bob@corp.com"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := p2.Pin("bob@corp.com", k2); err != nil {
		t.Fatal(err)
	}
	if e, _ := p2.Get("bob@corp.com"); e.Verified || e.KeyChanged != nil {
		t.Fatalf("re-pin state %+v", e)
	}
}
