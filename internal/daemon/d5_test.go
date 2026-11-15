package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/autostart"
	"github.com/golbinski/kalka/internal/core"
)

const lockers = "# Parcel lockers\n\n## Plan\n\nPut lockers at every depot.\n"

func fast(t *testing.T) {
	oj, op := core.JoinEvery, core.PushDelay
	core.JoinEvery, core.PushDelay = 200*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { core.JoinEvery, core.PushDelay = oj, op })
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// teamPair makes depot (owner) and courier (member) of team parcels.
func teamPair(t *testing.T, opts ...func(*Options)) (*node, *node) {
	t.Helper()
	fast(t)
	w := newWorld(t)
	depot, courier := w.node("depot@parcels.example", opts...), w.node("courier@parcels.example", opts...)
	waitFor(t, "registrations", func() bool { return depot.d.State().Online && courier.d.State().Online })
	if _, err := depot.d.CreateTeam("parcels"); err != nil {
		t.Fatal(err)
	}
	inv, err := depot.d.Invite("", courier.email, 0)
	if err != nil {
		t.Fatal(err)
	}
	j, err := courier.d.Join(context.Background(), inv.Token)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the join", func() bool {
		for _, x := range courier.d.Joins() {
			if x.ID == j.ID && x.State == core.JoinDone {
				return true
			}
		}
		return false
	})
	return depot, courier
}

func holds(n *node, want string) bool {
	for _, in := range n.d.Store.List() {
		if raw, err := os.ReadFile(in.Path); err == nil && strings.Contains(string(raw), want) {
			return true
		}
	}
	return false
}

// TestSyncWithNoWindow is the required area of D5: replication runs with
// no API listener and no page, and still after a page came and went.
func TestSyncWithNoWindow(t *testing.T) {
	noAPI := func(o *Options) { o.APIAddr = "-" }
	depot, courier := teamPair(t, noAPI)
	if depot.d.APIAddr() != "" || depot.d.Bus().Clients() != 0 {
		t.Fatal("expected no listener and no page")
	}
	depot.write("parcels/lockers.md", lockers)
	waitFor(t, "the index", func() bool { return len(depot.d.Store.List()) == 1 })
	if _, err := depot.d.Publish(depot.path("parcels/lockers.md"), "", ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pushed document", func() bool { return holds(courier, "every depot") })

	// A page connects and leaves; the next change still travels.
	_, unsub := depot.d.Bus().Subscribe()
	unsub()
	depot.write("parcels/lockers.md", strings.Replace(depot.read("parcels/lockers.md"), "every depot", "every depot and station", 1))
	waitFor(t, "the edit indexed", func() bool { return holds(depot, "every depot and station") })
	if _, err := depot.d.Publish(depot.path("parcels/lockers.md"), "", "stations too"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pushed edit", func() bool { return holds(courier, "every depot and station") })
}

type fakeAutostart struct {
	mu sync.Mutex
	on bool
}

func (f *fakeAutostart) Get() (autostart.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return autostart.Status{Enabled: f.on}, nil
}
func (f *fakeAutostart) Enable() (autostart.Status, error) {
	f.mu.Lock()
	f.on = true
	f.mu.Unlock()
	return f.Get()
}
func (f *fakeAutostart) Disable() error { f.mu.Lock(); f.on = false; f.mu.Unlock(); return nil }

// TestSettingsAndPause: the settings API, state.yaml, pause over the API,
// and a pause that survives a restart with one notice.
func TestSettingsAndPause(t *testing.T) {
	w := newWorld(t)
	auto := &fakeAutostart{}
	var mu sync.Mutex
	var shown []core.Notice
	a := w.node("alice@corp.com", func(o *Options) {
		o.Autostart = auto
		o.Notify = func(n core.Notice) { mu.Lock(); shown = append(shown, n); mu.Unlock() }
	})
	c, _ := Connect(a.cfgDir)
	ctx := context.Background()

	s, err := c.Settings(ctx)
	if err != nil || *s.Paused || !*s.Notifications || !*s.KeepRunningOnClose || !s.StartAtLoginAvailable || *s.StartAtLogin {
		t.Fatalf("defaults: %+v %v", s, err)
	}
	f := false
	if s, err = c.ApplySettings(ctx, Settings{Notifications: &f, StartAtLogin: ptr(true)}); err != nil || *s.Notifications || !*s.StartAtLogin {
		t.Fatalf("apply: %+v %v", s, err)
	}
	if !auto.on {
		t.Fatal("start at login not enabled")
	}
	if s, err = c.SetPaused(ctx, true); err != nil || !*s.Paused {
		t.Fatalf("pause: %+v %v", s, err)
	}
	if st, _ := c.Status(ctx); !st.Paused {
		t.Fatal("status not paused")
	}
	if sum, err := c.Summary(ctx); err != nil || !sum.Paused || sum.Online {
		t.Fatalf("summary: %+v %v", sum, err)
	}
	raw, _ := os.ReadFile(filepath.Join(a.cfgDir, StateFile))
	if !strings.Contains(string(raw), "paused: true") || !strings.Contains(string(raw), "notifications: false") || strings.Contains(string(raw), "start_at_login") {
		t.Fatalf("state.yaml:\n%s", raw)
	}

	// Notifications back on; a restart keeps the pause and says so once.
	if _, err := c.ApplySettings(ctx, Settings{Notifications: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	a.stop()
	a.start()
	if !a.d.Paused() || a.d.State().Online {
		t.Fatal("the pause did not survive the restart")
	}
	waitFor(t, "the paused notice", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(shown) == 1 && shown[0].Kind == "paused"
	})
	c, _ = Connect(a.cfgDir)
	if s, err = c.SetPaused(ctx, false); err != nil || *s.Paused {
		t.Fatalf("resume: %+v %v", s, err)
	}
	waitFor(t, "online after resume", func() bool { return a.d.State().Online })

	// Without an autostart, start at login is reported unavailable.
	b := w.node("bob@corp.com")
	cb, _ := Connect(b.cfgDir)
	if s, _ := cb.Settings(ctx); s.StartAtLoginAvailable || s.StartAtLogin != nil {
		t.Fatalf("no autostart: %+v", s)
	}
	if _, err := cb.ApplySettings(ctx, Settings{StartAtLogin: ptr(true)}); err == nil {
		t.Fatal("changed start at login without an autostart")
	}
}
