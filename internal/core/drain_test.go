package core

import (
	"testing"
	"time"
)

// TestStopDrains: a stop lets running background work (a replication
// round) finish with a live context, and cancels what is still running
// after DrainTimeout.
func TestStopDrains(t *testing.T) {
	w := newWorld(t)
	a := w.node("alice@corp.com")
	finished := make(chan error, 1)
	a.d.background(func() {
		time.Sleep(300 * time.Millisecond) // a round in flight
		finished <- a.d.work.Err()
	})
	start := time.Now()
	a.stop()
	if err := <-finished; err != nil {
		t.Fatalf("round's context cancelled by the stop: %v", err)
	}
	if time.Since(start) < 250*time.Millisecond {
		t.Fatal("stop did not wait for the round")
	}

	old := DrainTimeout
	DrainTimeout = 200 * time.Millisecond
	defer func() { DrainTimeout = old }()
	b := w.node("bob@corp.com")
	b.d.background(func() { <-b.d.work.Done() }) // a round that never ends
	start = time.Now()
	b.stop()
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("stop took %s", d)
	}
}
