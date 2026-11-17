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

// Proto is the highest data channel protocol version this kalka speaks,
// and ProtoMin the lowest (spec 9.5). A session runs at the lower of the
// two sides' Proto, and fails only if that is below either side's
// ProtoMin. The data channel label never changes; versions live here.
const (
	Proto    = 1
	ProtoMin = 1
)

// Features are the optional capabilities this kalka announces in hello.
// A peer ignores names it does not know.
var Features = []string{}

// ProtoError is a version mismatch: the session cannot run at any
// version both sides speak.
type ProtoError struct{ PeerMin, PeerMax int }

func (e *ProtoError) Error() string {
	if e.PeerMax < 1 || e.PeerMin > e.PeerMax {
		return fmt.Sprintf("the peer announced no usable protocol version (%d to %d)", e.PeerMin, e.PeerMax)
	}
	if e.PeerMax < ProtoMin {
		return fmt.Sprintf("the peer runs an older kalka (protocol %d to %d; this kalka needs at least %d): the peer needs to update", e.PeerMin, e.PeerMax, ProtoMin)
	}
	return fmt.Sprintf("the peer runs a newer kalka (protocol %d to %d; this kalka speaks %d to %d): update kalka", e.PeerMin, e.PeerMax, ProtoMin, Proto)
}

// Negotiate is the version a session runs at, given the peer's hello.
func Negotiate(peerMin, peerMax int) (int, error) {
	if peerMin == 0 {
		peerMin = peerMax
	}
	v := min(Proto, peerMax)
	if peerMax < 1 || peerMin > peerMax || v < ProtoMin || v < peerMin {
		return 0, &ProtoError{peerMin, peerMax}
	}
	return v, nil
}

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
	// Proto is the version the session runs at; Features are the names
	// the peer announced.
	Proto    int
	Features []string
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
	Type     string   `json:"type"`
	Proto    int      `json:"proto"`
	ProtoMin int      `json:"proto_min,omitempty"`
	Features []string `json:"features"`
	Email    string   `json:"email"`
	PubKey   string   `json:"pubkey"`
	Nonce    string   `json:"nonce"`
	// Invite is sent only by a joiner (spec 19.7).
	Invite *InviteClaim `json:"invite,omitempty"`
}

type authMsg struct {
	Type string `json:"type"`
	Sig  string `json:"sig"`
}

// b64 encodes pubkey, nonce and sig in hello and auth: base64url without
// padding, like every other key and signature (spec 8.3).
var b64 = base64.RawURLEncoding

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
// connection, if the two sides share no protocol version, the peer's signature does
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
	if err := c.Send(helloMsg{Type: "hello", Proto: Proto, ProtoMin: ProtoMin, Features: Features, Email: self.Email,
		PubKey: b64.EncodeToString(pub), Nonce: b64.EncodeToString(nonce), Invite: inv}); err != nil {
		return Peer{}, err
	}

	var h helloMsg
	if err := expect(c, "hello", &h); err != nil {
		return Peer{}, err
	}
	version, err := Negotiate(h.ProtoMin, h.Proto)
	if err != nil {
		_ = c.Send(map[string]string{"type": "bye", "reason": "proto", "detail": fmt.Sprintf("%d-%d", ProtoMin, Proto)})
		return Peer{}, err
	}
	peerKey, err := b64.DecodeString(h.PubKey)
	if err != nil || len(peerKey) != ed25519.PublicKeySize {
		return Peer{}, errors.New("peer sent an invalid public key")
	}
	peerNonce, err := b64.DecodeString(h.Nonce)
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
	if err := c.Send(authMsg{Type: "auth", Sig: b64.EncodeToString(sig)}); err != nil {
		return Peer{}, err
	}

	var a authMsg
	if err := expect(c, "auth", &a); err != nil {
		return Peer{}, err
	}
	peerSig, err := b64.DecodeString(a.Sig)
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
	var features []string
	for _, f := range h.Features {
		if len(features) < 64 && len(f) <= 64 {
			features = append(features, f)
		}
	}
	return Peer{Email: h.Email, Key: peerKey, Invite: claim, Proto: version, Features: features}, nil
}

func expect(c *Conn, typ string, v any) error {
	env, err := c.Receive(HandshakeTimeout)
	if err != nil {
		return fmt.Errorf("waiting for %s: %w", typ, err)
	}
	if env.Type == "bye" {
		var b struct{ Reason, Detail string }
		_ = env.Decode(&b)
		if b.Reason == "proto" {
			var lo, hi int
			if _, err := fmt.Sscanf(b.Detail, "%d-%d", &lo, &hi); err == nil {
				return &ProtoError{lo, hi}
			}
		}
		return fmt.Errorf("peer closed the connection during the handshake")
	}
	if env.Type != typ {
		return fmt.Errorf("expected %s, peer sent %q", typ, env.Type)
	}
	return env.Decode(v)
}
