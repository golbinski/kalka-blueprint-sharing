package blueprint

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

const specTrace = "```trace\n" + `lanes: [business, code]
nodes:
  - {id: B1, lane: business, text: Receive uploaded files, next: [B2]}
  - {id: B2, lane: business, text: Extract text, next: [B3]}
  - {id: B3, lane: business, text: Classify the document, next: [B4]}
  - {id: B4, lane: business, text: Notify the user}

  - {id: C1, lane: code, text: Upload endpoint handlers, realizes: [B1],
     ref: "gh:src/upload/handlers.py#L40-L120", next: [C2]}
  - {id: C2, lane: code, text: Skip files larger than 20 MB,
     ref: "gh:src/import/size_guard.py#L12-L48", next: [C3],
     why: the parser runs out of memory on larger files}
  - {id: C3, lane: code, text: Extract and classify in one pass,
     realizes: [B2, B3], ref: "gh:src/import/parse.py#L200-L310", next: [C4],
     why: avoids parsing each file twice}
  - {id: C4, lane: code, text: Batch and rate-limit per user,
     realizes: [B4], ref: "gh:src/notify/batch.py#L15-L90"}
` + "```\n"

func traceDoc(t *testing.T, blocks ...string) *Document {
	t.Helper()
	src := "---\nkalka_id: 7b0b3f5e-2c1a-4d8e-9f3b-1a2b3c4d5e6f\nowner: alice@corp.com\n---\n# Import pipeline refactor\n\n## Import pipeline\n\n" +
		strings.Join(blocks, "\nSome prose.\n\n")
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// block wraps YAML in a trace fence.
func block(yaml string) string { return "```trace\n" + yaml + "```\n" }

func divs(tr Trace) []string {
	var out []string
	for _, d := range tr.Divergences {
		s := d.Class + ":" + d.Node
		if len(d.Nodes) > 0 {
			s += "[" + strings.Join(d.Nodes, ",") + "]"
		}
		if d.Lane != "" {
			s += "@" + d.Lane
		}
		out = append(out, s)
	}
	return out
}

func wantDivs(t *testing.T, tr Trace, want ...string) {
	t.Helper()
	if !tr.Valid() {
		t.Fatalf("invalid: %+v", tr.Errors)
	}
	if got := divs(tr); !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
		t.Fatalf("divergences\n got %q\nwant %q", got, want)
	}
}

func TestTraceSpecExample(t *testing.T) {
	d := traceDoc(t, specTrace)
	trs := d.Traces()
	if len(trs) != 1 {
		t.Fatalf("%d traces", len(trs))
	}
	tr := trs[0]
	if tr.Section != "import-pipeline" || tr.Title != "Import pipeline" || tr.Line != 9 || tr.Nested {
		t.Fatalf("placement %+v", tr)
	}
	if !reflect.DeepEqual(tr.Lanes, []string{"business", "code"}) || len(tr.Nodes) != 8 {
		t.Fatalf("lanes %v nodes %d", tr.Lanes, len(tr.Nodes))
	}
	c3 := tr.Node("C3")
	if c3.Why != "avoids parsing each file twice" || c3.Ref != "gh:src/import/parse.py#L200-L310" || !reflect.DeepEqual(c3.Realizes, []string{"B2", "B3"}) || c3.Line != 22 {
		t.Fatalf("C3 %+v", c3)
	}
	// The mockup's two divergences, and nothing else.
	wantDivs(t, tr, "implementation-only:C2", "merged:C3[B2,B3]")
	if len(d.Problems()) != 0 {
		t.Fatalf("problems %+v", d.Problems())
	}
}

