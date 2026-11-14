package daemon

// Team replication scenarios (spec 20, R7 acceptance criteria), with three
// or four daemons over the in-process rendezvous and real WebRTC.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/syncproto"
	"github.com/golbinski/kalka/internal/transport"
)

const lockers = `# Parcel lockers at the depot

## Problem

Couriers wait at the counter.

## Plan

Install lockers by the loading bay.
`

// teamOfN starts n daemons: the first creates team "parcels" and invites
// the others, who join.
func teamOfN(t *testing.T, emails ...string) (*world, []*node) {
	t.Helper()
	fastJoins(t)
	w := newWorld(t)
	var ns []*node
	for _, e := range emails {
		ns = append(ns, w.node(e))
	}
	for _, n := range ns {
		waitKeyOnline(t, n)
	}
	if _, err := ns[0].d.CreateTeam("parcels"); err != nil {
		t.Fatal(err)
	}
	for _, n := range ns[1:] {
		inv, err := ns[0].d.Invite("", n.email, 0)
		if err != nil {
			t.Fatal(err)
		}
		j, err := n.d.Join(context.Background(), inv.Token)
		if err != nil {
			t.Fatal(err)
		}
		waitJoin(t, n, j.ID, JoinDone)
	}
	return w, ns
}

func (n *node) team() string {
	n.w.t.Helper()
	ts := n.d.teamDirs()
	if len(ts) != 1 {
		n.w.t.Fatalf("%s holds %d teams", n.email, len(ts))
	}
	return ts[0].Team
}

// replicate runs one session from n to m and returns its result.
func (n *node) replicate(m *node) SessionResult {
	n.w.t.Helper()
	h, err := n.d.heldTeam(n.team())
	if err != nil {
		n.w.t.Fatal(err)
	}
	key, ok := h.memberKey(m.email)
	if !ok {
		n.w.t.Fatalf("%s does not count %s as a member", n.email, m.email)
	}
	res, err := n.d.replicateWith(context.Background(), h, m.email, key)
	if err != nil {
		n.w.t.Fatalf("%s replicating with %s: %v", n.email, m.email, err)
	}
	return res
}

// teamFile finds the file holding kalka id in n's folder.
func (n *node) teamFile(id string) (string, *bp.Document) {
	n.w.t.Helper()
	f, err := n.d.Store.Find(id)
	if err != nil {
		n.w.t.Fatal(err)
	}
	if f == nil {
		return "", nil
	}
	return f.Path, f.Doc
}

func (n *node) publish(name string) PublishResult {
	n.w.t.Helper()
	r, err := n.d.Publish(n.path(name), "", "")
	if err != nil {
		n.w.t.Fatalf("%s publishing %s: %v", n.email, name, err)
	}
	return r
}

func (n *node) edit(name, from, to string) {
	n.w.t.Helper()
	n.write(name, strings.Replace(n.read(name), from, to, 1))
	waitStored(n.w.t, n, n.path(name), to)
}

