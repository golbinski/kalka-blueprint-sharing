package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/rendezvous"
)

// TestMain lets tests run this test binary as the kalka binary: with
// KALKA_CLI_MAIN=1 it is `kalka <args>`. A daemon the CLI spawns in the
// background inherits the variable, so it works for those too.
func TestMain(m *testing.M) {
	if os.Getenv("KALKA_CLI_MAIN") == "1" {
		os.Exit(Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
	// Tests never read the developer's configuration: kalka check would
	// read domain versions from their folder.
	if os.Getenv("KALKA_CONFIG_DIR") == "" {
		dir, err := os.MkdirTemp("", "kalka-cli-test")
		if err != nil {
			panic(err)
		}
		os.Setenv("KALKA_CONFIG_DIR", dir)
		code := m.Run()
		os.RemoveAll(dir)
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// kalkaCmd is the kalka binary (this test binary) with args.
func kalkaCmd(args ...string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "KALKA_CLI_MAIN=1")
	return cmd
}

func endpoint(t *testing.T, dir string) daemon.Endpoint {
	t.Helper()
	var e daemon.Endpoint
	raw, err := os.ReadFile(filepath.Join(dir, daemon.DaemonFile))
	if err != nil || json.Unmarshal(raw, &e) != nil {
		t.Fatalf("daemon.json: %v %s", err, raw)
	}
	return e
}

func waitAnswering(t *testing.T, dir string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for answering(ctx, dir) == nil {
		if ctx.Err() != nil {
			t.Fatal("daemon did not answer")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestSingleDaemon: one daemon per config
// directory, across processes, ports and a crash.
func TestSingleDaemon(t *testing.T) {
	if testing.Short() {
		t.Skip("starts daemons in subprocesses")
	}
	srv := &rendezvous.Server{ExpireTimeout: time.Second}
	mux := http.NewServeMux()
	srv.Mount(mux, "/")
	hs := httptest.NewServer(mux)
	t.Cleanup(func() { hs.Close(); srv.Close() })

	root := t.TempDir()
	dir := filepath.Join(root, "cfg")
	t.Setenv("KALKA_CONFIG_DIR", dir)
	setup := func(port int) {
		t.Helper()
		var out, errb bytes.Buffer
		if code := run([]string{"setup", "--email", "alice@corp.com", "--rendezvous", hs.URL,
			"--folder", filepath.Join(root, "bp"), "--ui-port", strconv.Itoa(port)}, nil, &out, &errb, time.Now); code != 0 {
			t.Fatalf("setup: %s", errb.String())
		}
	}
	setup(freePort(t))
	var pids []int
	t.Cleanup(func() { // whatever is left running
		for _, pid := range pids {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
	})

	// The first daemon.
	first := kalkaCmd("daemon", "--no-notifications")
	var firstOut bytes.Buffer
	first.Stdout, first.Stderr = &firstOut, &firstOut
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	pids = append(pids, first.Process.Pid)
	waitAnswering(t, dir)
	if e := endpoint(t, dir); e.PID != first.Process.Pid {
		t.Fatalf("daemon.json pid %d, want %d", e.PID, first.Process.Pid)
	}

	// A second start says so and exits 0; the first still serves.
	second := func() {
		t.Helper()
		out, err := kalkaCmd("daemon", "--no-notifications").CombinedOutput()
		want := "already running (pid " + strconv.Itoa(first.Process.Pid) + ")"
		if err != nil || !strings.Contains(string(out), want) {
			t.Fatalf("second start: %v %q", err, out)
		}
		waitAnswering(t, dir)
	}
	second()
	// Another ui_port on the same config directory: still one daemon.
	setup(freePort(t))
	second()

	// The first killed with no clean-up: daemon.json is stale, and a new
	// start takes the lock at once.
	_ = first.Process.Kill()
	_ = first.Wait()
	if e := endpoint(t, dir); e.PID != first.Process.Pid {
		t.Fatal("expected a stale daemon.json")
	}
	if answering(context.Background(), dir) != nil {
		t.Fatal("killed daemon still answers")
	}
	third := kalkaCmd("daemon", "--no-notifications")
	if err := third.Start(); err != nil {
		t.Fatal(err)
	}
	pids = append(pids, third.Process.Pid)
	waitAnswering(t, dir)
	if e := endpoint(t, dir); e.PID != third.Process.Pid {
		t.Fatalf("daemon.json pid %d, want %d", e.PID, third.Process.Pid)
	}

	// `kalka stop` stops it and waits.
	var out, errb bytes.Buffer
	if code := run([]string{"stop", "--json"}, nil, &out, &errb, time.Now); code != 0 {
		t.Fatalf("stop: %d %s", code, errb.String())
	}
	var res struct {
		WasRunning bool `json:"was_running"`
		PID        int  `json:"pid"`
	}
	if json.Unmarshal(out.Bytes(), &res) != nil || !res.WasRunning || res.PID != third.Process.Pid {
		t.Fatalf("stop said %s", out.String())
	}
	done := make(chan error, 1)
	go func() { done <- third.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stopped daemon exited with %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not exit after stop")
	}
	if _, err := os.Stat(filepath.Join(dir, daemon.DaemonFile)); !os.IsNotExist(err) {
		t.Fatal("daemon.json left behind")
	}
	out.Reset()
	if code := run([]string{"stop"}, nil, &out, &errb, time.Now); code != 0 || !strings.Contains(out.String(), "not running") {
		t.Fatalf("stop with none running: %d %q", code, out.String())
	}

	// A command that needs a daemon starts one in the background (this
	// test binary, run as kalka through the inherited variable).
	t.Setenv("KALKA_CLI_MAIN", "1")
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	c, err := ensureDaemon(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	pids = append(pids, endpoint(t, dir).PID)
	if _, err := c.Status(ctx); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "kalka.log"))
	if !strings.Contains(string(log), "starting in the background") {
		t.Fatalf("kalka.log: %q", log)
	}
	out.Reset()
	if code := run([]string{"stop"}, nil, &out, &errb, time.Now); code != 0 || !strings.Contains(out.String(), "stopped") {
		t.Fatalf("stop background: %d %q %q", code, out.String(), errb.String())
	}
}

// cliWorld is a config directory with a rendezvous, for running the CLI
// in subprocesses.
type cliWorld struct {
	t      *testing.T
	dir    string // config directory
	folder string
}

func newCLIWorld(t *testing.T) *cliWorld {
	t.Helper()
	srv := &rendezvous.Server{ExpireTimeout: time.Second}
	mux := http.NewServeMux()
	srv.Mount(mux, "/")
	hs := httptest.NewServer(mux)
	t.Cleanup(func() { hs.Close(); srv.Close() })
	root := t.TempDir()
	w := &cliWorld{t: t, dir: filepath.Join(root, "cfg"), folder: filepath.Join(root, "bp")}
	t.Setenv("KALKA_CONFIG_DIR", w.dir)
	t.Setenv("KALKA_CLI_MAIN", "1") // spawned daemons run this binary as kalka
	var errb bytes.Buffer
	if code := run([]string{"setup", "--email", "alice@corp.com", "--rendezvous", hs.URL,
		"--folder", w.folder, "--ui-port", strconv.Itoa(freePort(t))}, nil, io.Discard, &errb, time.Now); code != 0 {
		t.Fatalf("setup: %s", errb.String())
	}
	t.Cleanup(func() { // stop whatever a test left running
		if e, err := readEndpoint(w.dir); err == nil {
			_ = run([]string{"stop", "--wait", "20s"}, nil, io.Discard, io.Discard, time.Now)
			if p, err := os.FindProcess(e.PID); err == nil {
				_ = p.Kill()
			}
		}
	})
	return w
}

// kalka runs the CLI as a subprocess, as an agent would.
func (w *cliWorld) kalka(args ...string) (int, string, string) {
	w.t.Helper()
	cmd := kalkaCmd(args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		w.t.Fatal(err)
	}
	return code, out.String(), errb.String()
}

// running reports whether a daemon holds the lock.
func (w *cliWorld) running() bool { return lockHolder(w.dir) != 0 }

// TestCLIClasses: each class of command with
// and without a running daemon (spec 12).
func TestCLIClasses(t *testing.T) {
	if testing.Short() {
		t.Skip("starts daemons in subprocesses")
	}
	w := newCLIWorld(t)
	plan := filepath.Join(w.folder, "plan.md")
	if err := os.WriteFile(plan, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	// File commands with no daemon: they work and start none.
	if code, _, errs := w.kalka("comment", plan, "--anchor", "plan", "-m", "First."); code != 0 {
		t.Fatalf("comment: %s", errs)
	}
	if code, _, errs := w.kalka("seed", plan, "--title", "Split off", "-m", "Later."); code != 0 {
		t.Fatalf("seed: %s", errs)
	}
	if w.running() {
		t.Fatal("a file command started a daemon")
	}

	// Views with no daemon: from the files, "daemon": false, none started.
	code, out, errs := w.kalka("domains", "--json")
	if code != 0 || !strings.Contains(out, `"daemon": false`) {
		t.Fatalf("domains: %d %s %s", code, out, errs)
	}
	code, out, _ = w.kalka("status", "--json")
	var st struct {
		Daemon bool   `json:"daemon"`
		Online *bool  `json:"online"`
		Email  string `json:"email"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &st) != nil || st.Daemon || st.Online != nil || st.Email != "alice@corp.com" {
		t.Fatalf("status: %d %s", code, out)
	}
	if w.running() {
		t.Fatal("a view started a daemon")
	}

	// A daemon command with --no-start, or KALKA_NO_START=1: not-running.
	code, out, _ = w.kalka("team", "create", "lending", "--json", "--no-start")
	if code != 1 || !strings.Contains(out, `"kind": "not-running"`) {
		t.Fatalf("--no-start: %d %s", code, out)
	}
	t.Setenv(NoStartEnv, "1")
	if code, out, _ = w.kalka("team", "create", "lending", "--json"); code != 1 || !strings.Contains(out, `"kind": "not-running"`) {
		t.Fatalf("%s: %d %s", NoStartEnv, code, out)
	}
	os.Unsetenv(NoStartEnv)
	if w.running() {
		t.Fatal("--no-start started a daemon")
	}

	// Two daemon commands at the same moment with no daemon: both
	// succeed, and exactly one daemon results.
	type res struct {
		code      int
		out, errs string
	}
	results := make(chan res, 2)
	for _, team := range []string{"lending", "returns"} {
		go func() {
			c, o, e := w.kalka("team", "create", team, "--json")
			results <- res{c, o, e}
		}()
	}
	for range 2 {
		r := <-results
		if r.code != 0 || !strings.Contains(r.out, `"name"`) {
			t.Fatalf("team create with auto-start: %d %s %s", r.code, r.out, r.errs)
		}
	}
	if !w.running() {
		t.Fatal("the daemon a command started is gone")
	}
	log, _ := os.ReadFile(filepath.Join(w.dir, "kalka.log"))
	starts := strings.Count(string(log), "starting in the background")
	lost := strings.Count(string(log), "already running")
	if starts-lost != 1 {
		t.Fatalf("%d starts, %d found one running:\n%s", starts, lost, log)
	}

	// Views with a daemon: live, "daemon": true.
	code, out, _ = w.kalka("status", "--json")
	if code != 0 || json.Unmarshal([]byte(out), &st) != nil || !st.Daemon || st.Online == nil {
		t.Fatalf("status with daemon: %d %s", code, out)
	}

	// A file command with a daemon: the daemon's index sees the entry.
	e := endpoint(t, w.dir)
	entries := func() int {
		req, _ := http.NewRequest(http.MethodGet, "http://"+e.Addr+"/api/blueprints", nil)
		req.Header.Set("Authorization", "Bearer "+e.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var list []struct {
			Path    string `json:"path"`
			Entries int    `json:"entries"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&list)
		for _, in := range list {
			if filepath.Base(in.Path) == "plan.md" {
				return in.Entries
			}
		}
		return -1
	}
	before := entries()
	if code, _, errs := w.kalka("comment", plan, "--anchor", "plan", "-m", "Second."); code != 0 {
		t.Fatalf("comment with daemon: %s", errs)
	}
	for i := 0; entries() != before+1; i++ {
		if i == 100 {
			t.Fatalf("index has %d entries, want %d", entries(), before+1)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Pause and resume: kept in state.yaml, seen by status, and a join
	// while paused fails.
	if code, out, errs := w.kalka("pause", "--json"); code != 0 || !strings.Contains(out, `"paused": true`) {
		t.Fatalf("pause: %d %s %s", code, out, errs)
	}
	if code, out, _ := w.kalka("status", "--json"); code != 0 || !strings.Contains(out, `"paused": true`) {
		t.Fatalf("status paused: %s", out)
	}
	if code, out, errs := w.kalka("join", "kalka-invite-x", "--json"); code == 0 || !strings.Contains(out, `"kind": "paused"`) {
		t.Fatalf("join while paused: %d %s %s", code, out, errs)
	}
	if code, out, _ := w.kalka("resume"); code != 0 || !strings.Contains(out, "syncing again") {
		t.Fatalf("resume: %d %s", code, out)
	}
	if raw, _ := os.ReadFile(filepath.Join(w.dir, daemon.StateFile)); !strings.Contains(string(raw), "paused: false") {
		t.Fatalf("state.yaml after resume:\n%s", raw)
	}

	// Join links (spec 19.7): the invite prints a link and a token; the
	// link opens the join screen, and --show never joins.
	code, out, errs = w.kalka("team", "invite", "bob@corp.com", "--team", "lending")
	lines := strings.Fields(out)
	if code != 0 || len(lines) != 2 || lines[0] != "kalka://join/"+lines[1] {
		t.Fatalf("invite: %d %q %s", code, out, errs)
	}
	link, token := lines[0], lines[1]
	code, out, errs = w.kalka("open", "--print", "<"+strings.ToLower(link)+">")
	if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), "#/join/"+token) || !strings.Contains(out, "?token=") {
		t.Fatalf("open --print <link>: %d %q %s", code, out, errs)
	}
	code, out, _ = w.kalka("open", "--print", "kalka://join/"+token+"?x=1")
	if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), "#/join/") {
		t.Fatalf("open --print <bad link>: %d %q", code, out)
	}
	code, out, errs = w.kalka("join", "--show", link, "--json")
	var pv core.JoinPreview
	if code != 0 || json.Unmarshal([]byte(out), &pv) != nil || pv.Problem != core.ProblemOwnInvite || pv.Inviter != "alice@corp.com" || pv.Email != "bob@corp.com" || pv.Name != "lending" {
		t.Fatalf("join --show: %d %s %s", code, out, errs)
	}
	code, out, _ = w.kalka("join", "--show", token)
	if code != 0 || !strings.Contains(out, "invited by") || !strings.Contains(out, "cannot join: this is an invite you made") {
		t.Fatalf("join --show text: %d %s", code, out)
	}
	if code, out, _ = w.kalka("join", "--show", "kalka://join/"+token+"/x", "--json"); code != 1 || !strings.Contains(out, `"kind": "invite-token"`) {
		t.Fatalf("join --show junk: %d %s", code, out)
	}
	if code, out, _ = w.kalka("join", link, "--json"); code != 1 || !strings.Contains(out, `"kind": "own-invite"`) {
		t.Fatalf("join own invite: %d %s", code, out)
	}

	// No configuration: a daemon command says so and starts nothing.
	other := filepath.Join(t.TempDir(), "empty-cfg")
	t.Setenv("KALKA_CONFIG_DIR", other)
	code, out, _ = w.kalka("team", "create", "lending", "--json")
	if code != 1 || !strings.Contains(out, `"kind": "not-set-up"`) {
		t.Fatalf("no config: %d %s", code, out)
	}
	if lockHolder(other) != 0 {
		t.Fatal("started a daemon with no config")
	}
	t.Setenv("KALKA_CONFIG_DIR", w.dir)
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
