package transport

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/peerid"
)

// Proto is the data channel protocol version.
const Proto = 1

// PeerID derives the email peer id (spec 9.2).
func PeerID(team, email string) string { return peerid.Email(team, email) }

// KeyPeerID derives the key peer id (spec 9.2).
func KeyPeerID(pub ed25519.PublicKey) string { return peerid.Key(pub) }

// CapKeyID is announced by peers that register and accept key ids.
const CapKeyID = "keyid/1"

// Identity is the local user.
type Identity struct {
	Email string
	Key   ed25519.PrivateKey
	// Caps are announced in hello (spec 9.5).
	Caps []string
}

// Peer is the authenticated other side.
type Peer struct {
	Email string
	Key   ed25519.PublicKey
	// Caps are the capabilities the peer announced; none from an older
	// kalka.
	Caps []string
	// ViaKey is set when the connection runs between key ids.
	ViaKey bool
}

// Has reports whether the peer announced capability c.
func (p Peer) Has(c string) bool { return slices.Contains(p.Caps, c) }

// Verifier decides whether a claimed key is acceptable for an email: it
// implements pinning (spec 8.2). It returns an error to refuse.
type Verifier func(email string, key ed25519.PublicKey) error

type helloMsg struct {
	Type   string `json:"type"`
	Proto  int    `json:"proto"`
	Email  string `json:"email"`
	PubKey string `json:"pubkey"`
	Nonce  string `json:"nonce"`
	// Caps lists capabilities (spec 9.5); older peers send none and
	// ignore it.
	Caps []string `json:"caps,omitempty"`
}

type authMsg struct {
	Type string `json:"type"`
	Sig  string `json:"sig"`
}

// HandshakeTimeout bounds hello and auth.
const HandshakeTimeout = 30 * time.Second

// authMessage is what each side signs: context, own DTLS fingerprint, the
// peer's DTLS fingerprint and the peer's nonce (spec 8.4).
func authMessage(own, peer [sha256.Size]byte, peerNonce []byte) []byte {
	m := []byte(name.Product + "-auth-v1")
	m = append(m, own[:]...)
	m = append(m, peer[:]...)
	return append(m, peerNonce...)
}

// Handshake runs hello/auth on a freshly opened connection before any
// blueprint data flows. It fails, and the caller must close the
// connection, if the protocol version differs, the peer's signature does
// not cover the DTLS fingerprints this side actually sees, the peer id does
// not belong to the claimed email, or verify refuses the key.
func Handshake(c *Conn, self Identity, team string, verify Verifier) (Peer, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return Peer{}, err
	}
	pub := self.Key.Public().(ed25519.PublicKey)
	if err := c.Send(helloMsg{Type: "hello", Proto: Proto, Email: self.Email,
		PubKey: base64.StdEncoding.EncodeToString(pub), Nonce: base64.StdEncoding.EncodeToString(nonce), Caps: self.Caps}); err != nil {
		return Peer{}, err
	}

	var h helloMsg
	if err := expect(c, "hello", &h); err != nil {
		return Peer{}, err
	}
	if h.Proto != Proto {
		_ = c.Send(map[string]string{"type": "bye"})
		return Peer{}, fmt.Errorf("the peer runs protocol version %d, this kalka speaks %d: both need the same kalka version", h.Proto, Proto)
	}
	peerKey, err := base64.StdEncoding.DecodeString(h.PubKey)
	if err != nil || len(peerKey) != ed25519.PublicKeySize {
		return Peer{}, errors.New("peer sent an invalid public key")
	}
	peerNonce, err := base64.StdEncoding.DecodeString(h.Nonce)
	if err != nil || len(peerNonce) < 16 {
		return Peer{}, errors.New("peer sent an invalid nonce")
	}

	local, remote := c.Fingerprints()
	sig := ed25519.Sign(self.Key, authMessage(local, remote, peerNonce))
	if err := c.Send(authMsg{Type: "auth", Sig: base64.StdEncoding.EncodeToString(sig)}); err != nil {
		return Peer{}, err
	}

	var a authMsg
	if err := expect(c, "auth", &a); err != nil {
		return Peer{}, err
	}
	peerSig, err := base64.StdEncoding.DecodeString(a.Sig)
	if err != nil {
		return Peer{}, errors.New("peer sent an invalid signature")
	}
	// The peer signed its own fingerprint (our remote) and ours (our local).
	if !ed25519.Verify(peerKey, authMessage(remote, local, nonce), peerSig) {
		return Peer{}, errors.New("channel binding failed: the peer's signature does not match this connection; the rendezvous server may be intercepting it")
	}
	// The peer id must belong to the claimed identity (spec 8.4 step 4),
	// and this comes before pinning: a squatter on someone's key id fails
	// here, without leaving a changed-key record.
	viaKey := peerid.IsKey(c.RemoteID())
	if viaKey {
		if c.RemoteID() != peerid.Key(peerKey) {
			return Peer{}, fmt.Errorf("the peer at key id %s presented another key: someone else holds that id on the rendezvous server", c.RemoteID())
		}
	} else if want := PeerID(team, h.Email); c.RemoteID() != want {
		return Peer{}, fmt.Errorf("peer claims to be %s but is connected as %s", h.Email, c.RemoteID())
	}
	caps := make([]string, 0, len(h.Caps))
	for _, cp := range h.Caps {
		if len(caps) < 32 && len(cp) <= 64 {
			caps = append(caps, cp)
		}
	}
	if verify != nil {
		if err := verify(h.Email, peerKey); err != nil {
			return Peer{}, err
		}
	}
	return Peer{Email: h.Email, Key: peerKey, Caps: caps, ViaKey: viaKey}, nil
}

func expect(c *Conn, typ string, v any) error {
	env, err := c.Receive(HandshakeTimeout)
	if err != nil {
		return fmt.Errorf("waiting for %s: %w", typ, err)
	}
	if env.Type == "bye" {
		return fmt.Errorf("peer closed the connection during the handshake")
	}
	if env.Type != typ {
		return fmt.Errorf("expected %s, peer sent %q", typ, env.Type)
	}
	return env.Decode(v)
}
