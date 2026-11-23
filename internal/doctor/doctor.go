// Package doctor checks that an installation can work (spec 12.5):
// configuration, key, daemon, network, clock, folder and the desktop
// integration. Every check reads the system through Probes, so tests run
// on fakes and never touch the real registration or network.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/golbinski/kalka/internal/plugin"
)

// States of a check.
const (
	OK   = "ok"
	Warn = "warn"
	Fail = "fail"
	// Skip is a check that could not run because one it needs failed.
	Skip = "skip"
)

// Result is one check.
type Result struct {
	Check  string `json:"check"`
	State  string `json:"state"`
	Detail string `json:"detail"`
	// Fix says what to do; empty for ok.
	Fix string `json:"fix,omitempty"`
}

// Report is every check, in the order of the spec's table.
type Report struct {
	Results []Result `json:"results"`
	Summary struct {
		OK   int `json:"ok"`
		Warn int `json:"warn"`
		Fail int `json:"fail"`
	} `json:"summary"`
}

// Exit is the exit code of `kalka doctor`: 0 all ok, 2 warnings only, 1 a
// failure.
func (r Report) Exit() int {
	switch {
	case r.Summary.Fail > 0:
		return 1
	case r.Summary.Warn > 0:
		return 2
	}
	return 0
}

// Config is what the checks need of the configuration.
type Config struct {
	Email, Rendezvous, Folder string
}

// Rendezvous is what the rendezvous server answered.
type Rendezvous struct {
	// Date is the server's clock (its Date header); zero when absent.
	Date time.Time
	// TURN is set when it offers TURN credentials.
	TURN bool
}

// DaemonState is what was found of a running daemon.
type DaemonState struct {
	Running bool
	// API is the error of a status call with the token; nil when it
	// answered.
	API error
}

// Probes read the system. A nil probe skips its check.
type Probes struct {
	OS         string // runtime.GOOS when empty
	Config     func() (Config, error)
	Key        func() error
	Daemon     func(ctx context.Context) DaemonState
	Rendezvous func(ctx context.Context, server string) (Rendezvous, error)
	Now        func() time.Time
	Folder     func(dir string) error
	// WebView2 reports the runtime's version (Windows).
	WebView2 func() (string, bool)
	// Autostart: whether an entry is registered, and whether it runs a
	// program that no longer exists.
	Autostart func() (enabled, stale bool, err error)
	// URLScheme: whether kalka:// opens this installation, and why not.
	URLScheme func() (ok bool, detail string, err error)
	// Plugins: each installed plugin's state, checked against its service
	// (spec 10.6).
	Plugins func(ctx context.Context) []plugin.Survey
}

// ErrNoConfig is a configuration that does not exist.
var ErrNoConfig = errors.New("no configuration")

// Clock limits (spec 12.5): invites and TURN credentials are time-bound.
const (
	ClockWarn = 2 * time.Minute
	ClockFail = 10 * time.Minute
	// Timeout bounds the rendezvous request.
	Timeout = 5 * time.Second
)

