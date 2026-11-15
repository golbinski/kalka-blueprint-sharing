package desktop

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/cli"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/teamlog"
)

func testLink(t *testing.T) (link, token string) {
	t.Helper()
	pub, _, _ := ed25519.GenerateKey(nil)
	iv := teamlog.Invite{TeamName: "lending", Team: strings.Repeat("ab", 32), Owner: teamlog.EncodeKey(pub),
		Inviter: "desk@library.example", Email: "alice@corp.com", Secret: bytes.Repeat([]byte{5}, 32),
		Expires: time.Now().Add(time.Hour).UTC().Truncate(time.Second), Rendezvous: "peerjs-cloud"}
	return iv.Link(), iv.Token()
}

func TestStartArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want Start
	}{
		{nil, Start{}},
		{[]string{"--hidden"}, Start{Hidden: true}},
		{[]string{"kalka://join/ABC"}, Start{Link: "kalka://join/ABC"}},
		{[]string{"KALKA://join/ABC"}, Start{Link: "KALKA://join/ABC"}},
		{[]string{"kalka:"}, Start{Link: "kalka:"}},
		{[]string{"kalka"}, Start{CLI: true}},
		{[]string{"kalkax://join/ABC"}, Start{CLI: true}},
		{[]string{"status"}, Start{CLI: true}},
		{[]string{"kalka://join/ABC", "status"}, Start{CLI: true}},
		{[]string{"--hidden", "kalka://join/ABC"}, Start{CLI: true}},
	} {
		if got := StartArgs(c.args); got != c.want {
			t.Errorf("StartArgs(%q) = %+v, want %+v", c.args, got, c.want)
		}
	}
}

// TestLinkHandOver (spec 19.7, 23.1a): a start with a link and no shell
// carries the route into the window; with a shell attached, it calls
// ui/show with the join route and exits; a link raised while the shell
// runs opens the join screen.
func TestLinkHandOver(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a daemon")
	}
	dir := world(t)
	ctx := context.Background()
	link, token := testLink(t)
	route := daemon.JoinRoute(link)
	if route != "#/join/"+token {
		t.Fatalf("route %q", route)
	}
	plan, err := Prepare(ctx, dir, true, route) // a link is never a hidden start
	if err != nil || plan.Action != StartInProcess || plan.Route != route {
		t.Fatalf("prepare: %+v %v", plan, err)
	}
	ui := newFakeUI()
	s := &Shell{UI: ui, Dir: dir, Hidden: true, TrayHost: true, Autostart: &fakeAutostart{}}
	s.Start(plan)
	t.Cleanup(func() { s.Stop() })
	eventually(t, "the join screen", func() bool {
		return ui.get(func(u *fakeUI) bool { return u.visible && len(u.hashes) > 0 && u.hashes[0] == route })
	})
	c := daemonClient(t, dir)
	eventually(t, "the shell attached", func() bool { sum, err := c.Summary(ctx); return err == nil && sum.Shell == "desktop" })

	// A second start with a link: handed to this shell.
	ui.HideWindow()
	other := "kalka://join/" + strings.ToLower(token) + "/"
	p, err := Prepare(ctx, dir, false, daemon.JoinRoute(other))
	if err != nil || p.Action != StartShowOther {
		t.Fatalf("second start: %+v %v", p, err)
	}
	eventually(t, "handed over", func() bool {
		return ui.get(func(u *fakeUI) bool { return u.visible && len(u.hashes) > 1 && u.hashes[len(u.hashes)-1] == route })
	})
	// A link the system sends to the running shell (macOS).
	s.Link("<kalka://join/garbage>")
	if h := ui.get(func(u *fakeUI) bool { return u.hashes[len(u.hashes)-1] == "#/join/" }); !h {
		t.Fatalf("a bad link: %v", ui.hashes)
	}
	// ui/show takes only page routes.
	for _, bad := range []string{"javascript:alert(1)", "#/join/<b>", "/join", "#/x\"y"} {
		if err := c.ShowRoute(ctx, bad); err == nil {
			t.Fatalf("route %q accepted", bad)
		}
	}
}

// TestLinkAttach: with a headless daemon and no shell, the start plan
// carries the route.
func TestLinkAttach(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a daemon")
	}
	dir := world(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() {
		done <- cli.ServeDaemon(ctx, cli.DaemonOptions{Dir: dir, NoNotifications: true, Autostart: &fakeAutostart{}, Stdout: io.Discard, Stderr: io.Discard})
	}()
	eventually(t, "the headless daemon", func() bool { return answering(ctx, dir) != nil })
	link, _ := testLink(t)
	if err := daemonClient(t, dir).ShowRoute(ctx, daemon.JoinRoute(link)); err != daemon.ErrNoShell {
		t.Fatalf("no shell: %v", err)
	}
	plan, err := Prepare(ctx, dir, false, daemon.JoinRoute(link))
	if err != nil || plan.Action != StartAttach || plan.Route != daemon.JoinRoute(link) {
		t.Fatalf("prepare: %+v %v", plan, err)
	}
	ui := newFakeUI()
	s := &Shell{UI: ui, Dir: dir, TrayHost: true}
	s.Start(plan)
	eventually(t, "the join screen", func() bool {
		return ui.get(func(u *fakeUI) bool { return len(u.hashes) > 0 && u.hashes[0] == daemon.JoinRoute(link) })
	})
	cancel()
	<-done
}
