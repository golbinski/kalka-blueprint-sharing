package peerjs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"
)

// TestCloudRelay checks that the public PeerJS cloud relays a message
// between two registered ids. It talks to 0.peerjs.com, so it runs only
// with KALKA_CLOUD_TEST=1. The cloud closes the sender's socket on an
// OFFER whose payload lacks label or serialization, or whose SDP is not
// well-formed (docs/m1-transport.md).
func TestCloudRelay(t *testing.T) {
	if os.Getenv("KALKA_CLOUD_TEST") == "" {
		t.Skip("set KALKA_CLOUD_TEST=1 to probe the public PeerJS cloud")
	}
	srv, _ := ParseRendezvous("peerjs-cloud")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id := func() string { b := make([]byte, 16); rand.Read(b); return "kalka-" + hex.EncodeToString(b) }
	idA, idB := id(), id()
	a, err := Dial(ctx, srv, idA)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer a.Close()
	b, err := Dial(ctx, srv, idB)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer b.Close()
	if err := a.Send(ctx, Offer, idB, map[string]any{"type": "data", "connectionId": "dc_probe", "label": "kalka/1", "serialization": "raw", "sdp": map[string]string{"type": "offer", "sdp": "v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case m, ok := <-b.Messages():
		if !ok || m.Type != Offer || m.Src != idA {
			t.Fatalf("B got %+v (open=%v, err=%v)", m, ok, b.Err())
		}
	case m, ok := <-a.Messages():
		t.Fatalf("cloud did not relay; sender got %+v (open=%v, err=%v)", m, ok, a.Err())
	case <-time.After(10 * time.Second):
		t.Fatal("cloud did not relay within 10 s")
	}
}
