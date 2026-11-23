package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/check"
	"github.com/golbinski/kalka/internal/name"
)

// cmdGraph is `kalka graph <file>`: the lens the graph canvas draws (spec
// 6.18, 11.1): the documents of the plan and domain paths and the
// document's children, their items, the edges, the states and the open
// items. Like context, it reads files only; it selects the same documents.
func cmdGraph(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("graph", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "machine-readable output")
	domain := fs.String("domain", "", "a document or a folder to read domain versions and plan nodes from (default the local folder)")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		fmt.Fprintf(stderr, "usage: %s graph <file> [--domain <file or folder>] [--json]\n", name.Binary)
		return exitUsage
	}
	raw, err := os.ReadFile(pos[0])
	if err == nil && len(raw) > bp.MaxFile {
		err = bp.ErrTooLarge
	}
	var doc *bp.Document
	if err == nil {
		doc, err = bp.Parse(raw)
	}
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	folder := checkFolder(*domain, pos)
	g := check.BuildGraph(pos[0], doc, check.GraphOptions{Options: check.Options{Domains: folder.Tree(), Docs: folder}})
	if *asJSON {
		writeJSON(stdout, g)
		return exitOK
	}
	fmt.Fprint(stdout, graphText(g))
	return exitOK
}

// graphText is the lens as an outline: the cards by band, then the open
// items.
func graphText(g check.Graph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Graph of %s\n", g.Title)
	if g.Alone {
		b.WriteString("Not part of a graph: no parent, no domain, no goals and no children.\n")
	}
	label := func(n check.GraphNode) string {
		s := n.Title
		if n.Kind == check.GraphDomain {
			s = fmt.Sprintf("%s@v%d", n.Domain, n.Version)
		} else if n.Issue != "" {
			_, key, _ := strings.Cut(n.Issue, ":")
			s = key + " " + n.Title
		}
		var notes []string
		if n.Focus {
			notes = append(notes, "focus")
		}
		if n.Child {
			notes = append(notes, "child")
		}
		notes = append(notes, n.Markers...)
		if n.Errors > 0 {
			notes = append(notes, plural(n.Errors, "error"))
		}
		if n.Warnings > 0 {
			notes = append(notes, plural(n.Warnings, "warning"))
		}
		if len(notes) > 0 {
			s += " (" + strings.Join(notes, ", ") + ")"
		}
		return s
	}
	for _, band := range []struct{ id, name string }{{"domain", "Domains"}, {"plan", "Plan"}} {
		var cards []string
		for _, n := range g.Nodes {
			if n.Card == "" && n.Band == band.id {
				cards = append(cards, "  "+label(n))
			}
		}
		if len(cards) > 0 {
			fmt.Fprintf(&b, "%s:\n%s\n", band.name, strings.Join(cards, "\n"))
		}
	}
	titles := map[string]string{}
	for _, n := range g.Nodes {
		if n.Card == "" {
			titles[n.ID] = label(check.GraphNode{Kind: n.Kind, Title: n.Title, Issue: n.Issue, Domain: n.Domain, Version: n.Version})
		}
	}
	if len(g.Open) == 0 {
		b.WriteString("Nothing open in the documents held here.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "%d open in the documents held here:\n", len(g.Open))
	last := ""
	for _, o := range g.Open {
		if o.Card != last {
			fmt.Fprintf(&b, "  %s\n", titles[o.Card])
			last = o.Card
		}
		fmt.Fprintf(&b, "    %s\n", o.Text)
	}
	return b.String()
}

func plural(n int, one string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %ss", n, one)
}
