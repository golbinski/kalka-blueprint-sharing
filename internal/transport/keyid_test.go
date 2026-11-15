package transport

import (
	"context"
	"crypto/ed25519"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/peerid"
	"github.com/golbinski/kalka/internal/peerjs"
	"github.com/golbinski/kalka/internal/rendezvous"
)

// keyNode registers the key id of priv (with a proof).
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
	return &node{id: Identity{Email: email, Key: priv}, sig: sig, ep: ep}, nil
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
		pb, errB = Handshake(c, b.id, nil)
	}()
	c, err := a.ep.Dial(ctx, b.sig.ID())
	if err != nil {
		cancel()
		<-done
		return pa, pb, err, errB
	}
	defer c.Close()
	pa, errA = Handshake(c, a.id, verifyA)
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
	if pa.Email != "stacks@library.example" || pb.Email != "desk@library.example" {
		t.Fatalf("peers: %+v / %+v", pa, pb)
	}
}

// On a server that does not check proofs, a squatter can hold someone's
// key id, but the dialer detects it in the handshake, before the key is
// checked against any team log (spec 8.3, 9.2).
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
	squatter := &node{id: Identity{Email: "stacks@library.example", Key: newKey()}, sig: sig, ep: ep}

	dialer, err := h.keyNode(t, "desk@library.example", newKey())
	if err != nil {
		t.Fatal(err)
	}
	verified := false
	verify := func(string, ed25519.PublicKey) error { verified = true; return nil }
	_, _, errA, _ := handshakeBoth(t, dialer, squatter, verify)
	if errA == nil || !strings.Contains(errA.Error(), "presented another key") {
		t.Fatalf("squatter not detected: %v", errA)
	}
	if verified {
		t.Fatal("the key check ran before the key id check")
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
