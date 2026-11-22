package cli

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/check"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/plugin"
	"github.com/golbinski/kalka/internal/rules"
)

// Exit codes of kalka check (spec 12.4).
const (
	checkClean    = 0
	checkErrors   = 1
	checkWarnings = 2
	checkUsage    = 3
)

// checkOutput is the --json output: byte-identical for the same input,
// kalka version and rule set (spec 6.17).
type checkOutput struct {
	Rules   string      `json:"rules"`
	Kalka   string      `json:"kalka"`
	Files   []checkFile `json:"files"`
	Summary counts      `json:"summary"`
}

type checkFile struct {
	File        string       `json:"file"`
	Diagnostics []bp.Problem `json:"diagnostics"`
	Traces      []traceInfo  `json:"traces,omitempty"`
}

type counts struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Info     int `json:"info"`
}

// traceInfo is a trace block for agents: where it is, whether it is
// valid, and the divergences computed from its graph (spec 6.10).
type traceInfo struct {
	Index       int             `json:"index"`
	Line        int             `json:"line"`
	Section     string          `json:"section,omitempty"`
	Title       string          `json:"title,omitempty"`
	Valid       bool            `json:"valid"`
	Lanes       []string        `json:"lanes,omitempty"`
	Nodes       int             `json:"nodes"`
	Divergences []bp.Divergence `json:"divergences"`
}

func cmdCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "machine-readable output")
	verbose := fs.Bool("verbose", false, "also print information")
	explain := fs.String("explain", "", "print what a rule means, why it exists, and examples")
	strict := fs.Bool("strict", false, "warnings fail as errors do")
	set := fs.String("rules", "", "the rule set to run, v1 to the newest (default the newest)")
	withPlugins := fs.Bool("with-plugins", false, "resolve reference targets with the installed plugins")
	domain := fs.String("domain", "", "a document or a folder to read domain versions and plan nodes from (default the local folder)")
	paths, err := parseInterspersed(fs, args)
	if err != nil {
		return checkUsage
	}
	if *explain != "" {
		r, ok := rules.Lookup(*explain)
		if !ok {
			fmt.Fprintf(stderr, "check: no rule %s; kalka check --explain takes an id such as T040\n", *explain)
			return checkUsage
		}
		if *asJSON {
			writeJSON(stdout, r)
		} else {
			fmt.Fprint(stdout, rules.Explain(r))
		}
		return checkClean
	}
	n, err := rules.ParseSet(*set)
	if err != nil {
		fmt.Fprintln(stderr, "check:", err)
		return checkUsage
	}
	if len(paths) == 0 {
		fmt.Fprintln(stderr, "check: name at least one file or folder")
		return checkUsage
	}
	files, err := expandPaths(paths)
	if err != nil {
		fmt.Fprintln(stderr, "check:", err)
		return checkUsage
	}
	folder := checkFolder(*domain, files)
	opt := check.Options{Set: n, Domains: folder.Tree(), Docs: folder}
	if *withPlugins {
		dir, _ := config.Dir()
		opt.Plugins = &plugin.Host{Dir: filepath.Join(dir, "plugins"), ExeDir: exeDir(), Log: io.Discard}
	}
	out := checkOutput{Rules: fmt.Sprintf("v%d", n), Kalka: version, Files: []checkFile{}}
	for _, f := range files {
		cf := checkPath(f, opt)
		for _, p := range cf.Diagnostics {
			switch p.Severity {
			case bp.Error:
				out.Summary.Errors++
			case bp.Warning:
				out.Summary.Warnings++
			default:
				out.Summary.Info++
			}
			if *asJSON || (p.Severity == bp.Info && !*verbose) {
				continue
			}
			fmt.Fprintf(stdout, "%s: %s: %s [%s]\n", location(f, p), p.Severity, p.Message, p.Rule)
		}
		if *verbose && !*asJSON {
			for _, t := range cf.Traces {
				for _, d := range t.Divergences {
					fmt.Fprintf(stdout, "%s:%d: divergence: trace %d: %s %s", f, t.Line, t.Index+1, d.Class, d.Node)
					if len(d.Nodes) > 0 {
						fmt.Fprintf(stdout, " (%s)", strings.Join(d.Nodes, ", "))
					}
					if d.Lane != "" {
						fmt.Fprintf(stdout, " in %s", d.Lane)
					}
					fmt.Fprintln(stdout)
				}
			}
		}
		out.Files = append(out.Files, cf)
	}
	if *asJSON {
		writeJSON(stdout, out)
	}
	switch {
	case out.Summary.Errors > 0:
		return checkErrors
	case out.Summary.Warnings > 0 && *strict:
		return checkErrors
	case out.Summary.Warnings > 0:
		return checkWarnings
	}
	return checkClean
}

func location(f string, p bp.Problem) string {
	switch {
	case p.Line > 0 && p.Column > 0:
		return fmt.Sprintf("%s:%d:%d", f, p.Line, p.Column)
	case p.Line > 0:
		return fmt.Sprintf("%s:%d", f, p.Line)
	}
	return f
}

// checkFolder is where domain versions and plan nodes are read: the files
// checked, and --domain, else the local folder when one is set up (spec
// 12.4).
func checkFolder(flagged string, files []string) *check.Folder {
	roots := []string{}
	if flagged != "" {
		roots = append(roots, flagged)
	} else if cfg, err := config.Load(); err == nil && cfg.Folder != "" {
		if st, err := os.Stat(cfg.Folder); err == nil && st.IsDir() {
			roots = append(roots, cfg.Folder)
		}
	}
	f := check.NewFolder(roots...)
	for _, p := range files {
		f.Add(p)
	}
	return f
}

// expandPaths turns folders into the *.md files under them, leaving out
// dot-directories and domain snapshots, in path order.
func expandPaths(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil || !st.IsDir() {
			out = append(out, p) // a missing file is a diagnostic (B000)
			continue
		}
		var found []string
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != p && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".md") && !bp.IsSnapshotName(d.Name()) {
				found = append(found, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		sort.Strings(found)
		out = append(out, found...)
	}
	return out, nil
}

// checkPath runs the checker on one file.
func checkPath(path string, opt check.Options) checkFile {
	cf := checkFile{File: filepath.ToSlash(path), Diagnostics: []bp.Problem{}}
	raw, err := os.ReadFile(path)
	if err == nil && len(raw) > bp.MaxFile {
		err = bp.ErrTooLarge
	}
	var doc *bp.Document
	if err == nil {
		doc, err = bp.Parse(raw)
	}
	if err != nil {
		msg := err.Error()
		if os.IsNotExist(err) {
			msg = "no such file"
		}
		cf.Diagnostics = append(cf.Diagnostics, bp.NewProblem("B000", 0, msg))
		return cf
	}
	cf.Diagnostics = check.Document(doc, opt).Problems
	for _, t := range doc.Traces() {
		ti := traceInfo{Index: t.Index, Line: t.Line, Section: t.Section, Title: t.Title, Valid: t.Valid(),
			Lanes: t.Lanes, Nodes: len(t.Nodes), Divergences: t.Divergences}
		if ti.Divergences == nil {
			ti.Divergences = []bp.Divergence{}
		}
		cf.Traces = append(cf.Traces, ti)
	}
	return cf
}
