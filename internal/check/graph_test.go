package check

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

const samplesDir = "../../samples/blueprints"

func sampleGraph(t *testing.T, sub, file string) Graph {
	t.Helper()
	dir := filepath.Join(samplesDir, sub)
	raw, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		t.Fatal(err)
	}
	d, err := bp.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	f := NewFolder(dir)
	return BuildGraph(file, d, GraphOptions{Options: Options{Domains: f.Tree(), Docs: f}})
}

func cardsOf(g Graph) []string {
	var out []string
	for _, n := range g.Nodes {
		if n.Card == "" {
			s := n.Band + " " + n.Title
			if n.Kind == GraphDomain {
				s = fmt.Sprintf("%s %s@v%d", n.Band, n.Domain, n.Version)
			}
			out = append(out, s)
		}
	}
	return out
}

// On the library samples, the graph of DEV-123 is library →
// library.reservations and DEV-100 → DEV-123, and what needs a person is
// G3 unclaimed and the code-only steps C2 and C5 (spec 11.1).
func TestGraphLibrary(t *testing.T) {
	g := sampleGraph(t, "library", "return-creates-hold.md")
	want := []string{"domain library@v2", "domain library.reservations@v1", "plan Holds and notification on return", "plan Return creates a hold and notifies the queue head"}
	if got := cardsOf(g); !slices.Equal(got, want) {
		t.Errorf("cards %q", got)
	}
	var open []string
	for _, o := range g.Open {
		open = append(open, o.Node[strings.LastIndex(o.Node, ":")+1:]+" "+o.Kind)
	}
	if strings.Join(open, ", ") != "G3 unclaimed, C2 implementation-only, C5 implementation-only" {
		t.Errorf("open %q", open)
	}
	focus := "plan:3e9b7a60-1f24-4d8e-b0c5-7a2d91e4f803"
	if g.Focus != focus {
		t.Errorf("focus %s", g.Focus)
	}
	has := func(from, to, kind string) bool {
		return slices.ContainsFunc(g.Edges, func(e GraphEdge) bool { return e.From == from && e.To == to && e.Kind == kind })
	}
	epic := "plan:8d1c0f2a-4b7e-4c39-9a51-2e6f0b3d7c11"
	for _, e := range []struct{ from, to, kind string }{
		{"domain:library@v2", "domain:library.reservations@v1", EdgeDomainParent},
		{epic, focus, EdgeParent},
		{focus, "domain:library.reservations@v1", EdgePin},
		{focus + "/criterion:AC1", epic + "/goal:G1", EdgeRealizes},
		{focus + "/step:1:B3", focus + "/criterion:AC1", EdgeCovers},
		{focus + "/step:1:C4", focus + "/step:1:B3", EdgeStep},
		{focus + "/step:1:C4", epic + "/invariant:E1", EdgeGuards},
		{focus, "domain:library.reservations@v1/invariant:R1", EdgeAffects},
		{"domain:library.reservations@v1/invariant:R2", "domain:library@v2/term:loan", EdgeRefines},
	} {
		if !has(e.from, e.to, e.kind) {
			t.Errorf("no %s edge %s → %s", e.kind, e.from, e.to)
		}
	}
	// The epic card counts its open item and its warning (P030).
	for _, n := range g.Nodes {
		if n.ID == epic && (n.Open != 1 || n.Warnings != 1) {
			t.Errorf("epic card %+v", n)
		}
	}
	// The same files give the same graph, byte for byte.
	a, _ := json.Marshal(g)
	b, _ := json.Marshal(sampleGraph(t, "library", "return-creates-hold.md"))
	if string(a) != string(b) {
		t.Error("two runs differ")
	}
	// From the epic, the leaf is a child; from a domain node, its subdomain.
	if got := cardsOf(sampleGraph(t, "library", "holds-on-return.md")); !slices.Contains(got, "plan Return creates a hold and notifies the queue head") {
		t.Errorf("epic cards %q", got)
	}
	if got := cardsOf(sampleGraph(t, "library", "team-lending-library-domain.md")); !slices.Equal(got, []string{"domain library@v2", "domain library.reservations@v1"}) {
		t.Errorf("domain cards %q", got)
	}
}

