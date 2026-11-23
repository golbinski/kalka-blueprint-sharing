package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/doctor"
	"github.com/golbinski/kalka/internal/plugin"
)

// doctorProbes are the probes `kalka doctor` runs; tests replace them.
var doctorProbes = func(dir string) doctor.Probes {
	p := doctor.System(dir)
	p.Daemon = func(ctx context.Context) doctor.DaemonState {
		if lockHolder(dir) == 0 {
			return doctor.DaemonState{}
		}
		c, err := daemon.Connect(dir)
		if err != nil {
			return doctor.DaemonState{Running: true, API: err}
		}
		sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		_, err = c.Status(sctx)
		return doctor.DaemonState{Running: true, API: err}
	}
	// The running daemon's plugins resolve references: ask it, and run
	// them here when none answers.
	p.Plugins = func(ctx context.Context) []plugin.Survey {
		if c := pluginDaemon(dir); c != nil {
			if s, err := c.PluginSurvey(ctx); err == nil {
				return s
			}
		}
		return pluginHost(dir).SurveyAll(ctx)
	}
	return p
}

// cmdDoctor checks this installation (spec 12.5).
func cmdDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return checkUsage
	}
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(stderr, "doctor:", err)
		return checkErrors
	}
	r := doctor.Run(context.Background(), doctorProbes(dir))
	if *asJSON {
		writeJSON(stdout, r)
		return r.Exit()
	}
	w := 10
	for _, x := range r.Results {
		w = max(w, len(x.Check))
	}
	for _, x := range r.Results {
		fmt.Fprintf(stdout, "%-4s  %-*s  %s\n", x.State, w, x.Check, x.Detail)
		if x.Fix != "" {
			fmt.Fprintf(stdout, "      %-*s  fix: %s\n", w, "", x.Fix)
		}
	}
	return r.Exit()
}
