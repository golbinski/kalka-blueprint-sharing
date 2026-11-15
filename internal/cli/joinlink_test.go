package cli

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/teamlog"
)

// TestJoinShowOffline: `kalka join --show` with no daemon reads the token
// alone, says the checks were not made, and starts nothing.
func TestJoinShowOffline(t *testing.T) {
	setup(t, doc)
	t.Setenv(NoStartEnv, "1")
	pub, _, _ := ed25519.GenerateKey(nil)
	iv := teamlog.Invite{TeamName: "lending", Team: strings.Repeat("ab", 32), Owner: teamlog.EncodeKey(pub),
		Inviter: "desk@library.example", Email: "bob@corp.com", Secret: bytes.Repeat([]byte{3}, 32),
		Expires: time.Now().Add(time.Hour).UTC().Truncate(time.Second), Rendezvous: "peerjs-cloud"}
	code, out, errs := call("", "join", "--show", iv.Link(), "--json")
	var p core.JoinPreview
	if code != 0 || json.Unmarshal([]byte(out), &p) != nil || p.Inviter != "desk@library.example" || p.Problem != "" || !strings.Contains(p.Message, "not running") {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	code, out, _ = call("", "join", "--show", "https://join/"+iv.Token(), "--json")
	if code != exitProblems || !strings.Contains(out, `"kind": "invite-token"`) {
		t.Fatalf("another scheme: %d %s", code, out)
	}
}