func TestDivergenceClasses(t *testing.T) {
	cases := []struct {
		name, yaml string
		want       []string
	}{
		{"unrealized", `lanes: [business, code]
nodes:
  - {id: B1, lane: business, text: a, next: [B2]}
  - {id: B2, lane: business, text: b}
  - {id: C1, lane: code, text: x, realizes: [B1]}
`, []string{"unrealized:B2"}},
		{"expanded", `lanes: [business, code]
nodes:
  - {id: B1, lane: business, text: a}
  - {id: C1, lane: code, text: x, realizes: [B1], next: [C2]}
  - {id: C2, lane: code, text: y, realizes: [B1]}
`, []string{"expanded:B1[C1,C2]"}},
		{"scattered", `lanes: [business, code]
nodes:
  - {id: B1, lane: business, text: a, next: [B2]}
  - {id: B2, lane: business, text: b}
  - {id: C1, lane: code, text: x, realizes: [B1], next: [C2]}
  - {id: C2, lane: code, text: y, realizes: [B2], next: [C3]}
  - {id: C3, lane: code, text: z, realizes: [B1]}
`, []string{"expanded:B1[C1,C3]", "scattered:B1[C1,C3]@code"}},
		{"reordered", `lanes: [business, code]
nodes:
  - {id: B1, lane: business, text: validate, next: [B2]}
  - {id: B2, lane: business, text: store}
  - {id: C1, lane: code, text: insert, realizes: [B2], next: [C2]}
  - {id: C2, lane: code, text: check, realizes: [B1]}
`, []string{"reordered:B1[B2,C1,C2]@code"}},
		{"implementation-only", `lanes: [business, code]
nodes:
  - {id: B1, lane: business, text: a}
  - {id: C1, lane: code, text: x, realizes: [B1], next: [C2]}
  - {id: C2, lane: code, text: retry}
`, []string{"implementation-only:C2"}},
		{"merged", `lanes: [business, code]
nodes:
  - {id: B1, lane: business, text: a, next: [B2]}
  - {id: B2, lane: business, text: b}
  - {id: C1, lane: code, text: both, realizes: [B1, B2]}
`, []string{"merged:C1[B1,B2]"}},
		// Three lanes: a code node may realize a business node directly;
		// the middle lane can be both implementation-only and unrealized.
		{"three lanes", `lanes: [business, service, code]
nodes:
  - {id: B1, lane: business, text: Accept parcels, next: [B2]}
  - {id: B2, lane: business, text: Deliver}
  - {id: S1, lane: service, text: Parcel intake API, realizes: [B1], next: [S2]}
  - {id: S2, lane: service, text: Address validation}
  - {id: C1, lane: code, text: handler, realizes: [S1], next: [C2]}
  - {id: C2, lane: code, text: delivery job, realizes: [B2]}
`, []string{"implementation-only:S2", "unrealized:S2"}},
		// A labelled branch: the two arms do not order each other, so their
		// realizers may come in any order.
		{"branch", `lanes: [business, code]
nodes:
  - {id: B1, lane: business, text: Check size, next: [{to: B2, label: small}, {to: B3, label: large}]}
  - {id: B2, lane: business, text: Import now}
  - {id: B3, lane: business, text: Queue}
  - {id: C1, lane: code, text: size switch, realizes: [B1], next: [C2, C3]}
  - {id: C2, lane: code, text: enqueue, realizes: [B3], next: [C3]}
  - {id: C3, lane: code, text: import, realizes: [B2]}
`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantDivs(t, traceDoc(t, block(c.yaml)).Traces()[0], c.want...)
		})
	}
	// The branch labels are kept for the view.
	tr := traceDoc(t, block(cases[len(cases)-1].yaml)).Traces()[0]
	if got := tr.Node("B1").Next; !reflect.DeepEqual(got, []TraceNext{{"B2", "small"}, {"B3", "large"}}) {
		t.Fatalf("branch %+v", got)
	}
}

