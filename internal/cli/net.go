package cli

// Network commands: the daemon itself, setup, and the commands that talk
// to the running daemon through the local API (spec 12, 13).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/golbinski/kalka/internal/core"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/golbinski/kalka/internal/autostart"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/identity"
	"github.com/golbinski/kalka/internal/lockfile"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/notify"
	"github.com/golbinski/kalka/internal/peerjs"
	"github.com/golbinski/kalka/internal/plugin"
)

// cmdDaemon is `kalka daemon`: run the daemon until stopped (spec 4.1,
// 12). It takes the single-daemon lock first, before the setup form; when
// another daemon holds it, it says so and exits 0. Without a config it
// first serves the setup form. With no UI page open, notices show as OS
// notifications (spec 22.4).
func cmdDaemon(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	background := fs.Bool("background", false, "run as at logon: output to <config>/kalka.log")
	noNotes := fs.Bool("no-notifications", false, "show no OS notifications; notices show only in the UI")
	noUI := fs.Bool("no-ui", false, "serve the local API for the CLI but not the web UI; implies --no-notifications")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "daemon: unexpected argument %q\n", fs.Arg(0))
		return exitUsage
	}
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return ServeDaemon(ctx, DaemonOptions{Dir: dir, Background: *background, NoNotifications: *noNotes, NoUI: *noUI,
		Stdout: stdout, Stderr: stderr})
}

// DaemonOptions are how `kalka daemon` and the desktop shell run the
// daemon in their own process.
type DaemonOptions struct {
	Dir                               string // config directory
	Background, NoNotifications, NoUI bool
	Stdout, Stderr                    io.Writer
	// Lock is the single-daemon lock, already taken by the caller; nil
	// takes it here.
	Lock *lockfile.Lock
	// OpenUI shows the UI at a URL (the setup form, a notification's
	// click); nil opens the browser. The shell shows its window.
	OpenUI func(url string)
	// Shell: the desktop shell presents notices itself, so the daemon
	// shows no toasts of its own.
	Shell bool
	// Started is called once the daemon exists, before it runs.
	Started func(*daemon.Daemon)
	// Autostart is the start-at-login back end; nil is the system's.
	Autostart daemon.Autostart
}

// ServeDaemon runs the daemon until ctx ends: the single-daemon lock, the
// setup form when there is no config, then the daemon itself. It returns
// an exit code; another daemon holding the lock is exit 0.
func ServeDaemon(ctx context.Context, o DaemonOptions) int {
	dir, stdout, stderr := o.Dir, o.Stdout, o.Stderr
	background, noNotes, noUI := &o.Background, &o.NoNotifications, &o.NoUI
	if o.Shell {
		*noNotes = true
	}
	if o.Autostart == nil {
		o.Autostart = daemon.SystemAutostart{}
	}
	if *background {
		logf, err := openLog(dir)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return exitProblems
		}
		defer logf.Close()
		stdout, stderr = logf, logf
		fmt.Fprintf(logf, "\n%s %s %s starting in the background\n", time.Now().Format(time.RFC3339), name.Binary, version)
	}
	lock := o.Lock
	if lock == nil {
		var err error
		lock, err = daemon.AcquireLock(dir)
		if errors.Is(err, lockfile.ErrHeld) {
			fmt.Fprintf(stdout, "%s %v\n", name.Product, err)
			return exitOK
		} else if err != nil {
			fmt.Fprintln(stderr, err)
			return exitProblems
		}
	}
	defer lock.Release() // after a failure before Run; Run releases it too
	tok := make([]byte, 32)
	_, _ = rand.Read(tok)
	token := hex.EncodeToString(tok)
	// openUI opens the browser on request: the setup form.
	openUI := func(url string) {
		if err := openBrowser(url); err != nil {
			fmt.Fprintf(stderr, "could not open a browser (%v); open %s\n", err, url)
		}
	}
	if o.OpenUI != nil {
		openUI = o.OpenUI
	}

	cfg, err := config.Load()
	if errors.Is(err, config.ErrMissing) {
		// Setup needs the browser even in the background: nothing works
		// until it is done.
		if cfg, err = runSetup(ctx, token, openUI, stdout, stderr); err != nil || cfg == nil {
			if err != nil {
				fmt.Fprintln(stderr, err)
				return exitProblems
			}
			return exitOK
		}
		if cfg, err = config.Load(); err != nil {
			fmt.Fprintln(stderr, err)
			return exitProblems
		}
	} else if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	ln, err := listen(cfg.UIPort)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	opts := daemon.Options{Config: cfg, ConfigDir: dir, Log: stderr, Listener: ln, Token: token, OpenUI: openUI, Lock: lock,
		NoUI: *noUI, OpenExternal: openBrowser, Autostart: o.Autostart,
		Plugins: &plugin.Host{Dir: filepath.Join(dir, "plugins"), ExeDir: exeDir(), Log: stderr}}
	var d *daemon.Daemon
	if (*noNotes || *noUI) && o.OpenUI == nil {
		opts.OpenUI = nil
	}
	if !*noNotes && !*noUI {
		// Notices as notification-centre toasts where the system has
		// them for kalka; elsewhere only the page shows them (spec 22.4).
		opts.Notify = func(n core.Notice) {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				_ = notify.Toast(ctx, n.Title, n.Message, d.URL())
			}()
		}
	}
	d, err = daemon.New(opts)
	if err != nil {
		ln.Close()
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	if o.Started != nil {
		o.Started(d)
	}
	go func() {
		select {
		case <-d.Ready():
		case <-ctx.Done():
			return
		}
		st := d.State()
		fmt.Fprintf(stdout, "%s running as %s, folder %s (pid %d)\n", name.Product, st.Email, st.Folder, os.Getpid())
		if *noUI {
			fmt.Fprintf(stdout, "API on %s; the web UI is off (--no-ui)\n", d.APIAddr())
		} else {
			fmt.Fprintf(stdout, "UI: %s\n", d.URL())
		}
		if st.Notice != "" {
			fmt.Fprintln(stdout, "NOTICE: "+st.Notice)
		}
	}()
	if err := d.Run(ctx); err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	fmt.Fprintf(stdout, "%s stopped\n", name.Product)
	return exitOK
}

