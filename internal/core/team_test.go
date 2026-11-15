package core

// The invite flow of spec 19.7 end to end, over the in-process rendezvous
// and real WebRTC on loopback (R6 acceptance criterion).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/teamlog"
)

func fastJoins(t *testing.T) {
	old := JoinEvery
	JoinEvery = 200 * time.Millisecond
	t.Cleanup(func() { JoinEvery = old })
}

// skip moves the shared clock forward.
func (w *world) skip(d time.Duration) { w.ticks.Add(int64(d / time.Second)) }

func waitJoin(t *testing.T, n *node, id, state string) JoinView {
	t.Helper()
	var last JoinView
	for i := 0; i < 300; i++ {
		for _, j := range n.d.Joins() {
			if j.ID == id {
				last = j
				if j.State == state {
					return j
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s: join %s is %q (%s), want %q", n.email, id, last.State, last.Detail, state)
	return last
}

func teamOf(t *testing.T, n *node) TeamView {
	t.Helper()
	ts := n.d.Teams()
	if len(ts) != 1 {
		t.Fatalf("%s holds %d teams", n.email, len(ts))
	}
	return ts[0]
}

func member(v TeamView, email string) (MemberView, bool) {
	for _, m := range v.Members {
		if m.Email == email {
			return m, true
		}
	}
	return MemberView{}, false
}

// attempt runs one join attempt synchronously, for the refusals.
func attempt(t *testing.T, n *node, token string) error {
	t.Helper()
	iv, err := teamlog.ParseToken(token)
	if err != nil {
		t.Fatal(err)
	}
	_, err = n.d.attemptJoin(context.Background(), iv)
	return err
}

func TestInviteFlow(t *testing.T) {
	fastJoins(t)
	w := newWorld(t)
	desk := w.node("desk@library.example")
	reader := w.node("reader@library.example")
	waitKeyOnline(t, desk)
	waitKeyOnline(t, reader)

	if _, err := desk.d.CreateTeam("lending"); err != nil {
		t.Fatal(err)
	}
	inv, err := desk.d.Invite("", reader.email, 0)
	if err != nil {
		t.Fatal(err)
	}
	if v := teamOf(t, desk); len(v.Invites) != 1 || v.Invites[0].State != "open" {
		t.Fatalf("open invites: %+v", v.Invites)
	}
	jv, err := reader.d.Join(context.Background(), inv.Token)
	if err != nil {
		t.Fatal(err)
	}
	if jv.Warning != "" {
		t.Fatalf("same rendezvous, but warned: %s", jv.Warning)
	}
	done := waitJoin(t, reader, jv.ID, JoinDone)
	if done.Owner != desk.email {
		t.Fatalf("owner learned as %q", done.Owner)
	}

	// Both logs hold the add-member, made with the invite.
	for _, n := range []*node{desk, reader} {
		v := teamOf(t, n)
		m, ok := member(v, reader.email)
		if !ok || m.Owner {
			t.Fatalf("%s: reader not a plain member: %+v", n.email, v.Members)
		}
		if v.Name != "lending" || filepath.Base(v.Dir) != "lending" || filepath.Base(v.Log) != "lending-team.log" {
			t.Fatalf("%s: team at %s", n.email, v.Log)
		}
		last := v.History[len(v.History)-1]
		if last.Op != teamlog.AddMember || last.Text != "added reader@library.example (by invite)" || !last.Effective || last.Signer != desk.email {
			t.Fatalf("%s: history %+v", n.email, last)
		}
	}
	if v := teamOf(t, reader); v.Me != "member" {
		t.Fatalf("reader is %q", v.Me)
	}
	if v := teamOf(t, desk); v.Me != "owner" || v.Invites[0].State != "used" {
		t.Fatalf("desk is %q, invite %+v", v.Me, v.Invites)
	}
	if runtime.GOOS != "windows" {
		for _, f := range []string{filepath.Join(desk.cfgDir, InvitesFile), filepath.Join(reader.cfgDir, JoinsFile)} {
			if st, err := os.Stat(f); err != nil || st.Mode().Perm() != 0o600 {
				t.Fatalf("%s: %v %v", f, st.Mode(), err)
			}
		}
	}
	// The reader is a member: joining again is refused locally.
	if _, err := reader.d.Join(context.Background(), inv.Token); err == nil || !strings.Contains(err.Error(), "already a member") {
		t.Fatalf("second join: %v", err)
	}
	// Reused token: another installation with the invited email (the
	// reader goes offline, so its email id is free).
	reader.stop()
	other := w.node(reader.email)
	waitKeyOnline(t, other)
	if err := attempt(t, other, inv.Token); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("reused token: %v", err)
	}

	// The backstop: a second add-member for the same invite id is
	// discarded at replay, even when an owner signs it.
	tv := teamOf(t, desk)
	td, _ := desk.d.teamByHash(tv.Team)
	st, ops, _, _, _ := desk.d.load(td)
	dup := desk.d.teamSigner(td).AddMember(st.Heads, "stacks@library.example", other.d.pub, inv.ID, w.now())
	st, _ = teamlog.Replay(tv.Team, append(ops, dup))
	if st.Effective[dup.Hash()] {
		t.Fatal("second add-member for one invite counted")
	}
	if _, ok := st.Members["stacks@library.example"]; ok {
		t.Fatal("second add-member made a member")
	}
}

func TestInviteRefusals(t *testing.T) {
	fastJoins(t)
	w := newWorld(t)
	desk := w.node("desk@library.example")
	carol := w.node("carol@library.example")
	waitKeyOnline(t, desk)
	waitKeyOnline(t, carol)
	if _, err := desk.d.CreateTeam("lending"); err != nil {
		t.Fatal(err)
	}

	// Wrong email: refused locally, and by the owner when sent anyway.
	forStacks, _ := desk.d.Invite("lending", "stacks@library.example", 0)
	if _, err := carol.d.Join(context.Background(), forStacks.Token); err == nil || !strings.Contains(err.Error(), "for stacks@library.example") {
		t.Fatalf("wrong email: %v", err)
	}
	if err := attempt(t, carol, forStacks.Token); err == nil || !strings.Contains(err.Error(), "another email") {
		t.Fatalf("wrong email at the owner: %v", err)
	}

	// Token for another team: the team hash in the token was changed.
	inv, _ := desk.d.Invite("lending", carol.email, 0)
	iv, _ := teamlog.ParseToken(inv.Token)
	iv.Team = strings.Repeat("ab", 32)
	if err := attempt(t, carol, iv.Token()); err == nil || !strings.Contains(err.Error(), "another team") {
		t.Fatalf("other team: %v", err)
	}
	// A damaged secret: the proof does not match.
	iv, _ = teamlog.ParseToken(inv.Token)
	iv.Secret[0] ^= 1
	if err := attempt(t, carol, iv.Token()); err == nil || !strings.Contains(err.Error(), "unknown") && !strings.Contains(err.Error(), "no such invite") {
		// A changed secret changes the id too, so the owner knows no
		// such invite.
		t.Fatalf("damaged secret: %v", err)
	}
	// Rendezvous mismatch hint.
	iv, _ = teamlog.ParseToken(inv.Token)
	iv.Rendezvous = "wss://elsewhere.example/peerjs"
	jv, err := carol.d.Join(context.Background(), iv.Token())
	if err != nil || !strings.Contains(jv.Warning, "wss://elsewhere.example/peerjs") {
		t.Fatalf("rendezvous hint: %q %v", jv.Warning, err)
	}
	waitJoin(t, carol, jv.ID, JoinDone) // the owner is reachable all the same

	// Expired token: refused locally, and by the owner.
	late, _ := desk.d.Invite("lending", "stacks@library.example", 2*time.Minute)
	stacks := w.node("stacks@library.example")
	waitKeyOnline(t, stacks)
	w.skip(time.Hour)
	if _, err := stacks.d.Join(context.Background(), late.Token); !errors.Is(err, teamlog.ErrInviteExpired) {
		t.Fatalf("expired, locally: %v", err)
	}
	if err := attempt(t, stacks, late.Token); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired at the owner: %v", err)
	}
	if v := teamOf(t, desk); len(v.Members) != 2 {
		t.Fatalf("members after refusals: %+v", v.Members)
	}
}

// The owner is offline when the joiner redeems the token, and the joiner
// restarts before they meet: the join completes once the owner is back.
func TestJoinRetries(t *testing.T) {
	fastJoins(t)
	w := newWorld(t)
	desk := w.node("desk@library.example")
	reader := w.node("reader@library.example")
	waitKeyOnline(t, desk)
	if _, err := desk.d.CreateTeam("lending"); err != nil {
		t.Fatal(err)
	}
	inv, _ := desk.d.Invite("", reader.email, 0)
	desk.stop()

	jv, err := reader.d.Join(context.Background(), inv.Token)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if j := reader.d.Joins()[0]; j.Tries >= 1 {
			if j.State != JoinPending || !strings.Contains(j.Detail, "not online") {
				t.Fatalf("while the owner is away: %+v", j)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	reader.stop()
	reader.start()
	desk.start()
	waitJoin(t, reader, jv.ID, JoinDone)
	if _, ok := member(teamOf(t, reader), reader.email); !ok {
		t.Fatal("not a member after the retry")
	}
	if _, ok := member(teamOf(t, desk), reader.email); !ok {
		t.Fatal("owner did not add the member")
	}
}

func TestTeamRoster(t *testing.T) {
	fastJoins(t)
	w := newWorld(t)
	desk := w.node("desk@library.example")
	reader := w.node("reader@library.example")
	waitKeyOnline(t, desk)
	waitKeyOnline(t, reader)
	if _, err := desk.d.CreateTeam("Lending"); err == nil {
		t.Fatal("upper-case team name accepted")
	}
	desk.d.CreateTeam("lending")
	if _, err := desk.d.CreateTeam("lending"); err == nil {
		t.Fatal("second team of one name accepted")
	}
	inv, _ := desk.d.Invite("", reader.email, 0)
	jv, _ := reader.d.Join(context.Background(), inv.Token)
	waitJoin(t, reader, jv.ID, JoinDone)

	// A member cannot change the roster.
	if _, err := reader.d.Invite("", "stacks@library.example", 0); err == nil || !strings.Contains(err.Error(), "only a team owner") {
		t.Fatalf("member invited: %v", err)
	}
	if _, err := desk.d.SetOwner("", desk.email, false); err == nil {
		t.Fatal("owner removed themselves")
	}
	v, err := desk.d.SetOwner("", reader.email, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Owners) != 2 || v.Owners[0] != desk.email {
		t.Fatalf("owners %v", v.Owners)
	}
	// Removal reassigns the member's published documents held here; an
	// unpublished file naming them is not one.
	reader.write("renewals.md", "# Renewals\n\n## Plan\n\nRenew online.\n")
	waitIndexed(t, reader, 1)
	reader.publish("renewals.md")
	reader.replicate(desk)
	desk.write("lending/holds.md", "---\nkalka_id: 7f1c2d3e-4b5a-4c6d-8e7f-0a1b2c3d4e5f\nowner: reader@library.example\nteam: lending\n---\n# Holds\n")
	waitIndexed(t, desk, 2)
	prev, err := desk.d.RemovalPreview("", reader.email)
	if err != nil || len(prev) != 1 || prev[0].Title != "Renewals" {
		t.Fatalf("preview %+v %v", prev, err)
	}
	if v, err = desk.d.Remove("", reader.email); err != nil {
		t.Fatal(err)
	}
	if _, ok := member(v, reader.email); ok || len(v.Owners) != 1 {
		t.Fatalf("after removal: %+v", v)
	}
	// The document the member owned goes to the remover, who releases it.
	var reassigned bool
	for _, h := range v.History[len(v.History)-2:] {
		reassigned = reassigned || (h.Op == teamlog.ReassignDocument && h.Effective && strings.Contains(h.Text, "to desk@library.example"))
	}
	if !reassigned {
		t.Fatalf("no reassignment: %+v", v.History[len(v.History)-2:])
	}
	waitOwner(t, desk, prev[0].KalkaID, desk.email)
}

func waitIndexed(t *testing.T, n *node, count int) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if len(n.d.Store.List()) >= count {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s indexed %d files", n.email, len(n.d.Store.List()))
}
