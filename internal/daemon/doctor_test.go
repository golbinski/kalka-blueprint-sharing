package daemon

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/golbinski/kalka/internal/doctor"
)

// Settings' Check setup runs kalka doctor's checks in the daemon (spec
// 12.5): the daemon answers, so it and its API are ok.
func TestDoctorAPI(t *testing.T) {
	w := newWorld(t)
	n := w.node("ida@library.example")
	c := NewClient(n.d.APIAddr(), n.d.Token())
	var r doctor.Report
	if err := c.do(context.Background(), http.MethodGet, "/api/doctor", nil, &r); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, x := range r.Results {
		got[x.Check] = x.State
	}
	for _, check := range []string{"config", "key", "daemon", "local-api", "rendezvous", "folder"} {
		if got[check] != doctor.OK {
			t.Errorf("%s is %q: %+v", check, got[check], r.Results)
		}
	}
	// No start-at-login entry is read in tests.
	if _, ok := got["autostart"]; ok {
		t.Error("autostart was read")
	}
	for _, x := range r.Results {
		if x.Check == "config" && !strings.Contains(x.Detail, "ida@library.example") {
			t.Errorf("config: %q", x.Detail)
		}
	}
}
