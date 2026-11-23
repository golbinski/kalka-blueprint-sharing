package doctor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/plugin"
)

var t0 = time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)

// healthy is an installation where every check passes.
func healthy() Probes {
	return Probes{
		OS: "windows",
		Config: func() (Config, error) {
			return Config{Email: "ida@library.example", Rendezvous: "wss://meet.library.example", Folder: "/f"}, nil
		},
		Key:        func() error { return nil },
		Daemon:     func(context.Context) DaemonState { return DaemonState{Running: true} },
		Rendezvous: func(context.Context, string) (Rendezvous, error) { return Rendezvous{Date: t0, TURN: true}, nil },
		Now:        func() time.Time { return t0.Add(30 * time.Second) },
		Folder:     func(string) error { return nil },
		WebView2:   func() (string, bool) { return "131.0.2903.70", true },
		Autostart:  func() (bool, bool, error) { return true, false, nil },
		URLScheme:  func() (bool, string, error) { return true, "", nil },
	}
}

func states(r Report) string {
	var out []string
	for _, x := range r.Results {
		out = append(out, x.Check+"="+x.State)
	}
	return strings.Join(out, " ")
}

func TestHealthy(t *testing.T) {
	r := Run(context.Background(), healthy())
	want := "config=ok key=ok daemon=ok local-api=ok rendezvous=ok turn=ok clock=ok folder=ok webview2=ok autostart=ok url-scheme=ok"
	if got := states(r); got != want || r.Exit() != 0 {
		t.Fatalf("%s (exit %d)", got, r.Exit())
	}
	for _, x := range r.Results {
		if x.Detail == "" || x.Fix != "" {
			t.Errorf("%s: detail %q fix %q", x.Check, x.Detail, x.Fix)
		}
	}
	// WebView2 is a Windows check only.
	p := healthy()
	p.OS = "darwin"
	if got := states(Run(context.Background(), p)); strings.Contains(got, "webview2") {
		t.Error(got)
	}
}

func TestEachProblem(t *testing.T) {
	for _, c := range []struct {
		name string
		mod  func(*Probes)
		want string // the states that change
		exit int
	}{
		{"not set up", func(p *Probes) { p.Config = func() (Config, error) { return Config{}, ErrNoConfig } },
			"config=fail key=skip rendezvous=skip turn=skip clock=skip folder=skip", 1},
		{"bad config", func(p *Probes) { p.Config = func() (Config, error) { return Config{}, errors.New("yaml: line 3") } }, "config=fail", 1},
		{"no key", func(p *Probes) { p.Key = func() error { return errors.New("no identity key yet") } }, "key=fail", 1},
		{"no daemon", func(p *Probes) { p.Daemon = func(context.Context) DaemonState { return DaemonState{} } }, "daemon=warn local-api=skip", 2},
		{"api refuses", func(p *Probes) {
			p.Daemon = func(context.Context) DaemonState {
				return DaemonState{Running: true, API: errors.New("401 Unauthorized")}
			}
		}, "local-api=fail", 1},
		{"no rendezvous", func(p *Probes) {
			p.Rendezvous = func(context.Context, string) (Rendezvous, error) {
				return Rendezvous{}, errors.New("dial tcp: i/o timeout")
			}
		}, "rendezvous=fail turn=skip clock=skip", 1},
		{"no turn", func(p *Probes) {
			p.Rendezvous = func(context.Context, string) (Rendezvous, error) { return Rendezvous{Date: t0}, nil }
		}, "turn=warn", 2},
		{"clock a little off", func(p *Probes) { p.Now = func() time.Time { return t0.Add(-3 * time.Minute) } }, "clock=warn", 2},
		{"clock far off", func(p *Probes) { p.Now = func() time.Time { return t0.Add(11 * time.Minute) } }, "clock=fail", 1},
		{"no server time", func(p *Probes) {
			p.Rendezvous = func(context.Context, string) (Rendezvous, error) { return Rendezvous{TURN: true}, nil }
		}, "clock=skip", 0},
		{"folder", func(p *Probes) { p.Folder = func(string) error { return errors.New("not writable") } }, "folder=fail", 1},
		{"no webview2", func(p *Probes) { p.WebView2 = func() (string, bool) { return "", false } }, "webview2=fail", 1},
		{"stale autostart", func(p *Probes) { p.Autostart = func() (bool, bool, error) { return true, true, nil } }, "autostart=warn", 2},
		{"autostart off", func(p *Probes) { p.Autostart = func() (bool, bool, error) { return false, false, nil } }, "", 0},
		{"links elsewhere", func(p *Probes) {
			p.URLScheme = func() (bool, string, error) { return false, "they open C:\\old\\kalka-desktop.exe", nil }
		}, "url-scheme=warn", 2},
	} {
		p := healthy()
		c.mod(&p)
		r := Run(context.Background(), p)
		got := states(r)
		for _, w := range strings.Fields(c.want) {
			if !strings.Contains(" "+got+" ", " "+w+" ") {
				t.Errorf("%s: %s, want %s", c.name, got, w)
			}
		}
		if r.Exit() != c.exit {
			t.Errorf("%s: exit %d, want %d", c.name, r.Exit(), c.exit)
		}
		for _, x := range r.Results {
			if (x.State == Warn || x.State == Fail) && x.Fix == "" {
				t.Errorf("%s: %s has no fix", c.name, x.Check)
			}
		}
	}
}

