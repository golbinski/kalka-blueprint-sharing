package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
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

// cmdClose is `kalka close <file> [--reopen]`: the owner confirms the
// document delivered (kalka_status: closed), or reopens it (spec 6.18).
func cmdClose(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("close", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	reopen := fs.Bool("reopen", false, "take a closed document back to open")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		fmt.Fprintf(stderr, "usage: %s close <file> [--reopen] [--json]\n", name.Binary)
		return exitUsage
	}
	c, code := daemonFor(*asJSON, stdout, stderr)
	if c == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	path := absPath(pos[0])
	if err := c.Close(ctx, path, *reopen); err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	what := "closed"
	if *reopen {
		what = "reopened"
	}
	if *asJSON {
		writeJSON(stdout, map[string]string{what: path})
		return exitOK
	}
	fmt.Fprintf(stdout, "%s %s\n", pos[0], what)
	return exitOK
}
