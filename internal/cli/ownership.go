package cli

// `kalka handoff` and `kalka reassign` (spec 7.2, 7.3).

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/name"
)

// remoteKind is the stable kind of a daemon error (spec 12.3), or "daemon".
func remoteKind(err error) string {
	var re *daemon.RemoteError
	if errors.As(err, &re) && re.Kind != "" {
		return re.Kind
	}
	return "daemon"
}

// fileAndEmail parses `<file> <email>` with --json.
func fileAndEmail(cmd string, args []string, stderr io.Writer) (file, email string, asJSON bool, code int) {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	j := fs.Bool("json", false, "print JSON")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return "", "", false, exitUsage
	}
	if len(pos) != 2 {
		fmt.Fprintf(stderr, "usage: %s %s <file> <email> [--json]\n", name.Binary, cmd)
		return "", "", false, exitUsage
	}
	if !bp.ValidEmail(pos[1]) {
		fmt.Fprintf(stderr, "%q is not an email address\n", pos[1])
		return "", "", false, exitUsage
	}
	return absPath(pos[0]), pos[1], *j, exitOK
}

// cmdHandoff implements `kalka handoff <file> <email>`: the owner releases
// the next version naming the recipient. No network is needed.
func cmdHandoff(args []string, stdout, stderr io.Writer) int {
	file, email, asJSON, code := fileAndEmail("handoff", args, stderr)
	if code != exitOK {
		return code
	}
	c, code := daemonFor(asJSON, stdout, stderr)
	if c == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := c.Handoff(ctx, file, email)
	if err != nil {
		return report(asJSON, stdout, stderr, remoteKind(err), err.Error())
	}
	if asJSON {
		writeJSON(stdout, r)
		return exitOK
	}
	fmt.Fprintf(stdout, "handed %s off to %s as version %d; it replicates when it can\n", r.Path, email, r.Version)
	return exitOK
}

// cmdReassign implements `kalka reassign <file> <email>`, for team owners.
func cmdReassign(args []string, stdout, stderr io.Writer) int {
	file, email, asJSON, code := fileAndEmail("reassign", args, stderr)
	if code != exitOK {
		return code
	}
	c, code := daemonFor(asJSON, stdout, stderr)
	if c == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := c.Reassign(ctx, file, email)
	if err != nil {
		return report(asJSON, stdout, stderr, remoteKind(err), err.Error())
	}
	if asJSON {
		writeJSON(stdout, r)
		return exitOK
	}
	fmt.Fprintf(stdout, "reassigned %s from %s to %s from version %d", r.Path, r.From, r.Owner, r.After+1)
	if r.Version > 0 {
		fmt.Fprintf(stdout, "; you released version %d", r.Version)
	}
	fmt.Fprintln(stdout)
	return exitOK
}
