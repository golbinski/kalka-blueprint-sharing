package core

import (
	"context"
	"testing"
	"time"
)

// reaches reports whether n can open a replication session to m now.
func (n *node) reaches(m *node) bool {
	n.w.t.Helper()
	h, err := n.d.heldTeam(n.team())
	if err != nil {
		n.w.t.Fatal(err)
	}
	key, ok := h.memberKey(m.email)
	if !ok {
		n.w.t.Fatalf("%s does not count %s as a member", n.email, m.email)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err = n.d.replicateWith(ctx, h, m.email, key)
	return err == nil
}

// The server changes while kalka runs: the node registers on the new
// server, teammates on the old one cannot reach it, and they reach each
// other again once both are on the same server.
func TestSetRendezvous(t *testing.T) {
	_, ns := teamOfN(t, "ada@example.org", "bo@example.org")
	ada, bo := ns[0], ns[1]
	if !ada.reaches(bo) {
		t.Fatal("the team members do not reach each other before the switch")
	}
	other := newWorld(t)

	if err := ada.d.SetRendezvous(other.url); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "ada registered on the new server", func() bool {
		st := ada.d.State()
		return st.Rendezvous == other.url && st.Online
	})
	if ada.reaches(bo) {
		t.Fatal("ada reached bo although they are on different servers")
	}

	if err := bo.d.SetRendezvous(other.url); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "bo registered on the new server", func() bool {
		st := bo.d.State()
		return st.Rendezvous == other.url && st.Online
	})
	waitFor(t, "ada reaching bo on the new server", func() bool { return ada.reaches(bo) })
}

// An invalid server is refused and nothing changes.
func TestSetRendezvousInvalid(t *testing.T) {
	w := newWorld(t)
	n := w.node("ada@example.org")
	waitKeyOnline(t, n)
	for _, bad := range []string{"", "not a server", "ftp://example.org"} {
		if err := n.d.SetRendezvous(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if st := n.d.State(); st.Rendezvous != w.url || !st.Online {
		t.Fatalf("the server changed or went offline: %+v", st)
	}
}

// Switching to the public cloud shows its notice; switching away drops it.
func TestSetRendezvousNotice(t *testing.T) {
	w := newWorld(t)
	n := w.node("ada@example.org")
	if n.d.State().Notice != "" {
		t.Fatal("a notice on a kalka server")
	}
	if err := n.d.SetRendezvous("peerjs-cloud"); err != nil {
		t.Fatal(err)
	}
	if n.d.State().Notice != CloudNotice {
		t.Fatal("no cloud notice after switching to peerjs-cloud")
	}
	if err := n.d.SetRendezvous(w.url); err != nil {
		t.Fatal(err)
	}
	if n.d.State().Notice != "" {
		t.Fatal("the cloud notice stayed")
	}
}
