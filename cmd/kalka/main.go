// Command kalka shares blueprints between teammates (see specification.md).
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/name"
)

// Set at release build time with -ldflags "-X main.version=...".
var version = "dev"

// Protocol numbers reported by `kalka version`.
const (
	wireProtocol   = 1 // data channel, spec 9.5
	pluginProtocol = 1 // plugin JSON, spec 10.3
)

// Exit codes.
const (
	exitOK       = 0
	exitProblems = 1 // check found errors, or a command failed
	exitUsage    = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, time.Now))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, now func() time.Time) int {
	if len(args) == 0 || (strings.HasPrefix(args[0], "-") && args[0] != "-h" && args[0] != "--help") {
		return cmdDaemon(args, stdin, stdout, stderr)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "check":
		return cmdCheck(rest, stdout, stderr)
	case "comment":
		return cmdComment(rest, stdin, stdout, stderr, now)
	case "version":
		return cmdVersion(rest, stdout, stderr)
	case "open":
		return cmdOpen(rest, stdout, stderr)
	case "setup":
		return cmdSetup(rest, stdout, stderr)
	case "send", "handoff":
		return cmdSync(cmd, rest, stdin, stdout, stderr)
	case "peers":
		return cmdPeers(rest, stdout, stderr)
	case "verify":
		return cmdVerify(rest, stdout, stderr)
	case "rendezvous":
		return cmdRendezvous(rest, stdout, stderr)
	case "autostart":
		return cmdAutostart(rest, stdout, stderr)
	case "help", "-h", "--help":
		usage(stdout)
		return exitOK
	}
	fmt.Fprintf(stderr, "%s: unknown command %q\n", name.Binary, cmd)
	usage(stderr)
	return exitUsage
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `usage:
  %[1]s [--no-open]           run the daemon and open the UI (prompts for incoming syncs are asked here too)
  %[1]s --background          run the daemon as at logon: no browser, notifications, output to kalka.log
  %[1]s autostart on|off|status [--json]   start kalka at logon
  %[1]s open [--print]        open the running daemon's UI
  %[1]s setup --email <e> --rendezvous <url|peerjs-cloud> --folder <dir> [--team <t>]
  %[1]s send <file> <email> [--json]
  %[1]s handoff <file> <email> [--json]
  %[1]s peers [--json] | peers add <email> | peers forget <email>
  %[1]s verify <email> [--confirm] [--json]
  %[1]s check <file>... [--json] [--verbose]
  %[1]s comment <file> [--anchor <slug>] [--kind deviation] [--re <id>] [-m <text>] [--json]
  %[1]s rendezvous [--listen :9000] [--tls-cert f --tls-key f] [--turn-ip ip] [--allow-origin p]... (server mode; -h lists all flags)
  %[1]s version [--json]
`, name.Binary)
}

// parseInterspersed parses flags that may come before or after positional
// arguments, as in `kalka comment plan.md --anchor plan`.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

type checkResult struct {
	File     string       `json:"file"`
	OK       bool         `json:"ok"`
	KalkaID  string       `json:"kalka_id,omitempty"`
	Owner    string       `json:"owner,omitempty"`
	Title    string       `json:"title,omitempty"`
	Rev      string       `json:"rev,omitempty"`
	Entries  int          `json:"entries"`
	Problems []bp.Problem `json:"problems"`
	Traces   []traceInfo  `json:"traces,omitempty"`
}

// traceInfo summarizes a trace block for agents: where it is, whether it
// is valid, and the divergences computed from its graph (spec 6.10).
type traceInfo struct {
	Index       int             `json:"index"`
	Line        int             `json:"line"`
	Section     string          `json:"section,omitempty"`
	Title       string          `json:"title,omitempty"`
	Valid       bool            `json:"valid"`
	Lanes       []string        `json:"lanes,omitempty"`
	Nodes       int             `json:"nodes"`
	Divergences []bp.Divergence `json:"divergences"`
	Errors      []bp.TraceError `json:"errors,omitempty"`
}

func cmdCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "machine-readable output")
	verbose := fs.Bool("verbose", false, "also show informational notes")
	files, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(files) == 0 {
		fmt.Fprintln(stderr, "check: name at least one file")
		return exitUsage
	}
	code := exitOK
	var results []checkResult
	for _, f := range files {
		r := checkFile(f)
		if !r.OK {
			code = exitProblems
		}
		results = append(results, r)
		if *asJSON {
			continue
		}
		for _, p := range r.Problems {
			if p.Severity == bp.Info && !*verbose {
				continue
			}
			loc := f
			if p.Line > 0 {
				loc = fmt.Sprintf("%s:%d", f, p.Line)
			}
			fmt.Fprintf(stdout, "%s: %s: %s [%s]\n", loc, p.Severity, p.Message, p.Code)
		}
		if *verbose {
			for _, t := range r.Traces {
				for _, d := range t.Divergences {
					fmt.Fprintf(stdout, "%s:%d: info: trace %d: %s %s", f, t.Line, t.Index+1, d.Class, d.Node)
					if len(d.Nodes) > 0 {
						fmt.Fprintf(stdout, " (%s)", strings.Join(d.Nodes, ", "))
					}
					fmt.Fprintln(stdout, " [divergence]")
				}
			}
		}
	}
	if *asJSON {
		writeJSON(stdout, results)
	}
	return code
}

