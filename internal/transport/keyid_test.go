package transport

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/identity"
	"github.com/golbinski/kalka/internal/peerid"
	"github.com/golbinski/kalka/internal/peerjs"
	"github.com/golbinski/kalka/internal/rendezvous"
)

// keyNode registers the key id of priv (with a proof) and announces the
// key-id capability.
func (h *harness) keyNode(t *testing.T, email string, priv ed25519.PrivateKey) (*node, error) {
	t.Helper()
	id, proof := peerid.Proof(priv, time.Now())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sig, err := peerjs.DialQuery(ctx, h.info, id, url.Values{peerid.ProofParam: {proof}})
	if err != nil {
		return nil, err
	}
	ep := NewEndpoint(sig, Options{Loopback: true})
	t.Cleanup(func() { ep.Close(); sig.Close() })
	return &node{id: Identity{Email: email, Key: priv, Caps: []string{CapKeyID}}, sig: sig, ep: ep}, nil
}

func newKey() ed25519.PrivateKey {
	_, k, _ := ed25519.GenerateKey(nil)
	return k
}

// handshakeBoth is connect with the peers' results.
func handshakeBoth(t *testing.T, a, b *node, verifyA Verifier) (pa, pb Peer, errA, errB error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := b.ep.Accept(ctx)
		if err != nil {
			errB = err
			return
		}
		defer c.Close()
		pb, errB = Handshake(c, b.id, "", nil)
	}()
	c, err := a.ep.Dial(ctx, b.sig.ID())
	if err != nil {
		cancel()
		<-done
		return pa, pb, err, errB
	}
	defer c.Close()
	pa, errA = Handshake(c, a.id, "", verifyA)
	<-done
	return
}

func TestKeyIDsNewToNew(t *testing.T) {
	h := newHarness(t, &rendezvous.Server{})
	a, err := h.keyNode(t, "desk@library.example", newKey())
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.keyNode(t, "stacks@library.example", newKey())
	if err != nil {
		t.Fatal(err)
	}
	pa, pb, errA, errB := handshakeBoth(t, a, b, nil)
	if errA != nil || errB != nil {
		t.Fatalf("handshake: %v / %v", errA, errB)
	}
	if !pa.ViaKey || !pb.ViaKey || !pa.Has(CapKeyID) || !pb.Has(CapKeyID) || pa.Email != "stacks@library.example" {
		t.Fatalf("peers: %+v / %+v", pa, pb)
	}
}

// Old and new peers meet on email ids: the old side sends no caps, and
// the new side learns that it must keep using email ids.
func TestKeyIDsOldAndNew(t *testing.T) {
	h := newHarness(t, &rendezvous.Server{})
	old, cur := h.node(t, "old@library.example", Options{}), h.node(t, "new@library.example", Options{})
	cur.id.Caps = []string{CapKeyID}
	for _, dir := range [][2]*node{{old, cur}, {cur, old}} {
		pa, pb, errA, errB := handshakeBoth(t, dir[0], dir[1], nil)
		if errA != nil || errB != nil {
			t.Fatalf("handshake: %v / %v", errA, errB)
		}
		for _, p := range []Peer{pa, pb} {
			if p.ViaKey {
				t.Fatal("email ids reported as key ids")
			}
			if p.Email == "old@library.example" && len(p.Caps) != 0 {
				t.Fatalf("old peer caps: %v", p.Caps)
			}
			if p.Email == "new@library.example" && !p.Has(CapKeyID) {
				t.Fatalf("new peer caps: %v", p.Caps)
			}
		}
	}
}

// The hello of a v0.3 kalka, as it was on the wire: no caps.
func TestOldHelloFixture(t *testing.T) {
	const old = `{"type":"hello","proto":1,"email":"old@library.example","pubkey":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=","nonce":"AAAAAAAAAAAAAAAAAAAAAA=="}`
	var h helloMsg
	if err := json.Unmarshal([]byte(old), &h); err != nil || h.Caps != nil || h.Email != "old@library.example" {
		t.Fatalf("%v %+v", err, h)
	}
	// And a new hello without caps marshals to exactly the old fields.
	b, _ := json.Marshal(helloMsg{Type: "hello", Proto: 1, Email: h.Email, PubKey: h.PubKey, Nonce: h.Nonce})
	if string(b) != old {
		t.Fatalf("hello changed shape:\n%s\n%s", b, old)
	}
}

// On a server that does not check proofs, a squatter can hold someone's
// key id, but the dialer detects it before pinning: no key_changed record,
// so the real owner is not locked out of syncs (spec 8.4, 9.2).
func TestKeyIDSquatter(t *testing.T) {
	h := newHarness(t, &rendezvous.Server{NoKeyProof: true})
	victim := newKey()
	vpub := victim.Public().(ed25519.PublicKey)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sig, err := peerjs.Dial(ctx, h.info, KeyPeerID(vpub)) // no proof needed here
	if err != nil {
		t.Fatal(err)
	}
	ep := NewEndpoint(sig, Options{Loopback: true})
	t.Cleanup(func() { ep.Close(); sig.Close() })
	squatter := &node{id: Identity{Email: "stacks@library.example", Key: newKey(), Caps: []string{CapKeyID}}, sig: sig, ep: ep}

	dialer, err := h.keyNode(t, "desk@library.example", newKey())
	if err != nil {
		t.Fatal(err)
	}
	peers, err := identity.OpenPeers(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := peers.Pin("stacks@library.example", vpub); err != nil {
		t.Fatal(err)
	}
	_, _, errA, _ := handshakeBoth(t, dialer, squatter, peers.Pin)
	if errA == nil || !strings.Contains(errA.Error(), "presented another key") {
		t.Fatalf("squatter not detected: %v", errA)
	}
	var kc *identity.KeyChangedError
	if errors.As(errA, &kc) {
		t.Fatal("reported as a changed key")
	}
	if p, _ := peers.Get("stacks@library.example"); p.KeyChanged != nil {
		t.Fatal("the squatter left a key_changed record")
	}
}

// kalka rendezvous refuses a key id without a proof, so the squatter
// cannot even register.
func TestKeyIDSquatterRefusedByKalkaRendezvous(t *testing.T) {
	h := newHarness(t, &rendezvous.Server{})
	victim := newKey()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := peerjs.Dial(ctx, h.info, KeyPeerID(victim.Public().(ed25519.PublicKey))); err == nil || !strings.Contains(err.Error(), "Key id refused") {
		t.Fatalf("registered without proof: %v", err)
	}
	// Somebody else's proof does not help either.
	_, proof := peerid.Proof(newKey(), time.Now())
	if _, err := peerjs.DialQuery(ctx, h.info, KeyPeerID(victim.Public().(ed25519.PublicKey)), url.Values{peerid.ProofParam: {proof}}); err == nil {
		t.Fatal("registered with another key's proof")
	}
}
