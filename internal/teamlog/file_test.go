package teamlog

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogFile(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(nil)
	at := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	root := Create(key, "parcels", "depot@parcels.example", at)
	s := Signer{Key: key, Team: root.Hash()}
	_, courier, _ := ed25519.GenerateKey(nil)
	add := s.AddMember([]string{root.Hash()}, "courier@parcels.example", courier.Public().(ed25519.PublicKey), "", at)

	path := filepath.Join(t.TempDir(), "parcels", LogName("parcels"))
	if ops, bad, err := ReadFile(path); err != nil || ops != nil || bad != nil {
		t.Fatalf("missing file: %v %v %v", ops, bad, err)
	}
	if _, err := AppendFile(path, []Op{root}); err != nil {
		t.Fatal(err)
	}
	// Another writer added a line kalka cannot read; it is kept.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("not an operation")
	f.Close()
	all, err := AppendFile(path, []Op{root, add, add})
	if err != nil || len(all) != 2 {
		t.Fatalf("after append: %d %v", len(all), err)
	}
	raw, _ := os.ReadFile(path)
	if lines := strings.Split(strings.TrimSpace(string(raw)), "\n"); len(lines) != 3 || lines[1] != "not an operation" {
		t.Fatalf("file:\n%s", raw)
	}
	ops, bad, _ := ReadFile(path)
	if len(ops) != 2 || len(bad) != 1 || bad[0].Line != 2 {
		t.Fatalf("read back: %d ops, bad %v", len(ops), bad)
	}
	st, _ := Replay(root.Hash(), ops)
	if _, ok := st.Members["courier@parcels.example"]; !ok {
		t.Fatal("member missing after a round trip through the file")
	}
}
