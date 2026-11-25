package cli

// The commands (teams, waiting, promote, seed backlink, team
// uninvite, team sync, settings), each with --json, views without a
// daemon, and the error kinds of the spec.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/golbinski/kalka/internal/core"
)

func TestNewCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("starts daemons in subprocesses")
	}
	w := newCLIWorld(t)
	plan := filepath.Join(w.folder, "plan.md")
	if err := os.WriteFile(plan, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	asJSON := func(what, out string, v any) {
		t.Helper()
		if err := json.Unmarshal([]byte(out), v); err != nil {
			t.Fatalf("%s: not JSON: %v\n%s", what, err, out)
		}
	}

	// Views with no daemon: from the files, none started.
	var teams struct {
		Daemon bool            `json:"daemon"`
		Teams  []core.TeamView `json:"teams"`
	}
	code, out, errs := w.kalka("teams", "--json")
	asJSON("teams", out, &teams)
	if code != 0 || teams.Daemon || len(teams.Teams) != 0 {
		t.Fatalf("teams: %d %s %s", code, out, errs)
	}
	var waiting struct {
		Daemon bool               `json:"daemon"`
		Items  []core.WaitingItem `json:"items"`
	}
	code, out, _ = w.kalka("waiting", "--json")
	asJSON("waiting", out, &waiting)
	if code != 0 || waiting.Daemon || waiting.Items == nil || len(waiting.Items) != 0 {
		t.Fatalf("waiting: %d %s", code, out)
	}
	var set settingsView
	code, out, _ = w.kalka("settings", "--json")
	asJSON("settings", out, &set)
	if code != 0 || set.Daemon || !set.Notifications || !set.KeepRunningOnClose {
		t.Fatalf("settings: %d %s", code, out)
	}
	if code, out, _ = w.kalka("transfers", "--json"); code != 0 || !strings.Contains(out, `"transfers": []`) || !strings.Contains(out, `"daemon": false`) {
		t.Fatalf("transfers: %d %s", code, out)
	}
	if code, out, _ = w.kalka("seeds", "--team", "personal", "--json"); code != 0 || !strings.Contains(out, `"seeds": []`) {
		t.Fatalf("seeds: %d %s", code, out)
	}
	if code, out, _ = w.kalka("team", "members", "--json"); code != 1 || !strings.Contains(out, `"kind": "no-team"`) {
		t.Fatalf("members with no team: %d %s", code, out)
	}
	if code, out, _ = w.kalka("waiting", "--team", "lending", "--json"); code != 1 || !strings.Contains(out, `"kind": "unknown-team"`) {
		t.Fatalf("waiting in a team not held: %d %s", code, out)
	}
	if w.running() {
		t.Fatal("a view started a daemon")
	}

	// Daemon commands.
	for _, team := range []string{"lending", "returns"} {
		if code, out, errs := w.kalka("team", "create", team, "--json"); code != 0 {
			t.Fatalf("team create: %d %s %s", code, out, errs)
		}
	}
	code, out, _ = w.kalka("teams", "--json")
	asJSON("teams", out, &teams)
	if code != 0 || !teams.Daemon || len(teams.Teams) != 2 || teams.Teams[0].Me != "owner" {
		t.Fatalf("teams with a daemon: %d %s", code, out)
	}
	for _, c := range []struct {
		args []string
		kind string
	}{
		{[]string{"team", "members", "--json"}, core.KindAmbiguousTeam},
		{[]string{"waiting", "--team", "nowhere", "--json"}, core.KindUnknownTeam},
		{[]string{"team", "remove", "alice@corp.com", "--team", "lending", "--json"}, core.KindLastOwner},
		{[]string{"team", "uninvite", "no-such-invite", "--team", "lending", "--json"}, core.KindError},
	} {
		if code, out, _ := w.kalka(c.args...); code != 1 || !strings.Contains(out, `"kind": "`+c.kind+`"`) {
			t.Fatalf("%v: %d %s, want kind %s", c.args, code, out, c.kind)
		}
	}

	var inv core.InviteResult
	code, out, _ = w.kalka("team", "invite", "bob@corp.com", "--team", "lending", "--json")
	asJSON("invite", out, &inv)
	if code != 0 || inv.ID == "" {
		t.Fatalf("invite: %d %s", code, out)
	}
	if code, out, errs := w.kalka("team", "uninvite", inv.ID, "--team", "lending", "--json"); code != 0 || !strings.Contains(out, inv.ID) {
		t.Fatalf("uninvite: %d %s %s", code, out, errs)
	}
	var tv core.TeamView
	_, out, _ = w.kalka("team", "invites", "--team", "lending", "--json")
	asJSON("invites", out, &tv)
	if len(tv.Invites) != 1 || tv.Invites[0].State != "canceled" {
		t.Fatalf("after uninvite: %+v", tv.Invites)
	}

	var st core.TeamStatus
	code, out, errs = w.kalka("team", "sync", "--team", "lending", "--json")
	asJSON("sync", out, &st)
	if code != 0 || st.Tried != 0 {
		t.Fatalf("sync alone: %d %s %s", code, out, errs)
	}

	code, out, _ = w.kalka("settings", "--notifications", "off", "--json")
	asJSON("settings", out, &set)
	if code != 0 || !set.Daemon || set.Notifications || !set.KeepRunningOnClose {
		t.Fatalf("settings off: %d %s", code, out)
	}
	if code, _, _ := w.kalka("settings", "--notifications", "maybe"); code != exitUsage {
		t.Fatalf("a bad value: %d", code)
	}

	// The seed commands: bud one, add the backlink (there already: a
	// file command), promote (through the daemon).
	var seed map[string]string
	code, out, errs = w.kalka("seed", plan, "--title", "Overdue reminders", "-m", "Later.", "--json")
	asJSON("seed", out, &seed)
	if code != 0 {
		t.Fatalf("seed: %s", errs)
	}
	var bl struct {
		Entry string `json:"entry"`
		Added bool   `json:"added"`
	}
	code, out, errs = w.kalka("seed", "backlink", seed["seed"], "--json")
	asJSON("backlink", out, &bl)
	if code != 0 || bl.Added {
		t.Fatalf("backlink: %d %s %s", code, out, errs)
	}
	code, out, errs = w.kalka("promote", seed["seed"], "--json")
	if code != 0 || !strings.Contains(out, `"status": "draft"`) {
		t.Fatalf("promote: %d %s %s", code, out, errs)
	}
	if raw, _ := os.ReadFile(seed["seed"]); strings.Contains(string(raw), "kalka_kind: seed") {
		t.Fatalf("still a seed:\n%s", raw)
	}

	var seen struct {
		Cleared bool `json:"cleared"`
	}
	code, out, _ = w.kalka("waiting", "--seen", plan, "--json")
	asJSON("seen", out, &seen)
	if code != 0 || seen.Cleared {
		t.Fatalf("seen: %d %s", code, out)
	}
}

