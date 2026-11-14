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
	"github.com/golbinski/kalka/internal/transport"
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
		return nil, report(asJSON, stdout, stderr, "config", err.Error())
	}
	if _, err := config.Load(); errors.Is(err, config.ErrMissing) {
		return nil, report(asJSON, stdout, stderr, "not-set-up", notSetUp)
	} else if err != nil {
		return nil, report(asJSON, stdout, stderr, "config", err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	if c := answering(ctx, dir); c != nil {
		return c, exitOK
	}
	if noStart || os.Getenv(NoStartEnv) == "1" {
		return nil, report(asJSON, stdout, stderr, "not-running",
			fmt.Sprintf("%s is not running, and --no-start (or %s=1) says not to start it", name.Product, NoStartEnv))
	}
	c, err := ensureDaemon(ctx, dir)
	if err != nil {
		return nil, report(asJSON, stdout, stderr, "not-running", err.Error())
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
		return nil, report(asJSON, stdout, stderr, "config", err.Error())
	}
	cfg, err := config.Load()
	if errors.Is(err, config.ErrMissing) {
		return nil, report(asJSON, stdout, stderr, "not-set-up", notSetUp)
	} else if err != nil {
		return nil, report(asJSON, stdout, stderr, "config", err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c := answering(ctx, dir); c != nil {
		return &views{c: c}, exitOK
	}
	n, err := core.Open(core.Options{Config: cfg, ConfigDir: dir, Log: io.Discard})
	if err != nil {
		return nil, report(asJSON, stdout, stderr, "local", err.Error())
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

func (v *views) peers(ctx context.Context) ([]core.PeerView, error) {
	if v.c != nil {
		return v.c.Peers(ctx)
	}
	return v.n.PeerList(), nil
}

func (v *views) team(ctx context.Context, ref string) (core.TeamView, error) {
	if v.c != nil {
		return v.c.Team(ctx, ref)
	}
	return v.n.Team(ref)
}

func (v *views) domains(ctx context.Context) ([]core.DomainRow, error) {
	if v.c != nil {
		return v.c.Domains(ctx)
	}
	return v.n.Domains(), nil
}

// notRunningNote ends a text view read from files.
func notRunningNote(w io.Writer) {
	fmt.Fprintf(w, "(%s is not running: read from the local files; presence unknown)\n", name.Product)
}

// statusView is `kalka status --json`.
type statusView struct {
	Daemon bool `json:"daemon"`
	PID    int  `json:"pid,omitempty"`
	core.Status
	// Online is null when no daemon runs: unknown, not offline.
	Online  *bool `json:"online"`
	Peers   int   `json:"peers"`
	Reached *int  `json:"peers_online"`
	Pending *int  `json:"pending_prompts"`
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
		return report(asJSON, stdout, stderr, "daemon", err.Error())
	}
	peers, err := v.peers(ctx)
	if err != nil {
		return report(asJSON, stdout, stderr, "daemon", err.Error())
	}
	out := statusView{Daemon: v.live(), Status: st, Peers: len(peers)}
	if v.live() {
		online, reached, pending := st.Online, 0, 0
		for _, p := range peers {
			if p.Presence == transport.Online {
				reached++
			}
		}
		if ps, err := v.c.Prompts(ctx); err == nil {
			pending = len(ps)
		}
		out.Online, out.Reached, out.Pending = &online, &reached, &pending
		if dir, err := config.Dir(); err == nil {
			if e, err := readEndpoint(dir); err == nil {
				out.PID = e.PID
			}
		}
	}
	if asJSON {
		writeJSON(stdout, out)
		return exitOK
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	row := func(k, val string) { fmt.Fprintf(tw, "%s\t%s\n", k, val) }
	if v.live() {
		state := "online"
		if !st.Online {
			state = "offline"
			if st.Error != "" {
				state += ": " + st.Error
			}
		}
		row("kalka", fmt.Sprintf("running (pid %d), %s", out.PID, state))
	} else {
		row("kalka", "not running")
	}
	row("you", st.Email+"  "+st.Fingerprint)
	row("folder", st.Folder)
	row("rendezvous", st.Rendezvous)
	if v.live() {
		row("peers", fmt.Sprintf("%d online of %d", *out.Reached, out.Peers))
		row("waiting", fmt.Sprintf("%d prompt(s) to answer in the UI", *out.Pending))
	} else {
		row("peers", fmt.Sprintf("%d known", out.Peers))
	}
	tw.Flush()
	if st.Notice != "" {
		fmt.Fprintln(stdout, "NOTICE: "+strings.TrimSpace(st.Notice))
	}
	return exitOK
}