// Run runs every check.
func Run(ctx context.Context, p Probes) Report {
	var r Report
	add := func(check, state, detail, fix string) {
		r.Results = append(r.Results, Result{Check: check, State: state, Detail: detail, Fix: fix})
		switch state {
		case OK:
			r.Summary.OK++
		case Warn:
			r.Summary.Warn++
		case Fail:
			r.Summary.Fail++
		}
	}
	goos := p.OS
	if goos == "" {
		goos = runtime.GOOS
	}
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}

	var cfg Config
	haveCfg := false
	if p.Config != nil {
		c, err := p.Config()
		switch {
		case errors.Is(err, ErrNoConfig):
			add("config", Fail, "kalka is not set up", "run `kalka setup`, or open kalka and fill in the setup form")
		case err != nil:
			add("config", Fail, "the configuration does not load: "+err.Error(), "fix or remove config.yaml, then run `kalka setup`")
		default:
			cfg, haveCfg = c, true
			add("config", OK, "set up for "+c.Email, "")
		}
	}

	if p.Key != nil {
		if !haveCfg {
			add("key", Skip, "not checked: no configuration", "")
		} else if err := p.Key(); err != nil {
			add("key", Fail, "the identity key cannot be read: "+err.Error(), "restore identity.key in the configuration folder from a backup; a new key means joining every team again")
		} else {
			add("key", OK, "the identity key is readable", "")
		}
	}

	if p.Daemon != nil {
		st := p.Daemon(ctx)
		switch {
		case !st.Running:
			add("daemon", Warn, "no daemon runs, so nothing syncs", "run `kalka`, or open the Kalka app")
			add("local-api", Skip, "not checked: no daemon runs", "")
		case st.API != nil:
			add("daemon", OK, "a daemon runs", "")
			add("local-api", Fail, "the daemon's API does not answer: "+st.API.Error(), "run `kalka stop`, then `kalka`")
		default:
			add("daemon", OK, "a daemon runs", "")
			add("local-api", OK, "the daemon's API answers", "")
		}
	}

	if p.Rendezvous != nil {
		if !haveCfg {
			for _, c := range []string{"rendezvous", "turn", "clock"} {
				add(c, Skip, "not checked: no configuration", "")
			}
		} else {
			rctx, cancel := context.WithTimeout(ctx, Timeout)
			rv, err := p.Rendezvous(rctx, cfg.Rendezvous)
			cancel()
			if err != nil {
				add("rendezvous", Fail, fmt.Sprintf("%s does not answer: %v", cfg.Rendezvous, err), "check the network, or the server named in `kalka setup`")
				add("turn", Skip, "not checked: no answer from the rendezvous", "")
				add("clock", Skip, "not checked: no answer from the rendezvous", "")
			} else {
				add("rendezvous", OK, cfg.Rendezvous+" answers", "")
				if rv.TURN {
					add("turn", OK, "the rendezvous gives TURN credentials", "")
				} else {
					add("turn", Warn, "the rendezvous gives no TURN server", "peers behind strict networks will not connect; ask whoever runs the server to enable TURN")
				}
				clock(add, now(), rv.Date)
			}
		}
	}

	if p.Folder != nil {
		switch {
		case !haveCfg:
			add("folder", Skip, "not checked: no configuration", "")
		case cfg.Folder == "":
			add("folder", Fail, "no folder is set", "run `kalka setup --folder <dir>`")
		default:
			if err := p.Folder(cfg.Folder); err != nil {
				add("folder", Fail, cfg.Folder+": "+err.Error(), "create the folder or give yourself write access to it")
			} else {
				add("folder", OK, cfg.Folder+" is writable", "")
			}
		}
	}

	if goos == "windows" && p.WebView2 != nil {
		if v, ok := p.WebView2(); ok {
			add("webview2", OK, "the WebView2 runtime "+v+" is installed", "")
		} else {
			add("webview2", Fail, "the WebView2 runtime is missing, so the window cannot open", "install the Evergreen WebView2 runtime from Microsoft")
		}
	}

	if p.Autostart != nil {
		enabled, stale, err := p.Autostart()
		switch {
		case err != nil:
			add("autostart", Warn, "the start-at-login entry cannot be read: "+err.Error(), "turn start at login off and on again in Settings")
		case stale:
			add("autostart", Warn, "start at login runs a program that no longer exists", "turn start at login off and on again in Settings")
		case enabled:
			add("autostart", OK, "kalka starts at login", "")
		default:
			add("autostart", OK, "kalka does not start at login", "")
		}
	}

	if p.URLScheme != nil {
		ok, detail, err := p.URLScheme()
		switch {
		case err != nil:
			add("url-scheme", Warn, "kalka:// links cannot be checked: "+err.Error(), "open Kalka once to register it")
		case !ok:
			add("url-scheme", Warn, "kalka:// links do not open this installation: "+detail, "open Kalka once to register it")
		default:
			add("url-scheme", OK, "kalka:// links open this installation", "")
		}
	}
	if p.Plugins != nil {
		for _, s := range p.Plugins(ctx) {
			pluginRow(add, s)
		}
	}
	return r
}

// pluginRow is a plugin's row: never a failure, since plugins are
// optional, and a plugin not set up is fine (spec 12.5).
func pluginRow(add func(check, state, detail, fix string), s plugin.Survey) {
	name := "plugin:" + s.Scheme
	fix := "open Settings, Plugins, or run `kalka plugin settings " + s.Scheme + " --check`"
	if s.Hint != nil && s.Hint.Command != "" {
		fix += "; or run " + s.Hint.Command
	}
	switch {
	case s.Problem != "":
		add(name, Warn, s.Problem, fix)
	case !s.Settings:
		add(name, OK, "resolves references; nothing to set up", "")
	case s.Status == nil:
		add(name, Warn, "the plugin gave no state", fix)
	case s.Status.State == plugin.StateReady:
		add(name, OK, "ready: "+s.Status.Message, "")
	case s.Status.State == plugin.StateNeedsSetup:
		add(name, OK, "not set up: "+s.Status.Message, "")
	default:
		add(name, Warn, s.Status.Message, fix)
	}
}

func clock(add func(check, state, detail, fix string), now, server time.Time) {
	if server.IsZero() {
		add("clock", Skip, "not checked: the rendezvous sent no time", "")
		return
	}
	d := now.Sub(server)
	if d < 0 {
		d = -d
	}
	d = d.Round(time.Second)
	fix := "set the clock to synchronise automatically"
	switch {
	case d > ClockFail:
		add("clock", Fail, fmt.Sprintf("the clock is %s off the rendezvous server's", d), fix)
	case d > ClockWarn:
		add("clock", Warn, fmt.Sprintf("the clock is %s off the rendezvous server's", d), fix)
	default:
		add("clock", OK, "the clock agrees with the rendezvous server", "")
	}
}
