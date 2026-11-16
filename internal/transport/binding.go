package transport

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/peerid"
)

// Proto is the data channel protocol version.
const Proto = 1

// KeyPeerID derives the key peer id (spec 9.2).
func KeyPeerID(pub ed25519.PublicKey) string { return peerid.Key(pub) }

// Identity is the local user.
type Identity struct {
	Email string
	Key   ed25519.PrivateKey
}

// Peer is the authenticated other side.
type Peer struct {
	Email string
	Key   ed25519.PublicKey
	// Invite is the join proof the peer sent in hello, if any (spec 19.7).
	Invite *InviteClaim
}

// InviteClaim is the optional invite field of hello: a joiner redeeming
// an invite (spec 19.7 step 4). The owner checks it after auth.
type InviteClaim struct {
	ID    string `json:"id"`
	Team  string `json:"team"`
	Proof string `json:"proof"`
}

// Verifier decides whether a claimed key is acceptable for an email: the
// initiator checks it against the key the team log or the invite named
// (spec 8.2, 8.3). It returns an error to refuse.
type Verifier func(email string, key ed25519.PublicKey) error

type helloMsg struct {
	Type   string `json:"type"`
	Proto  int    `json:"proto"`
	Email  string `json:"email"`
	PubKey string `json:"pubkey"`
	Nonce  string `json:"nonce"`
	// Invite is sent only by a joiner (spec 19.7).
	Invite *InviteClaim `json:"invite,omitempty"`
}

type authMsg struct {
	Type string `json:"type"`
	Sig  string `json:"sig"`
}

// HandshakeTimeout bounds hello and auth.
const HandshakeTimeout = 30 * time.Second

// authMessage is what each side signs: context, own DTLS fingerprint, the
// peer's DTLS fingerprint and the peer's nonce (spec 8.3).
func authMessage(own, peer [sha256.Size]byte, peerNonce []byte) []byte {
	m := []byte(name.Product + "-auth-v1")
	m = append(m, own[:]...)
	m = append(m, peer[:]...)
	return append(m, peerNonce...)
}

// Handshake runs hello/auth on a freshly opened connection before any
// blueprint data flows. It fails, and the caller must close the
// connection, if the protocol version differs, the peer's signature does
// not cover the DTLS fingerprints this side actually sees, the peer id is
// not the key id of the presented key, or verify refuses the key.
func Handshake(c *Conn, self Identity, verify Verifier) (Peer, error) {
	return HandshakeInvite(c, self, verify, nil)
}

// HandshakeInvite is Handshake for a joiner: hello also carries inv.
func HandshakeInvite(c *Conn, self Identity, verify Verifier, inv *InviteClaim) (Peer, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return Peer{}, err
	}
	pub := self.Key.Public().(ed25519.PublicKey)
	if err := c.Send(helloMsg{Type: "hello", Proto: Proto, Email: self.Email,
		PubKey: base64.StdEncoding.EncodeToString(pub), Nonce: base64.StdEncoding.EncodeToString(nonce), Invite: inv}); err != nil {
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

	// The peer id must be the key id of the presented key (spec 8.3 step
	// 4): a squatter on someone's key id fails here, before this side
	// signs anything for it. The hello is enough to tell.
	if c.RemoteID() != peerid.Key(peerKey) {
		return Peer{}, fmt.Errorf("the peer at %s presented another key: someone else holds that id on the rendezvous server", c.RemoteID())
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
	if verify != nil {
		if err := verify(h.Email, peerKey); err != nil {
			return Peer{}, err
		}
	}
	var claim *InviteClaim
	if iv := h.Invite; iv != nil && len(iv.ID) <= 64 && len(iv.Team) <= 64 && len(iv.Proof) <= 128 {
		claim = iv
	}
	return Peer{Email: h.Email, Key: peerKey, Invite: claim}, nil
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
