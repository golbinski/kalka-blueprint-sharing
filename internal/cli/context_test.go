package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golbinski/kalka/internal/check"
)

// kalka context reads the plan nodes and domain nodes from the folder and
// prints what the document's work needs for an agent (spec 6.18); --full
// prints everything on the paths, --term one term.
func TestContextCommand(t *testing.T) {
	lib := filepath.Join("..", "..", "samples", "blueprints", "library")
	leaf := filepath.Join(lib, "return-creates-hold.md")
	code, out, errs := runCLI("context", leaf, "--domain", lib)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, want := range []string{"## Focus: DEV-123", "## Gaps and boundaries", "### DEV-100 Holds and notification on return (parent)", "- Left out: G3.", "### Domain path team-lending/library.reservations@v1", "Terms left out: branch."} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	code, out, _ = runCLI("context", leaf, "--domain", lib, "--json")
	var c check.AgentContext
	if err := json.Unmarshal([]byte(out), &c); err != nil || code != exitOK || c.Mode != check.ContextFocused || len(c.Goals) != 1 || len(c.Invariants.Domains) != 1 || len(c.Gaps.NotHere) != 0 {
		t.Fatalf("json %d %v %+v", code, err, c)
	}
	_, again, _ := runCLI("context", leaf, "--domain", lib, "--json")
	if again != out {
		t.Error("the JSON differs between two runs")
	}
	// Without the folder: what is missing is said.
	code, out, _ = runCLI("context", leaf, "--domain", leaf, "--json")
	if err := json.Unmarshal([]byte(out), &c); err != nil || code != exitOK || len(c.Gaps.NotHere) == 0 {
		t.Fatalf("missing %d %v %+v", code, err, c.Gaps.NotHere)
	}
	// --full: every goal, with the why and verified-by lines.
	code, out, _ = runCLI("context", leaf, "--domain", lib, "--full")
	if code != exitOK || !strings.Contains(out, "- goal G3: Members can see their queue position") || !strings.Contains(out, "  - why two members cannot hold the same physical item") || !strings.Contains(out, "- library#branch: ") {
		t.Errorf("full:\n%s", out)
	}
	// --term: one term with its narrowings.
	code, out, _ = runCLI("context", leaf, "--domain", lib, "--term", "hold")
	if code != exitOK || !strings.HasPrefix(out, "# Term library.reservations#hold\n") || !strings.Contains(out, "hold: A copy set aside for the queue head after a return") {
		t.Errorf("term:\n%s", out)
	}
	if code, _, _ := runCLI("context", leaf, "--domain", lib, "--term", "fee"); code == exitOK {
		t.Error("an unknown term")
	}
	if code, _, _ := runCLI("context", leaf, "--full", "--term", "hold"); code != exitUsage {
		t.Errorf("--full with --term: %d", code)
	}
	if code, _, _ := runCLI("context"); code != exitUsage {
		t.Fatalf("no file: %d", code)
	}
}

// kalka graph prints the lens the graph canvas draws, the same bytes for
// the same files (spec 6.18, 11.1).
func TestGraphCommand(t *testing.T) {
	lib := filepath.Join("..", "..", "samples", "blueprints", "library")
	leaf := filepath.Join(lib, "return-creates-hold.md")
	code, out, errs := runCLI("graph", leaf, "--domain", lib)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, want := range []string{"Domains:\n  library@v2\n  library.reservations@v1", "DEV-123 Return creates a hold and notifies the queue head (focus)", "3 open in the documents held here", "goal G3 is unclaimed", "step C5: implementation-only"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	_, a, _ := runCLI("graph", leaf, "--domain", lib, "--json")
	_, b, _ := runCLI("graph", leaf, "--domain", lib, "--json")
	var g check.Graph
	if err := json.Unmarshal([]byte(a), &g); err != nil || len(g.Open) != 3 || a != b {
		t.Fatalf("json %v %d open, same %v", err, len(g.Open), a == b)
	}
	if code, _, _ := runCLI("graph"); code != exitUsage {
		t.Fatalf("no file: %d", code)
	}
}