// kalka context and kalka graph select the same documents for every
// sample: the graph adds the focus's children, and the context only names
// the siblings that claim the parent's goals, in its claimed-by lines,
// which are an addition to the lens and not a change of it.
func TestGraphAndContextSelectTheSame(t *testing.T) {
	n := 0
	err := filepath.WalkDir(samplesDir, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		d, err := bp.Parse(raw)
		if err != nil {
			return err
		}
		f := NewFolder(filepath.Dir(path))
		o := Options{Domains: f.Tree(), Docs: f}
		c := Context(path, d, o, false)
		fromContext := []string{"plan:" + c.Focus.KalkaID}
		for _, l := range c.Goals {
			fromContext = append(fromContext, "plan:"+l.Doc.KalkaID)
		}
		for _, p := range c.Invariants.Domains {
			for _, n := range p.Nodes {
				name, v, _ := strings.Cut(n, "@v")
				ver, _ := strconv.ParseInt(v, 10, 64)
				fromContext = append(fromContext, domainKey(name, ver))
			}
		}
		fromContext = slices.Compact(slices.Sorted(slices.Values(fromContext)))
		g := BuildGraph(path, d, GraphOptions{Options: o})
		var fromGraph []string
		for _, x := range g.Nodes {
			if x.Card == "" && !x.Child && !x.Placeholder {
				id := x.ID
				if x.Focus && d.KalkaID() == "" {
					id = "plan:"
				}
				if x.Focus && d.IsDomain() {
					continue // a domain node is its own domain path's last node
				}
				fromGraph = append(fromGraph, id)
			}
		}
		if d.IsDomain() {
			fromContext = slices.DeleteFunc(fromContext, func(s string) bool { return s == domainKey(d.DomainName(), d.DomainVersion()) })
			fromContext = slices.DeleteFunc(fromContext, func(s string) bool { return s == "plan:"+d.KalkaID() })
		}
		slices.Sort(fromContext)
		slices.Sort(fromGraph)
		if !slices.Equal(fromContext, fromGraph) {
			t.Errorf("%s: context %q, graph %q", path, fromContext, fromGraph)
		}
		n++
		return nil
	})
	if err != nil || n < 10 {
		t.Fatalf("%v, %d samples", err, n)
	}
}

// A parent or a domain version not held here is a placeholder card with a
// dashed edge; a cycle is cut, with the edge back marked.
func TestGraphPlaceholdersAndCycles(t *testing.T) {
	c := plans(t, func(d string) {
		planDoc(t, d, "leaf.md", leafID, epicID, "kalka_domain: lending/library.reservations@v9\n", "Leaf", goals(criterion))
	})
	raw, _ := os.ReadFile(filepath.Join(c.dir, "leaf.md"))
	d, _ := bp.Parse(raw)
	f := library(t, nil)
	g := BuildGraph("leaf.md", d, GraphOptions{Options: Options{Domains: f.Tree(), Docs: c.f}})
	var placeholders []string
	for _, n := range g.Nodes {
		if n.Placeholder {
			placeholders = append(placeholders, n.ID)
		}
	}
	if !slices.Equal(placeholders, []string{"domain:library.reservations@v9", "plan:" + epicID}) {
		t.Errorf("placeholders %q", placeholders)
	}
	if !slices.ContainsFunc(g.Edges, func(e GraphEdge) bool { return e.From == "plan:"+epicID && e.Kind == EdgeParent && e.Dashed }) {
		t.Errorf("no dashed edge from the missing parent: %+v", g.Edges)
	}

	c = plans(t, func(d string) {
		planDoc(t, d, "epic.md", epicID, storyID, "", "Epic", "")
		planDoc(t, d, "story.md", storyID, epicID, "", "Story", "")
		planDoc(t, d, "leaf.md", leafID, storyID, "", "Leaf", "")
	})
	raw, _ = os.ReadFile(filepath.Join(c.dir, "leaf.md"))
	d, _ = bp.Parse(raw)
	g = BuildGraph("leaf.md", d, GraphOptions{Options: Options{Docs: c.f}})
	if !slices.ContainsFunc(g.Edges, func(e GraphEdge) bool { return e.Marker == MarkCycle }) {
		t.Errorf("no cycle edge: %+v", g.Edges)
	}
	if len(cardsOf(g)) != 3 {
		t.Errorf("cards %q", cardsOf(g))
	}
	// A document outside any graph is alone.
	d, _ = bp.Parse([]byte("# Lone\n\nJust prose.\n"))
	if g := BuildGraph("lone.md", d, GraphOptions{}); !g.Alone || len(g.Nodes) != 1 || !g.Nodes[0].Focus {
		t.Errorf("lone %+v", g)
	}
}
