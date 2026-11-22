package blueprint

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func traceDoc(t *testing.T, blocks ...string) *Document {
	t.Helper()
	src := "---\nkalka_id: 7b0b3f5e-2c1a-4d8e-9f3b-1a2b3c4d5e6f\nkalka_owner: alice@library.example\n---\n# Import pipeline refactor\n\n## Import pipeline\n\n" +
		strings.Join(blocks, "\nSome prose.\n\n")
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// block wraps a block's content in a trace v2 fence.
func block(src string) string { return "```trace v2\n" + src + "```\n" }

// ty writes a block from a compact form: the lanes, then one step per
// string, "ID lane text" followed by " | key: value" parts in the flow
// style of YAML ("next: [B2]", "next: {B2: small}"), each written as v2
// attribute lines. Steps are grouped under their lanes, in order.
func ty(lanes string, nodes ...string) string {
	var b strings.Builder
	for li, lane := range strings.Split(lanes, ", ") {
		if li > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "lane %s\n", lane)
		for _, n := range nodes {
			parts := strings.Split(n, " | ")
			f := strings.SplitN(parts[0], " ", 3)
			if f[1] != lane {
				continue
			}
			fmt.Fprintf(&b, "step %s: %s\n", f[0], f[2])
			for _, p := range parts[1:] {
				b.WriteString(attrLines(p))
			}
		}
	}
	return b.String()
}

// attrLines turns "key: value" in YAML's flow style into v2 attributes.
func attrLines(p string) string {
	var m yaml.Node
	if err := yaml.Unmarshal([]byte(p), &m); err != nil || len(m.Content) == 0 {
		panic(fmt.Sprintf("attr %q: %v", p, err))
	}
	root := m.Content[0]
	var b strings.Builder
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i].Value, root.Content[i+1]
		switch v.Kind {
		case yaml.SequenceNode:
			for _, it := range v.Content {
				fmt.Fprintf(&b, "  %s %s\n", k, it.Value)
			}
		case yaml.MappingNode:
			for j := 0; j+1 < len(v.Content); j += 2 {
				if l := v.Content[j+1].Value; l != "" {
					fmt.Fprintf(&b, "  %s %s: %s\n", k, v.Content[j].Value, l)
				} else {
					fmt.Fprintf(&b, "  %s %s\n", k, v.Content[j].Value)
				}
			}
		default:
			fmt.Fprintf(&b, "  %s %s\n", k, v.Value)
		}
	}
	return b.String()
}

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

func tcodes(es []TraceError) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Code)
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

// Every example in the language specification is valid, draws no
// warning, and has exactly the divergences listed under it.
func TestTraceSpecExamples(t *testing.T) {
	raw, err := os.ReadFile("../../docs/trace.md")
	if err != nil {
		t.Fatal(err)
	}
	// The summary in the main specification has a valid example too.
	main, err := os.ReadFile("../../specification.md")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw) + "\n# The main specification\n" + string(main)
	fenceRe := regexp.MustCompile("(?s)\n```trace v2\n(.*?)\n```\n")
	itemRe := regexp.MustCompile("(?m)^- `([^`]+)`")
	matches := fenceRe.FindAllStringSubmatchIndex(src, -1)
	examples := 0
	for i, m := range matches {
		body := src[m[2]:m[3]] + "\n"
		d := traceDoc(t, block(body))
		tr := d.Traces()[0]
		if !tr.Valid() || len(tr.Warnings) > 0 {
			t.Fatalf("block %d: errors %+v warnings %+v", i+1, tr.Errors, tr.Warnings)
		}
		// The list follows "Divergences:" before the next fence or heading.
		rest := src[m[1]:]
		if j := strings.Index(rest, "\n#"); j >= 0 {
			rest = rest[:j]
		}
		k := strings.Index(rest, "Divergences:")
		if k < 0 {
			continue // the agents' guide repeats an example without a list
		}
		examples++
		after := rest[k+len("Divergences:"):]
		var want []string
		if !strings.HasPrefix(strings.TrimSpace(after), "none") {
			for _, it := range itemRe.FindAllStringSubmatch(after, -1) {
				want = append(want, it[1])
			}
		}
		wantDivs(t, tr, want...)
	}
	if examples < 6 {
		t.Fatalf("%d examples with divergences found", examples)
	}
}

