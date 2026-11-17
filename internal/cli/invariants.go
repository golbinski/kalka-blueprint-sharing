package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/check"
)

// invariantsOutput is `kalka invariants --json` (spec 12.4).
type invariantsOutput struct {
	File string `json:"file"`
	// Domain is the domain version the references resolve against.
	Domain     string      `json:"domain,omitempty"`
	Invariants []check.Row `json:"invariants"`
	// Diagnostics are check's invariant rules (I…) and R001 when the
	// domain version is not here.
	Diagnostics []bp.Problem `json:"diagnostics"`
}

// cmdInvariants prints a document's invariants matrix; it exits as check
// does on the invariant diagnostics.
func cmdInvariants(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("invariants", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "machine-readable output")
	domain := fs.String("domain", "", "a domain document, or a folder of them, to read domain versions from")
	paths, err := parseInterspersed(fs, args)
	if err != nil {
		return checkUsage
	}
	if len(paths) != 1 {
		fmt.Fprintln(stderr, "invariants: name one file")
		return checkUsage
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		fmt.Fprintln(stderr, "invariants:", err)
		return checkErrors
	}
	doc, err := bp.Parse(data)
	if err != nil {
		fmt.Fprintf(stderr, "invariants: %s: %v\n", paths[0], err)
		return checkErrors
	}
	res := check.Document(doc, check.Options{Domains: checkDomains(*domain)})
	out := invariantsOutput{File: paths[0], Invariants: check.Matrix(doc, res.Domain), Diagnostics: []bp.Problem{}}
	if res.Domain != nil {
		out.Domain = res.Glossary.Ref
	}
	errs, warns := 0, 0
	for _, p := range res.Problems {
		if p.Rule[0] != 'I' && p.Rule != "R001" {
			continue
		}
		out.Diagnostics = append(out.Diagnostics, p)
		switch p.Severity {
		case bp.Error:
			errs++
		case bp.Warning:
			warns++
		}
	}
	if *asJSON {
		writeJSON(stdout, out)
	} else {
		if len(out.Invariants) == 0 {
			fmt.Fprintf(stdout, "%s: no invariants\n", paths[0])
		}
		for _, r := range out.Invariants {
			st := r.Statement
			if !r.Resolved {
				st = "(not found in " + orNone(out.Domain) + ")"
			}
			fmt.Fprintf(stdout, "%s", r.Ref)
			if r.Status != "" {
				fmt.Fprintf(stdout, " [%s]", r.Status)
			}
			fmt.Fprintf(stdout, "  %s\n", st)
			var g []string
			for _, n := range r.GuardedBy {
				g = append(g, n.Node)
			}
			fmt.Fprintf(stdout, "    guarded by: %s\n", orNone(strings.Join(g, ", ")))
			fmt.Fprintf(stdout, "    verified by: %s\n", orNone(strings.Join(r.VerifiedBy, "; ")))
			if len(r.Gaps) > 0 {
				fmt.Fprintf(stdout, "    gaps: %s\n", strings.Join(r.Gaps, ", "))
			}
		}
		for _, p := range out.Diagnostics {
			fmt.Fprintf(stdout, "%s: %s: %s [%s]\n", location(paths[0], p), p.Severity, p.Message, p.Rule)
		}
	}
	switch {
	case errs > 0:
		return checkErrors
	case warns > 0:
		return checkWarnings
	}
	return checkClean
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
