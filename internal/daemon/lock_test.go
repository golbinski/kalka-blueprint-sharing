package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/lockfile"
)

// TestOneDaemonPerConfigDir: a second daemon on the same config directory
// is refused, whatever its port; once the first stops, a new one starts.
func TestOneDaemonPerConfigDir(t *testing.T) {
	w := newWorld(t)
	a := w.node("alice@corp.com")
	second := Options{Options: core.Options{
		Config:    &config.Config{Email: a.email, Rendezvous: w.url, Folder: a.folder, UIPort: 2},
		ConfigDir: a.cfgDir, Loopback: true,
	}, APIAddr: "127.0.0.1:0"}
	_, err := New(second)
	var held *lockfile.Held
	if !errors.As(err, &held) || held.PID != os.Getpid() {
		t.Fatalf("second daemon: %v", err)
	}
	// The first still serves.
	c, err := Connect(a.cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Status(ctx); err != nil {
		t.Fatalf("first daemon: %v", err)
	}

	// Shutdown over the API stops it and frees the lock.
	if pid, err := c.Shutdown(ctx); err != nil || pid != os.Getpid() {
		t.Fatalf("shutdown: %d %v", pid, err)
	}
	for i := 0; ; i++ {
		if _, err := os.Stat(filepath.Join(a.cfgDir, DaemonFile)); errors.Is(err, os.ErrNotExist) {
			break
		}
		if i == 200 {
			t.Fatal("daemon.json still there after shutdown")
		}
		time.Sleep(25 * time.Millisecond)
	}
	var d *Daemon
	for i := 0; ; i++ {
		if d, err = New(second); err == nil {
			break
		}
		if i == 200 {
			t.Fatalf("lock not freed: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	go func() { _ = d.Run(ctx) }()
	select {
	case <-d.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("new daemon not ready")
	}
	d.Shutdown()
}
