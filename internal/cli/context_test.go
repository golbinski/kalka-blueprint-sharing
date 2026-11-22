package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golbinski/kalka/internal/check"
)

// kalka context reads the plan nodes and domain nodes from the folder and
// prints them for an agent (spec 6.18).
func TestContextCommand(t *testing.T) {
	lib := filepath.Join("..", "..", "samples", "blueprints", "library")
	leaf := filepath.Join(lib, "return-creates-hold.md")
	code, out, errs := runCLI("context", leaf, "--domain", lib)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, want := range []string{"## Plan path", "jira:DEV-100 Holds and notification on return", "(this document)", "## Domain path", "### library@v2", "### library.reservations@v1"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	code, out, _ = runCLI("context", leaf, "--domain", lib, "--json")
	var c check.AgentContext
	if err := json.Unmarshal([]byte(out), &c); err != nil || code != exitOK || len(c.Plan) != 2 || len(c.Domains) != 2 || len(c.Missing) != 0 {
		t.Fatalf("json %d %v %+v", code, err, c)
	}
	// Without the folder: what is missing is said.
	code, out, _ = runCLI("context", leaf, "--domain", leaf, "--json")
	if err := json.Unmarshal([]byte(out), &c); err != nil || code != exitOK || len(c.Missing) == 0 {
		t.Fatalf("missing %d %v %+v", code, err, c.Missing)
	}
	if code, _, _ := runCLI("context"); code != exitUsage {
		t.Fatalf("no file: %d", code)
	}
}
