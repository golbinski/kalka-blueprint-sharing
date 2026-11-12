package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/syncproto"
)

func TestAPIGuards(t *testing.T) {
	w := newWorld(t)
	a := w.node("alice@corp.com")
	addr, tok := a.d.APIAddr(), a.d.Token()
	_, port, _ := strings.Cut(addr, ":")

	do := func(method, host, auth, cookie, origin string) int {
		req, _ := http.NewRequest(method, "http://"+addr+"/api/status", nil)
		if method == http.MethodPost {
			req, _ = http.NewRequest(method, "http://"+addr+"/api/peers", strings.NewReader(`{"email":"x@corp.com"}`))
		}
		req.Host = host
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: CookieName, Value: cookie})
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	local := "127.0.0.1:" + port
	cases := []struct {
		name                               string
		method, host, auth, cookie, origin string
		want                               int
	}{
		{"bearer", "GET", local, "Bearer " + tok, "", "", 200},
		{"localhost name", "GET", "localhost:" + port, "Bearer " + tok, "", "", 200},
		{"rebinding host", "GET", "evil.example:" + port, "Bearer " + tok, "", "", 403},
		{"other port", "GET", "127.0.0.1:1", "Bearer " + tok, "", "", 403},
		{"no token", "GET", local, "", "", "", 403},
		{"wrong token", "GET", local, "Bearer x" + tok, "", "", 403},
		{"token without Bearer", "GET", local, tok, "", "", 403},
		{"cookie read", "GET", local, "", tok, "", 200},
		{"cookie write, no origin", "POST", local, "", tok, "", 403},
		{"cookie write, foreign origin", "POST", local, "", tok, "http://evil.example", 403},
		{"cookie write, own origin", "POST", local, "", tok, "http://" + local, 200},
	}
	for _, c := range cases {
		if got := do(c.method, c.host, c.auth, c.cookie, c.origin); got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}

	// daemon.json: private, and names this daemon.
	path := filepath.Join(a.cfgDir, DaemonFile)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("daemon.json mode %v", info.Mode().Perm())
	}
	var e Endpoint
	raw, _ := os.ReadFile(path)
	if json.Unmarshal(raw, &e) != nil || e.Token != tok || e.Addr != addr {
		t.Fatalf("daemon.json %s", raw)
	}
	a.stop()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("daemon.json left behind")
	}
}

// TestCLISyncThroughAPI drives a first send the way `kalka send` does:
// prompts stream to the initiating client, the recipient answers from its
// own front end through /api/prompts.
func TestCLISyncThroughAPI(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.node("alice@corp.com"), w.node("bob@corp.com")
	alice.d.opts.Prompter, bob.d.opts.Prompter = nil, nil
	alice.write("plan.md", plan)
	ca, err := Connect(alice.cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	cb, _ := Connect(bob.cfgDir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Bob's front end: answer every prompt, remember the code shown.
	bobCode := make(chan string, 1)
	go func() {
		for ctx.Err() == nil {
			var ps []*Pending
			_ = cb.do(ctx, http.MethodGet, "/api/prompts", nil, &ps)
			for _, p := range ps {
				if p.Kind == syncproto.PromptVerify {
					select {
					case bobCode <- p.Code:
					default:
					}
				}
				_ = cb.Answer(ctx, p.ID, true)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()

	var kinds []string
	var result *syncproto.Result
	var aliceCode string
	err = ca.Sync(ctx, SyncRequest{Path: alice.path("plan.md"), To: bob.email}, func(ev Event) {
		kinds = append(kinds, ev.Type)
		switch ev.Type {
		case "prompt":
			aliceCode = ev.Prompt.Code
			if err := ca.Answer(ctx, ev.Prompt.ID, true); err != nil {
				t.Error(err)
			}
		case "result":
			result = ev.Result
		case "error":
			t.Errorf("sync error: %s (%s)", ev.Error, ev.Kind)
		}
	})
	if err != nil || result == nil {
		t.Fatalf("stream: %v %v", err, kinds)
	}
	if c := <-bobCode; c != aliceCode || c == "" {
		t.Fatalf("codes differ: %q %q", aliceCode, c)
	}
	equalFiles(t, "plan.md", alice, bob)

	v, err := ca.Code(ctx, bob.email)
	if err != nil || !v.Verified || v.Code != aliceCode {
		t.Fatalf("code view %+v %v", v, err)
	}
	peers, err := ca.Peers(ctx)
	if err != nil || len(peers) != 1 || peers[0].Presence != "online" || !peers[0].Verified {
		t.Fatalf("peers %+v %v", peers, err)
	}
	// An error reaches the stream with its kind.
	var kind string
	_ = ca.Sync(ctx, SyncRequest{Path: alice.path("plan.md"), To: "dave@corp.com"}, func(ev Event) {
		if ev.Type == "error" {
			kind = ev.Kind
		}
	})
	if kind != "offline" {
		t.Fatalf("offline kind %q", kind)
	}
}
