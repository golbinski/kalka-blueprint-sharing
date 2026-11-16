package core

// Personal and team folders (spec 14, 20.1, 20.5).

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/golbinski/kalka/internal/syncproto"
)

// abs is rel under the store's resolved root, as the store reports paths.
func (n *node) abs(rel string) string { return filepath.Join(n.d.Store.Dir(), filepath.FromSlash(rel)) }

// joinTeam has owner invite n to the team with hash team and waits for
// the join.
func joinTeam(t *testing.T, owner, n *node, team string) {
	t.Helper()
	inv, err := owner.d.Invite(team, n.email, 0)
	if err != nil {
		t.Fatal(err)
	}
	j, err := n.d.Join(context.Background(), inv.Token)
	if err != nil {
		t.Fatal(err)
	}
	waitJoin(t, n, j.ID, JoinDone)
}

// roundIn runs one session from n to m for team (a hash).
func roundIn(t *testing.T, n, m *node, team string) SessionResult {
	t.Helper()
	h, err := n.d.heldTeam(team)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := h.memberKey(m.email)
	if !ok {
		t.Fatalf("%s does not count %s as a member", n.email, m.email)
	}
	res, err := n.d.replicateWith(context.Background(), h, m.email, key)
	if err != nil {
		t.Fatalf("%s replicating with %s: %v", n.email, m.email, err)
	}
	return res
}

// tap collects what a node sends in all its sessions; each session gets
// its own recorder, writing under the tap's lock.
type tap struct {
	mu    sync.Mutex
	sent  []string
	asked map[string]string
}

// tapSwitch is installed once as a node's wrapChan, before any session
// it records, so tests switch taps without writing the field while a
// session reads it.
type tapSwitch struct {
	mu  sync.Mutex
	cur *tap
	ask []string
}

// installTaps installs the switch on each node; recording starts with
// recordAll.
func installTaps(ns ...*node) {
	for _, n := range ns {
		sw := &tapSwitch{}
		n.taps = sw
		n.d.wrapChan = func(c syncproto.Channel) syncproto.Channel {
			sw.mu.Lock()
			tp, ask := sw.cur, sw.ask
			sw.mu.Unlock()
			if tp == nil {
				return c
			}
			return &recorder{Channel: c, sent: &tp.sent, ask: ask, asked: tp.asked, shared: &tp.mu}
		}
	}
}

// recordAll starts a new tap of what n sends in its sessions, asking for
// ask in each team_want.
func recordAll(n *node, ask ...string) *tap {
	tp := &tap{asked: map[string]string{}}
	n.taps.mu.Lock()
	n.taps.cur, n.taps.ask = tp, ask
	n.taps.mu.Unlock()
	return tp
}

// stopRecording ends n's tap.
func stopRecording(ns ...*node) {
	for _, n := range ns {
		n.taps.mu.Lock()
		n.taps.cur, n.taps.ask = nil, nil
		n.taps.mu.Unlock()
	}
}

// snapshot copies what was recorded so far.
func (tp *tap) snapshot() ([]string, map[string]string) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	return slices.Clone(tp.sent), maps.Clone(tp.asked)
}

// leaks returns the first of secrets that appears in sent, skipping the
// empty answers to the peer's own probes for the ids in probed.
func leaks(sent []string, probed []string, secrets ...string) string {
	var others []string
	for _, m := range sent {
		var off syncproto.TeamDoc
		if json.Unmarshal([]byte(m), &off) == nil && off.Type == "team_doc" && off.Content == "" && slices.Contains(probed, off.KalkaID) {
			continue
		}
		others = append(others, m)
	}
	all := strings.Join(others, "\n")
	for _, s := range secrets {
		if strings.Contains(all, s) {
			return s
		}
	}
	return ""
}

const (
	routesID  = "a1111111-2222-4333-8444-555555555555"
	routesDoc = "---\nkalka_id: " + routesID + "\nowner: ewa@parcels.example\n---\n# Courier routes\n\n## Plan\n\nRoute vans by postcode.\n"
	finesID   = "b1111111-2222-4333-8444-555555555555"
	finesDoc  = "---\nkalka_id: " + finesID + "\nowner: ewa@parcels.example\n---\n# Overdue fines\n\n## Plan\n\nWaive fines in January.\n"
	unknownID = "99999999-2222-4333-8444-555555555555"
)

