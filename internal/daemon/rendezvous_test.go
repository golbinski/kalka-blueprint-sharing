package daemon

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/core"
)

// rawPost posts to the daemon's API and returns the status code.
func rawPost(t *testing.T, a *node, path, body string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+a.d.APIAddr()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+a.d.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// POST /api/rendezvous: a bad value is refused, a server that does not
// answer needs force, and a switch keeps the other settings.
func TestRendezvousAPI(t *testing.T) {
	w := newWorld(t)
	other := newWorld(t)
	a := w.node("ada@example.org")
	t.Setenv(config.DirEnv, a.cfgDir)
	orig := &config.Config{Email: a.email, Rendezvous: w.url, Folder: a.folder, UIPort: 4242, RelayOnly: true,
		ICEServers: []config.ICEServer{{URLs: []string{"stun:stun.example.org:3478"}}}}
	if err := config.Save(orig); err != nil {
		t.Fatal(err)
	}
	c, err := Connect(a.cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for _, bad := range []string{"", "   ", "not a server"} {
		if code := rawPost(t, a, "/api/rendezvous", `{"rendezvous":"`+bad+`"}`); code != http.StatusBadRequest {
			t.Errorf("%q: status %d, want 400", bad, code)
		}
	}

	dead := "ws://127.0.0.1:1"
	_, err = c.SetRendezvous(ctx, dead, false)
	var re *RemoteError
	if err == nil || !errors.As(err, &re) || re.Kind != core.KindRendezvousSilent || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("an unreachable server: %v", err)
	}
	if code := rawPost(t, a, "/api/rendezvous", `{"rendezvous":"`+dead+`"}`); code != http.StatusConflict {
		t.Fatalf("status %d, want 409", code)
	}
	if got, _ := config.Load(); got.Rendezvous != w.url {
		t.Fatalf("config.yaml changed to %q", got.Rendezvous)
	}

	// The same server changes nothing.
	if st, err := c.SetRendezvous(ctx, w.url, false); err != nil || st.Rendezvous != w.url {
		t.Fatalf("same server: %+v %v", st, err)
	}

	// A server that answers is switched to, and so is a forced one.
	if st, err := c.SetRendezvous(ctx, other.url, false); err != nil || st.Rendezvous != other.url {
		t.Fatalf("switch: %+v %v", st, err)
	}
	if _, err := c.SetRendezvous(ctx, dead, true); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Rendezvous != dead || got.Email != orig.Email || got.Folder != orig.Folder || got.UIPort != 4242 || !got.RelayOnly || len(got.ICEServers) != 1 {
		t.Fatalf("config.yaml: %+v", got)
	}
	if st, err := c.Status(ctx); err != nil || st.Rendezvous != dead {
		t.Fatalf("status: %+v %v", st, err)
	}
	// The doctor reports the server in use.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.d.State().Rendezvous == dead {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cfg, _ := a.d.doctorProbes().Config()
	if cfg.Rendezvous != dead {
		t.Fatalf("doctor sees %q", cfg.Rendezvous)
	}
}
