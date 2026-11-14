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

// Spec 9.2: the email id for first contact and older peers, the key id
// once the peer's key is pinned and it announced key ids, and the email id
// again when the key id does not answer.
func TestPeerIDChoice(t *testing.T) {
	w := newWorld(t)
	desk := w.node("desk@library.example")
	stacks := &node{w: w, email: "stacks@library.example", cfgDir: t.TempDir() + "/config", folder: t.TempDir(), answers: map[string]bool{}, legacy: true}
	stacks.start()
	desk.write("plan.md", plan)

	// An old peer: email ids both ways, and no capabilities recorded.
	if got := dialledBy(desk.syncSteps("plan.md", stacks)); got != "its email id" {
		t.Fatalf("first contact by %q", got)
	}
	if p, _ := desk.d.Peers.Get(stacks.email); len(p.Caps) != 0 {
		t.Fatalf("caps of an old peer: %v", p.Caps)
	}
	if got := dialledBy(stacks.syncSteps("plan.md", desk)); got != "its email id" {
		t.Fatalf("old peer dialled by %q", got)
	}
	if got := dialledBy(desk.syncSteps("plan.md", stacks)); got != "its email id" {
		t.Fatalf("old peer reached by %q", got)
	}

	// The peer upgrades: the next contact is still by email, and records
	// the capability; after that, the key id.
	stacks.stop()
	stacks.legacy = false
	stacks.start()
	waitKeyOnline(t, stacks)
	if got := dialledBy(desk.syncSteps("plan.md", stacks)); got != "its email id" {
		t.Fatalf("after the upgrade by %q", got)
	}
	if p, _ := desk.d.Peers.Get(stacks.email); !p.Has(transport.CapKeyID) {
		t.Fatalf("capability not recorded: %v", p.Caps)
	}
	if got := dialledBy(desk.syncSteps("plan.md", stacks)); got != "its key id" {
		t.Fatalf("known new peer reached by %q", got)
	}
	// Both ways: stacks learned desk's capability from the syncs.
	if got := dialledBy(stacks.syncSteps("plan.md", desk)); got != "its key id" {
		t.Fatalf("desk reached by %q", got)
	}
	if v := desk.d.PeerList(); len(v) != 1 || !v[0].KeyID || v[0].Fingerprint == "" {
		t.Fatalf("peer view: %+v", v)
	}
	if s := desk.d.State(); !s.KeyOnline || !strings.HasPrefix(s.KeyPeerID, "kalka-k") || s.Fingerprint == "" {
		t.Fatalf("status: %+v", s)
	}

	// The peer goes back to an old kalka: the key id is offline, the
	// email id still works, and the capability is cleared.
	stacks.stop()
	stacks.legacy = true
	stacks.start()
	steps := desk.syncSteps("plan.md", stacks)
	if got := dialledBy(steps); got != "its key id" || !strings.Contains(strings.Join(steps, "\n"), "by its email id") {
		t.Fatalf("fallback steps: %v", steps)
	}
	if p, _ := desk.d.Peers.Get(stacks.email); len(p.Caps) != 0 {
		t.Fatalf("caps after the downgrade: %v", p.Caps)
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
