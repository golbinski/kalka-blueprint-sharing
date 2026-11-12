package transport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/peerjs"
)

// TestCloudProbe checks that the public PeerJS cloud relays presence probes
// and reports absent ids. Opt-in: KALKA_CLOUD_TEST=1.
func TestCloudProbe(t *testing.T) {
	if os.Getenv("KALKA_CLOUD_TEST") == "" {
		t.Skip("set KALKA_CLOUD_TEST=1 to test against the public PeerJS cloud")
	}
	srv, _ := peerjs.ParseRendezvous("peerjs-cloud")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	tag := hex.EncodeToString(b)
	dial := func(email string) *Endpoint {
		sig, err := peerjs.Dial(ctx, srv, PeerID(tag, email))
		if err != nil {
			t.Fatal(err)
		}
		ep := NewEndpoint(sig, Options{})
		t.Cleanup(func() { ep.Close(); sig.Close() })
		return ep
	}
	a, _ := dial("alice@corp.com"), dial("bob@corp.com")
	if p := a.Probe(ctx, PeerID(tag, "bob@corp.com")); p != Online {
		t.Fatalf("bob: %s", p)
	}
	if p := a.Probe(ctx, PeerID(tag, "nobody@corp.com")); p != Offline {
		t.Fatalf("nobody: %s", p)
	}
	// The socket must still be usable: the cloud closes it on messages it
	// dislikes.
	if p := a.Probe(ctx, PeerID(tag, "bob@corp.com")); p != Online {
		t.Fatalf("bob again: %s", p)
	}
}
