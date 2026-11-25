package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/userpath"
)

// cmdPath adds a folder to the user's PATH or takes it out (spec 23.1),
// by default the folder kalka runs from. The Windows installer and
// uninstaller run it, so the whole PATH is read and written here.
func cmdPath(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("path", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) < 1 || len(pos) > 2 || (pos[0] != "add" && pos[0] != "remove") {
		fmt.Fprintf(stderr, "usage: %s path add|remove [<folder>] [--json]\n", name.Binary)
		return exitUsage
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "%s path %s: %v\n", name.Binary, pos[0], err)
		return exitProblems
	}
	dir := ""
	if len(pos) == 2 {
		dir = pos[1]
	} else {
		exe, err := os.Executable()
		if err != nil {
			return fail(err)
		}
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		dir = filepath.Dir(exe)
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return fail(err)
	}
	var changed bool
	if pos[0] == "add" {
		changed, err = userpath.Add(dir)
	} else {
		changed, err = userpath.Remove(dir)
	}
	if err != nil {
		return fail(err)
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{"folder": dir, "changed": changed})
		return exitOK
	}
	switch {
	case pos[0] == "add" && changed:
		fmt.Fprintf(stdout, "Added %s to your PATH; terminals opened from now on find %s.\n", dir, name.Binary)
	case pos[0] == "add":
		fmt.Fprintf(stdout, "%s is on your PATH already.\n", dir)
	case changed:
		fmt.Fprintf(stdout, "Took %s out of your PATH.\n", dir)
	default:
		fmt.Fprintf(stdout, "%s was not on your PATH.\n", dir)
	}
	return exitOK
}
