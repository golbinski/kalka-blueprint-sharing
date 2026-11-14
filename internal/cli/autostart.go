package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/golbinski/kalka/internal/autostart"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/name"
)

// cmdAutostart turns starting at logon on or off (implementation plan Q3).
func cmdAutostart(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("autostart", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || (pos[0] != "on" && pos[0] != "off" && pos[0] != "status") {
		fmt.Fprintf(stderr, "usage: %s autostart on|off|status [--json]\n", name.Binary)
		return exitUsage
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "%s autostart %s: %v\n", name.Binary, pos[0], err)
		return exitProblems
	}
	var st autostart.Status
	started := false
	switch pos[0] {
	case "on":
		exe, err := autostart.Executable()
		if err != nil {
			return fail(err)
		}
		if st, err = autostart.Enable(exe); err != nil {
			return fail(err)
		}
		if !autostart.StartsOnEnable && !running() {
			if err := autostart.Start(exe); err != nil {
				return fail(fmt.Errorf("saved, but starting now failed: %w", err))
			}
			started = true
		}
	case "off":
		if err := autostart.Disable(); err != nil {
			return fail(err)
		}
		if st, err = autostart.Get(); err != nil {
			return fail(err)
		}
	case "status":
		if st, err = autostart.Get(); err != nil {
			return fail(err)
		}
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{"autostart": st, "running": running()})
		return exitOK
	}
	switch {
	case !st.Enabled:
		fmt.Fprintf(stdout, "%s does not start at logon (%s).\n", name.Product, st.Where)
		if pos[0] == "off" && running() {
			fmt.Fprintf(stdout, "The running %s keeps running until you log out.\n", name.Product)
		}
	default:
		fmt.Fprintf(stdout, "%s starts at logon: %s\n(%s)\n", name.Product, st.Command, st.Where)
		if st.Stale {
			fmt.Fprintf(stdout, "WARNING: that program no longer exists; run `%s autostart on` from the installed copy.\n", name.Binary)
		}
		if started {
			fmt.Fprintf(stdout, "Started it now in the background; `%s open` opens its page.\n", name.Binary)
		}
	}
	return exitOK
}

// running reports whether a daemon answers for this config directory.
func running() bool {
	dir, err := config.Dir()
	if err != nil {
		return false
	}
	c, err := daemon.Connect(dir)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = c.Status(ctx)
	return err == nil
}
