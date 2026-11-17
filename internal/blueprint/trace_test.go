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

// block wraps YAML in a trace v1 fence.
func block(yaml string) string { return "```trace v1\n" + yaml + "```\n" }

// ty writes a block in the house style: lanes, then one node per string,
// "ID lane text" followed by " | key: value" parts, one per line.
func ty(lanes string, nodes ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "lanes: [%s]\nnodes:\n", lanes)
	for _, n := range nodes {
		parts := strings.Split(n, " | ")
		f := strings.SplitN(parts[0], " ", 3)
		fmt.Fprintf(&b, "  - id: %s\n    lane: %s\n    text: %q\n", f[0], f[1], f[2])
		for _, p := range parts[1:] {
			fmt.Fprintf(&b, "    %s\n", p)
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
	fenceRe := regexp.MustCompile("(?s)\n```trace v1\n(.*?)\n```\n")
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

// The old flow style lost text at a comma without a word; the block style
// with quoted text keeps it, and scalars are text, never numbers or
// booleans.
func TestTraceText(t *testing.T) {
	tr := traceDoc(t, block(`lanes: [business, code]
nodes:
  - id: B1
    lane: business
    text: "Check proof, email and expiry; add the member"
  - id: C1
    lane: code
    text: yes
    why: 404
    realizes: B1
    ref: "gh:src/join.go#L1-L9"
`)).Traces()[0]
	if !tr.Valid() || len(tr.Warnings) > 0 {
		t.Fatalf("%+v %+v", tr.Errors, tr.Warnings)
	}
	if tr.Node("B1").Text != "Check proof, email and expiry; add the member" || tr.Node("C1").Text != "yes" || tr.Node("C1").Why != "404" || !reflect.DeepEqual(tr.Node("C1").Realizes, []string{"B1"}) {
		t.Fatalf("nodes %+v", tr.Nodes)
	}
	// Folded text becomes one line.
	tr = traceDoc(t, block("lanes: [a, b]\nnodes:\n  - id: X\n    lane: a\n    text: >\n      two\n      lines\n  - id: Y\n    lane: b\n    text: \"u\"\n    realizes: [X]\n")).Traces()[0]
	if tr.Node("X").Text != "two lines" {
		t.Fatalf("text %q", tr.Node("X").Text)
	}
}

func TestTraceErrors(t *testing.T) {
	two := "lanes: [a, b]\nnodes:\n"
	node := func(id, lane, extra string) string {
		s := "  - id: " + id + "\n    lane: " + lane + "\n    text: \"t\"\n"
		if extra != "" {
			s += "    " + strings.ReplaceAll(extra, "\n", "\n    ") + "\n"
		}
		return s
	}
	many := two
	for i := 0; i <= MaxTraceNodes; i++ {
		many += node(fmt.Sprintf("N%d", i), "a", "")
	}
	cases := []struct{ name, info, yaml, code, node, want string }{
		{"newer version", "trace v2", two + node("X", "a", ""), "T005", "", "needs a newer kalka"},
		{"not a version", "trace lanes", two + node("X", "a", ""), "T005", "", "not a version"},
		{"yaml", "", "lanes: [a, b\nnodes: []\n", "T001", "", "does not parse"},
		{"alias", "", "lanes: &l [a, b]\nnodes:\n" + node("X", "a", ""), "T002", "", "anchors"},
		{"two documents", "", two + node("X", "a", "") + "---\nx: 1\n", "T002", "", "more than one YAML document"},
		{"tag", "", "lanes: !!seq [a, b]\nnodes:\n" + node("X", "a", ""), "T002", "", "tags"},
		{"repeated key", "", two + node("X", "a", "text: \"again\""), "T003", "", "text appears twice"},
		{"not a mapping", "", "- a\n- b\n", "T004", "", "must be a mapping"},
		{"no lanes", "", "nodes:\n" + node("X", "a", ""), "T010", "", "lanes is missing"},
		{"one lane", "", "lanes: [a]\nnodes:\n" + node("X", "a", ""), "T011", "", "1 lanes"},
		{"six lanes", "", "lanes: [a, b, c, d, e, f]\nnodes:\n" + node("X", "a", ""), "T011", "", "6 lanes"},
		{"lane twice", "", "lanes: [a, a]\nnodes:\n" + node("X", "a", ""), "T012", "", "listed twice"},
		{"empty nodes", "", "lanes: [a, b]\nnodes: []\n", "T020", "", "nodes is empty"},
		{"flow node", "", two + "  - {id: X, lane: a, text: t}\n", "T021", "", "block mapping"},
		{"too many", "", many, "T022", "", "at most 40"},
		{"no id", "", two + "  - lane: a\n    text: \"t\"\n", "T030", "", "no id"},
		{"bad id", "", two + node("1x", "a", ""), "T030", "1x", "starts with a letter"},
		{"duplicate id", "", two + node("X", "a", "") + node("X", "a", ""), "T031", "X", "used twice"},
		{"unknown lane", "", two + node("X", "c", ""), "T032", "X", `lane "c" is not in lanes`},
		{"no text", "", two + "  - id: X\n    lane: a\n", "T033", "X", "text is missing"},
		{"old label form", "", two + node("X", "a", "next: [{to: Y, label: l}]") + node("Y", "a", ""), "T034", "X", "mapping of id to label"},
		{"realizes mapping", "", two + node("X", "a", "") + node("Y", "b", "realizes: {X: \"part\"}"), "T034", "Y", "realizes is a list"},
		{"text list", "", two + "  - id: X\n    lane: a\n    text: [t]\n", "T034", "", "single text"},
		{"next unknown", "", two + node("X", "a", "next: [Y]"), "T040", "X", "next names Y"},
		{"next across lanes", "", two + node("X", "a", "next: [Y]") + node("Y", "b", ""), "T041", "X", "within one lane"},
		{"next itself", "", two + node("X", "a", "next: [X]"), "T042", "X", "itself"},
		{"cycle", "", two + node("X", "a", "next: [Y]") + node("Y", "a", "next: [Z]") + node("Z", "a", "next: [X]"), "T043", "Z", "cycle through X, Y, Z; write the edge that returns under back"},
		{"back forward", "", two + node("X", "a", "next: [Y]\nback: [Y]") + node("Y", "a", ""), "T044", "X", "does not come before X"},
		{"realizes down", "", two + node("X", "a", "realizes: [Y]") + node("Y", "b", ""), "T045", "X", "not above"},
		{"realizes same lane", "", two + node("X", "b", "realizes: [Y]") + node("Y", "b", ""), "T045", "X", "not above"},
		{"change", "", two + node("X", "a", "change: added"), "T046", "X", "new, modified or removed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := block(c.yaml)
			if c.info != "" {
				b = "```" + c.info + "\n" + c.yaml + "```\n"
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
			// Reported with block, node and code, as a warning: it never
			// blocks a sync.
			p := d.Problems()[0]
			if p.Code != "trace-invalid" || p.Severity != Warning || !strings.HasPrefix(p.Message, "trace 1 (Import pipeline)") || !strings.Contains(p.Message, c.code+": ") || d.HasErrors() {
				t.Fatalf("problem %+v", d.Problems())
			}
			if c.node != "" && !strings.Contains(p.Message, "node "+c.node+":") {
				t.Fatalf("problem lacks the node: %s", p.Message)
			}
		})
	}
	// A later stage does not run after an error: one error, not two.
	tr := traceDoc(t, block(two+node("X", "c", "next: [Q]"))).Traces()[0]
	if !reflect.DeepEqual(tcodes(tr.Errors), []string{"T032"}) {
		t.Fatalf("errors %+v", tr.Errors)
	}
	// Errors of one stage are collected.
	tr = traceDoc(t, block(two+node("X", "c", "")+node("Y", "d", ""))).Traces()[0]
	if !reflect.DeepEqual(tcodes(tr.Errors), []string{"T032", "T032"}) {
		t.Fatalf("errors %+v", tr.Errors)
	}
	// Line numbers point into the file: the fence is line 9.
	tr = traceDoc(t, block(two+node("X", "a", "")+node("Y", "c", ""))).Traces()[0]
	if got := tr.Errors[0].Line; got != 9+6 {
		t.Fatalf("error line %d, want 15", got)
	}
	tr = traceDoc(t, block("lanes: [a, b]\nnodes:\n  - id: X\n    lane: a\n    text: \"t\n")).Traces()[0]
	if tr.Errors[0].Code != "T001" || tr.Errors[0].Line <= 9 {
		t.Fatalf("yaml error %+v", tr.Errors)
	}
}

func TestTraceWarnings(t *testing.T) {
	ok := func(extra ...string) string {
		return ty("business, code", append([]string{"B1 business a"}, append([]string{"C1 code x | realizes: [B1]"}, extra...)...)...)
	}
	cases := []struct{ name, info, yaml, code, node string }{
		{"no version", "trace", ok(), "T101", ""},
		{"unknown key", "", ty("business, code", "B1 business a", "C1 code x | realises: [B1]"), "T102", "C1"},
		{"comment after a value", "", strings.Replace(ok(), "lanes: [business, code]", "lanes: [business, code] # levels", 1), "T103", ""},
		{"duplicate in a list", "", ty("business, code", "B1 business a", "C1 code x | realizes: [B1, B1]"), "T104", "C1"},
		{"duplicate in back", "", ty("business, code", "B1 business a",
			"C1 code x | realizes: [B1] | next: [C2]", "C2 code y | realizes: [B1] | back: [C1, C1]"), "T104", "C2"},
		{"redundant link", "", ty("business, service, code", "B1 business a", "S1 service s | realizes: [B1]", "C1 code x | realizes: [S1, B1]"), "T105", "C1"},
		{"empty lane", "", ty("business, service, code", "B1 business a", "C1 code x | realizes: [B1]"), "T106", ""},
		{"disconnected lane", "", ty("business, code", "B1 business a | next: [B2]", "B2 business b", "C1 code x | realizes: [B1]", "C2 code y | realizes: [B2]"), "T107", "C2"},
		{"no why", "", ok("C2 code retry"), "T110", "C2"},
		{"case", "", ty("business, code", "B1 business a | next: [b1]", "b1 business b", "C1 code x | realizes: [B1, b1]"), "T111", "b1"},
		{"ref form", "", ty("business, code", "B1 business a", `C1 code x | realizes: [B1] | ref: "src/a.go L10"`), "T112", "C1"},
		{"some labels", "", ty("business, code", `B1 business a | next: {B2: "small", B3: ""}`, "B2 business b", "B3 business c",
			"C1 code x | realizes: [B1, B2, B3]"), "T113", "B1"},
		{"long text", "", ty("business, code", "B1 business "+strings.Repeat("a", 101), "C1 code x | realizes: [B1]"), "T109", "B1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := block(c.yaml)
			if c.info != "" {
				b = "```" + c.info + "\n" + c.yaml + "```\n"
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
				found = found || p.Code == "trace-warning" && strings.Contains(p.Message, c.code+": ")
			}
			if !found || d.HasErrors() {
				t.Fatalf("problems %+v", d.Problems())
			}
		})
	}
	// The suggestion names the key meant; x- keys are the author's.
	tr := traceDoc(t, block(ty("business, code", "B1 business a | x-owner: bob", "C1 code x | realises: [B1]"))).Traces()[0]
	if len(tr.Warnings) != 2 || !strings.Contains(tr.Warnings[0].Message, "did you mean realizes?") || tr.Warnings[1].Code != "T110" {
		t.Fatalf("warnings %+v", tr.Warnings)
	}
	// An unknown key is ignored, so the misspelt realizes leaves C1
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
		"\n\n## Second\n\n```yaml\nlanes: [a, b]\nnodes: []\n```\n\n```yaml\nnot: a trace\n```\n\n## Review log\n\n"
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	e, err := Append(d, KindComment, "bob@library.example", time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC), "first#Y", "", "", "Why here?\n\n```trace v1\nlanes: [a]\n```", Signer{})
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
	// The same id twice under one heading is ambiguous (T120); a yaml
	// block with lanes and nodes was probably meant as a trace (T122).
	if got := tcodes(trs[1].Warnings); !reflect.DeepEqual(got, []string{"T120", "T120"}) {
		t.Fatalf("warnings %v", got)
	}
	var msgs []string
	for _, p := range d.Problems() {
		msgs = append(msgs, p.Message)
	}
	if all := strings.Join(msgs, "\n"); strings.Count(all, "T122") != 1 || strings.Count(all, "T120") != 2 {
		t.Fatalf("problems\n%s", all)
	}

	// Removing the node orphans the comment.
	gone := strings.ReplaceAll(string(d.Bytes()), "id: Y\n", "id: Q\n")
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