func waitStored(t *testing.T, n *node, path, want string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if f, err := n.d.Store.Load(path); err == nil && strings.Contains(string(f.Raw), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s: %s does not hold %q", n.email, path, want)
}

// recorder keeps every message a daemon sends in replication sessions,
// and can ask for extra documents, to probe what a responder gives away.
type recorder struct {
	syncproto.Channel
	mu    sync.Mutex
	sent  *[]string
	ask   []string          // ids added to the team_want
	asked map[string]string // answers to them: kalka_id -> content
}

func (r *recorder) Send(v any) error {
	if w, ok := v.(syncproto.TeamWant); ok && len(r.ask) > 0 {
		w.IDs = append(w.IDs, r.ask...)
		v = w
	}
	b, _ := json.Marshal(v)
	r.mu.Lock()
	*r.sent = append(*r.sent, string(b))
	r.mu.Unlock()
	return r.Channel.Send(v)
}

func (r *recorder) Receive(timeout time.Duration) (transport.Envelope, error) {
	for {
		env, err := r.Channel.Receive(timeout)
		if err != nil || env.Type != "sync_offer" || len(r.ask) == 0 {
			return env, err
		}
		var off syncproto.Offer
		_ = env.Decode(&off)
		hit := false
		for _, id := range r.ask {
			if off.KalkaID == id {
				r.mu.Lock()
				r.asked[id] = off.Content
				r.mu.Unlock()
				hit = true
			}
		}
		if !hit {
			return env, nil
		}
	}
}

// Private documents never leave: no id, title or byte of one appears in
// what a member sends, and asking for a private id directly gets the same
// answer as asking for one that does not exist.
func TestReplicationPrivateNeverLeaves(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example", "hub@parcels.example")
	depot, courier, hub := ns[0], ns[1], ns[2]

	const privateTitle = "Locker master codes"
	const privateBody = "the master code is under the counter mat"
	depot.write("codes.md", "---\nkalka_id: 11111111-2222-4333-8444-555555555555\nowner: depot@parcels.example\n---\n# "+privateTitle+"\n\n"+privateBody+"\n")
	depot.write("lockers.md", lockers)
	waitIndexed(t, depot, 2)
	r := depot.publish("lockers.md")
	if r.Version != 1 || r.Team != "parcels" {
		t.Fatalf("publish: %+v", r)
	}
	_, pub := depot.teamFile(mustID(t, depot, "lockers.md"))

	var sent []string
	depot.d.wrapChan = func(c syncproto.Channel) syncproto.Channel { return &recorder{Channel: c, sent: &sent} }
	var courierSent []string
	probe := &recorder{sent: &courierSent, ask: []string{"11111111-2222-4333-8444-555555555555", "99999999-2222-4333-8444-555555555555"}, asked: map[string]string{}}
	courier.d.wrapChan = func(c syncproto.Channel) syncproto.Channel { probe.Channel = c; return probe }

	res := depot.replicate(courier)
	if len(res.Sent) != 1 || res.Sent[0] != pub.KalkaID() {
		t.Fatalf("sent %v", res.Sent)
	}
	depot.replicate(hub)
	for _, n := range []*node{courier, hub} {
		p, doc := n.teamFile(pub.KalkaID())
		if doc == nil || filepath.Base(filepath.Dir(p)) != "parcels" || doc.Owner() != depot.email {
			t.Fatalf("%s holds %q %v", n.email, p, doc)
		}
	}
	// The answers to the courier's own probes name the id it asked for,
	// with nothing else; everything else must not mention it.
	var others []string
	for _, m := range sent {
		var off syncproto.Offer
		if json.Unmarshal([]byte(m), &off) == nil && off.Type == "sync_offer" && off.Content == "" && slices.Contains(probe.ask, off.KalkaID) {
			continue
		}
		others = append(others, m)
	}
	all := strings.Join(others, "\n")
	for _, secret := range []string{"11111111-2222-4333-8444-555555555555", privateTitle, privateBody} {
		if strings.Contains(all, secret) {
			t.Fatalf("private %q left the depot", secret)
		}
	}
	if len(probe.asked) != 2 || probe.asked["11111111-2222-4333-8444-555555555555"] != "" || probe.asked["99999999-2222-4333-8444-555555555555"] != "" {
		t.Fatalf("answers to direct requests: %v", probe.asked)
	}
	for _, n := range []*node{courier, hub} {
		if f, _ := n.d.Store.Find("11111111-2222-4333-8444-555555555555"); f != nil {
			t.Fatalf("%s holds the private document", n.email)
		}
	}
	// Equal peers finish in one round trip.
	depot.d.wrapChan, courier.d.wrapChan = nil, nil
	if again := depot.replicate(courier); !again.Equal {
		t.Fatalf("second session not equal: %+v", again)
	}
}

func mustID(t *testing.T, n *node, name string) string {
	t.Helper()
	f, err := n.d.Store.Load(n.path(name))
	if err != nil {
		t.Fatal(err)
	}
	return f.Doc.KalkaID()
}

// Tombstones survive a stale peer: the hub is offline holding version 3,
// the depot deletes, the hub comes back and syncs with the courier only;
// the file does not come back anywhere.
func TestReplicationTombstoneSurvivesStalePeer(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example", "hub@parcels.example")
	depot, courier, hub := ns[0], ns[1], ns[2]
	depot.write("lockers.md", lockers)
	waitIndexed(t, depot, 1)
	depot.publish("lockers.md")
	depot.edit("lockers.md", "by the loading bay", "by the loading bay, with 40 doors")
	depot.publish("lockers.md")
	depot.edit("lockers.md", "40 doors", "48 doors")
	if r := depot.publish("lockers.md"); r.Version != 3 {
		t.Fatalf("version %d", r.Version)
	}
	id := mustID(t, depot, "lockers.md")
	depot.replicate(courier)
	depot.replicate(hub)
	if _, doc := hub.teamFile(id); doc == nil || !strings.Contains(doc.Body(), "48 doors") {
		t.Fatal("hub does not hold version 3")
	}

	hub.stop()
	if err := depot.d.Delete(depot.path("lockers.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(depot.path("lockers.md")); !os.IsNotExist(err) {
		t.Fatal("the depot's file is still there")
	}
	depot.replicate(courier)
	depot.stop()

	hub.start()
	waitKeyOnline(t, hub)
	res := hub.replicate(courier)
	if len(res.Received) != 1 {
		t.Fatalf("hub received %v", res.Received)
	}
	depot.start()
	waitKeyOnline(t, depot)
	depot.replicate(hub)
	depot.replicate(courier)
	courier.replicate(hub)
	for _, n := range []*node{depot, courier, hub} {
		if p, doc := n.teamFile(id); doc != nil {
			t.Fatalf("%s holds the deleted document at %s", n.email, p)
		}
		h, _ := n.d.heldTeam(n.team())
		td, ok := n.d.replicaSet(h)[id]
		if !ok || !td.tomb || td.item.Version != 4 || !td.item.Deleted {
			t.Fatalf("%s tombstone: %+v", n.email, td.item)
		}
		if strings.Contains(string(td.raw), "doors") {
			t.Fatalf("%s: the stub keeps the plan", n.email)
		}
	}
	if trash, _ := filepath.Glob(filepath.Join(hub.folder, "parcels", ".kalka", "trash", "*.md")); len(trash) != 1 {
		t.Fatalf("hub trash: %v", trash)
	}
	if v := teamOf(t, courier); len(v.Deleted) != 1 || v.Deleted[0].Title != "Parcel lockers at the depot" || v.Published != 0 {
		t.Fatalf("courier's team view: %+v %d", v.Deleted, v.Published)
	}
}

// Concurrent owner operations converge over the network, including a
// mutual removal across a partition: the senior owner stays.
func TestReplicationMutualRemoval(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example", "hub@parcels.example")
	depot, courier, hub := ns[0], ns[1], ns[2]
	if _, err := depot.d.SetOwner("", courier.email, true); err != nil {
		t.Fatal(err)
	}
	depot.replicate(courier)
	depot.replicate(hub)
	// Partition: each removes the other without hearing of it.
	if _, err := depot.d.Remove("", courier.email); err != nil {
		t.Fatal(err)
	}
	if _, err := courier.d.Remove("", depot.email); err != nil {
		t.Fatal(err)
	}
	// The courier reaches the hub first, then the depot does.
	courier.replicate(hub)
	if _, ok := member(teamOf(t, hub), depot.email); ok {
		t.Fatal("the hub should not yet know of the depot's removal of the courier")
	}
	depot.replicate(hub)
	for _, n := range []*node{depot, hub} {
		v := teamOf(t, n)
		if _, ok := member(v, courier.email); ok {
			t.Fatalf("%s still counts the courier", n.email)
		}
		if m, ok := member(v, depot.email); !ok || !m.Owner {
			t.Fatalf("%s lost the senior owner: %+v", n.email, v.Members)
		}
	}
	a, b := teamOf(t, depot), teamOf(t, hub)
	if len(a.History) != len(b.History) {
		t.Fatalf("logs differ: %d / %d", len(a.History), len(b.History))
	}
	// The removed owner is refused by the members.
	h, _ := courier.d.heldTeam(courier.team())
	key, _ := h.memberKey(hub.email)
	if _, err := courier.d.replicateWith(context.Background(), h, hub.email, key); err == nil || !strings.Contains(err.Error(), "member") {
		t.Fatalf("removed owner replicated: %v", err)
	}
}

// A reassignment to the removing owner arrives on all peers, and is undone
// on all when the removal it relies on loses (spec 19.5).
func TestReplicationReassignment(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example", "hub@parcels.example", "van@parcels.example")
	depot, courier, hub, van := ns[0], ns[1], ns[2], ns[3]
	if _, err := depot.d.SetOwner("", courier.email, true); err != nil {
		t.Fatal(err)
	}
	hub.write("lockers.md", lockers)
	waitIndexed(t, hub, 1)
	hub.d.Store.Load(hub.path("lockers.md"))
	// The hub needs the log with the courier's ownership before syncing.
	depot.replicate(hub)
	depot.replicate(courier)
	depot.replicate(van)
	hub.publish("lockers.md")
	id := mustID(t, hub, "lockers.md")
	for _, n := range []*node{depot, courier, van} {
		hub.replicate(n)
	}

	// The depot, the senior owner, removes the courier while offline.
	if _, err := depot.d.Remove("", courier.email); err != nil {
		t.Fatal(err)
	}
	// Meanwhile the courier removes the hub; the hub's document is
	// reassigned to the courier, who releases it as its new owner.
	if _, err := courier.d.Remove("", hub.email); err != nil {
		t.Fatal(err)
	}
	waitOwner(t, courier, id, courier.email)
	courier.replicate(van)
	waitOwner(t, van, id, courier.email) // the reassignment arrived

	// The depot's removal reaches the van: the courier's removal of the
	// hub is blocked, its reassignment falls, and the version the
	// courier released is void. The van goes back to the hub's version.
	depot.replicate(van)
	depot.replicate(hub)
	for _, n := range []*node{depot, van, hub} {
		_, doc := n.teamFile(id)
		if doc == nil || doc.Owner() != hub.email {
			t.Fatalf("%s: owner %v", n.email, doc)
		}
		h, _ := n.d.heldTeam(n.team())
		if _, ok := h.st.Reassigned[id]; ok {
			t.Fatalf("%s: reassignment still effective", n.email)
		}
		if _, ok := memberOf(h.st, hub.email); !ok {
			t.Fatalf("%s: the hub was removed", n.email)
		}
	}
}

func waitOwner(t *testing.T, n *node, id, owner string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if _, doc := n.teamFile(id); doc != nil && doc.Owner() == owner {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, doc := n.teamFile(id)
	t.Fatalf("%s: owner of %s is not %s (%v)", n.email, id, owner, doc)
}

// Relay: a member passes on a document while its owner is offline;
// entries replicate both ways; a forged release does not take.
func TestReplicationRelayAndEntries(t *testing.T) {
	w, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example", "hub@parcels.example")
	depot, courier, hub := ns[0], ns[1], ns[2]
	depot.write("lockers.md", lockers)
	waitIndexed(t, depot, 1)
	depot.publish("lockers.md")
	id := mustID(t, depot, "lockers.md")
	depot.replicate(courier)
	depot.stop()
	courier.replicate(hub)
	path, doc := hub.teamFile(id)
	if doc == nil {
		t.Fatal("relay did not reach the hub")
	}
	// The hub comments; the comment reaches the courier.
	if _, err := hub.d.AddEntry(EntryRequest{Path: path, Kind: "comment", Anchor: "plan", Body: "Which side of the bay?"}); err != nil {
		t.Fatal(err)
	}
	// An unsigned entry stays local.
	raw, _ := os.ReadFile(path)
	hd, _ := bp.Parse(raw)
	un, err := bp.NewEntry(bp.KindComment, hub.email, w.now(), "plan", hd.Rev(), "hand-written")
	if err != nil {
		t.Fatal(err)
	}
	hd.AddEntry(un)
	os.WriteFile(path, hd.Bytes(), 0o644)
	waitStored(t, hub, path, "hand-written")
	hub.replicate(courier)
	_, cdoc := courier.teamFile(id)
	if cdoc == nil || !strings.Contains(string(cdoc.Bytes()), "Which side of the bay?") {
		t.Fatal("the comment did not replicate")
	}
	if strings.Contains(string(cdoc.Bytes()), "hand-written") {
		t.Fatal("an unsigned entry left the hub")
	}

	// The courier forges a new version: it is not the owner, so the
	// release is void and the plan does not change on the hub.
	cpath, _ := courier.teamFile(id)
	b, _ := os.ReadFile(cpath)
	os.WriteFile(cpath, []byte(strings.Replace(string(b), "Install lockers", "Remove lockers", 1)), 0o644)
	waitStored(t, courier, cpath, "Remove lockers")
	if _, err := courier.d.Publish(cpath, "", ""); err == nil || !strings.Contains(err.Error(), "only depot@parcels.example") {
		t.Fatalf("a non-owner published: %v", err)
	}
	courier.replicate(hub)
	if _, doc := hub.teamFile(id); strings.Contains(doc.Body(), "Remove lockers") {
		t.Fatal("an unreleased edit by a non-owner was taken")
	}
}

// A local change is pushed by the replication loop without a click.
func TestReplicationPush(t *testing.T) {
	old := PushDelay
	PushDelay = 100 * time.Millisecond
	t.Cleanup(func() { PushDelay = old })
	fastJoins(t)
	w := newWorld(t)
	depot := &node{w: w, email: "depot@parcels.example", cfgDir: t.TempDir() + "/c", folder: t.TempDir(), answers: map[string]bool{}, autoRepl: true}
	depot.start()
	courier := &node{w: w, email: "courier@parcels.example", cfgDir: t.TempDir() + "/c", folder: t.TempDir(), answers: map[string]bool{}, autoRepl: true}
	courier.start()
	waitKeyOnline(t, depot)
	waitKeyOnline(t, courier)
	depot.d.CreateTeam("parcels")
	inv, _ := depot.d.Invite("", courier.email, 0)
	j, _ := courier.d.Join(context.Background(), inv.Token)
	waitJoin(t, courier, j.ID, JoinDone)
	depot.write("lockers.md", lockers)
	waitIndexed(t, depot, 1)
	depot.publish("lockers.md")
	id := mustID(t, depot, "lockers.md")
	for i := 0; i < 200; i++ {
		if _, doc := courier.teamFile(id); doc != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the published document was not pushed")
}