// The rendezvous is asked for /kalka/ice: TURN is offered when a turn:
// URL is listed; a stock PeerJS server answers 404 and offers none. The
// clock comes from the Date header.
func TestAskRendezvous(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", t0.Format(http.TimeFormat))
		switch {
		case r.URL.Path == "/kalka/ice" && r.Host != "" && strings.HasPrefix(r.Header.Get("User-Agent"), "Go"):
			fmt.Fprint(w, `{"iceServers":[{"urls":"stun:meet.library.example:3478"},{"urls":["turn:meet.library.example:3478"],"username":"u","credential":"c"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	rv, err := AskRendezvous(context.Background(), "ws://"+strings.TrimPrefix(srv.URL, "http://"))
	if err != nil || !rv.TURN || !rv.Date.Equal(t0) {
		t.Fatalf("%+v %v", rv, err)
	}
	stock := httptest.NewServer(http.NotFoundHandler())
	defer stock.Close()
	rv, err = AskRendezvous(context.Background(), "ws://"+strings.TrimPrefix(stock.URL, "http://"))
	if err != nil || rv.TURN {
		t.Fatalf("stock: %+v %v", rv, err)
	}
	stock.Close()
	if _, err := AskRendezvous(context.Background(), "ws://"+strings.TrimPrefix(stock.URL, "http://")); err == nil {
		t.Fatal("a closed server answered")
	}
}

func TestWritable(t *testing.T) {
	dir := t.TempDir()
	if err := Writable(dir); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("left %v", entries)
	}
	if err := Writable(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing folder is writable")
	}
	f := filepath.Join(dir, "file")
	os.WriteFile(f, nil, 0o644)
	if err := Writable(f); err == nil {
		t.Fatal("a file is a folder")
	}
}

// Plugin rows: never a failure; not set up is fine (spec 12.5).
func TestPluginRows(t *testing.T) {
	st := func(state, msg string) *plugin.Status { return &plugin.Status{State: state, Message: msg} }
	r := Run(context.Background(), Probes{OS: "linux", Plugins: func(context.Context) []plugin.Survey {
		return []plugin.Survey{
			{Scheme: "gh", Settings: true, Status: st(plugin.StateNeedsSetup, "gh is not logged in"), Hint: &plugin.Hint{Command: "gh auth login"}},
			{Scheme: "jira", Settings: true, Status: st(plugin.StateError, "Jira rejected the credentials")},
			{Scheme: "lib", Problem: "plugin speaks protocol 1, kalka speaks 2"},
			{Scheme: "code"},
			{Scheme: "wiki", Settings: true, Status: st(plugin.StateReady, "wiki.example")},
		}
	}})
	got := map[string]Result{}
	for _, x := range r.Results {
		got[x.Check] = x
	}
	want := map[string]string{"plugin:gh": OK, "plugin:jira": Warn, "plugin:lib": Warn, "plugin:code": OK, "plugin:wiki": OK}
	for k, v := range want {
		if got[k].State != v {
			t.Errorf("%s: %+v", k, got[k])
		}
	}
	if !strings.Contains(got["plugin:jira"].Fix, "kalka plugin settings jira --check") || got["plugin:wiki"].Detail != "ready: wiki.example" {
		t.Errorf("%+v", got)
	}
	for _, x := range r.Results {
		if x.State == Fail {
			t.Errorf("fails: %+v", x)
		}
	}
}
