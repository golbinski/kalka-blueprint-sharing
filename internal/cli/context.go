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

// cmdContext is `kalka context <file>`: the flattened context of a
// document for an agent (spec 6.18): the plan path with goals and
// invariants, the domain path with terms, at the pinned versions. Like
// check, it reads files only.
func cmdContext(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("context", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "machine-readable output")
	domain := fs.String("domain", "", "a document or a folder to read domain versions and plan nodes from (default the local folder)")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		fmt.Fprintf(stderr, "usage: %s context <file> [--domain <file or folder>] [--json]\n", name.Binary)
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
	c := check.Context(pos[0], doc, check.Options{Domains: folder.Tree(), Docs: folder})
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
