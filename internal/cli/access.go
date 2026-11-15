package cli

// How commands reach kalka (spec 12): daemon commands go through the
// running daemon and start one when none answers; views use the daemon
// when it runs and read the local files otherwise; file commands never
// need one.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/name"
)

// NoStartEnv set to 1 makes daemon commands fail instead of starting a
// daemon, like --no-start.
const NoStartEnv = "KALKA_NO_START"

// noStart is set by --no-start for this run (see run).
var noStart bool

// cliError is printed as text, or as {"error", "kind"} with --json.
type cliError struct {
	Error string `json:"error"`
	Kind  string `json:"kind"`
}

func report(asJSON bool, stdout, stderr io.Writer, kind, msg string) int {
	if asJSON {
		writeJSON(stdout, cliError{Error: msg, Kind: kind})
	} else {
		fmt.Fprintln(stderr, msg)
	}
	return exitProblems
}

// notSetUp is the message for a command that needs a configuration.
var notSetUp = fmt.Sprintf("%s is not set up: run `%s` and finish setup in the browser, or `%[2]s setup --email ... --rendezvous ... --folder ...`", name.Product, name.Binary)

// daemonFor returns a client of the running daemon for a daemon command,
// starting one in the background when none answers. On failure it has
// reported the problem and returns the exit code.
func daemonFor(asJSON bool, stdout, stderr io.Writer) (*daemon.Client, int) {
	dir, err := config.Dir()
	if err != nil {
		return nil, failWith(asJSON, stdout, stderr, err)
	}
	if _, err := config.Load(); errors.Is(err, config.ErrMissing) {
		return nil, report(asJSON, stdout, stderr, core.KindNotSetUp, notSetUp)
	} else if err != nil {
		return nil, failWith(asJSON, stdout, stderr, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	if c := answering(ctx, dir); c != nil {
		return c, exitOK
	}
	if noStart || os.Getenv(NoStartEnv) == "1" {
		return nil, report(asJSON, stdout, stderr, core.KindNotRunning,
			fmt.Sprintf("%s is not running, and --no-start (or %s=1) says not to start it", name.Product, NoStartEnv))
	}
	c, err := ensureDaemon(ctx, dir)
	if err != nil {
		return nil, report(asJSON, stdout, stderr, core.KindNotRunning, err.Error())
	}
	fmt.Fprintf(stderr, "started %s in the background; `%s stop` stops it\n", name.Product, name.Binary)
	return c, exitOK
}

// views answers the view commands from the daemon when it runs, else from
// a read-only node over the local files, which starts nothing.
type views struct {
	c *daemon.Client
	n *core.Node
}

func openViews(asJSON bool, stdout, stderr io.Writer) (*views, int) {
	dir, err := config.Dir()
	if err != nil {
		return nil, failWith(asJSON, stdout, stderr, err)
	}
	cfg, err := config.Load()
	if errors.Is(err, config.ErrMissing) {
		return nil, report(asJSON, stdout, stderr, core.KindNotSetUp, notSetUp)
	} else if err != nil {
		return nil, failWith(asJSON, stdout, stderr, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c := answering(ctx, dir); c != nil {
		return &views{c: c}, exitOK
	}
	n, err := core.Open(core.Options{Config: cfg, ConfigDir: dir, Log: io.Discard})
	if err != nil {
		return nil, failWith(asJSON, stdout, stderr, err)
	}
	if st, err := daemon.LoadState(dir); err == nil {
		n.SetSeen(func() map[string]string { return st.Seen })
	}
	return &views{n: n}, exitOK
}

// live reports whether the daemon answers (presence and online state are
// known only then).
func (v *views) live() bool { return v.c != nil }

func (v *views) status(ctx context.Context) (core.Status, error) {
	if v.c != nil {
		return v.c.Status(ctx)
	}
	return v.n.State(), nil
}

func (v *views) team(ctx context.Context, ref string) (core.TeamView, error) {
	if v.c != nil {
		return v.c.Team(ctx, ref)
	}
	return v.n.Team(ref)
}

func (v *views) domains(ctx context.Context, team string) ([]core.DomainRow, error) {
	if v.c != nil {
		return v.c.Domains(ctx, team)
	}
	sc, err := v.n.ResolveScope(team)
	if err != nil {
		return nil, err
	}
	return v.n.DomainsIn(sc), nil
}

func (v *views) seeds(ctx context.Context, team string) ([]core.SeedView, error) {
	if v.c != nil {
		return v.c.Seeds(ctx, team)
	}
	sc, err := v.n.ResolveScope(team)
	if err != nil {
		return nil, err
	}
	return v.n.SeedsIn(sc), nil
}

func (v *views) teams(ctx context.Context) ([]core.TeamView, error) {
	if v.c != nil {
		return v.c.Teams(ctx)
	}
	return v.n.Teams(), nil
}

// waiting lists the waiting items. Without a daemon the seen marks come
// from state.yaml, which only the daemon writes.
func (v *views) waiting(ctx context.Context, team string) ([]core.WaitingItem, error) {
	if v.c != nil {
		return v.c.Waiting(ctx, team)
	}
	return v.n.Waiting(team)
}

func (v *views) transfers(ctx context.Context, team string) ([]core.TransferView, error) {
	if v.c != nil {
		return v.c.Transfers(ctx, team)
	}
	sc, err := v.n.ResolveScope(team)
	if err != nil {
		return nil, err
	}
	return v.n.TransfersIn(sc), nil
}

// notRunningNote ends a text view read from files.
func notRunningNote(w io.Writer) {
	fmt.Fprintf(w, "(%s is not running: read from the local files; presence unknown)\n", name.Product)
}

// statusView is `kalka status --json`. Fields only a running daemon
// knows are null without one: unknown, not zero.
type statusView struct {
	Daemon bool `json:"daemon"`
	PID    int  `json:"pid,omitempty"`
	core.Status
	Online      *bool      `json:"online"`
	Paused      bool       `json:"paused"`
	PeersKnown  int        `json:"peers_known"`
	PeersOnline *int       `json:"peers_online"`
	LastSync    *time.Time `json:"last_sync"`
	Joins       int        `json:"joins"`
	Pushes      *int       `json:"pushes"`
	// Shell is "desktop" while the desktop window is attached.
	Shell string `json:"shell,omitempty"`
}

// cmdStatus is `kalka status`, a view (spec 12).
func cmdStatus(args []string, stdout, stderr io.Writer) int {
	asJSON := len(args) == 1 && args[0] == "--json"
	if len(args) > 0 && !asJSON {
		fmt.Fprintf(stderr, "usage: %s status [--json]\n", name.Binary)
		return exitUsage
	}
	v, code := openViews(asJSON, stdout, stderr)
	if v == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := v.status(ctx)
	if err != nil {
		return failWith(asJSON, stdout, stderr, err)
	}
	out := statusView{Daemon: v.live(), Status: st, Paused: st.Paused}
	if v.live() {
		sum, err := v.c.Summary(ctx)
		if err != nil {
			return failWith(asJSON, stdout, stderr, err)
		}
		out.Online, out.PeersOnline, out.Pushes = &sum.Online, &sum.PeersOnline, &sum.Pushes
		out.PeersKnown, out.Joins, out.Paused, out.Shell = sum.PeersKnown, sum.Joins, sum.Paused, sum.Shell
		if !sum.LastSync.IsZero() {
			out.LastSync = &sum.LastSync
		}
		if dir, err := config.Dir(); err == nil {
			if e, err := readEndpoint(dir); err == nil {
				out.PID = e.PID
			}
		}
	} else {
		sum := v.n.Summary()
		out.PeersKnown, out.Joins = sum.PeersKnown, sum.Joins
		if dir, err := config.Dir(); err == nil {
			if ds, err := daemon.LoadState(dir); err == nil {
				out.Paused = ds.Paused
			}
		}
	}
	out.Status.Paused = out.Paused
	if asJSON {
		writeJSON(stdout, out)
		return exitOK
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	row := func(k, val string) { fmt.Fprintf(tw, "%s\t%s\n", k, val) }
	switch {
	case v.live() && out.Paused:
		row("kalka", fmt.Sprintf("running (pid %d), paused: not syncing until `%s resume`", out.PID, name.Binary))
	case v.live():
		state := "online"
		if !st.Online {
			state = "offline"
			if st.Error != "" {
				state += ": " + st.Error
			}
		}
		row("kalka", fmt.Sprintf("running (pid %d), %s", out.PID, state))
	case out.Paused:
		row("kalka", "not running (paused)")
	default:
		row("kalka", "not running")
	}
	row("you", st.Email+"  "+st.Fingerprint)
	row("folder", st.Folder)
	row("rendezvous", st.Rendezvous)
	if v.live() {
		row("members", fmt.Sprintf("%d reachable of %d", *out.PeersOnline, out.PeersKnown))
		last := "none since start"
		if out.LastSync != nil {
			last = out.LastSync.Local().Format("2006-01-02 15:04")
		}
		row("last sync", last)
		row("waiting", fmt.Sprintf("%d join(s) in progress, %d team push(es) queued", out.Joins, *out.Pushes))
		if out.Shell != "" {
			row("window", "the desktop app is attached")
		}
	} else {
		row("members", fmt.Sprintf("%d known", out.PeersKnown))
		if out.Joins > 0 {
			row("waiting", fmt.Sprintf("%d join(s) in progress", out.Joins))
		}
	}
	tw.Flush()
	if st.Notice != "" {
		fmt.Fprintln(stdout, "NOTICE: "+strings.TrimSpace(st.Notice))
	}
	return exitOK
}

// cmdPause is `kalka pause` and `kalka resume` (spec 22.3).
func cmdPause(paused bool, args []string, stdout, stderr io.Writer) int {
	asJSON := len(args) == 1 && args[0] == "--json"
	if len(args) > 0 && !asJSON {
		fmt.Fprintf(stderr, "usage: %s pause|resume [--json]\n", name.Binary)
		return exitUsage
	}
	c, code := daemonFor(asJSON, stdout, stderr)
	if c == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := c.SetPaused(ctx, paused)
	if err != nil {
		return failWith(asJSON, stdout, stderr, err)
	}
	if asJSON {
		writeJSON(stdout, s)
		return exitOK
	}
	if paused {
		fmt.Fprintf(stdout, "%s is paused: offline until `%s resume`, also after a restart. Local work goes on and is pushed after the resume.\n", name.Product, name.Binary)
	} else {
		fmt.Fprintf(stdout, "%s is syncing again\n", name.Product)
	}
	return exitOK
}