func TestDivergenceClasses(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want []string
	}{
		{"unrealized", ty("business, code",
			"B1 business a | next: [B2]", "B2 business b",
			"C1 code x | realizes: [B1]"),
			[]string{"unrealized:B2"}},
		{"expanded", ty("business, code",
			"B1 business a",
			"C1 code x | realizes: [B1] | next: [C2]", "C2 code y | realizes: [B1]"),
			[]string{"expanded:B1[C1,C2]@code"}},
		// Interleaving is scattered, not reordered.
		{"scattered", ty("business, code",
			"B1 business a | next: [B2]", "B2 business b",
			"C1 code x | realizes: [B1] | next: [C2]", "C2 code y | realizes: [B2] | next: [C3]", "C3 code z | realizes: [B1]"),
			[]string{"expanded:B1[C1,C3]@code", "scattered:B1[C1,C3,C2]@code"}},
		{"reordered", ty("business, code",
			"B1 business validate | next: [B2]", "B2 business store",
			"C1 code insert | realizes: [B2] | next: [C2]", "C2 code check | realizes: [B1]"),
			[]string{"reordered:B1[B2,C1,C2]@code"}},
		{"implementation-only", ty("business, code",
			"B1 business a",
			"C1 code x | realizes: [B1] | next: [C2]", `C2 code retry | why: "the store times out"`),
			[]string{"implementation-only:C2"}},
		// Realizing one node in each of two lanes is not merged.
		{"merged", ty("business, code",
			"B1 business a | next: [B2]", "B2 business b",
			"C1 code both | realizes: [B1, B2]"),
			[]string{"merged:C1[B1,B2]@business"}},
		{"three lanes", ty("business, service, code",
			"B1 business Accept parcels | next: [B2]", "B2 business Deliver",
			"S1 service Parcel intake API | realizes: [B1] | next: [S2]", `S2 service Address validation | why: "the courier needs it"`,
			"C1 code handler | realizes: [S1] | next: [C2]", "C2 code delivery job | realizes: [B2]"),
			[]string{"implementation-only:S2", "unrealized:S2", "skipped:B2[C2]@service"}},
		// The arms of a branch do not order each other.
		{"branch", ty("business, code",
			`B1 business Check size | next: {B2: "small", B3: "large"}`, "B2 business Import now", "B3 business Queue",
			"C1 code size switch | realizes: [B1] | next: [C2, C3]", "C2 code enqueue | realizes: [B3] | next: [C3]", "C3 code import | realizes: [B2]"),
			nil},
		// Both arms realize one node: expanded, not scattered.
		{"diamond", ty("business, code",
			"B1 business store",
			`C1 code size switch | realizes: [B1] | next: {C2: "small", C3: "large"}`, "C2 code store small | realizes: [B1]", "C3 code store large | realizes: [B1]"),
			[]string{"expanded:B1[C1,C2,C3]@code"}},
		// A step that realizes nothing does not scatter the ones around it.
		{"implementation-only inside an expansion", ty("business, code",
			"B1 business store",
			"C1 code write | realizes: [B1] | next: [C2]", `C2 code wait | why: "rate limit" | next: [C3]`, "C3 code flush | realizes: [B1]"),
			[]string{"implementation-only:C2", "expanded:B1[C1,C3]@code"}},
		// A chain of four realized in reverse: three records, not six.
		{"reversed chain", ty("business, code",
			"B1 business a | next: [B2]", "B2 business b | next: [B3]", "B3 business c | next: [B4]", "B4 business d",
			"C4 code d | realizes: [B4] | next: [C3]", "C3 code c | realizes: [B3] | next: [C2]", "C2 code b | realizes: [B2] | next: [C1]", "C1 code a | realizes: [B1]"),
			[]string{"reordered:B1[B2,C2,C1]@code", "reordered:B2[B3,C3,C2]@code", "reordered:B3[B4,C4,C3]@code"}},
		// One step moved to the front: one record.
		{"one step moved first", ty("business, code",
			"B1 business a | next: [B2]", "B2 business b | next: [B3]", "B3 business c | next: [B4]", "B4 business d",
			"C4 code d | realizes: [B4] | next: [C1]", "C1 code a | realizes: [B1] | next: [C2]", "C2 code b | realizes: [B2] | next: [C3]", "C3 code c | realizes: [B3]"),
			[]string{"reordered:B3[B4,C4,C3]@code"}},
		// A loop: back takes no part in order.
		{"loop", ty("business, code",
			"A1 business fetch | next: [A2]", "A2 business import",
			"F1 code page | realizes: [A1] | next: [F2]", `F2 code import page | realizes: [A2] | back: {F1: "while pages remain"}`),
			nil},
		// Removed steps take no part, but order passes through them.
		{"removed", ty("business, code",
			"B1 business a | next: [B2]", "B2 business b | next: [B3]", "B3 business c | change: removed",
			"C1 code a | realizes: [B1] | next: [C2]", `C2 code guard | change: removed | next: [C3]`, `C3 code c | realizes: [B3] | why: "B3 goes, the code stays" | next: [C4]`, "C4 code b | realizes: [B2]"),
			[]string{"implementation-only:C3"}},
		{"removed realizer", ty("business, code",
			"B1 business a",
			"C1 code a | realizes: [B1] | change: removed"),
			[]string{"unrealized:B1"}},
		{"new steps", ty("business, code",
			"B1 business a | change: new",
			"C1 code a | realizes: [B1] | change: new"),
			nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := traceDoc(t, block(c.yaml)).Traces()[0]
			wantDivs(t, tr, c.want...)
			if len(tr.Warnings) > 0 {
				t.Fatalf("warnings %+v", tr.Warnings)
			}
		})
	}
	// Labels and back edges are kept for the view.
	tr := traceDoc(t, block(cases[7].yaml)).Traces()[0]
	if got := tr.Node("B1").Next; !reflect.DeepEqual(got, []TraceEdge{{"B2", "small"}, {"B3", "large"}}) {
		t.Fatalf("branch %+v", got)
	}
	tr = traceDoc(t, block(cases[12].yaml)).Traces()[0]
	if got := tr.Node("F2").Back; !reflect.DeepEqual(got, []TraceEdge{{"F1", "while pages remain"}}) {
		t.Fatalf("back %+v", got)
	}
}

