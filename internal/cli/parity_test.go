package cli

// Parity (spec 12.2): parity.json against the command table.
// The web side (web/src/parity.test.ts) checks it against the controls.

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

//go:embed parity.json
var parityJSON []byte

type parityRow struct {
	Command string `json:"command"`
	Class   string `json:"class"`
	Action  string `json:"action"`
	Place   string `json:"place"`
	Exempt  string `json:"exempt"`
}

func parityRows(t *testing.T) []parityRow {
	t.Helper()
	var f struct {
		Rows []parityRow `json:"rows"`
	}
	if err := json.Unmarshal(parityJSON, &f); err != nil {
		t.Fatalf("parity.json: %v", err)
	}
	return f.Rows
}

func TestParityTable(t *testing.T) {
	rows := parityRows(t)
	actionID := regexp.MustCompile(`^[a-z]+\.[a-z]+$`)
	byCommand := map[string][]parityRow{}
	lines := map[string]bool{}
	for _, r := range rows {
		if lines[r.Command] {
			t.Errorf("%q has two rows", r.Command)
		}
		lines[r.Command] = true
		switch r.Class {
		case ClassWorkflow:
			if !actionID.MatchString(r.Action) {
				t.Errorf("workflow %q has no action id (%q)", r.Command, r.Action)
			}
		case ClassView:
			if r.Place == "" {
				t.Errorf("view %q has no place in the UI", r.Command)
			}
			if r.Action != "" && !actionID.MatchString(r.Action) {
				t.Errorf("view %q: bad action id %q", r.Command, r.Action)
			}
		case ClassProcess:
			if r.Exempt == "" || r.Action != "" {
				t.Errorf("process %q needs the reason it is exempt, and no action", r.Command)
			}
		default:
			t.Errorf("%q: class %q", r.Command, r.Class)
		}
		top := strings.Fields(r.Command)[0]
		if _, ok := commandByName(top); !ok {
			t.Errorf("parity.json names %q, which is not in the command table", top)
		}
		byCommand[top] = append(byCommand[top], r)
	}
	// Every command has a row, and its class in the table is what its
	// rows say: workflow if any line of it is one.
	for _, c := range commands {
		rs := byCommand[c.name]
		if len(rs) == 0 {
			t.Errorf("command %q has no row in parity.json", c.name)
			continue
		}
		want := ClassView
		for _, r := range rs {
			if r.Class == ClassWorkflow {
				want = ClassWorkflow
			}
			if r.Class == ClassProcess {
				want = ClassProcess
			}
		}
		if c.class != want {
			t.Errorf("command %q is %s in the table, %s in parity.json", c.name, c.class, want)
		}
	}
}

// Every command line that is not a process accepts --json. Asking for
// --help never acts, so this runs each one in process, with no daemon
// allowed to start.
func TestParityJSONFlag(t *testing.T) {
	t.Setenv("KALKA_CONFIG_DIR", t.TempDir())
	t.Setenv("KALKA_NO_START", "1")
	for _, r := range parityRows(t) {
		if r.Class == ClassProcess {
			continue
		}
		args := append(strings.Fields(r.Command), "--json", "--help")
		var out, errs strings.Builder
		code := run(args, strings.NewReader(""), &out, &errs, time.Now)
		// The general usage mentions --json too: it must be the
		// command's own flags that do.
		if strings.Contains(errs.String(), "unknown command") || strings.Contains(errs.String(), "make sure the daemon runs") ||
			!strings.Contains(errs.String(), "-json") {
			t.Errorf("%s: no --json in its flags (exit %d):\n%s%s", r.Command, code, out.String(), errs.String())
		}
	}
}
