package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

func TestSeedCommands(t *testing.T) {
	// A local parent with no kalka_id: budding gives it one first.
	path := setup(t, "# Redelivery scheduling\n\n## Scope\nThe redelivery module.\n")
	code, out, errs := call("", "comment", path, "--anchor", "scope", "--kind", "deviation", "-m", "Lockers too.", "--json")
	if code != 0 {
		t.Fatalf("comment: %s", errs)
	}
	var c map[string]string
	json.Unmarshal([]byte(out), &c)
	// The comment was written before the parent had an id: unsigned, but
	// it can still be the origin.
	code, out, errs = call("", "seed", path, "--from", c["id"], "--title", "Locker pickup", "--json")
	if code != 0 {
		t.Fatalf("seed: %d %s", code, errs)
	}
	var r map[string]string
	json.Unmarshal([]byte(out), &r)
	if filepath.Dir(r["seed"]) != filepath.Dir(path) {
		t.Fatalf("seed not beside its parent: %s", r["seed"])
	}
	raw, _ := os.ReadFile(r["seed"])
	seed, _ := bp.Parse(raw)
	parentRaw, _ := os.ReadFile(path)
	parent, _ := bp.Parse(parentRaw)
	if parent.KalkaID() == "" || seed.OriginDoc() != parent.KalkaID() || func() string { _, e, _ := seed.Origin(); return e }() != c["id"] || !seed.IsSeed() {
		t.Fatalf("seed:\n%s\nparent:\n%s", raw, parentRaw)
	}
	last := parent.Items()[len(parent.Items())-1].Entry
	if id, _, ok := last.Budded(); !ok || id != seed.KalkaID() || last.Sig == "" {
		t.Fatalf("backlink %q", last.Header())
	}
	// A second seed with the same title gets its own file.
	if code, _, errs := call("", "seed", path, "--title", "Locker pickup", "-m", "Another angle."); code != 0 {
		t.Fatal(errs)
	}
	code, out, _ = call("", "seeds", "--json")
	var view struct {
		Seeds []seedRow `json:"seeds"`
	}
	err := json.Unmarshal([]byte(out), &view)
	rows := view.Seeds
	if code != 0 || err != nil || len(rows) != 2 || rows[0].ParentTitle != "Redelivery scheduling" {
		t.Fatalf("seeds: %d %v %s", code, err, out)
	}
	if code, out, _ := call("", "seeds"); code != 0 || !strings.Contains(out, "from: Redelivery scheduling") {
		t.Fatalf("seeds text: %s", out)
	}
	if code, _, _ := call("", "seed", path); code != exitUsage {
		t.Fatal("seed without title")
	}
}
