package core

// Waiting items (spec 11.3): each appears for the right people only and
// clears as the spec says.

import (
	"os"
	"strings"
	"testing"
)

// waitingOf lists n's waiting items, by kind and document id or transfer.
func waitingOf(t *testing.T, n *node) []WaitingItem {
	t.Helper()
	items, err := n.d.Waiting("")
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func waitKinds(items []WaitingItem) string {
	var s []string
	for _, it := range items {
		s = append(s, it.Kind)
	}
	return strings.Join(s, ",")
}

func TestWaitingHanded(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example", "hub@parcels.example")
	depot, courier, hub := ns[0], ns[1], ns[2]
	depot.write("lockers.md", lockers)
	waitIndexed(t, depot, 1)
	id := depot.share("lockers.md", courier, hub)
	for _, n := range ns {
		if k := waitKinds(waitingOf(t, n)); k != "" {
			t.Fatalf("%s waits for %s before anything happened", n.email, k)
		}
	}

	// Handed off: the courier only.
	if _, err := depot.d.Handoff(must(depot.teamFile(id)), courier.email); err != nil {
		t.Fatal(err)
	}
	depot.replicate(courier)
	items := waitingOf(t, courier)
	if len(items) != 1 || items[0].Kind != WaitHanded || items[0].By != depot.email || items[0].KalkaID != id ||
		!strings.HasPrefix(items[0].Target, "#/doc/") || items[0].TeamName != "parcels" {
		t.Fatalf("courier: %+v", items)
	}
	if k := waitKinds(waitingOf(t, depot)) + waitKinds(waitingOf(t, hub)); k != "" {
		t.Fatalf("others wait for %s", k)
	}
	sum := courier.d.Summary().Waiting
	if sum.Total != 1 || sum.Oldest == nil || sum.Oldest.Target != "#/t/parcels/waiting" || len(sum.Teams) != 1 || sum.Teams[0].Count != 1 {
		t.Fatalf("summary: %+v", sum)
	}
	if ts := courier.d.Teams(); ts[0].Waiting != 1 {
		t.Fatalf("teams list: %+v", ts[0].Waiting)
	}

	// Seen: the mark clears it; a mark of another head does not.
	marks := courier.d.WaitingMarks()
	courier.d.SetSeen(func() map[string]string { return map[string]string{id: "something-else"} })
	if len(waitingOf(t, courier)) != 1 {
		t.Fatal("a stale mark cleared the item")
	}
	courier.d.SetSeen(func() map[string]string { return marks })
	if k := waitKinds(waitingOf(t, courier)); k != "" {
		t.Fatalf("seen, still waits: %s", k)
	}
	courier.d.SetSeen(nil)

	// Any release of the courier's own clears it too.
	path := must(courier.teamFile(id))
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), "Parcel lockers", "Parcel lockers, east", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	waitStored(t, courier, path, "lockers, east")
	if _, err := courier.d.Release(path, "", nil); err != nil {
		t.Fatal(err)
	}
	if k := waitKinds(waitingOf(t, courier)); k != "" {
		t.Fatalf("after a release of one's own: %s", k)
	}

	// Reassigned by a team owner: the new owner, whose kalka released on
	// its own, still has it; the team owner taking it over has nothing.
	courier.replicate(depot)
	if _, err := depot.d.Reassign(must(depot.teamFile(id)), hub.email); err != nil {
		t.Fatal(err)
	}
	depot.replicate(hub) // the log, then version 3 that its cut-off waits for
	waitOwner(t, hub, id, hub.email)
	items = waitingOf(t, hub)
	if len(items) != 1 || items[0].Kind != WaitHanded || items[0].By != depot.email || !strings.Contains(items[0].Message, "reassigned") {
		t.Fatalf("hub: %+v", items)
	}
	hub.replicate(depot)
	if _, err := depot.d.Reassign(must(depot.teamFile(id)), depot.email); err != nil {
		t.Fatal(err)
	}
	waitOwner(t, depot, id, depot.email)
	if k := waitKinds(waitingOf(t, depot)); k != "" {
		t.Fatalf("a take-over waits for its own author: %s", k)
	}
	depot.replicate(hub)
	if k := waitKinds(waitingOf(t, hub)); k != "" {
		t.Fatalf("the hub's item outlived the reassignment away: %s", k)
	}
}

// Ownership disputed: one key signed two different releases of one
// version (a restore from a backup). The team owners see it; a member
// does not; a reassignment settles it.
func TestWaitingDisputed(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	courier.write("lockers.md", lockers)
	waitIndexed(t, courier, 1)
	id := courier.share("lockers.md", depot)
	path := must(courier.teamFile(id))
	backup, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	courier.edit("lockers.md", "Parcel lockers", "Parcel lockers, north")
	courier.publish("lockers.md")
	courier.replicate(depot)
	// The backup comes back, and a different version 2 is released.
	if err := os.WriteFile(path, backup, 0o644); err != nil {
		t.Fatal(err)
	}
	waitStored(t, courier, path, "# Parcel lockers at the depot")
	courier.edit("lockers.md", "Parcel lockers", "Parcel lockers, south")
	courier.publish("lockers.md")
	courier.replicate(depot)

	items := waitingOf(t, depot)
	if len(items) != 1 || items[0].Kind != WaitDisputed || items[0].KalkaID != id {
		t.Fatalf("depot: %+v", items)
	}
	if k := waitKinds(waitingOf(t, courier)); k != "" {
		t.Fatalf("a member sees %s", k)
	}
	if _, err := depot.d.Reassign(must(depot.teamFile(id)), depot.email); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the dispute settled", func() bool { return waitKinds(waitingOf(t, depot)) == "" })
}