// Text is free: commas, colons and quotes are ordinary characters, and
// values are texts, never numbers or booleans.
func TestTraceText(t *testing.T) {
	tr := traceDoc(t, block(`lane business
step B1: Check proof, email and expiry; add the member: "now"

lane code
step C1: yes
  why 404
  realizes B1
  ref gh:src/join.go#L1-L9
  next C2: queue: empty
step C2: no
  why x
`)).Traces()[0]
	if !tr.Valid() || len(tr.Warnings) > 0 {
		t.Fatalf("%+v %+v", tr.Errors, tr.Warnings)
	}
	if tr.Node("B1").Text != `Check proof, email and expiry; add the member: "now"` || tr.Node("C1").Text != "yes" || tr.Node("C1").Why != "404" ||
		!reflect.DeepEqual(tr.Node("C1").Realizes, []string{"B1"}) || tr.Node("C1").Ref != "gh:src/join.go#L1-L9" {
		t.Fatalf("nodes %+v", tr.Nodes)
	}
	if got := tr.Node("C1").Next; !reflect.DeepEqual(got, []TraceEdge{{"C2", "queue: empty"}}) {
		t.Fatalf("label %+v", got)
	}
}

func TestTraceErrors(t *testing.T) {
	two := "lane a\nstep A: t\n\nlane b\n"
	node := func(id, extra string) string {
		s := "step " + id + ": t\n"
		for _, l := range strings.Split(extra, "\n") {
			if l != "" {
				s += "  " + l + "\n"
			}
		}
		return s
	}
	many := "lane a\n"
	for i := 0; i <= MaxTraceNodes; i++ {
		many += node(fmt.Sprintf("N%d", i), "")
	}
	many += "lane b\n"
	cases := []struct{ name, info, src, code, node, want string }{
		{"newer version", "trace v3", two + node("X", ""), "S012", "", "needs a newer kalka"},
		{"older version", "trace v1", "lanes: [a, b]\n", "S012", "", "reads only trace v2"},
		{"not a version", "trace lanes", two + node("X", ""), "S012", "", "not a version"},
		{"tab", "", two + "step X: t\n\tnext A\n", "S001", "", "tab"},
		{"capital", "", two + "Step X: t\n", "S002", "", "lower case"},
		{"colon after keyword", "", two + "step X: t\n  why: because\n", "S003", "", "no colon after the keyword why"},
		{"third level", "", two + "step X: t\n    next A\n", "S004", "", "indented by 4"},
		{"no text", "", two + "step X:\n", "S005", "X", "no text"},
		{"no colon", "", two + "step X t\n", "S005", "", "step <name>: <text>"},
		{"attribute first", "", "  next A\n" + two, "S006", "", "no item above it"},
		{"empty value", "", two + "step X: t\n  next\n", "S007", "X", "no value"},
		{"attribute in column 0", "", two + "step X: t\nwhy because\n", "S008", "", "indent it"},
		{"list on a line", "", two + "step X: t\n  realizes A, B\n", "S009", "X", "one reference"},
		{"yaml list", "", "lanes: [a, b]\nnodes:\n  - id: X\n", "S010", "", "is YAML"},
		{"yaml item", "", two + "- id: X\n", "S010", "", "YAML list item"},
		{"comment", "", two + "# a remark\nstep X: t\n", "S011", "", "no comments"},
		{"why twice", "", two + node("X", "why a\nwhy b"), "S021", "X", "once per step"},
		{"change", "", two + node("X", "change added"), "S022", "X", "new, modified or removed"},
		{"bad id", "", two + node("1x", ""), "S030", "1x", "starts with a letter"},
		{"one lane", "", "lane a\n" + node("X", ""), "T011", "", "1 lanes"},
		{"six lanes", "", "lane a\nlane b\nlane c\nlane d\nlane e\nlane f\n" + node("X", ""), "T011", "", "6 lanes"},
		{"lane twice", "", "lane a\nlane a\n" + node("X", ""), "T012", "", "opened twice"},
		{"no steps", "", "lane a\nlane b\n", "T020", "", "no steps"},
		{"too many", "", many, "T022", "", "at most 40"},
		{"before a lane", "", node("X", "") + "lane a\nlane b\n", "T032", "X", "before any lane"},
		{"duplicate id", "", two + node("X", "") + node("X", ""), "T031", "X", "used twice"},
		{"next unknown", "", two + node("X", "next Y"), "T040", "X", "next names Y"},
		{"next across lanes", "", two + node("X", "next A"), "T041", "X", "within one lane"},
		{"next itself", "", two + node("X", "next X"), "T042", "X", "itself"},
		{"cycle", "", two + node("X", "next Y") + node("Y", "next Z") + node("Z", "next X"), "T043", "Z", "cycle through X, Y, Z; write the edge that returns as back"},
		{"back forward", "", two + node("X", "next Y\nback Y") + node("Y", ""), "T044", "X", "does not come before X"},
		{"realizes down", "", "lane a\n" + node("A", "realizes X") + "lane b\n" + node("X", ""), "T045", "A", "not above"},
		{"realizes same lane", "", two + node("X", "realizes Y") + node("Y", ""), "T045", "X", "not above"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := block(c.src)
			if c.info != "" {
				b = "```" + c.info + "\n" + c.src + "```\n"
			}
			d := traceDoc(t, b)
			tr := d.Traces()[0]
			if tr.Valid() || len(tr.Nodes) != 0 {
				t.Fatalf("accepted: %+v", tr)
			}
			e := tr.Errors[0]
			if e.Code != c.code || !strings.Contains(e.Message, c.want) || e.Node != c.node || e.Line < tr.Line {
				t.Fatalf("errors %+v", tr.Errors)
			}
			// Reported with block and step under its own rule, as an error
			// that never blocks a sync.
			p := d.Problems()[0]
			if p.Rule != c.code || p.Severity != Error || p.Blocks || !strings.HasPrefix(p.Message, "trace 1 (Import pipeline)") || d.HasErrors() {
				t.Fatalf("problem %+v", d.Problems())
			}
			if c.node != "" && !strings.Contains(p.Message, "step "+c.node+":") {
				t.Fatalf("problem lacks the step: %s", p.Message)
			}
		})
	}
	// The lexical errors carry a fix.
	tr := traceDoc(t, block(two+"step X: t\n  why: because\n")).Traces()[0]
	if f := tr.Errors[0].Fix; f == nil || f.Text != "  why because" || f.Line != tr.Line+6 {
		t.Fatalf("fix %+v", tr.Errors[0])
	}
	// A later stage does not run after an error: one error, not two.
	tr = traceDoc(t, block(node("X", "next Q")+"lane a\nlane b\n")).Traces()[0]
	if !reflect.DeepEqual(tcodes(tr.Errors), []string{"T032"}) {
		t.Fatalf("errors %+v", tr.Errors)
	}
	// Errors of one stage are collected.
	tr = traceDoc(t, block(two+node("X", "next Q")+node("Y", "next R"))).Traces()[0]
	if !reflect.DeepEqual(tcodes(tr.Errors), []string{"T040", "T040"}) {
		t.Fatalf("errors %+v", tr.Errors)
	}
	// Line numbers point into the file: the fence is line 9.
	tr = traceDoc(t, block(two+node("X", "")+node("Y", "next Q"))).Traces()[0]
	if got := tr.Errors[0].Line; got != 9+6 {
		t.Fatalf("error line %d, want 15", got)
	}
}

