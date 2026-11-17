package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAPIGuards(t *testing.T) {
	w := newWorld(t)
	a := w.node("alice@corp.com")
	addr, tok := a.d.APIAddr(), a.d.Token()
	_, port, _ := strings.Cut(addr, ":")

	type req struct {
		name                                     string
		method, path, host, auth, cookie, origin string
		site                                     string // Sec-Fetch-Site
		want                                     int
	}
	do := func(c req) int {
		var body *strings.Reader
		if c.method == http.MethodPost {
			body = strings.NewReader(`{"email":"x@corp.com"}`)
		} else {
			body = strings.NewReader("")
		}
		r, _ := http.NewRequest(c.method, "http://"+addr+c.path, body)
		r.Host = c.host
		if c.auth != "" {
			r.Header.Set("Authorization", c.auth)
		}
		if c.cookie != "" {
			r.AddCookie(&http.Cookie{Name: CookieName, Value: c.cookie})
		}
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.site != "" {
			r.Header.Set("Sec-Fetch-Site", c.site)
		}
		resp, err := noRedirect.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if strings.HasPrefix(c.path, "/?token=") && resp.StatusCode == http.StatusSeeOther {
			// The token exchange: 303 either way; it worked if a cookie came.
			if len(resp.Cookies()) == 0 {
				return http.StatusForbidden
			}
			return http.StatusOK
		}
		return resp.StatusCode
	}
	local := "127.0.0.1:" + port
	own := "http://" + local
	bearer := "Bearer " + tok
	const st, pe = "/api/status", "/api/resume"
	cases := []req{
		{"bearer", "GET", st, local, bearer, "", "", "", 200},
		{"localhost name", "GET", st, "localhost:" + port, bearer, "", "", "", 200},
		{"rebinding host", "GET", st, "evil.example:" + port, bearer, "", "", "", 403},
		{"non-loopback host", "GET", st, "192.168.1.20:" + port, bearer, "", "", "", 403},
		{"other port", "GET", st, "127.0.0.1:1", bearer, "", "", "", 403},
		{"no token", "GET", st, local, "", "", "", "", 403},
		{"wrong token", "GET", st, local, "Bearer x" + tok, "", "", "", 403},
		{"token without Bearer", "GET", st, local, tok, "", "", "", 403},
		{"cookie read", "GET", st, local, "", tok, "", "", 200},
		{"cookie read, same-origin", "GET", st, local, "", tok, "", "same-origin", 200},
		{"cookie read, cross-site", "GET", st, local, "", tok, "", "cross-site", 403},
		{"cookie read, same-site", "GET", st, local, "", tok, "", "same-site", 403},
		{"bearer, cross-site", "GET", st, local, bearer, "", "", "cross-site", 403},
		{"cookie write, no origin", "POST", pe, local, "", tok, "", "", 403},
		{"cookie write, foreign origin", "POST", pe, local, "", tok, "http://evil.example", "", 403},
		{"cookie write, foreign origin, cross-site", "POST", pe, local, "", tok, "http://evil.example", "cross-site", 403},
		{"cookie write, own origin", "POST", pe, local, "", tok, own, "", 200},
		{"cookie write, own origin, same-origin", "POST", pe, local, "", tok, own, "same-origin", 200},
		{"cookie write, own origin but cross-site", "POST", pe, local, "", tok, own, "cross-site", 403},
		{"token link opened by kalka", "GET", "/?token=" + tok, local, "", "", "", "none", 200},
		{"token link from a chat page", "GET", "/?token=" + tok, local, "", "", "", "cross-site", 200},
		{"wrong token link", "GET", "/?token=x" + tok, local, "", "", "", "none", 403},
		{"static file, cross-site (Mermaid frame)", "GET", "/", local, "", "", "", "cross-site", 200},
	}
	for _, c := range cases {
		if got := do(c); got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}

	// daemon.json: private, and names this daemon.
	path := filepath.Join(a.cfgDir, DaemonFile)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no mode bits; the file lies in the user's own profile.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
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

var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// TestNoUI: with --no-ui the API answers the CLI and no page is served.
func TestNoUI(t *testing.T) {
	w := newWorld(t)
	a := w.node("alice@corp.com", func(o *Options) { o.NoUI = true })
	for _, p := range []string{"/", "/?token=" + a.d.Token(), "/mermaid/"} {
		resp, err := noRedirect.Get("http://" + a.d.APIAddr() + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound || len(resp.Cookies()) > 0 {
			t.Errorf("GET %s with --no-ui: %d", p, resp.StatusCode)
		}
	}
	c, _ := Connect(a.cfgDir)
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatalf("API with --no-ui: %v", err)
	}
}

// TestOpenFolder: only folders inside the blueprint folder are shown.
func TestOpenFolder(t *testing.T) {
	w := newWorld(t)
	opened := make(chan string, 4)
	a := w.node("alice@parcels.example", func(o *Options) {
		o.OpenFolder = func(dir string) error { opened <- dir; return nil }
	})
	c, _ := Connect(a.cfgDir)
	ctx := context.Background()
	for _, bad := range []string{"..", "../..", "personal/../..", "no-such-folder"} {
		if err := c.do(ctx, http.MethodPost, "/api/open-folder", map[string]string{"path": bad}, nil); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if len(opened) > 0 {
		t.Fatalf("opened %q", <-opened)
	}
	if err := c.do(ctx, http.MethodPost, "/api/open-folder", map[string]string{"path": "personal"}, nil); err != nil {
		t.Fatal(err)
	}
	if d := <-opened; filepath.Base(d) != "personal" {
		t.Fatalf("opened %q", d)
	}
}

// TestOpenExternal: only http and https links leave the window.
func TestOpenExternal(t *testing.T) {
	w := newWorld(t)
	opened := make(chan string, 4)
	a := w.node("alice@corp.com", func(o *Options) {
		o.OpenExternal = func(u string) error { opened <- u; return nil }
	})
	c, _ := Connect(a.cfgDir)
	ctx := context.Background()
	for _, bad := range []string{"file:///etc/passwd", "javascript:alert(1)", "ms-settings:", "vscode://x", "https://", "//evil.example/", "http://user:pw@host/", "relative/path"} {
		if err := c.do(ctx, http.MethodPost, "/api/open-external", map[string]string{"url": bad}, nil); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if len(opened) > 0 {
		t.Fatalf("opened %q", <-opened)
	}
	if err := c.do(ctx, http.MethodPost, "/api/open-external", map[string]string{"url": " https://example.org/docs?a=1#b "}, nil); err != nil {
		t.Fatal(err)
	}
	if u := <-opened; u != "https://example.org/docs?a=1#b" {
		t.Fatalf("opened %q", u)
	}
}
