package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/teamlog"
)

// TestJoinPreview: the join screen's answers (spec 11.1, 19.7), and that
// opening a link never joins.
func TestJoinPreview(t *testing.T) {
	fastJoins(t)
	w := newWorld(t)
	desk := w.node("desk@library.example")
	carol := w.node("carol@library.example")
	waitKeyOnline(t, desk)
	waitKeyOnline(t, carol)
	if _, err := desk.d.CreateTeam("lending"); err != nil {
		t.Fatal(err)
	}
	inv, err := desk.d.Invite("lending", carol.email, 0)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Link != teamlog.LinkPrefix+inv.Token {
		t.Fatalf("link %q", inv.Link)
	}

	p, err := carol.d.JoinPreview("<" + inv.Link + ">")
	if err != nil {
		t.Fatal(err)
	}
	if p.Problem != "" || p.Name != "lending" || p.Inviter != desk.email || p.Email != carol.email ||
		!p.Expires.Equal(inv.Expires) || p.Token != inv.Token || p.Joining != nil {
		t.Fatalf("preview: %+v", p)
	}
	if len(carol.d.Joins()) != 0 {
		t.Fatal("a preview joined")
	}

	problem := func(n *node, token, want, text string) {
		t.Helper()
		p, err := n.d.JoinPreview(token)
		if err != nil || p.Problem != want || !strings.Contains(p.Message, text) {
			t.Fatalf("want %s (%q): %+v %v", want, text, p, err)
		}
		if len(n.d.Joins()) != 0 && want != ProblemMember {
			t.Fatal("a preview joined")
		}
	}
	// The owner opens their own link.
	problem(desk, inv.Link, ProblemOwnInvite, "you made")
	// Another person opens it.
	stacks := w.node("stacks@library.example")
	problem(stacks, inv.Link, KindInviteEmail, "this invite is for carol@library.example, you are stacks@library.example")
	// Another rendezvous server.
	iv, _ := teamlog.ParseToken(inv.Token)
	iv.Rendezvous = "wss://elsewhere.example/"
	problem(carol, iv.Link(), KindRendezvousMismatch, "another rendezvous server")
	// The same server, written another way, is no mismatch.
	iv.Rendezvous = carol.d.cfg.Rendezvous + "/"
	if p, err := carol.d.JoinPreview(iv.Token()); err != nil || (p.Problem != "" && p.Problem != KindRendezvousMismatch) {
		t.Fatalf("%+v %v", p, err)
	}
	// Not a token.
	if _, err := carol.d.JoinPreview("kalka://join/" + inv.Token + "?x=1"); KindOf(err) != KindInviteToken {
		t.Fatalf("query: %v", err)
	}

	// Joining: the preview says so, then the member line.
	jv, err := carol.d.Join(context.Background(), inv.Link)
	if err != nil {
		t.Fatal(err)
	}
	waitJoin(t, carol, jv.ID, JoinDone)
	p, err = carol.d.JoinPreview(inv.Token)
	if err != nil || p.Problem != ProblemMember || p.Joining == nil || p.Joining.State != JoinDone || p.Joining.Inviter != desk.email {
		t.Fatalf("member: %+v %v", p, err)
	}

	// Expired.
	late, _ := desk.d.Invite("lending", stacks.email, 2*time.Minute)
	w.skip(time.Hour)
	problem(stacks, late.Token, KindInviteExpired, "expired")
}

// TestPreviewInvite is the setup form's prefill: the parse alone, with
// no configuration.
func TestPreviewInvite(t *testing.T) {
	w := newWorld(t)
	desk := w.node("desk@library.example")
	if _, err := desk.d.CreateTeam("lending"); err != nil {
		t.Fatal(err)
	}
	inv, _ := desk.d.Invite("lending", "olga@library.example", 0)
	p, err := PreviewInvite(strings.ToLower(inv.Link))
	if err != nil || p.Email != "olga@library.example" || p.Rendezvous != desk.d.cfg.Rendezvous || p.Inviter != desk.email || p.Name != "lending" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := PreviewInvite("kalka://join/AAAA"); KindOf(err) != KindInviteToken {
		t.Fatalf("junk: %v", err)
	}
}
