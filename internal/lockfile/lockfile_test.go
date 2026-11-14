package lockfile

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAcquireTwice(t *testing.T) {
	p := filepath.Join(t.TempDir(), "daemon.lock")
	a, err := Acquire(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(p); !errors.Is(err, ErrHeld) {
		t.Fatalf("second acquire: %v", err)
	} else if h := err.(*Held); h.PID != os.Getpid() {
		t.Fatalf("held by %d, want %d", h.PID, os.Getpid())
	}
	if err := a.Release(); err != nil {
		t.Fatal(err)
	}
	b, err := Acquire(p)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	b.Release()
}

// TestKilledHolder: a holder killed with no clean-up leaves a file with
// its PID, and the lock is free at once.
func TestKilledHolder(t *testing.T) {
	if os.Getenv("LOCKFILE_HOLD") != "" {
		if _, err := Acquire(os.Getenv("LOCKFILE_HOLD")); err != nil {
			os.Exit(3)
		}
		os.Stdout.WriteString("held\n")
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	p := filepath.Join(t.TempDir(), "daemon.lock")
	cmd := exec.Command(os.Args[0], "-test.run=TestKilledHolder$")
	cmd.Env = append(os.Environ(), "LOCKFILE_HOLD="+p)
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := out.Read(buf); err != nil || !strings.HasPrefix(string(buf), "held") {
		t.Fatalf("child: %q %v", buf, err)
	}
	if _, err := Acquire(p); !errors.Is(err, ErrHeld) || err.(*Held).PID != cmd.Process.Pid {
		t.Fatalf("while child holds: %v", err)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	l, err := Acquire(p)
	if err != nil {
		t.Fatalf("after kill: %v", err)
	}
	l.Release()
}
