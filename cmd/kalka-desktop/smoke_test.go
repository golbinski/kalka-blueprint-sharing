//go:build desktop

package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/cli"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/desktop"
	"github.com/golbinski/kalka/internal/rendezvous"
)

// TestMain lets the smoke test run this test binary as kalka-desktop.
func TestMain(m *testing.M) {
	if os.Getenv("KALKA_DESKTOP_MAIN") == "1" {
		os.Exit(desktop.Main(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// TestSmoke (plan D6): the shell starts hidden, the CLI answers, the
// window can be asked to show, and `kalka stop` ends the shell.
func TestSmoke(t *testing.T) {
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("no display")
	}
	srv := &rendezvous.Server{ExpireTimeout: time.Second}
	mux := http.NewServeMux()
	srv.Mount(mux, "/")
	hs := httptest.NewServer(mux)
	t.Cleanup(func() { hs.Close(); srv.Close() })
	root := t.TempDir()
	dir := filepath.Join(root, "cfg")
	t.Setenv("KALKA_CONFIG_DIR", dir)
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	var errb bytes.Buffer
	if code := cli.Run([]string{"setup", "--email", "alice@corp.com", "--rendezvous", hs.URL,
		"--folder", filepath.Join(root, "bp"), "--ui-port", strconv.Itoa(port)}, nil, io.Discard, &errb); code != 0 {
		t.Fatalf("setup: %s", errb.String())
	}
	// Never write a real start-at-login entry from a test.
	if err := os.WriteFile(filepath.Join(dir, "state.yaml"), []byte("autostart_initialized: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "--hidden")
	// Nor a URL scheme handler (spec 23.1a).
	cmd.Env = append(os.Environ(), "KALKA_DESKTOP_MAIN=1", "KALKA_NO_URL_SCHEME=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	ctx := context.Background()
	var c *daemon.Client
	deadline := time.Now().Add(30 * time.Second)
	for {
		if cc, err := daemon.Connect(dir); err == nil {
			if sum, err := cc.Summary(ctx); err == nil && sum.Shell == "desktop" {
				c = cc
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the shell did not come up")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if code := cli.Run([]string{"status", "--json", "--no-start"}, nil, io.Discard, io.Discard); code != 0 {
		t.Fatalf("status exit %d", code)
	}
	if err := c.ShowUI(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if code := cli.Run([]string{"stop"}, nil, io.Discard, io.Discard); code != 0 {
		t.Fatalf("stop exit %d", code)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("shell exit: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("kalka stop did not end the shell")
	}
}
