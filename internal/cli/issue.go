package cli

// kalka export and kalka archive (spec 10.5, 12.1).

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/plugin"
)

// cmdExport is `kalka export <file>`: the released version goes to the
// issue kalka_issue names, through the plugin for its scheme, and a
// signed exported entry records it.
func cmdExport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		fmt.Fprintf(stderr, "usage: %s export <file> [--json]\n", name.Binary)
		return exitUsage
	}
	c, code := daemonFor(*asJSON, stdout, stderr)
	if c == nil {
		return code
	}
	// The plugin may take up to its export timeout; leave room around it.
	ctx, cancel := context.WithTimeout(context.Background(), plugin.ExportTimeout+30*time.Second)
	defer cancel()
	res, err := c.Export(ctx, absPath(pos[0]))
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	if *asJSON {
		writeJSON(stdout, res)
		return exitOK
	}
	verb := "exported"
	if res.Updated {
		verb = "exported again, replacing the earlier export:"
	}
	fmt.Fprintf(stdout, "%s v%d %s %s\n", pos[0], res.Version, verb, res.Issue)
	if res.Approved {
		fmt.Fprintf(stdout, "v%d is the approved version\n", res.Version)
	} else if res.Approval != "" {
		fmt.Fprintf(stdout, "v%d is not approved (%s)\n", res.Version, res.Approval)
	}
	if res.URL != "" {
		fmt.Fprintln(stdout, res.URL)
	}
	return exitOK
}

// cmdArchive is `kalka archive <file> [--undo]`.
func cmdArchive(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	undo := fs.Bool("undo", false, "unarchive")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		fmt.Fprintf(stderr, "usage: %s archive <file> [--undo] [--json]\n", name.Binary)
		return exitUsage
	}
	c, code := daemonFor(*asJSON, stdout, stderr)
	if c == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	path := absPath(pos[0])
	if err := c.Archive(ctx, path, !*undo); err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{"path": path, "archived": !*undo})
		return exitOK
	}
	if *undo {
		fmt.Fprintf(stdout, "%s is no longer archived\n", pos[0])
	} else {
		fmt.Fprintf(stdout, "%s is archived; lists hide it unless you show archived documents\n", pos[0])
	}
	return exitOK
}