func TestTraceWarnings(t *testing.T) {
	ok := func(extra ...string) string {
		return ty("business, code", append([]string{"B1 business a"}, append([]string{"C1 code x | realizes: [B1]"}, extra...)...)...)
	}
	cases := []struct{ name, info, src, code, node string }{
		{"no version", "trace", ok(), "S013", ""},
		{"unknown keyword", "", ty("business, code", "B1 business a", "C1 code x | realises: [B1]"), "S020", ""},
		{"duplicate", "", ty("business, code", "B1 business a", "C1 code x | realizes: [B1, B1]"), "T104", "C1"},
		{"duplicate in back", "", ty("business, code", "B1 business a",
			"C1 code x | realizes: [B1] | next: [C2]", "C2 code y | realizes: [B1] | back: [C1, C1]"), "T104", "C2"},
		{"redundant link", "", ty("business, service, code", "B1 business a", "S1 service s | realizes: [B1]", "C1 code x | realizes: [S1, B1]"), "T105", "C1"},
		{"empty lane", "", ty("business, service, code", "B1 business a", "C1 code x | realizes: [B1]"), "T106", ""},
		{"disconnected lane", "", ty("business, code", "B1 business a | next: [B2]", "B2 business b", "C1 code x | realizes: [B1]", "C2 code y | realizes: [B2]"), "T107", "C2"},
		{"no why", "", ok("C2 code retry"), "T110", "C2"},
		{"case", "", ty("business, code", "B1 business a | next: [b1]", "b1 business b", "C1 code x | realizes: [B1, b1]"), "T111", "b1"},
		{"ref form", "", ty("business, code", "B1 business a", `C1 code x | realizes: [B1] | ref: "src/a.go"`), "T112", "C1"},
		{"some labels", "", ty("business, code", `B1 business a | next: {B2: "small", B3: ""}`, "B2 business b", "B3 business c",
			"C1 code x | realizes: [B1, B2, B3]"), "T113", "B1"},
		{"long text", "", ty("business, code", "B1 business "+strings.Repeat("a", 101), "C1 code x | realizes: [B1]"), "T109", "B1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := block(c.src)
			if c.info != "" {
				b = "```" + c.info + "\n" + c.src + "```\n"
			}
			d := traceDoc(t, b)
			tr := d.Traces()[0]
			if !tr.Valid() || len(tr.Divergences) == 0 && len(tr.Nodes) == 0 {
				t.Fatalf("not a trace: %+v", tr.Errors)
			}
			if !strings.Contains(strings.Join(tcodes(tr.Warnings), ","), c.code) {
				t.Fatalf("warnings %+v", tr.Warnings)
			}
			for _, w := range tr.Warnings {
				if w.Code == c.code && w.Node != c.node {
					t.Fatalf("warning on %q, want %q: %+v", w.Node, c.node, w)
				}
			}
			found := false
			for _, p := range d.Problems() {
				found = found || p.Rule == c.code && p.Severity == Warning
			}
			if !found || d.HasErrors() {
				t.Fatalf("problems %+v", d.Problems())
			}
		})
	}
	// The suggestion names the keyword meant; x- keywords are the author's.
	tr := traceDoc(t, block(ty("business, code", "B1 business a | x-owner: bob", "C1 code x | realises: [B1]"))).Traces()[0]
	if len(tr.Warnings) != 2 || !strings.Contains(tr.Warnings[0].Message, "did you mean realizes?") || tr.Warnings[1].Code != "T110" {
		t.Fatalf("warnings %+v", tr.Warnings)
	}
	// An unknown keyword is ignored, so the misspelt realizes leaves C1
	// implementation-only: the warning is what tells the author.
	wantDivs(t, tr, "implementation-only:C1", "unrealized:B1")

	many := []string{"B1 business a"}
	for i := 1; i <= AdvisedTraceNodes; i++ {
		next := ""
		if i < AdvisedTraceNodes {
			next = fmt.Sprintf(" | next: [C%d]", i+1)
		}
		many = append(many, fmt.Sprintf("C%d code x | realizes: [B1]%s", i, next))
	}
	tr = traceDoc(t, block(ty("business, code", many...))).Traces()[0]
	if !strings.Contains(strings.Join(tcodes(tr.Warnings), ","), "T108") {
		t.Fatalf("warnings %+v", tr.Warnings)
	}
}

