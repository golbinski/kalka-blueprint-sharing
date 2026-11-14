package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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

// TestSingleDaemon is the required area of D2: one daemon per config
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
	first := kalkaCmd("daemon", "--no-popups")
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
		out, err := kalkaCmd("daemon", "--no-popups").CombinedOutput()
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
	third := kalkaCmd("daemon", "--no-popups")
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