// Ewa and Marek share parcel-ops, Ewa and Olga share library-desk: across
// full rounds both ways, Marek gets no byte, id or title of library-desk
// and Olga none of parcel-ops; a request for a foreign id is answered as
// one for an unknown id.
func TestTeamIsolation(t *testing.T) {
	fastJoins(t)
	w := newWorld(t)
	ewa, marek, olga := w.node("ewa@parcels.example"), w.node("marek@parcels.example"), w.node("olga@library.example")
	for _, n := range []*node{ewa, marek, olga} {
		waitKeyOnline(t, n)
	}
	pv, err := ewa.d.CreateTeam("parcel-ops")
	if err != nil {
		t.Fatal(err)
	}
	lv, err := ewa.d.CreateTeam("library-desk")
	if err != nil {
		t.Fatal(err)
	}
	joinTeam(t, ewa, marek, pv.Team)
	joinTeam(t, ewa, olga, lv.Team)
	installTaps(ewa, marek, olga)

	ewa.write("personal/routes.md", routesDoc)
	ewa.write("personal/fines.md", finesDoc)
	waitIndexed(t, ewa, 2)
	if _, err := ewa.d.Publish(ewa.path("personal/routes.md"), "parcel-ops", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ewa.d.Publish(ewa.path("personal/fines.md"), "library-desk", ""); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		peer    *node
		team    string
		gets    string
		foreign []string
	}{
		{marek, pv.Team, routesID, []string{finesID, "Overdue fines", "Waive fines", "library-desk", lv.Team}},
		{olga, lv.Team, finesID, []string{routesID, "Courier routes", "Route vans", "parcel-ops", pv.Team}},
	}
	for _, c := range cases {
		probe := []string{c.foreign[0], unknownID}
		et := recordAll(ewa)
		pt := recordAll(c.peer, probe...)
		roundIn(t, ewa, c.peer, c.team) // Ewa initiates
		roundIn(t, c.peer, ewa, c.team) // the peer initiates
		stopRecording(ewa, c.peer)

		ewaSent, _ := et.snapshot()
		_, asked := pt.snapshot()
		if s := leaks(ewaSent, probe, c.foreign...); s != "" {
			t.Fatalf("%q reached %s", s, c.peer.email)
		}
		if f, _ := c.peer.d.Store.Find(c.gets); f == nil {
			t.Fatalf("%s did not get its own team's document", c.peer.email)
		}
		if f, _ := c.peer.d.Store.Find(c.foreign[0]); f != nil {
			t.Fatalf("%s holds the other team's document", c.peer.email)
		}
		a, ok1 := asked[c.foreign[0]]
		b, ok2 := asked[unknownID]
		if !ok1 || !ok2 || a != b {
			t.Fatalf("answers differ: foreign %q (%v), unknown %q (%v)", a, ok1, b, ok2)
		}
	}
}

