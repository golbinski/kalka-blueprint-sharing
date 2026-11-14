// Package peerid derives the two PeerJS ids of a kalka user (spec 9.2) and
// the proof that registers a key id on `kalka rendezvous` (spec 9.6).
//
//	email_id = "kalka-"  + hex(SHA-256("kalka-peer-v1|" + team + "|" + lowercase(email)))[0:32]
//	key_id   = "kalka-k" + hex(SHA-256("kalka-key-v1|" + pubkey))[0:32]
//
// The two cannot be confused: hex never starts with "k".
package peerid

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/golbinski/kalka/internal/name"
)

const (
	emailPrefix = name.Product + "-"
	keyPrefix   = name.Product + "-k"

	// ProofParam is the query parameter holding the proof.
	ProofParam = "kproof"
	// MaxSkew is how far a proof's time may be from the server's clock.
	MaxSkew = 5 * time.Minute
)

// Email derives the email id. team is the optional config salt (spec 5).
func Email(team, email string) string {
	sum := sha256.Sum256([]byte(name.Product + "-peer-v1|" + team + "|" + strings.ToLower(email)))
	return emailPrefix + hex.EncodeToString(sum[:])[:32]
}

// Key derives the key id of a raw ed25519 public key.
func Key(pub ed25519.PublicKey) string {
	return keyPrefix + Fingerprint(pub)
}

// Fingerprint is the 32 hex digits of a key id, also shown to users as
// the key's fingerprint.
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(append([]byte(name.KeyIDContext+"|"), pub...))
	return hex.EncodeToString(sum[:])[:32]
}

// Display groups a fingerprint for reading aloud: "1a2b 3c4d …".
func Display(fp string) string {
	var b strings.Builder
	for i := 0; i < len(fp); i += 4 {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(fp[i:min(i+4, len(fp))])
	}
	return b.String()
}

// IsKey reports whether id has the shape of a key id.
func IsKey(id string) bool {
	if len(id) != len(keyPrefix)+32 || !strings.HasPrefix(id, keyPrefix) {
		return false
	}
	_, err := hex.DecodeString(id[len(keyPrefix):])
	return err == nil
}

func proofMessage(id string, ts int64) []byte {
	return []byte(name.KeyProofContext + "\n" + id + "\n" + strconv.FormatInt(ts, 10))
}

var b64 = base64.RawURLEncoding

// Proof returns the proof for registering key's id at time now:
// base64url(pubkey) "." unix-seconds "." base64url(signature).
func Proof(key ed25519.PrivateKey, now time.Time) (id, proof string) {
	pub := key.Public().(ed25519.PublicKey)
	id = Key(pub)
	ts := now.Unix()
	sig := ed25519.Sign(key, proofMessage(id, ts))
	return id, b64.EncodeToString(pub) + "." + strconv.FormatInt(ts, 10) + "." + b64.EncodeToString(sig)
}

// Errors of CheckProof.
var (
	ErrNoProof  = errors.New("a key peer id needs a proof of the key")
	ErrBadProof = errors.New("the key proof is malformed or its signature does not verify")
	ErrWrongKey = errors.New("the key proof is for another peer id")
	ErrStale    = errors.New("the key proof is too old or from the future; check the clock")
)

// CheckProof verifies that proof registers id at time now. Nothing is
// stored: a fresh signature is all it takes.
func CheckProof(id, proof string, now time.Time) error {
	if proof == "" {
		return ErrNoProof
	}
	parts := strings.Split(proof, ".")
	if len(parts) != 3 {
		return ErrBadProof
	}
	pub, err := b64.DecodeString(parts[0])
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return ErrBadProof
	}
	ts, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return ErrBadProof
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrBadProof
	}
	if Key(pub) != id {
		return ErrWrongKey
	}
	if d := now.Sub(time.Unix(ts, 0)); d > MaxSkew || d < -MaxSkew {
		return ErrStale
	}
	if !ed25519.Verify(pub, proofMessage(id, ts), sig) {
		return ErrBadProof
	}
	return nil
}
