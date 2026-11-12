package main

// Network commands: the daemon itself, setup, and the commands that talk
// to the running daemon through the local API (spec 12, 13).

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/peerjs"
)

// cmdDaemon runs the daemon in the foreground. Prompts for incoming syncs
// are asked in this terminal (and through the API).
func cmdDaemon(stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, err := config.Load()
	if errors.Is(err, config.ErrMissing) {
		fmt.Fprintf(stderr, "%s is not set up yet. Run:\n  %s setup --email you@corp.com --rendezvous <wss://server/ or peerjs-cloud> --folder <path>\n", name.Product, name.Binary)
		return exitUsage
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	dir, _ := config.Dir()
	d, err := daemon.New(daemon.Options{Config: cfg, ConfigDir: dir, Log: stderr})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitProblems
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go terminalPrompts(ctx, d.Prompts, stdin, stderr)
	go func() {
		select {
		case <-d.Ready():
		case <-ctx.Done():
			return
		}
		st := d.State()
		fmt.Fprintf(stdout, "%s running as %s, folder %s\nlocal API http://%s\n", name.Product, st.Email, st.Folder, d.APIAddr())
		if st.Notice != "" {
			fmt.Fprintln(stdout, "NOTICE: "+st.Notice)
		}
	}()
	if err := d.Run(ctx); err != nil {
		fmt.Fprintln(stderr, err)
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
