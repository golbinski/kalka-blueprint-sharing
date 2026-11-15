package daemon

import (
	"context"
	"errors"
	"testing"
)

// The transfer endpoints answer with stable kinds (spec 12.3, 20.7).
func TestTransferAPI(t *testing.T) {
	w := newWorld(t)
	a := w.node("ewa@parcels.example")
	c := NewClient(a.d.APIAddr(), a.d.Token())
	ctx := context.Background()
	ts, err := c.Transfers(ctx)
	if err != nil || len(ts) != 0 {
		t.Fatalf("transfers %v %v", ts, err)
	}
	var re *RemoteError
	if _, err := c.DecideTransfer(ctx, "6f1c2b9e-4d7a-4c3e-9b1a-2e5f8c7d6a41", "accept", ""); !errors.As(err, &re) || re.Kind != "unknown-transfer" {
		t.Fatalf("unknown transfer: %v", err)
	}
	if _, err := c.Transfer(ctx, TransferRequest{Path: "nothing.md", Team: "library-desk", Mode: "swap"}); err == nil {
		t.Fatal("a bad mode was taken")
	}
}
