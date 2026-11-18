package daemon

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/golbinski/kalka/samples"
)

// The samples the installer asked for before setup are added when the
// daemon starts; POST /api/samples then finds them in place.
func TestSamplesAtStart(t *testing.T) {
	w := newWorld(t)
	asked := func(o *Options) {
		if err := samples.Ask(o.ConfigDir); err != nil {
			t.Fatal(err)
		}
	}
	n := w.node("ida@library.example", asked)
	if _, err := os.Stat(filepath.Join(n.folder, "personal", "samples", "lending", "loans-domain.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(n.cfgDir, samples.PendingFile)); !os.IsNotExist(err) {
		t.Fatalf("request kept: %v", err)
	}
	var r struct{ Written []string }
	c := NewClient(n.d.APIAddr(), n.d.Token())
	if err := c.do(context.Background(), http.MethodPost, "/api/samples", struct{}{}, &r); err != nil || r.Written == nil || len(r.Written) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}
