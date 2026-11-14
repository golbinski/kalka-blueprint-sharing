package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/config"
)

func setup(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "cfg")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("email: bob@corp.com\nrendezvous: peerjs-cloud\nfolder: "+dir+"\n"), 0o600)
	t.Setenv("KALKA_CONFIG_DIR", cfgDir)
	path := filepath.Join(dir, "plan.md")
	os.WriteFile(path, []byte(content), 0o644)
	return path
}

func call(stdin string, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	now := func() time.Time { return time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC) }
	code := run(args, strings.NewReader(stdin), &out, &errb, now)
	return code, out.String(), errb.String()
}

const doc = "# Title\r\n\r\n## Plan\r\nDo it.\r\n"

func TestCommentAndCheck(t *testing.T) {
	path := setup(t, doc)
	code, out, errs := call("", "comment", path, "--anchor", "plan", "-m", "# Too big\nLooks good.", "--json")
	if code != 0 {
		t.Fatalf("comment: %d %s", code, errs)
	}
	var res map[string]string
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	code, _, errs = call("Agreed.\n", "comment", path, "--kind", "deviation", "--re", res["id"])
	if code != 0 {
		t.Fatalf("reply: %d %s", code, errs)
	}

	raw, _ := os.ReadFile(path)
	d, err := bp.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Items()) != 2 || d.Items()[1].Entry.ReplyTo() != res["id"] || d.Items()[1].Entry.Kind != "deviation" {
		t.Fatalf("entries wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "#### Too big\r\n") || d.EOL() != "\r\n" {
		t.Fatalf("heading not demoted or CRLF lost:\n%q", raw)
	}

	code, out, _ = call("", "check", path, "--json")
	if code != 0 {
		t.Fatalf("check exit %d: %s", code, out)
	}
}

func TestCommentRejectsUnknownAnchor(t *testing.T) {
	path := setup(t, doc)
	if code, _, errs := call("", "comment", path, "--anchor", "nope", "-m", "x"); code == 0 || !strings.Contains(errs, "plan") {
		t.Fatalf("unknown anchor accepted: %d %s", code, errs)
	}
}

func TestCheckExitCodes(t *testing.T) {
	path := setup(t, "---\nkalka_id: not-a-uuid\n---\n# T\n")
	if code, out, _ := call("", "check", path); code != 1 || !strings.Contains(out, "id-invalid") {
		t.Fatalf("check: %d %s", code, out)
	}
	if code, _, _ := call("", "check"); code != 2 {
		t.Fatalf("usage: %d", code)
	}
	if code, _, _ := call("", "check", filepath.Join(t.TempDir(), "missing.md")); code != 1 {
		t.Fatalf("missing file: %d", code)
	}
}

func TestSetupWritesConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KALKA_CONFIG_DIR", dir)
	var out, errb strings.Builder
	folder := filepath.Join(dir, "bp")
	if code := run([]string{"setup", "--email", "bob", "--rendezvous", "peerjs-cloud", "--folder", folder}, nil, &out, &errb, time.Now); code != exitUsage {
		t.Fatalf("bad email accepted: %d", code)
	}
	if code := run([]string{"setup", "--email", "bob@corp.com", "--rendezvous", "peerjs-cloud", "--folder", folder, "--ui-port", "7800"}, nil, &out, &errb, time.Now); code != exitOK {
		t.Fatalf("setup: %d %s", code, errb.String())
	}
	cfg, err := config.Load()
	if err != nil || cfg.Email != "bob@corp.com" || cfg.Folder != folder || cfg.UIPort != 7800 {
		t.Fatalf("%+v %v", cfg, err)
	}
	// Network commands without a daemon say how to start it.
	errb.Reset()
	if code := run([]string{"peers"}, nil, &out, &errb, time.Now); code != exitProblems || !strings.Contains(errb.String(), "not running") {
		t.Fatalf("peers without daemon: %d %q", code, errb.String())
	}
}

// `kalka comment` signs with the identity key (spec 6.5), and `kalka
// check` counts the signature as verified with that key.
func TestCommentSigns(t *testing.T) {
	path := setup(t, "---\nkalka_id: 6f1c2b9e-4d7a-4c3e-9b1a-2e5f8c7d6a40\nowner: bob@corp.com\n---\n# Locker pickup\n\n## Plan\nAdd a pickup code.\n")
	if code, _, errs := call("", "comment", path, "--anchor", "plan", "-m", "Codes expire after 48 hours."); code != 0 {
		t.Fatalf("comment: %d %s", code, errs)
	}
	raw, _ := os.ReadFile(path)
	d, _ := bp.Parse(raw)
	if e := d.Items()[0].Entry; e.Sig == "" {
		t.Fatalf("unsigned:\n%s", raw)
	}
	// A hand-written entry next to it is unsigned.
	e, _ := bp.NewEntry(bp.KindComment, "bob@corp.com", time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC), "plan", d.Rev(), "Typed by hand.")
	d.AddEntry(e)
	os.WriteFile(path, d.Bytes(), 0o644)

	code, out, _ := call("", "check", path, "--json")
	var res []checkResult
	if err := json.Unmarshal([]byte(strings.ReplaceAll(out, `"severity": "info"`, `"severity": 0`)), &res); code != 0 || err != nil {
		t.Fatalf("check: %d %v %s", code, err, out)
	}
	if s := res[0].Signatures; s.Verified != 1 || s.Unsigned != 1 || s.Invalid != 0 {
		t.Fatalf("counts %+v", s)
	}
	if code, out, _ := call("", "check", path, "--verbose"); code != 0 || !strings.Contains(out, "signatures: 1 verified") {
		t.Fatalf("verbose: %s", out)
	}
}
