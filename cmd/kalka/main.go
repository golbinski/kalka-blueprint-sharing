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
	if len(args) == 0 {
		fmt.Fprintln(stderr, "the daemon and web UI are not implemented yet (milestone M2/M3); available: check, comment, version")
		return exitProblems
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "check":
		return cmdCheck(rest, stdout, stderr)
	case "comment":
		return cmdComment(rest, stdin, stdout, stderr, now)
	case "version":
		return cmdVersion(rest, stdout, stderr)
	case "send", "handoff", "peers", "verify", "rendezvous":
		fmt.Fprintf(stderr, "%s %s: not implemented yet\n", name.Binary, cmd)
		return exitProblems
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
  %[1]s check <file>... [--json] [--verbose]
  %[1]s comment <file> [--anchor <slug>] [--kind deviation] [--re <id>] [-m <text>] [--json]
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
	if *re != "" {
		text = "re: " + *re + "\n" + text
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
		if *anchor != "" {
			slug, _, _ := strings.Cut(*anchor, "#")
			if !d.HasSection(slug) {
				fmt.Fprintf(stderr, "comment: no heading with slug %q; headings are: %s\n", slug, slugList(d))
				return exitProblems
			}
		}
		if *re != "" && !hasEntry(d, *re) {
			fmt.Fprintf(stderr, "comment: no entry with id %q\n", *re)
			return exitProblems
		}
		e, err := bp.NewEntry(*kind, cfg.Email, now(), *anchor, d.Rev(), text)
		if err != nil {
			fmt.Fprintln(stderr, "comment:", err)
			return exitProblems
		}
		if err := d.AddEntry(e); err != nil {
			fmt.Fprintf(stderr, "comment: %s: %v\n", path, err)
			return exitProblems
		}
		out := d.Bytes()
		if len(out) > bp.MaxFile {
			fmt.Fprintf(stderr, "comment: %s would exceed %d bytes\n", path, bp.MaxFile)
			return exitProblems
		}
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

func slugList(d *bp.Document) string {
	var s []string
	for _, h := range d.Headings() {
		s = append(s, h.Slug)
	}
	if len(s) == 0 {
		return "(none)"
	}
	return strings.Join(s, ", ")
}

func hasEntry(d *bp.Document, id string) bool {
	for _, it := range d.Items() {
		if it.Entry.ID == id {
			return true
		}
	}
	return false
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
