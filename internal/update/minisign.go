package update

// Release signatures (spec 24.2) are minisign signatures, so a person can
// check a download with the minisign tool as the updater does:
//
//	minisign -Vm SHA256SUMS -P <the public key in SECURITY.md>
//
// Only the prehashed form ("ED": Ed25519 over the BLAKE2b-512 of the file)
// is made and accepted. The trusted comment is signed too, and names the
// release, so a signature cannot be moved to another release.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// SigAsset is the signature of SumsAsset every release has.
const SigAsset = SumsAsset + ".minisig"

// ReleaseKeys are the public keys a release's SHA256SUMS may be signed
// with, in minisign's encoding. A new key is added here a release before
// the old one is retired.
var ReleaseKeys = []string{
	"RWR1mOYbi9qbgJl371Oqgkl243JEuiMNnlOKS0dntAZynje7S5Espx/f", // 2026-11-23
}

var algPrehashed = [2]byte{'E', 'D'}

// PublicKey is a minisign public key.
type PublicKey struct {
	ID  [8]byte
	Key ed25519.PublicKey
}

// ParsePublicKey reads a public key: the base64 line alone, or the whole
// key file with its untrusted comment.
func ParsePublicKey(s string) (PublicKey, error) {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[len(lines)-1]))
	if err != nil || len(raw) != 2+8+ed25519.PublicKeySize || raw[0] != 'E' || raw[1] != 'd' {
		return PublicKey{}, errors.New("not a minisign Ed25519 public key")
	}
	var k PublicKey
	copy(k.ID[:], raw[2:10])
	k.Key = ed25519.PublicKey(bytes.Clone(raw[10:]))
	return k, nil
}

// String is the key in minisign's encoding.
func (k PublicKey) String() string {
	return base64.StdEncoding.EncodeToString(append(append([]byte("Ed"), k.ID[:]...), k.Key...))
}

// KeyFor makes the key pair of a 32-byte Ed25519 seed; its id is the
// first 8 bytes of the public key's SHA-256, so the seed alone is the
// secret to keep.
func KeyFor(seed []byte) (ed25519.PrivateKey, PublicKey, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, PublicKey{}, fmt.Errorf("a signing key is %d bytes, not %d", ed25519.SeedSize, len(seed))
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	sum := sha256.Sum256(pub)
	var k PublicKey
	copy(k.ID[:], sum[:8])
	k.Key = pub
	return priv, k, nil
}

// Sign makes the minisign signature file of msg.
func Sign(priv ed25519.PrivateKey, pub PublicKey, msg []byte, trusted string) []byte {
	if strings.ContainsAny(trusted, "\r\n") {
		panic("a trusted comment is one line")
	}
	h := blake2b.Sum512(msg)
	sig := ed25519.Sign(priv, h[:])
	global := ed25519.Sign(priv, append(bytes.Clone(sig), trusted...))
	var b strings.Builder
	fmt.Fprintf(&b, "untrusted comment: signature from kalka release key\n")
	b.WriteString(base64.StdEncoding.EncodeToString(append(append(algPrehashed[:2:2], pub.ID[:]...), sig...)))
	fmt.Fprintf(&b, "\ntrusted comment: %s\n", trusted)
	b.WriteString(base64.StdEncoding.EncodeToString(global))
	b.WriteString("\n")
	return []byte(b.String())
}

// Verify checks a minisign signature file over msg with one of keys and
// returns its trusted comment.
func Verify(msg, sigFile []byte, keys []PublicKey) (string, error) {
	lines := strings.Split(strings.ReplaceAll(string(sigFile), "\r\n", "\n"), "\n")
	if len(lines) < 4 || !strings.HasPrefix(lines[0], "untrusted comment:") || !strings.HasPrefix(lines[2], "trusted comment: ") {
		return "", errors.New("not a minisign signature")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(raw) != 2+8+ed25519.SignatureSize {
		return "", errors.New("not a minisign signature")
	}
	if raw[0] != algPrehashed[0] || raw[1] != algPrehashed[1] {
		return "", errors.New("the signature is not of the prehashed kind (ED)")
	}
	trusted := strings.TrimPrefix(lines[2], "trusted comment: ")
	global, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil || len(global) != ed25519.SignatureSize {
		return "", errors.New("not a minisign signature")
	}
	var id [8]byte
	copy(id[:], raw[2:10])
	sig := raw[10:]
	for _, k := range keys {
		if k.ID != id {
			continue
		}
		h := blake2b.Sum512(msg)
		if !ed25519.Verify(k.Key, h[:], sig) {
			return "", errors.New("the signature does not match")
		}
		if !ed25519.Verify(k.Key, append(bytes.Clone(sig), trusted...), global) {
			return "", errors.New("the trusted comment's signature does not match")
		}
		return trusted, nil
	}
	return "", errors.New("signed with a key this kalka does not know")
}

// TrustedComment is what a release's signature must say.
func TrustedComment(tag string) string {
	return "kalka " + tag + " " + SumsAsset
}

func releaseKeys(ks []string) ([]PublicKey, error) {
	var out []PublicKey
	for _, s := range ks {
		k, err := ParsePublicKey(s)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}
