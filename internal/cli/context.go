package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/check"
	"github.com/golbinski/kalka/internal/name"
)

// cmdContext is `kalka context <file>`: the context of a document for an
// agent (spec 6.18): only what its work needs from the plan path and the
// domain path, how current each part is, and what is missing. --full
// prints every goal, invariant and term on the paths; --term one term.
// Like check, it reads files only, and writes nothing.
func cmdContext(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("context", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "machine-readable output")
	domain := fs.String("domain", "", "a document or a folder to read domain versions and plan nodes from (default the local folder)")
	full := fs.Bool("full", false, "every goal, invariant and term on the paths")
	term := fs.String("term", "", "print one term of the domain path, with its narrowings")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || *full && *term != "" {
		fmt.Fprintf(stderr, "usage: %s context <file> [--full | --term <name>] [--domain <file or folder>] [--json]\n", name.Binary)
		return exitUsage
	}
	raw, err := os.ReadFile(pos[0])
	if err == nil && len(raw) > bp.MaxFile {
		err = bp.ErrTooLarge
	}
	var doc *bp.Document
	if err == nil {
		doc, err = bp.Parse(raw)
	}
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	folder := checkFolder(*domain, pos)
	o := check.Options{Domains: folder.Tree(), Docs: folder}
	if *term != "" {
		t, err := check.TermContext(doc, o, *term)
		if err != nil {
			return failWith(*asJSON, stdout, stderr, err)
		}
		if *asJSON {
			writeJSON(stdout, t)
		} else {
			fmt.Fprint(stdout, check.TermMarkdown(t))
		}
		return exitOK
	}
	c := check.Context(pos[0], doc, o, *full)
	if *asJSON {
		writeJSON(stdout, c)
		return exitOK
	}
	fmt.Fprint(stdout, c.Markdown())
	return exitOK
}

// cmdApprove is `kalka approve <file> [-m <note>] [--incorporates <ids>]`:
// the owner's release of the approved version (spec 6.18). Unreleased
// edits are released first; with none, the latest release is approved.
// It never exports: `kalka export` is its own step.
func cmdApprove(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("approve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	note := fs.String("m", "", "a note: on the release when there are unreleased edits, else on the approval")
	inc := fs.String("incorporates", "", "comma-separated ids of the deviations the release takes up")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		fmt.Fprintf(stderr, "usage: %s approve <file> [-m <note>] [--incorporates <id>,...] [--json]\n", name.Binary)
		return exitUsage
	}
	var ids []string
	for _, x := range strings.Split(*inc, ",") {
		if x = strings.TrimSpace(x); x != "" {
			ids = append(ids, x)
		}
	}
	c, code := daemonFor(*asJSON, stdout, stderr)
	if c == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := c.Approve(ctx, absPath(pos[0]), *note, ids)
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	if *asJSON {
		writeJSON(stdout, r)
		return exitOK
	}
	switch {
	case !r.Changed:
		fmt.Fprintf(stdout, "%s is %s already; nothing to do\n", pos[0], r.State)
	case r.Released:
		fmt.Fprintf(stdout, "released and approved version %d of %s; it replicates to the members of %s\n", r.Version, pos[0], r.Team)
	default:
		fmt.Fprintf(stdout, "approved version %d of %s; it replicates to the members of %s\n", r.Version, pos[0], r.Team)
	}
	return exitOK
}
