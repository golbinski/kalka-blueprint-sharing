package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/transport"
)

// noticesOf collects the notices n's bus carries from now on.
func noticesOf(t *testing.T, n *node) func() []Notice {
	t.Helper()
	ch, stop := n.d.bus.Watch()
	t.Cleanup(stop)
	return func() []Notice {
		var out []Notice
		for {
			select {
			case e := <-ch:
				if nt, ok := e.Data.(Notice); ok && e.Type == "notice" {
					out = append(out, nt)
				}
			case <-time.After(300 * time.Millisecond):
				return out
			}
		}
	}
}

func kinds(ns []Notice) string {
	var k []string
	for _, n := range ns {
		k = append(k, n.Kind)
	}
	return strings.Join(k, ",")
}

// TestPause: paused, kalka is offline (its registration closed, a
// member's probe says offline, no round, no join); local work goes
// on and reaches the peer after the resume.
func TestPause(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	ctx := context.Background()

	depot.d.SetPaused(true)
	for i := 0; ; i++ {
		st := depot.d.State()
		if !st.Online && st.Paused {
			break
		}
		if i == 200 {
			t.Fatalf("still registered while paused: %+v", st)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if p := courier.d.Probe(ctx, depot.email); p != transport.Offline {
		t.Fatalf("courier's probe of a paused depot: %s", p)
	}
	if _, err := depot.d.ReplicateNow(ctx, depot.team()); !errors.Is(err, ErrPaused) {
		t.Fatalf("round while paused: %v", err)
	}
	depot.write("lockers.md", lockers)
	waitIndexed(t, depot, 1)
	if _, err := depot.d.Join(ctx, "x"); !errors.Is(err, ErrPaused) {
		t.Fatalf("join while paused: %v", err)
	}
	depot.publish("lockers.md") // local work goes on
	id := mustID(t, depot, "lockers.md")
	if s := depot.d.Summary(); !s.Paused || s.Pushes != 1 {
		t.Fatalf("summary while paused: %+v", s)
	}

	depot.d.SetPaused(false)
	waitKeyOnline(t, depot)
	if _, err := depot.d.ReplicateNow(ctx, depot.team()); err != nil {
		t.Fatal(err)
	}
	if _, doc := courier.teamFile(id); doc == nil {
		t.Fatal("the document published while paused did not reach the peer")
	}
	if s := depot.d.Summary(); s.Paused || s.Pushes != 0 || s.LastSync.IsZero() || s.PeersOnline != 1 || s.PeersKnown != 1 {
		t.Fatalf("summary after resume: %+v", s)
	}
}

// TestCommentNotices: the owner hears of comments by others, once per
// round; nobody hears of their own entries or of a first copy; nothing
// when notifications are off.
func TestCommentNotices(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	depot.write("lockers.md", lockers)
	waitIndexed(t, depot, 1)
	depot.publish("lockers.md")
	id := mustID(t, depot, "lockers.md")
	depotN, courierN := noticesOf(t, depot), noticesOf(t, courier)
	depot.replicate(courier)
	if got := kinds(append(depotN(), courierN()...)); got != "" {
		t.Fatalf("first copy raised %q", got)
	}
	name := courier.rel(id)
	courier.comment(name, "plan", "Missing a weekend slot.")
	courier.comment(name, "plan", "And a key for the night shift.")
	courier.replicate(depot)
	got := depotN()
	if len(got) != 1 || got[0].Kind != NoticeComments || !strings.Contains(got[0].Message, "2 new comments") || !strings.Contains(got[0].Message, courier.email) {
		t.Fatalf("owner's notices: %+v", got)
	}
	if got := kinds(courierN()); got != "" {
		t.Fatalf("commenter heard %q", got)
	}
	depot.comment(depot.rel(id), "plan", "Added both.")
	depot.replicate(courier)
	if got := kinds(append(depotN(), courierN()...)); got != "" {
		t.Fatalf("own comment raised %q", got)
	}
	depot.d.SetNotifications(false)
	courier.comment(name, "plan", "One more.")
	courier.replicate(depot)
	if got := kinds(depotN()); got != "" {
		t.Fatalf("notifications off, heard %q", got)
	}
}

// TestJoinAndReplicaNotices: a join on both sides; a comment that
// arrives by replication; a newer domain version.
func TestJoinAndReplicaNotices(t *testing.T) {
	fastJoins(t)
	w := newWorld(t)
	depot, courier := w.node("depot@parcels.example"), w.node("courier@parcels.example")
	waitKeyOnline(t, depot)
	waitKeyOnline(t, courier)
	depotN, courierN := noticesOf(t, depot), noticesOf(t, courier)
	if _, err := depot.d.CreateTeam("parcels"); err != nil {
		t.Fatal(err)
	}
	inv, _ := depot.d.Invite("", courier.email, 0)
	j, err := courier.d.Join(context.Background(), inv.Token)
	if err != nil {
		t.Fatal(err)
	}
	waitJoin(t, courier, j.ID, JoinDone)
	if got := kinds(depotN()); got != NoticeJoined {
		t.Fatalf("owner heard %q", got)
	}
	if got := kinds(courierN()); got != NoticeJoinDone {
		t.Fatalf("joiner heard %q", got)
	}

	// A comment by replication on the depot's published document.
	depot.write("lockers.md", lockers)
	waitIndexed(t, depot, 1)
	depot.publish("lockers.md")
	id := mustID(t, depot, "lockers.md")
	depot.replicate(courier)
	if got := kinds(courierN()); got != "" {
		t.Fatalf("first replica raised %q", got)
	}
	cpath, _ := courier.teamFile(id)
	if _, err := courier.d.AddEntry(EntryRequest{Path: cpath, Kind: bp.KindComment, Anchor: "", Body: "Which depot?"}); err != nil {
		t.Fatal(err)
	}
	courier.replicate(depot)
	if ns := depotN(); len(ns) != 1 || ns[0].Kind != NoticeComments || ns[0].Path == "" {
		t.Fatalf("replicated comment: %+v", ns)
	}

	// A newer version of a domain the courier's draft references.
	path := depot.writeDomain("parcels-lockers-domain.md")
	if _, err := depot.d.AssignDomain("", "lockers", depot.email); err != nil {
		t.Fatal(err)
	}
	depot.release(path, "")
	depot.replicate(courier)
	courier.write("locker-pickup.md", "---\ndomain: parcels/lockers@v1\n---\n# Locker pickup\n")
	waitStored(t, courier, courier.path("locker-pickup.md"), "Locker pickup")
	courierN()
	editFile(t, depot, path, "the lockers at one depot.", "the lockers at one depot, up to 40.")
	depot.release(path, "sizes")
	depot.replicate(courier)
	ns := courierN()
	if len(ns) != 1 || ns[0].Kind != NoticeDomain || !strings.Contains(ns[0].Message, "version 2") {
		t.Fatalf("domain notice: %+v", ns)
	}
	depot.replicate(courier) // the same version again: said once
	if got := kinds(courierN()); got != "" {
		t.Fatalf("repeated domain notice %q", got)
	}
}
