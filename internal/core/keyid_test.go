package core

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/syncproto"
	"github.com/golbinski/kalka/internal/transport"
)

// Spec 9.2: one signaling registration per node, on the key id of its
// identity key; no email id is ever registered.
func TestOneRegistration(t *testing.T) {
	w := newWorld(t)
	desk := w.node("desk@library.example")
	stacks := w.node("stacks@library.example")
	waitKeyOnline(t, desk)
	waitKeyOnline(t, stacks)
	want := []string{desk.d.State().PeerID, stacks.d.State().PeerID}
	slices.Sort(want)
	if got := w.srv.IDs(); !slices.Equal(got, want) {
		t.Fatalf("registrations %v, want %v", got, want)
	}
	for _, n := range []*node{desk, stacks} {
		s := n.d.State()
		if !strings.HasPrefix(s.PeerID, "kalka-k") || s.PeerID != transport.KeyPeerID(n.d.PublicKey()) || s.Fingerprint == "" {
			t.Fatalf("status: %+v", s)
		}
	}
}

// Spec 8.2: a peer sharing no team is told not-member before any file is
// read, and its document request never reaches the store.
func TestStrangerIsNotMember(t *testing.T) {
	_, ns := teamOfN(t, "desk@library.example", "reader@library.example")
	desk := ns[0]
	desk.write("loans.md", lockers)
	desk.publish("loans.md")
	stranger := desk.w.node("stranger@library.example")
	waitKeyOnline(t, stranger)

	var sent []string
	rec := &recorder{sent: &sent}
	desk.d.wrapChan = func(ch syncproto.Channel) syncproto.Channel { rec.Channel = ch; return rec }
	if bye := outsiderSession(t, stranger, desk, desk.team()); bye != syncproto.ByeNotMember {
		t.Fatalf("bye %q, want not-member", bye)
	}
	// The answer carried the log heads only: no digest, inventory or
	// document, so nothing about the files was read.
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, m := range sent {
		var head struct{ Type, Digest string }
		_ = json.Unmarshal([]byte(m), &head)
		if (head.Type != "team_sync" && head.Type != "bye") || head.Digest != "" || strings.Contains(m, "Parcel lockers") {
			t.Fatalf("sent to a stranger: %s", m)
		}
	}
}

// A member's email with another key than the log binds is refused like a
// stranger (spec 8.2); the initiator side refuses a key the log does not
// bind in the handshake.
func TestMemberWithOtherKeyRefused(t *testing.T) {
	w, ns := teamOfN(t, "desk@library.example", "reader@library.example")
	desk, reader := ns[0], ns[1]
	impostor := w.node(reader.email)
	waitKeyOnline(t, impostor)
	if bye := outsiderSession(t, impostor, desk, desk.team()); bye != syncproto.ByeNotMember {
		t.Fatalf("bye %q, want not-member", bye)
	}
	h, err := desk.d.heldTeam(desk.team())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := desk.d.replicateWith(context.Background(), h, reader.email, impostor.d.PublicKey()); err == nil {
		t.Fatalf("session with the impostor's key: %v", err)
	}
	desk.replicate(reader) // the bound key still works
}

// outsiderSession dials to from n, opens a replication session for team
// as an initiator bringing no log operations, and returns the bye reason
// it gets.
func outsiderSession(t *testing.T, n, to *node, team string) string {
	t.Helper()
	ep, err := n.d.endpoint()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := ep.Dial(ctx, transport.KeyPeerID(to.d.PublicKey()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := transport.Handshake(c, n.d.self, nil); err != nil {
		t.Fatal(err)
	}
	_ = c.Send(syncproto.TeamSync{Type: "team_sync", Team: team})
	for {
		env, err := c.Receive(5 * time.Second)
		if err != nil {
			return "closed: " + err.Error()
		}
		if env.Type == "bye" {
			var b syncproto.Bye
			_ = env.Decode(&b)
			return b.Reason
		}
		switch env.Type {
		case "keepalive":
		case "team_sync": // the heads: bring no operations
			_ = c.Send(syncproto.TeamLog{Type: "team_log", Team: team})
		default:
			return "got " + env.Type
		}
	}
}

func waitKeyOnline(t *testing.T, n *node) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if n.d.State().Online {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s did not register its key id", n.email)
}
