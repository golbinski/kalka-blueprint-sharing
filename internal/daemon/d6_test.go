package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/core"
)

// TestShellStream (spec 22): POST /api/ui/show needs an attached shell;
// with one attached the summary says so, the request reaches it as an
// event, and the daemon's own notice presenter is silent.
func TestShellStream(t *testing.T) {
	w := newWorld(t)
	var mu sync.Mutex
	var shown []core.Notice
	a := w.node("alice@corp.com", func(o *Options) {
		o.Notify = func(n core.Notice) { mu.Lock(); shown = append(shown, n); mu.Unlock() }
	})
	c, _ := Connect(a.cfgDir)
	ctx := context.Background()
	if err := c.ShowUI(ctx, ""); !errors.Is(err, ErrNoShell) {
		t.Fatalf("show without a shell: %v", err)
	}
	// Without a shell the daemon presents notices.
	a.d.Bus().Publish(core.BusEvent{Type: "notice", Data: core.Notice{Kind: "joined", Title: "one"}})
	waitFor(t, "the daemon's notice", func() bool { mu.Lock(); defer mu.Unlock(); return len(shown) == 1 })

	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	got := make(chan string, 16)
	go func() {
		_ = c.ShellEvents(sctx, func(typ string, data json.RawMessage) {
			if typ == "ui-show" {
				got <- string(data)
			}
		})
	}()
	waitFor(t, "the shell attached", func() bool { s, _ := c.Summary(ctx); return s.Shell == "desktop" })
	if a.d.Bus().Clients() != 0 {
		t.Fatal("a shell counts as a page")
	}
	if err := c.ShowUI(ctx, "plans/a.md"); err != nil {
		t.Fatal(err)
	}
	select {
	case d := <-got:
		if d != `{"path":"plans/a.md"}` {
			t.Fatalf("event %s", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no ui-show event")
	}
	a.d.Bus().Publish(core.BusEvent{Type: "notice", Data: core.Notice{Kind: "joined", Title: "two"}})
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	n := len(shown)
	mu.Unlock()
	if n != 1 {
		t.Fatal("the daemon presented a notice while a shell is attached")
	}
	cancel()
	waitFor(t, "the shell detached", func() bool { s, _ := c.Summary(ctx); return s.Shell == "" })
}

// TestWindowCookie: the token link with window=1 marks the page as the
// desktop window, readable by the page (not HttpOnly).
func TestWindowCookie(t *testing.T) {
	w := newWorld(t)
	a := w.node("alice@corp.com")
	r := get(t, noRedirect, a.d.URL()+"&"+WindowParam+"=1", nil)
	r.Body.Close()
	var win *http.Cookie
	for _, c := range r.Cookies() {
		if c.Name == WindowCookie {
			win = c
		}
	}
	if win == nil || win.Value != "1" || win.HttpOnly {
		t.Fatalf("cookies %v", r.Cookies())
	}
	r = get(t, noRedirect, a.d.URL(), nil)
	r.Body.Close()
	for _, c := range r.Cookies() {
		if c.Name == WindowCookie {
			t.Fatal("a browser tab got the window cookie")
		}
	}
}
