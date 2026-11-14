package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/transport"
)

// syncSteps syncs and returns the progress lines.
func (n *node) syncSteps(name string, to *node) []string {
	n.w.t.Helper()
	var steps []string
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := n.d.Sync(ctx, SyncRequest{Path: n.path(name), To: to.email}, "", func(s string) { steps = append(steps, s) }); err != nil {
		n.w.t.Fatalf("%s syncs with %s: %v", n.email, to.email, err)
	}
	return steps
}

func dialledBy(steps []string) string {
	for _, s := range steps {
		if strings.HasPrefix(s, "connecting to ") {
			return s[strings.LastIndex(s, " by ")+4:]
		}
	}
	return ""
}

// Spec 9.2: the email id for first contact, the key id once the peer's
// key is pinned, and the email id again when the key id does not answer.
func TestPeerIDChoice(t *testing.T) {
	w := newWorld(t)
	desk := w.node("desk@library.example")
	stacks := w.node("stacks@library.example")
	waitKeyOnline(t, desk)
	waitKeyOnline(t, stacks)
	desk.write("plan.md", plan)

	if got := dialledBy(desk.syncSteps("plan.md", stacks)); got != "its email id" {
		t.Fatalf("first contact by %q", got)
	}
	if got := dialledBy(desk.syncSteps("plan.md", stacks)); got != "its key id" {
		t.Fatalf("known peer reached by %q", got)
	}
	// Both ways: stacks pinned desk's key in the first sync.
	if got := dialledBy(stacks.syncSteps("plan.md", desk)); got != "its key id" {
		t.Fatalf("desk reached by %q", got)
	}
	if v := desk.d.PeerList(); len(v) != 1 || v[0].Fingerprint == "" {
		t.Fatalf("peer view: %+v", v)
	}
	if s := desk.d.State(); !s.KeyOnline || !strings.HasPrefix(s.KeyPeerID, "kalka-k") || s.Fingerprint == "" {
		t.Fatalf("status: %+v", s)
	}

	// The peer's key id is not registered (as when someone squats it on a
	// server without proofs): dialing falls back to the email id.
	stacks.stop()
	stacks.noKeyID = true
	stacks.start()
	steps := desk.syncSteps("plan.md", stacks)
	if got := dialledBy(steps); got != "its key id" || !strings.Contains(strings.Join(steps, "\n"), "by its email id") {
		t.Fatalf("fallback steps: %v", steps)
	}
	if desk.d.Probe(context.Background(), stacks.email) != transport.Online {
		t.Fatal("probe did not fall back to the email id")
	}
	equalFiles(t, "plan.md", desk, stacks)
}

func waitKeyOnline(t *testing.T, n *node) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if n.d.State().KeyOnline {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s did not register its key id", n.email)
}
