package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/reference"
)

// cmdReference exports the embedded blueprint reference (spec 6.13).
func cmdReference(args []string, stdout, stderr io.Writer) int {
	usage := func() int {
		fmt.Fprintf(stderr, "usage: %[1]s reference export [-o <file or dir>] [--force] | %[1]s reference version [--json]\n", name.Binary)
		return exitUsage
	}
	if len(args) == 0 {
		return usage()
	}
	fs := flag.NewFlagSet("reference "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "", "write to this file, or into this directory as "+reference.FileName+" (default: stdout)")
	force := fs.Bool("force", false, "overwrite an existing file")
	asJSON := fs.Bool("json", false, "machine-readable output")
	pos, err := parseInterspersed(fs, args[1:])
	if err != nil || len(pos) > 0 {
		return usage()
	}
	switch args[0] {
	case "version":
		if *asJSON {
			writeJSON(stdout, map[string]string{"reference": reference.Version})
		} else {
			fmt.Fprintln(stdout, reference.Version)
		}
		return exitOK
	case "export":
	default:
		return usage()
	}
	if *out == "" || *out == "-" {
		_, err := stdout.Write(reference.Bytes())
		if err != nil {
			return failWith(*asJSON, stdout, stderr, err)
		}
		return exitOK
	}
	path := *out
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		path = filepath.Join(path, reference.FileName)
	}
	if _, err := os.Stat(path); err == nil && !*force {
		fmt.Fprintf(stderr, "%s already exists; add --force to overwrite it\n", path)
		return exitProblems
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return failWith(*asJSON, stdout, stderr, err)
	}
	if err := fsutil.WriteAtomic(path, reference.Bytes(), nil); err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	_ = os.Chmod(path, 0o644)
	if *asJSON {
		writeJSON(stdout, map[string]string{"path": path, "reference": reference.Version})
	} else {
		fmt.Fprintf(stdout, "wrote %s (%s)\n", path, reference.Version)
	}
	return exitOK
}
