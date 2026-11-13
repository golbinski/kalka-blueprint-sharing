package main

// Network commands: the daemon itself, setup, and the commands that talk
// to the running daemon through the local API (spec 12, 13).

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/notify"
	"github.com/golbinski/kalka/internal/peerjs"
	"github.com/golbinski/kalka/internal/plugin"
)

// cmdDaemon runs the daemon and opens the UI. Without a config it first
// serves the setup form on the same port. A prompt for an incoming sync
// shows in the UI; with no UI open, a native popup offers to open it. When
// started from a terminal, prompts are asked there too.
func cmdDaemon(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(name.Binary, flag.ContinueOnError)
	fs.SetOutput(stderr)
	noOpen := fs.Bool("no-open", false, "do not open the UI in a browser, and show no popups")
	background := fs.Bool("background", false, "run at logon: no browser at start, prompts as notifications, output to <config>/kalka.log")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	if *background {
		logf, err := openLog(dir)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return exitProblems
		}
		defer logf.Close()
		stdout, stderr = logf, logf
		stdin = nil
		fmt.Fprintf(logf, "\n%s %s %s starting in the background\n", time.Now().Format(time.RFC3339), name.Binary, version)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tok := make([]byte, 32)
	_, _ = rand.Read(tok)
	token := hex.EncodeToString(tok)
	// openUI opens the browser on request (a popup's "Open kalka"); the
	// first page at start is skipped in the background.
	openUI := func(url string) {
		if *noOpen {
			return
		}
		if err := openBrowser(url); err != nil {
			fmt.Fprintf(stderr, "could not open a browser (%v); open %s\n", err, url)
		}
	}
	open := openUI
	if *background {
		open = func(string) {}
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
	opts := daemon.Options{Config: cfg, ConfigDir: dir, Log: stderr, Listener: ln, Token: token, OpenUI: openUI,
		Plugins: &plugin.Host{Dir: filepath.Join(dir, "plugins"), Log: stderr}}
	var d *daemon.Daemon
	if !*noOpen {
		// A notification-centre toast where the system has one for kalka
		// (Windows, after `kalka autostart on`), else a popup.
		opts.Popup = func(ctx context.Context, title, msg string) (bool, error) {
			err := notify.Toast(ctx, title, msg, d.URL())
			if errors.Is(err, notify.ErrUnavailable) {
				return notify.Ask(ctx, title, msg)
			}
			return false, err
		}
	}
	d, err = daemon.New(opts)
	if err != nil {
		ln.Close()
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	if isTerminal(stdin) {
		go terminalPrompts(ctx, d.Prompts, stdin, stderr)
	}
	go func() {
		select {
		case <-d.Ready():
		case <-ctx.Done():
			return
		}
		st := d.State()
		fmt.Fprintf(stdout, "%s running as %s, folder %s\nUI: %s\n", name.Product, st.Email, st.Folder, d.URL())
		if st.Notice != "" {
			fmt.Fprintln(stdout, "NOTICE: "+st.Notice)
		}
		open(d.URL())
	}()
	if err := d.Run(ctx); err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	return exitOK
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

// cmdOpen opens the running daemon's UI.
func cmdOpen(args []string, stdout, stderr io.Writer) int {
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	raw, err := os.ReadFile(filepath.Join(dir, daemon.DaemonFile))
	if err != nil {
		fmt.Fprintln(stderr, daemon.ErrNotRunning)
		return exitProblems
	}
	var e daemon.Endpoint
	if err := json.Unmarshal(raw, &e); err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	url := "http://" + e.Addr + "/?token=" + e.Token
	if len(args) > 0 && args[0] == "--print" {
		fmt.Fprintln(stdout, url)
		return exitOK
	}
	if err := openBrowser(url); err != nil {
		fmt.Fprintf(stderr, "could not open a browser (%v); open %s\n", err, url)
		return exitProblems
	}
	return exitOK
}

// terminalPrompts shows prompts for incoming syncs and reads y/n answers;
// an answer applies to the oldest open prompt.
func terminalPrompts(ctx context.Context, hub *daemon.Hub, stdin io.Reader, out io.Writer) {
	notices, cancel := hub.Subscribe("")
	defer cancel()
	lines := readLines(stdin)
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-notices:
			if !n.Closed && n.Prompt.Scope == "" {
				fmt.Fprintf(out, "\n>>> %s\n    answer y or n: ", n.Prompt.Message)
			}
		case line, ok := <-lines:
			if !ok {
				lines = nil
				continue
			}
			open := hub.List()
			if len(open) == 0 {
				continue
			}
			p := open[0]
			_ = hub.Answer(p.ID, yes(line))
		}
	}
}

// isTerminal reports whether r is an interactive terminal. At logon kalka
// runs with no terminal, and there is no one to read answers from.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func readLines(r io.Reader) chan string {
	ch := make(chan string)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			ch <- sc.Text()
		}
	}()
	return ch
}

func yes(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "y" || s == "yes"
}

func cmdSetup(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	email := fs.String("email", "", "who you are (required)")
	rdv := fs.String("rendezvous", "", "rendezvous server URL, or peerjs-cloud (required)")
	folder := fs.String("folder", "", "where blueprints live (required)")
	team := fs.String("team", "", "optional team string mixed into peer ids")
	port := fs.Int("ui-port", 0, "optional localhost port of the UI and API (default 7717)")
	if _, err := parseInterspersed(fs, args); err != nil {
		return exitUsage
	}
	if *folder != "" {
		if abs, err := filepath.Abs(*folder); err == nil {
			*folder = abs
		}
	}
	cfg := &config.Config{Email: strings.TrimSpace(*email), Rendezvous: strings.TrimSpace(*rdv), Folder: *folder, Team: *team, UIPort: *port}
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
	fmt.Fprintf(stdout, "saved %s\nstart %s with `%s`\n", filepath.Join(dir, config.FileName), name.Product, name.Binary)
	return exitOK
}

