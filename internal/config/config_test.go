package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Only the rendezvous changes: comments, other keys and the absent
// ui_port stay as they were.
func TestSetRendezvous(t *testing.T) {
	dir := t.TempDir()
	if err := SetRendezvous(dir, "wss://meet.library.example"); !errors.Is(err, ErrMissing) {
		t.Fatalf("no file: %v", err)
	}
	in := "# the reading room\nemail: ida@library.example\nrendezvous: peerjs-cloud # demo\nfolder: /srv/blueprints\nrelay_only: true\n"
	p := filepath.Join(dir, FileName)
	os.WriteFile(p, []byte(in), 0o600)
	if err := SetRendezvous(dir, "wss://meet.library.example"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	want := "# the reading room\nemail: ida@library.example\nrendezvous: wss://meet.library.example # demo\nfolder: /srv/blueprints\nrelay_only: true\n"
	if string(b) != want {
		t.Fatalf("got\n%s", b)
	}
	t.Setenv(DirEnv, dir)
	c, err := Load()
	if err != nil || c.Rendezvous != "wss://meet.library.example" || c.Email != "ida@library.example" || !c.RelayOnly {
		t.Fatalf("%+v %v", c, err)
	}
	// A file without the key gets it.
	os.WriteFile(p, []byte("email: ida@library.example\nfolder: /f\n"), 0o600)
	if err := SetRendezvous(dir, PeerJSCloud); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load(); c.Rendezvous != PeerJSCloud {
		t.Fatalf("%+v", c)
	}
}