// The owner is no longer a member: a document the remover did not hold
// at the removal stays with the removed member; the team owners see it.
func TestWaitingOwnerGone(t *testing.T) {
	staleView(t) // the depot removes the courier before it learns of the document
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example", "hub@parcels.example")
	depot, courier, hub := ns[0], ns[1], ns[2]
	depot.replicate(courier) // the courier learns of the hub
	courier.write("lockers.md", lockers)
	waitIndexed(t, courier, 1)
	id := courier.share("lockers.md", hub) // the depot does not hold it
	if _, err := depot.d.Remove("", courier.email); err != nil {
		t.Fatal(err)
	}
	depot.replicate(hub)
	hub.replicate(depot)
	items := waitingOf(t, depot)
	if len(items) != 1 || items[0].Kind != WaitOwnerGone || items[0].By != courier.email || items[0].KalkaID != id || items[0].At == "" {
		t.Fatalf("depot: %+v", items)
	}
	if k := waitKinds(waitingOf(t, hub)); k != "" {
		t.Fatalf("a member sees %s", k)
	}
	if _, err := depot.d.Reassign(must(depot.teamFile(id)), hub.email); err != nil {
		t.Fatal(err)
	}
	if k := waitKinds(waitingOf(t, depot)); k != "" {
		t.Fatalf("after the reassignment: %s", k)
	}
}

// A pending transfer waits for the destination's owners, not for the
// actor or a plain member; a decline clears it.
func TestWaitingTransfer(t *testing.T) {
	x := twoTeams(t)
	_, path := x.lockersIn(t)
	r, err := x.ewa.d.Copy(path, "library-desk")
	if err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.ewa, x.jan, x.lib)
	roundIn(t, x.jan, x.olga, x.lib)
	items, _ := x.jan.d.Waiting("library-desk")
	if len(items) != 1 || items[0].Kind != WaitTransfer || items[0].Transfer != r.ID || items[0].By != x.ewa.email || items[0].Target != WaitingRoute("library-desk") {
		t.Fatalf("jan: %+v", items)
	}
	if items, _ := x.jan.d.Waiting("personal"); len(items) != 0 {
		t.Fatalf("Personal: %+v", items)
	}
	for _, n := range []*node{x.ewa, x.olga, x.marek} {
		if k := waitKinds(waitingOf(t, n)); k != "" {
			t.Fatalf("%s waits for %s", n.email, k)
		}
	}
	if err := x.jan.d.DeclineTransfer(r.ID); err != nil {
		t.Fatal(err)
	}
	if k := waitKinds(waitingOf(t, x.jan)); k != "" {
		t.Fatalf("after the decline: %s", k)
	}
}

// The summary over three teams: total, per-team counts in the order the
// teams are held, and the oldest item's team.
func TestWaitingSummaryOrder(t *testing.T) {
	dirs := []teamDir{{Team: "a1", Name: "lending"}, {Team: "b2", Name: "returns"}, {Team: "c3", Name: "parcels"}}
	items := []WaitingItem{
		{Team: "b2", TeamName: "returns", At: "2026-11-03T10:00:00Z"},
		{Team: "c3", TeamName: "parcels", At: "2026-11-01T09:00:00Z"},
		{Team: "b2", TeamName: "returns", At: "2026-11-02T08:00:00Z"},
	}
	s := summarize(items, dirs)
	if s.Total != 3 || s.Oldest == nil || s.Oldest.Team != "parcels" || s.Oldest.Target != "#/t/parcels/waiting" {
		t.Fatalf("%+v", s)
	}
	if len(s.Teams) != 2 || s.Teams[0].Name != "returns" || s.Teams[0].Count != 2 || s.Teams[1].Name != "parcels" || s.Teams[1].Count != 1 {
		t.Fatalf("teams: %+v", s.Teams)
	}
	if e := summarize(nil, dirs); e.Total != 0 || e.Oldest != nil || e.Teams == nil {
		t.Fatalf("empty: %+v", e)
	}
}

// staleView makes the test's reassignments and removals sign from what the
// node holds, without the round that brings it up to date first.
func staleView(t *testing.T) {
	old := CatchUpTimeout
	CatchUpTimeout = 0
	t.Cleanup(func() { CatchUpTimeout = old })
}

// A reassignment first runs a round: a release the signer has not seen
// arrives before the cut-off is chosen. With no member reachable it is
// signed all the same, with a warning.
func TestReassignCatchesUp(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	courier.write("lockers.md", lockers)
	waitIndexed(t, courier, 1)
	id := courier.share("lockers.md", depot)
	path := must(courier.teamFile(id))
	editFile(t, courier, path, "lockers", "parcel lockers")
	courier.release(path, "v2")
	r, err := depot.d.Reassign(must(depot.teamFile(id)), depot.email)
	if err != nil || r.After != 2 || r.Warning != "" {
		t.Fatalf("reassign: %+v %v", r, err)
	}
	courier.stop()
	r, err = depot.d.Reassign(must(depot.teamFile(id)), courier.email)
	if err != nil || r.Warning == "" {
		t.Fatalf("offline reassign: %+v %v", r, err)
	}
}