func TestTraceValidation(t *testing.T) {
	many := "lanes: [a, b]\nnodes:\n"
	for i := 0; i <= MaxTraceNodes; i++ {
		many += fmt.Sprintf("  - {id: N%d, lane: a, text: t}\n", i)
	}
	cases := []struct{ name, yaml, node, want string }{
		{"yaml", "lanes: [a, b\nnodes: []\n", "", "YAML does not parse"},
		{"not a mapping", "- a\n- b\n", "", "must be a mapping"},
		{"no lanes", "nodes: []\n", "", "lanes is missing"},
		{"one lane", "lanes: [a]\nnodes: []\n", "", "at least two lanes"},
		{"duplicate id", "lanes: [a, b]\nnodes:\n  - {id: X, lane: a, text: t}\n  - {id: X, lane: a, text: u}\n", "X", "used twice"},
		{"bad id", "lanes: [a, b]\nnodes:\n  - {id: 1x, lane: a, text: t}\n", "1x", "must start with a letter"},
		{"unknown lane", "lanes: [a, b]\nnodes:\n  - {id: X, lane: c, text: t}\n", "X", `lane "c" is not in lanes`},
		{"no text", "lanes: [a, b]\nnodes:\n  - {id: X, lane: a}\n", "X", "text is missing"},
		{"next unknown", "lanes: [a, b]\nnodes:\n  - {id: X, lane: a, text: t, next: [Y]}\n", "X", "next names Y"},
		{"next across lanes", "lanes: [a, b]\nnodes:\n  - {id: X, lane: a, text: t, next: [Y]}\n  - {id: Y, lane: b, text: u}\n", "X", "next stays within one lane"},
		{"realizes down", "lanes: [a, b]\nnodes:\n  - {id: X, lane: a, text: t, realizes: [Y]}\n  - {id: Y, lane: b, text: u}\n", "X", "not more abstract"},
		{"realizes same lane", "lanes: [a, b]\nnodes:\n  - {id: X, lane: b, text: t, realizes: [Y]}\n  - {id: Y, lane: b, text: u}\n", "X", "not more abstract"},
		{"too many", many, "", "at most 40"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := traceDoc(t, block(c.yaml))
			tr := d.Traces()[0]
			if tr.Valid() || len(tr.Nodes) != 0 {
				t.Fatalf("accepted: %+v", tr)
			}
			e := tr.Errors[0]
			if !strings.Contains(e.Message, c.want) || e.Node != c.node || e.Line < tr.Line {
				t.Fatalf("error %+v", tr.Errors)
			}
			// Reported with block and node, as a warning: it never blocks a sync.
			p := d.Problems()[0]
			if p.Code != "trace-invalid" || p.Severity != Warning || !strings.HasPrefix(p.Message, "trace 1 (Import pipeline)") || d.HasErrors() {
				t.Fatalf("problem %+v", d.Problems())
			}
			if c.node != "" && !strings.Contains(p.Message, "node "+c.node+":") {
				t.Fatalf("problem lacks the node: %s", p.Message)
			}
		})
	}
	// Line numbers point into the file.
	d := traceDoc(t, block("lanes: [a, b]\nnodes:\n  - {id: X, lane: a, text: t}\n  - {id: Y, lane: c, text: u}\n"))
	if got := d.Traces()[0].Errors[0].Line; got != 13 {
		t.Fatalf("error line %d, want 13", got)
	}
	// Unknown keys are ignored.
	d = traceDoc(t, block("lanes: [a, b]\ntitle: extra\nnodes:\n  - {id: X, lane: a, text: t, owner: bob}\n  - {id: Y, lane: b, text: u, realizes: [X]}\n"))
	if !d.Traces()[0].Valid() {
		t.Fatal(d.Traces()[0].Errors)
	}
}

func TestTracePlacementAndNodeAnchors(t *testing.T) {
	ok := block("lanes: [a, b]\nnodes:\n  - {id: X, lane: a, text: t}\n  - {id: Y, lane: b, text: u, realizes: [X]}\n")
	src := "# T\n\n## First\n\n" + ok + "\n- item\n\n  " + strings.ReplaceAll(strings.TrimSuffix(ok, "\n"), "\n", "\n  ") + "\n\n## Second\n\n```yaml\nnot: a trace\n```\n\n## Review log\n\n"
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	e, err := Append(d, KindComment, "bob@corp.com", time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC), "first#Y", "", "", "Why here?\n\n```trace\nlanes: [a]\n```", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Append(d, KindComment, "bob@corp.com", time.Now(), "first#Z", "", "", "x", nil); !errors.Is(err, ErrNoAnchor) {
		t.Fatalf("unknown node accepted: %v", err)
	}
	if _, err := Append(d, KindComment, "bob@corp.com", time.Now(), "second#X", "", "", "x", nil); !errors.Is(err, ErrNoAnchor) {
		t.Fatalf("node under the wrong heading accepted: %v", err)
	}
	d, _ = d.Reparse()
	trs := d.Traces()
	// The fence in the entry body is not part of the body.
	if len(trs) != 2 || trs[0].Section != "first" || trs[0].Nested || !trs[1].Nested {
		t.Fatalf("traces %+v", trs)
	}
	if !d.HasAnchor("first#Y") || d.HasAnchor("first#Q") || d.HasAnchor("second#Y") {
		t.Fatal("HasAnchor")
	}
	// The same id twice under one heading is ambiguous.
	var codes []string
	for _, p := range d.Problems() {
		codes = append(codes, p.Code)
	}
	if !strings.Contains(strings.Join(codes, ","), "trace-ambiguous-id") {
		t.Fatalf("problems %v", codes)
	}

	// Removing the node orphans the comment.
	gone := strings.ReplaceAll(string(d.Bytes()), "{id: Y, ", "{id: Q, ")
	d2, _ := Parse([]byte(gone))
	if d2.HasAnchor("first#Y") {
		t.Fatal("node still found")
	}
	found := false
	for _, p := range d2.Problems() {
		if p.Code == "orphaned" && strings.Contains(p.Message, "no trace node") {
			found = true
		}
	}
	if !found || e.Node() != "Y" {
		t.Fatalf("no orphan warning: %+v", d2.Problems())
	}
}
