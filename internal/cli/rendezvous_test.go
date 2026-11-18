package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golbinski/kalka/internal/config"
)

// `kalka settings --rendezvous` with no daemon running writes
// config.yaml: a server that answers is taken, one that does not needs
// --force, and a bad value is a usage error.
func TestSettingsRendezvous(t *testing.T) {
	if testing.Short() {
		t.Skip("starts daemons in subprocesses")
	}
	w := newCLIWorld(t)
	t.Setenv(NoStartEnv, "1")
	answering := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) { rw.WriteHeader(http.StatusOK) }))
	defer answering.Close()
	before, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	if code, _, errs := w.kalka("settings", "--rendezvous", "not a server"); code != exitUsage {
		t.Fatalf("a bad value: exit %d (%s)", code, errs)
	}
	if code, _, _ := w.kalka("settings", "--force"); code != exitUsage {
		t.Fatalf("--force alone: exit %d", code)
	}

	dead := "ws://127.0.0.1:1"
	code, out, _ := w.kalka("settings", "--rendezvous", dead, "--json")
	if code != exitProblems || !strings.Contains(out, `"kind": "rendezvous-silent"`) || !strings.Contains(out, "--force") {
		t.Fatalf("a silent server: exit %d %s", code, out)
	}
	if got, _ := config.Load(); got.Rendezvous != before.Rendezvous {
		t.Fatalf("config.yaml changed to %q", got.Rendezvous)
	}

	if code, out, errs := w.kalka("settings", "--rendezvous", answering.URL, "--json"); code != 0 || !strings.Contains(out, answering.URL) {
		t.Fatalf("an answering server: exit %d %s %s", code, out, errs)
	}
	got, _ := config.Load()
	if got.Rendezvous != answering.URL || got.Email != before.Email || got.Folder != before.Folder || got.UIPort != before.UIPort {
		t.Fatalf("config.yaml: %+v", got)
	}

	if code, _, errs := w.kalka("settings", "--rendezvous", dead, "--force"); code != 0 {
		t.Fatalf("forced: exit %d %s", code, errs)
	}
	if got, _ := config.Load(); got.Rendezvous != dead {
		t.Fatalf("config.yaml has %q", got.Rendezvous)
	}
	if w.running() {
		t.Fatal("a daemon was started")
	}
}