// Two teams with the same name on one machine exchange nothing: location,
// not the name, decides which team a file belongs to.
func TestSameNameTeamsExchangeNothing(t *testing.T) {
	fastJoins(t)
	w := newWorld(t)
	ewa, marek, olga := w.node("ewa@parcels.example"), w.node("marek@parcels.example"), w.node("olga@parcels.example")
	for _, n := range []*node{ewa, marek, olga} {
		waitKeyOnline(t, n)
	}
	one, err := ewa.d.CreateTeam("parcels")
	if err != nil {
		t.Fatal(err)
	}
	joinTeam(t, ewa, marek, one.Team)
	two, err := olga.d.CreateTeam("parcels")
	if err != nil {
		t.Fatal(err)
	}
	joinTeam(t, olga, ewa, two.Team)
	installTaps(ewa, marek, olga)
	v2, err := ewa.d.Team(two.Team)
	if err != nil || filepath.Base(v2.Dir) != "parcels-2" {
		t.Fatalf("second parcels folder %q %v", v2.Dir, err)
	}

	ewa.write("parcels/routes.md", strings.Replace(routesDoc, "---\n#", "team: parcels\n---\n#", 1))
	ewa.write("parcels-2/fines.md", strings.Replace(finesDoc, "---\n#", "team: parcels\n---\n#", 1))
	waitIndexed(t, ewa, 2)
	for _, p := range []string{"parcels/routes.md", "parcels-2/fines.md"} {
		r, err := ewa.d.Publish(ewa.path(p), "", "")
		if err != nil || r.Path != ewa.abs(p) {
			t.Fatalf("publish %s in place: %+v %v", p, r, err)
		}
	}
	mt := recordAll(ewa)
	roundIn(t, ewa, marek, one.Team)
	roundIn(t, marek, ewa, one.Team)
	ot := recordAll(ewa)
	roundIn(t, ewa, olga, two.Team)
	roundIn(t, olga, ewa, two.Team)
	stopRecording(ewa)
	toMarek, _ := mt.snapshot()
	toOlga, _ := ot.snapshot()
	if s := leaks(toMarek, nil, finesID, "Overdue fines", two.Team); s != "" {
		t.Fatalf("%q reached marek", s)
	}
	if s := leaks(toOlga, nil, routesID, "Courier routes", one.Team); s != "" {
		t.Fatalf("%q reached olga", s)
	}
	if f, _ := marek.d.Store.Find(routesID); f == nil {
		t.Fatal("marek lacks routes")
	}
	if f, _ := olga.d.Store.Find(finesID); f == nil {
		t.Fatal("olga lacks fines")
	}
	for _, c := range []struct {
		n  *node
		id string
	}{{marek, finesID}, {olga, routesID}, {ewa, ""}} {
		if c.id != "" {
			if f, _ := c.n.d.Store.Find(c.id); f != nil {
				t.Fatalf("%s holds the other team's %s", c.n.email, c.id)
			}
		}
	}
	// Ewa still holds exactly one copy of each, where she put it.
	if p, _ := ewa.teamFile(routesID); p != ewa.abs("parcels/routes.md") {
		t.Fatalf("routes at %s", p)
	}
	if p, _ := ewa.teamFile(finesID); p != ewa.abs("parcels-2/fines.md") {
		t.Fatalf("fines at %s", p)
	}
}

