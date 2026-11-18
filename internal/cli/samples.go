package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/store"
	"github.com/golbinski/kalka/samples"
)

// cmdSamples copies the sample blueprints into Personal (spec 12.1). Before
// setup it asks for them to be added once kalka is set up, as the Windows
// installer does.
func cmdSamples(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("samples", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return exitUsage
	}
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(stderr, "samples:", err)
		return exitProblems
	}
	cfg, err := config.Load()
	if errors.Is(err, config.ErrMissing) {
		if err := samples.Ask(dir); err != nil {
			fmt.Fprintln(stderr, "samples:", err)
			return exitProblems
		}
		fmt.Fprintln(stdout, "kalka is not set up yet: the samples will be added to Personal when it is")
		return exitOK
	}
	if err != nil {
		fmt.Fprintln(stderr, "samples:", err)
		return exitProblems
	}
	written, err := samples.Seed(cfg.Folder)
	if err != nil {
		fmt.Fprintln(stderr, "samples:", err)
		return exitProblems
	}
	where := filepath.Join(cfg.Folder, store.PersonalDir, samples.Dir)
	if len(written) == 0 {
		fmt.Fprintf(stdout, "the samples are already in %s\n", where)
		return exitOK
	}
	fmt.Fprintf(stdout, "added %d sample blueprints to %s\n", len(written), where)
	return exitOK
}