// cmdStart is bare `kalka`: make sure a daemon runs, then open the UI:
// the desktop shell's window when one is attached, else the browser
// (spec 12, 22).
func cmdStart(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "%s: unexpected argument %q (did you mean `%[1]s daemon %[2]s`?)\n", name.Binary, args[0])
		usage(stderr)
		return exitUsage
	}
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	if _, err := config.Load(); errors.Is(err, config.ErrMissing) {
		// The new daemon opens the setup form itself.
		if err := spawnDaemon(); err != nil {
			fmt.Fprintln(stderr, err)
			return exitProblems
		}
		fmt.Fprintf(stdout, "%s is not set up yet: finish setup in the browser that opens.\n", name.Product)
		return exitOK
	}
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	c, err := ensureDaemon(ctx, dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	// The desktop window when a shell is attached, else the browser.
	if err := c.ShowUI(ctx, ""); err == nil {
		return exitOK
	} else if !errors.Is(err, daemon.ErrNoShell) {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	return cmdOpen(nil, stdout, stderr)
}

// startTimeout bounds how long the CLI waits for a daemon it started.
const startTimeout = 20 * time.Second

// ensureDaemon returns a client of the running daemon, starting one in
// the background when none answers. A second start racing this one finds
// the lock held and exits, so there is still one daemon.
func ensureDaemon(ctx context.Context, dir string) (*daemon.Client, error) {
	if c := answering(ctx, dir); c != nil {
		return c, nil
	}
	if err := spawnDaemon(); err != nil {
		return nil, err
	}
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		if c := answering(ctx, dir); c != nil {
			return c, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("started %s in the background, but it did not answer; see %s", name.Product, filepath.Join(dir, name.Product+".log"))
		case <-t.C:
		}
	}
}

// answering returns a client if a daemon answers its status call.
func answering(ctx context.Context, dir string) *daemon.Client {
	c, err := daemon.Connect(dir)
	if err != nil {
		return nil
	}
	sctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := c.Status(sctx); err != nil {
		return nil
	}
	return c
}

// spawnDaemon starts `kalka daemon --background` detached.
func spawnDaemon() error {
	exe, err := autostart.Executable()
	if err != nil {
		return err
	}
	return autostart.Spawn(exe)
}

// cmdStop is `kalka stop`: ask the daemon to stop and wait until it has,
// running rounds included. With none running there is nothing to do.
func cmdStop(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "machine-readable output")
	wait := fs.Duration("wait", time.Minute, "how long to wait for running rounds to finish")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	report := func(stopped bool, pid int) int {
		if *asJSON {
			writeJSON(stdout, map[string]any{"was_running": stopped, "pid": pid})
		} else if stopped {
			fmt.Fprintf(stdout, "%s stopped (pid %d)\n", name.Product, pid)
		} else {
			fmt.Fprintf(stdout, "%s is not running\n", name.Product)
		}
		return exitOK
	}
	ctx, cancel := context.WithTimeout(context.Background(), *wait)
	defer cancel()
	c := answering(ctx, dir)
	if c == nil {
		if pid := lockHolder(dir); pid != 0 {
			fmt.Fprintf(stderr, "%s holds the lock (pid %d) but its API does not answer; it may still be starting or waiting for setup\n", name.Product, pid)
			return exitProblems
		}
		return report(false, 0)
	}
	pid, err := c.Shutdown(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for lockHolder(dir) != 0 {
		select {
		case <-ctx.Done():
			fmt.Fprintf(stderr, "%s (pid %d) is still finishing after %s\n", name.Product, pid, *wait)
			return exitProblems
		case <-t.C:
		}
	}
	return report(true, pid)
}