// Every command is in the table with a class, and the table holds
// nothing else.
func TestCommandTable(t *testing.T) {
	classes := map[string]bool{ClassWorkflow: true, ClassView: true, ClassProcess: true}
	seen := map[string]bool{}
	for _, c := range commands {
		if !classes[c.class] || c.run == nil || seen[c.name] {
			t.Fatalf("bad row %+v", c)
		}
		seen[c.name] = true
	}
	for _, want := range []string{"teams", "waiting", "promote", "settings", "team", "seed", "join", "status"} {
		if !seen[want] {
			t.Fatalf("%s is not in the table", want)
		}
	}
	if got := commandNames(); !sort.StringsAreSorted(got) || len(got) != len(commands) {
		t.Fatalf("%v", got)
	}
}

// kalka close is gone with the delivered states: a usage error (spec
// 6.18); approve takes its place.
func TestCloseIsGone(t *testing.T) {
	if code, _, errs := runCLI("close", "plan.md"); code != exitUsage || !strings.Contains(errs, "unknown command") {
		t.Fatalf("close: exit %d %s", code, errs)
	}
	if _, ok := commandByName("approve"); !ok {
		t.Fatal("no approve command")
	}
}

// The error kinds: every kind the code declares is in the spec's list,
// and every one in the list is declared (spec 12.3).
func TestErrorKinds(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "core", "errors.go"))
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s+Kind[A-Za-z]+\s*=\s*"([a-z-]+)"`).FindAllStringSubmatch(string(src), -1) {
		declared[m[1]] = true
	}
	spec, err := os.ReadFile(filepath.Join("..", "..", "specification.md"))
	if err != nil {
		t.Fatal(err)
	}
	listed := specKinds(t, string(spec))
	for k := range declared {
		if !listed[k] {
			t.Errorf("kind %q is not in the spec's list (12.3)", k)
		}
	}
	for k := range listed {
		if !declared[k] {
			t.Errorf("the spec lists %q, which no code declares", k)
		}
	}
	// Each one is produced somewhere outside its declaration.
	var code strings.Builder
	for _, dir := range []string{"../core", "../daemon", "."} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			raw, _ := os.ReadFile(f)
			code.Write(raw)
		}
	}
	all := code.String()
	for _, m := range regexp.MustCompile(`(?m)^\s+(Kind[A-Za-z]+)\s*=\s*"`).FindAllStringSubmatch(string(src), -1) {
		if strings.Count(all, m[1]) < 2 {
			t.Errorf("%s is declared but nothing returns it", m[1])
		}
	}
}

// specKinds reads the kinds from spec 12.3: the backquoted words in the
// first column of the table between the error-kinds markers.
func specKinds(t *testing.T, spec string) map[string]bool {
	t.Helper()
	i := strings.Index(spec, "<!-- error-kinds -->")
	j := strings.Index(spec, "<!-- /error-kinds -->")
	if i < 0 || j < i {
		t.Fatal("specification.md has no error-kinds block")
	}
	out := map[string]bool{}
	for _, line := range strings.Split(spec[i:j], "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			continue
		}
		for _, m := range regexp.MustCompile("`([a-z-]+)`").FindAllStringSubmatch(cells[1], -1) {
			out[m[1]] = true
		}
	}
	return out
}

// The server moved to its own binary (spec 9.6): the old command says
// where, with the usage exit code, and is in no table.
func TestRendezvousMoved(t *testing.T) {
	var out, errs strings.Builder
	if code := run([]string{"rendezvous", "--listen", ":9000"}, nil, &out, &errs, nil); code != exitUsage || !strings.Contains(errs.String(), "kalka-rendezvous") {
		t.Fatalf("exit %d: %q", code, errs.String())
	}
	if _, ok := commandByName("rendezvous"); ok {
		t.Fatal("rendezvous is still a command")
	}
}