func TestTracePlacementAndNodeAnchors(t *testing.T) {
	ok := block(ty("a, b", "X a t", "Y b u | realizes: [X]"))
	src := "# T\n\n## First\n\n" + ok + "\n- item\n\n  " + strings.ReplaceAll(strings.TrimSuffix(ok, "\n"), "\n", "\n  ") +
		"\n\n## Second\n\n```yaml\nlanes: [a, b]\nnodes: []\n```\n\n## Review log\n\n"
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	e, err := Append(d, KindComment, "bob@library.example", time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC), "first#Y", "", "", "Why here?\n\n```trace v2\nlane a\n```", Signer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Append(d, KindComment, "bob@library.example", time.Now(), "first#Z", "", "", "x", Signer{}); !errors.Is(err, ErrNoAnchor) {
		t.Fatalf("unknown node accepted: %v", err)
	}
	if _, err := Append(d, KindComment, "bob@library.example", time.Now(), "second#X", "", "", "x", Signer{}); !errors.Is(err, ErrNoAnchor) {
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
	// The same id twice under one heading is ambiguous (T120).
	if got := tcodes(trs[1].Warnings); !reflect.DeepEqual(got, []string{"T120", "T120"}) {
		t.Fatalf("warnings %v", got)
	}
	var ids []string
	for _, p := range d.Problems() {
		ids = append(ids, p.Rule)
	}
	if all := strings.Join(ids, " "); strings.Count(all, "T120") != 2 {
		t.Fatalf("problems %s", all)
	}

	// Removing the node orphans the comment.
	gone := strings.ReplaceAll(string(d.Bytes()), "step Y:", "step Q:")
	d2, _ := Parse([]byte(gone))
	if d2.HasAnchor("first#Y") {
		t.Fatal("node still found")
	}
	found := false
	for _, p := range d2.Problems() {
		if p.Rule == "B015" && strings.Contains(p.Message, "no trace node") {
			found = true
		}
	}
	if !found || e.Node() != "Y" {
		t.Fatalf("no orphan warning: %+v", d2.Problems())
	}

	// A block with no heading above it cannot be commented on (T121).
	d3, _ := Parse([]byte(ok))
	if got := tcodes(d3.Traces()[0].Warnings); !reflect.DeepEqual(got, []string{"T121"}) {
		t.Fatalf("warnings %v", got)
	}
}

// specTrace is the import example of the specification.
var specTrace = block(ty("business, code",
	"D1 business Receive uploaded files | next: [D2]", "D2 business Extract text | next: [D3]",
	"D3 business Classify the document | next: [D4]", "D4 business Notify the user",
	`I1 code Upload endpoint handlers | realizes: [D1] | ref: "gh:src/upload/handlers.py#L40-L120" | next: [I2]`,
	`I2 code Skip files larger than 20 MB | ref: "gh:src/import/size_guard.py#L12-L48" | why: "the parser runs out of memory" | next: [I3]`,
	`I3 code Extract and classify in one pass | realizes: [D2, D3] | ref: "gh:src/import/parse.py#L200-L310" | next: [I4]`,
	`I4 code Batch notifications | realizes: [D4] | ref: "gh:src/notify/batch.py#L15-L90"`))
