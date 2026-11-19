package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/name"
)

// cmdUpdate is `kalka update [--check]` (spec 24): look for a new release;
// without --check, install it when this kalka can, which restarts it.
func cmdUpdate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	only := fs.Bool("check", false, "only look for an update; install nothing")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) > 0 {
		fmt.Fprintf(stderr, "usage: %s update [--check] [--json]\n", name.Binary)
		return exitUsage
	}
	c, code := daemonFor(*asJSON, stdout, stderr)
	if c == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	st, err := c.UpdateStatus(ctx, true)
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	install := !*only && st.Latest != "" && st.CanInstall
	if install {
		if err := c.InstallUpdate(ctx); err != nil {
			return failWith(*asJSON, stdout, stderr, err)
		}
	}
	if *asJSON {
		writeJSON(stdout, st)
		return exitOK
	}
	fmt.Fprintln(stdout, updateLine(st, install))
	return exitOK
}

func updateLine(st daemon.UpdateStatus, installing bool) string {
	switch {
	case !st.Checks:
		return fmt.Sprintf("%s %s is a development build; it does not check for updates", name.Product, st.Current)
	case st.Latest == "":
		return fmt.Sprintf("%s %s is up to date", name.Product, st.Current)
	case installing:
		return fmt.Sprintf("installing %s %s; %[1]s restarts", name.Product, st.Latest)
	case !st.CanInstall:
		return fmt.Sprintf("%s %s is available, and this installation does not update itself: %s", name.Product, st.Latest, st.NotesURL)
	case st.State == daemon.UpdateReady:
		return fmt.Sprintf("%s %s is downloaded; run `%s update` to restart into it", name.Product, st.Latest, name.Binary)
	}
	return fmt.Sprintf("%s %s is available (%s)", name.Product, st.Latest, st.NotesURL)
}