// lockHolder returns the PID holding the single-daemon lock, -1 when it is
// held by an unknown process, or 0 when it is free.
func lockHolder(dir string) int {
	l, err := daemon.AcquireLock(dir)
	if err == nil {
		l.Release()
		return 0
	}
	var h *lockfile.Held
	if errors.As(err, &h) {
		if h.PID > 0 {
			return h.PID
		}
		return -1
	}
	return 0
}

// openLog opens <config>/kalka.log for appending, keeping one older file
// when it passes 5 MB.
func openLog(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, name.Product+".log")
	if st, err := os.Stat(p); err == nil && st.Size() > 5<<20 {
		_ = os.Rename(p, p+".1")
	}
	return os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

func listen(port int) (net.Listener, error) {
	if port == 0 {
		port = config.DefaultUIPort
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("cannot listen on %s: %w (is %s already running? `%s open` opens its UI)", addr, err, name.Product, name.Binary)
	}
	return ln, nil
}

// runSetup serves the setup form until it is saved; it returns nil when ctx
// ends first.
func runSetup(ctx context.Context, token string, open func(string), stdout, stderr io.Writer) (*config.Config, error) {
	ln, err := listen(config.DefaultUIPort)
	if err != nil {
		return nil, err
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	done := make(chan *config.Config, 1)
	srv := &http.Server{Handler: daemon.SetupHandler(token, func() string { return port }, done), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	url := "http://127.0.0.1:" + port + "/?token=" + token
	fmt.Fprintf(stdout, "%s is not set up yet. Finish setup in the browser: %s\n(or run `%s setup --email ... --rendezvous ... --folder ...`)\n", name.Product, url, name.Binary)
	open(url)
	var cfg *config.Config
	select {
	case cfg = <-done:
		time.Sleep(200 * time.Millisecond) // let the response reach the browser
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	return cfg, nil
}

// openBrowser opens url in the default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// readEndpoint reads daemon.json.
func readEndpoint(dir string) (daemon.Endpoint, error) {
	var e daemon.Endpoint
	raw, err := os.ReadFile(filepath.Join(dir, daemon.DaemonFile))
	if err != nil {
		return e, daemon.ErrNotRunning
	}
	return e, json.Unmarshal(raw, &e)
}

// cmdOpen opens the daemon's UI, starting the daemon if needed.
func cmdOpen(args []string, stdout, stderr io.Writer) int {
	print := len(args) > 0 && args[0] == "--print"
	if c, code := daemonFor(false, stdout, stderr); c == nil {
		return code
	}
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	e, err := readEndpoint(dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	url := "http://" + e.Addr + "/?token=" + e.Token
	if print {
		fmt.Fprintln(stdout, url)
		return exitOK
	}
	if err := openBrowser(url); err != nil {
		fmt.Fprintf(stderr, "could not open a browser (%v); open %s\n", err, url)
		return exitProblems
	}
	return exitOK
}

func cmdSetup(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	email := fs.String("email", "", "who you are (required)")
	rdv := fs.String("rendezvous", "", "rendezvous server URL, or peerjs-cloud (required)")
	folder := fs.String("folder", "", "where blueprints live (required)")
	port := fs.Int("ui-port", 0, "optional localhost port of the UI and API (default 7717)")
	if _, err := parseInterspersed(fs, args); err != nil {
		return exitUsage
	}
	if *folder != "" {
		if abs, err := filepath.Abs(*folder); err == nil {
			*folder = abs
		}
	}
	cfg := &config.Config{Email: strings.TrimSpace(*email), Rendezvous: strings.TrimSpace(*rdv), Folder: *folder, UIPort: *port}
	if err := daemon.Validate(cfg); err != nil {
		fmt.Fprintln(stderr, "setup:", err)
		return exitUsage
	}
	if _, err := peerjs.ParseRendezvous(cfg.Rendezvous); err != nil {
		fmt.Fprintln(stderr, "setup:", err)
		return exitUsage
	}
	if err := os.MkdirAll(cfg.Folder, 0o755); err != nil {
		fmt.Fprintln(stderr, "setup:", err)
		return exitProblems
	}
	if err := config.Save(cfg); err != nil {
		fmt.Fprintln(stderr, "setup:", err)
		return exitProblems
	}
	dir, _ := config.Dir()
	// The identity key too, so views work before the daemon first runs.
	if _, err := identity.LoadOrCreateKey(dir); err != nil {
		fmt.Fprintln(stderr, "setup:", err)
		return exitProblems
	}
	fmt.Fprintf(stdout, "saved %s\nstart %s with `%s`\n", filepath.Join(dir, config.FileName), name.Product, name.Binary)
	return exitOK
}

func exeDir() string {
	exe, err := autostart.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}