// Personal files never leave, even with `team:` set or a genuine release
// in them: no inventory line, content or id appears on any channel.
func TestPersonalNeverLeaves(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	installTaps(depot, courier)

	const draftID = "c1111111-2222-4333-8444-555555555555"
	depot.write("personal/draft.md", "---\nkalka_id: "+draftID+"\nowner: depot@parcels.example\nteam: parcels\n---\n# Locker draft\n\nNot ready.\n")
	// A file released for the team, then moved back to Personal by hand:
	// its release is real, its place is not the team's.
	depot.write("personal/codes.md", strings.Replace(routesDoc, "ewa@", "depot@", 1))
	depot.write("lockers.md", lockers)
	waitIndexed(t, depot, 3)
	r, err := depot.d.Publish(depot.path("personal/codes.md"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(r.Path, depot.path("personal/codes.md")); err != nil {
		t.Fatal(err)
	}
	waitStored(t, depot, depot.path("personal/codes.md"), "Courier routes")
	pub := depot.publish("lockers.md")
	waitStored(t, depot, pub.Path, "Parcel lockers")

	probe := []string{draftID, routesID, unknownID}
	dt := recordAll(depot)
	ct := recordAll(courier, probe...)
	depot.replicate(courier)
	courier.replicate(depot)
	stopRecording(depot, courier)
	depotSent, _ := dt.snapshot()
	_, asked := ct.snapshot()

	if s := leaks(depotSent, probe, draftID, "Locker draft", "Not ready", routesID, "Courier routes", "Route vans"); s != "" {
		t.Fatalf("personal %q left the depot", s)
	}
	if f, _ := courier.d.Store.Find(mustID(t, depot, "lockers.md")); f == nil {
		t.Fatal("the team document did not travel")
	}
	for _, id := range probe {
		if a, ok := asked[id]; !ok || a != "" {
			t.Fatalf("answer for %s: %q %v", id, a, ok)
		}
	}

	// Both are listed as not shared, with why.
	v, err := depot.d.Team("")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range v.NotShared {
		got[d.Rel] = d.Reason
	}
	for _, rel := range []string{"personal/draft.md", "personal/codes.md"} {
		if got[rel] != MismatchInPersonal {
			t.Fatalf("not shared: %+v", v.NotShared)
		}
	}
	dv, err := depot.d.Document(context.Background(), depot.path("personal/draft.md"))
	if err != nil || dv.Scope != ScopePersonal || dv.Mismatch != MismatchInPersonal {
		t.Fatalf("draft view: %+v %v", dv, err)
	}
}

// Publishing a Personal file moves it into the team's folder and reports
// the new path; a file already in a team's folder stays where it is and
// cannot be published for another team.
func TestPublishMoves(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example")
	depot := ns[0]
	depot.write("personal/lockers.md", lockers)
	waitIndexed(t, depot, 1)
	r, err := depot.d.Publish(depot.path("personal/lockers.md"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Team != "parcels" || r.Version != 1 {
		t.Fatalf("publish: %+v", r)
	}
	if want := depot.abs("parcels/lockers.md"); r.Path != want {
		t.Fatalf("path %q, want %q", r.Path, want)
	}
	if _, err := os.Stat(depot.path("personal/lockers.md")); !os.IsNotExist(err) {
		t.Fatalf("the Personal copy is still there: %v", err)
	}
	dv, err := depot.d.Document(context.Background(), r.Path)
	if err != nil || dv.Scope != "parcels" || dv.Mismatch != "" || dv.Team == nil || !dv.Team.Held {
		t.Fatalf("published view: %+v %v", dv, err)
	}

	// Publish is for Personal files; the next version is a release, in
	// place.
	depot.edit("parcels/lockers.md", "by the loading bay", "by the bay and the gate")
	if _, err := depot.d.Publish(r.Path, "", ""); err == nil || !strings.Contains(err.Error(), "already published") {
		t.Fatalf("published twice: %v", err)
	}
	r2, err := depot.d.Release(r.Path, "stations", nil)
	if err != nil || r2.Path != r.Path || r2.Version != 2 {
		t.Fatalf("second publish: %+v %v", r2, err)
	}
	if _, err := depot.d.Publish(r.Path, "library-desk", ""); err == nil {
		t.Fatal("published a parcels file for another team")
	}
}

// Files in a team's folder that are not team documents are listed with
// why, and never replicated.
func TestMismatchesNotReplicated(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	installTaps(depot, courier)
	const (
		draftID = "d1111111-2222-4333-8444-555555555555"
		otherID = "e1111111-2222-4333-8444-555555555555"
		bareID  = "f1111111-2222-4333-8444-555555555555"
	)
	depot.write("parcels/draft.md", "---\nkalka_id: "+draftID+"\nowner: depot@parcels.example\n---\n# Draft locker plan\n")
	depot.write("parcels/other.md", "---\nkalka_id: "+otherID+"\nowner: depot@parcels.example\nteam: library-desk\n---\n# Other team plan\n")
	depot.write("parcels/bare.md", "---\nkalka_id: "+bareID+"\nowner: depot@parcels.example\nteam: parcels\n---\n# Unreleased plan\n")
	waitIndexed(t, depot, 3)

	v, err := depot.d.Team("")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"parcels/draft.md": MismatchNotShared,
		"parcels/other.md": MismatchOtherTeam,
		"parcels/bare.md":  MismatchUnreleased,
	}
	got := map[string]string{}
	for _, d := range v.NotShared {
		got[d.Rel] = d.Reason
	}
	for rel, why := range want {
		if got[rel] != why {
			t.Fatalf("not shared %+v, want %v", v.NotShared, want)
		}
	}
	tp := recordAll(depot)
	depot.replicate(courier)
	courier.replicate(depot)
	stopRecording(depot)
	sent, _ := tp.snapshot()
	if s := leaks(sent, nil, draftID, otherID, bareID, "Draft locker plan", "Other team plan", "Unreleased plan"); s != "" {
		t.Fatalf("%q replicated", s)
	}
	dv, err := depot.d.Document(context.Background(), depot.path("parcels/other.md"))
	if err != nil || dv.Mismatch != MismatchOtherTeam {
		t.Fatalf("view: %+v %v", dv, err)
	}
}

// personal is the Personal folder, never a team.
func TestPersonalNameRefused(t *testing.T) {
	w := newWorld(t)
	n := w.node("depot@parcels.example")
	if _, err := n.d.CreateTeam("personal"); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("created team personal: %v", err)
	}
	if _, err := os.Stat(n.path("personal")); err != nil {
		t.Fatalf("no Personal folder: %v", err)
	}
	if len(n.d.teamDirs()) != 0 {
		t.Fatal("the Personal folder counts as a team")
	}
}