func client(stderr io.Writer) *daemon.Client {
	dir, err := config.Dir()
	if err == nil {
		var c *daemon.Client
		if c, err = daemon.Connect(dir); err == nil {
			return c
		}
	}
	fmt.Fprintln(stderr, err)
	return nil
}

// cmdSync implements `kalka send` and `kalka handoff`.
func cmdSync(cmd string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print events and the result as JSON lines")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) != 2 {
		fmt.Fprintf(stderr, "usage: %s %s <file> <email> [--json]\n", name.Binary, cmd)
		return exitUsage
	}
	file, to := pos[0], pos[1]
	if abs, err := filepath.Abs(file); err == nil {
		file = abs
	}
	intent := daemon.IntentSync
	if cmd == "handoff" {
		intent = daemon.IntentHandoff
	}
	c := client(stderr)
	if c == nil {
		return exitProblems
	}
	ctx := context.Background()
	lines := readLines(stdin)
	var mu sync.Mutex
	closed := map[string]chan struct{}{}
	code := exitProblems
	err = c.Sync(ctx, daemon.SyncRequest{Path: file, To: to, Intent: intent}, func(ev daemon.Event) {
		if *asJSON {
			writeJSON(stdout, ev)
		}
		switch ev.Type {
		case "progress":
			if !*asJSON {
				fmt.Fprintln(stderr, "· "+ev.Message)
			}
		case "prompt":
			done := make(chan struct{})
			mu.Lock()
			closed[ev.Prompt.ID] = done
			mu.Unlock()
			if !*asJSON {
				fmt.Fprintf(stderr, "%s [y/N] ", ev.Prompt.Message)
			}
			go func(id string) {
				select {
				case line, ok := <-lines:
					_ = c.Answer(ctx, id, ok && yes(line))
				case <-done:
				}
			}(ev.Prompt.ID)
		case "prompt-closed":
			mu.Lock()
			if ch := closed[ev.Prompt.ID]; ch != nil {
				close(ch)
				delete(closed, ev.Prompt.ID)
			}
			mu.Unlock()
		case "result":
			code = exitOK
			if !*asJSON {
				r := ev.Result
				if intent == daemon.IntentHandoff {
					fmt.Fprintf(stdout, "handed off %q to %s: they are the owner now\n", r.Title, r.Peer)
				} else {
					fmt.Fprintf(stdout, "synced %q with %s: %s\n", r.Title, r.Peer, r.Status)
				}
				if r.Conflict != "" {
					fmt.Fprintf(stdout, "your previous copy was saved as %s\n", r.Conflict)
				}
				for _, n := range r.Notes {
					fmt.Fprintln(stdout, "note: "+n)
				}
			}
		case "error":
			if !*asJSON {
				fmt.Fprintln(stderr, "error: "+ev.Error)
			}
		}
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	return code
}

func cmdPeers(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("peers", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	c := client(stderr)
	if c == nil {
		return exitProblems
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if len(pos) == 2 && pos[0] == "forget" {
		if err := c.Forget(ctx, pos[1]); err != nil {
			fmt.Fprintln(stderr, err)
			return exitProblems
		}
		fmt.Fprintf(stdout, "removed the pinned key for %s; the next contact pins their key again and asks for the code\n", pos[1])
		return exitOK
	}
	if len(pos) == 2 && pos[0] == "add" {
		if !bp.ValidEmail(pos[1]) {
			fmt.Fprintf(stderr, "%q is not an email address\n", pos[1])
			return exitUsage
		}
		if _, err := c.AddPeer(ctx, pos[1]); err != nil {
			fmt.Fprintln(stderr, err)
			return exitProblems
		}
	} else if len(pos) != 0 {
		fmt.Fprintf(stderr, "usage: %[1]s peers [--json] | %[1]s peers add <email> | %[1]s peers forget <email>\n", name.Binary)
		return exitUsage
	}
	peers, err := c.Peers(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	if *asJSON {
		writeJSON(stdout, peers)
		return exitOK
	}
	if len(peers) == 0 {
		fmt.Fprintln(stdout, "no peers yet")
		return exitOK
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "EMAIL\tPRESENCE\tKEY\tCODE")
	for _, p := range peers {
		key := "not pinned"
		switch {
		case p.KeyChanged:
			key = "KEY CHANGED: syncs blocked"
		case p.Verified:
			key = "verified"
		case p.Pinned:
			key = "pinned, not verified"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.Email, p.Presence, key, p.Code)
	}
	tw.Flush()
	return exitOK
}

func cmdVerify(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	confirm := fs.Bool("confirm", false, "mark the peer verified after comparing the code with them")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) != 1 {
		fmt.Fprintf(stderr, "usage: %s verify <email> [--confirm] [--json]\n", name.Binary)
		return exitUsage
	}
	c := client(stderr)
	if c == nil {
		return exitProblems
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	v, err := c.Code(ctx, pos[0])
	if err == nil && *confirm {
		v, err = c.Confirm(ctx, pos[0])
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	if *asJSON {
		writeJSON(stdout, v)
		return exitOK
	}
	state := "not verified: compare it with them, then run with --confirm"
	switch {
	case v.KeyChanged:
		state = "KEY CHANGED: syncs are blocked; see `" + name.Binary + " peers`"
	case v.Verified:
		state = "verified"
	}
	fmt.Fprintf(stdout, "verification code for %s: %s (%s)\n", v.Email, v.Code, state)
	return exitOK
}