func checkFile(path string) checkResult {
	r := checkResult{File: path, Problems: []bp.Problem{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		r.Problems = append(r.Problems, bp.Problem{Severity: bp.Error, Code: "read", Message: err.Error()})
		return r
	}
	d, err := bp.Parse(raw)
	if err != nil {
		r.Problems = append(r.Problems, bp.Problem{Severity: bp.Error, Code: "unreadable", Message: err.Error()})
		return r
	}
	r.OK = !d.HasErrors()
	r.KalkaID, r.Owner, r.Title, r.Rev, r.Entries = d.KalkaID(), d.Owner(), d.Title(), d.Rev(), len(d.Items())
	r.Problems = append(r.Problems, d.Problems()...)
	for _, t := range d.Traces() {
		ti := traceInfo{Index: t.Index, Line: t.Line, Section: t.Section, Title: t.Title, Valid: t.Valid(),
			Lanes: t.Lanes, Nodes: len(t.Nodes), Divergences: t.Divergences, Errors: t.Errors}
		if ti.Divergences == nil {
			ti.Divergences = []bp.Divergence{}
		}
		r.Traces = append(r.Traces, ti)
	}
	return r
}

func cmdComment(args []string, stdin io.Reader, stdout, stderr io.Writer, now func() time.Time) int {
	fs := flag.NewFlagSet("comment", flag.ContinueOnError)
	fs.SetOutput(stderr)
	anchor := fs.String("anchor", "", "heading slug (or slug#node) the entry refers to; empty for a general entry")
	kind := fs.String("kind", bp.KindComment, "entry kind: comment, deviation or a custom kind")
	re := fs.String("re", "", "id of the entry this one replies to")
	msg := fs.String("m", "", "entry text; read from stdin when omitted")
	asJSON := fs.Bool("json", false, "machine-readable output")
	files, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(files) != 1 {
		fmt.Fprintln(stderr, "comment: name exactly one file")
		return exitUsage
	}
	if *kind == bp.KindHandoff {
		fmt.Fprintf(stderr, "comment: use `%s handoff` to hand a blueprint over\n", name.Binary)
		return exitUsage
	}
	*anchor = strings.TrimPrefix(*anchor, "§")

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, "comment:", err)
		return exitProblems
	}
	if !bp.ValidEmail(cfg.Email) {
		fmt.Fprintf(stderr, "comment: configured email %q is not an email address\n", cfg.Email)
		return exitProblems
	}

	text := *msg
	if text == "" {
		b, err := io.ReadAll(io.LimitReader(stdin, bp.MaxEntryBody+1))
		if err != nil {
			fmt.Fprintln(stderr, "comment:", err)
			return exitProblems
		}
		text = string(b)
	}
	path := files[0]
	// Re-read and redo if the file changes while we write (spec 14).
	for attempt := 0; ; attempt++ {
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(stderr, "comment:", err)
			return exitProblems
		}
		d, err := bp.Parse(raw)
		if err != nil {
			fmt.Fprintf(stderr, "comment: %s: %v\n", path, err)
			return exitProblems
		}
		e, err := bp.Append(d, *kind, cfg.Email, now(), *anchor, *re, "", text)
		if err != nil {
			fmt.Fprintf(stderr, "comment: %s: %v\n", path, err)
			return exitProblems
		}
		out := d.Bytes()
		err = fsutil.WriteAtomic(path, out, raw)
		if errors.Is(err, fsutil.ErrChanged) && attempt < 3 {
			continue
		}
		if err != nil {
			fmt.Fprintln(stderr, "comment:", err)
			return exitProblems
		}
		if *asJSON {
			writeJSON(stdout, map[string]string{"file": path, "id": e.ID, "kind": e.Kind, "anchor": e.Anchor, "rev": e.Rev})
		} else {
			fmt.Fprintf(stdout, "added %s %s to %s\n", e.Kind, e.ID, path)
		}
		return exitOK
	}
}

func cmdVersion(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "machine-readable output")
	if _, err := parseInterspersed(fs, args); err != nil {
		return exitUsage
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{"version": version, "wire_protocol": wireProtocol, "plugin_protocol": pluginProtocol})
	} else {
		fmt.Fprintf(stdout, "%s %s (wire protocol %d, plugin protocol %d)\n", name.Binary, version, wireProtocol, pluginProtocol)
	}
	return exitOK
}
