package daemon

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/config"
)

func get(t *testing.T, c *http.Client, u string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestUIServingAndTokenCookie(t *testing.T) {
	w := newWorld(t)
	a := w.node("alice@corp.com")
	base := "http://" + a.d.APIAddr()
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// The page loads without a token and carries the CSP.
	r := get(t, noRedirect, base+"/", nil)
	r.Body.Close()
	csp := r.Header.Get("Content-Security-Policy")
	if r.StatusCode != 200 || !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-inline") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("index: %d CSP %q", r.StatusCode, csp)
	}
	// A client-side route falls back to the page.
	r = get(t, noRedirect, base+"/no/such/file", nil)
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("fallback %d", r.StatusCode)
	}
	// The API does not.
	r = get(t, noRedirect, base+"/api/status", nil)
	r.Body.Close()
	if r.StatusCode != 403 || r.Header.Get("Content-Security-Policy") == "" {
		t.Fatalf("api without token %d", r.StatusCode)
	}

	// A wrong token sets no cookie.
	r = get(t, noRedirect, base+"/?token=nope", nil)
	r.Body.Close()
	if len(r.Cookies()) != 0 || r.StatusCode != http.StatusSeeOther {
		t.Fatalf("wrong token: %d %v", r.StatusCode, r.Cookies())
	}
	// The right one becomes an HttpOnly, SameSite=Strict cookie and the URL
	// loses it.
	r = get(t, noRedirect, a.d.URL(), nil)
	r.Body.Close()
	cs := r.Cookies()
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/" || len(cs) != 1 {
		t.Fatalf("bootstrap: %d %q %v", r.StatusCode, r.Header.Get("Location"), cs)
	}
	c := cs[0]
	if c.Name != CookieName || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Value != a.d.Token() {
		t.Fatalf("cookie %+v", c)
	}
	r = get(t, noRedirect, base+"/api/status", map[string]string{"Cookie": c.Name + "=" + c.Value})
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("api with cookie %d", r.StatusCode)
	}
	// Static paths accept no writes.
	req, _ := http.NewRequest(http.MethodPost, base+"/", nil)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST / %d", resp.StatusCode)
	}
}

func TestDocumentModelAndEntries(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.node("alice@corp.com"), w.node("bob@corp.com")
	review(t, alice, bob)
	bob.comment("plan.md", "plan", "first")
	bob.mustSync("plan.md", alice, IntentSync)
	alice.mustSync("plan.md", bob, IntentHandoff)

	v, err := bob.d.Document("plan.md")
	if err != nil {
		t.Fatal(err)
	}
	if v.Role != RoleOwner || v.Rel != "plan.md" || v.Title != "Import pipeline refactor" || len(v.Sections) != 4 {
		t.Fatalf("view %+v", v)
	}
	if v.Sections[2].Heading.Slug != "plan" || !strings.HasPrefix(v.Sections[2].Markdown, "## Plan") {
		t.Fatalf("sections %+v", v.Sections)
	}
	var handoff *EntryView
	for i := range v.Entries {
		if v.Entries[i].Kind == bp.KindHandoff {
			handoff = &v.Entries[i]
		}
	}
	if handoff == nil || handoff.Handoff != "verified" || handoff.HandoffTarget != bob.email {
		t.Fatalf("handoff %+v", handoff)
	}
	if v2, _ := alice.d.Document("plan.md"); v2.Role != RoleReviewer {
		t.Fatalf("alice role %s", v2.Role)
	}

	// Through the API: add a quoted reply, then read it back.
	c, _ := Connect(alice.cfgDir)
	ctx := context.Background()
	first := v.Entries[0]
	var out map[string]string
	err = c.do(ctx, http.MethodPost, "/api/entries", EntryRequest{Path: "plan.md", Kind: "deviation", Anchor: "plan", Re: first.ID, Quote: "Stream rows.", Body: "# Heading\nWe batch instead."}, &out)
	if err != nil {
		t.Fatal(err)
	}
	var doc DocView
	if err := c.do(ctx, http.MethodGet, "/api/doc?path="+url.QueryEscape("plan.md"), nil, &doc); err != nil {
		t.Fatal(err)
	}
	var reply *EntryView
	for i := range doc.Entries {
		if doc.Entries[i].ID == out["id"] {
			reply = &doc.Entries[i]
		}
	}
	if reply == nil || reply.ReplyTo != first.ID || reply.Kind != "deviation" || !strings.HasPrefix(reply.Body, "> Stream rows.") || !strings.Contains(reply.Body, "#### Heading") || reply.Outdated {
		t.Fatalf("reply %+v", reply)
	}
	// The UI names files relative to the folder, for syncs too.
	if _, err := alice.d.Sync(ctx, SyncRequest{Path: "plan.md", To: bob.email}, "", nil); err != nil {
		t.Fatalf("sync by relative path: %v", err)
	}
	equalFiles(t, "plan.md", alice, bob)
	// Errors are reported, not written.
	if err := c.do(ctx, http.MethodPost, "/api/entries", EntryRequest{Path: "plan.md", Anchor: "nope", Body: "x"}, nil); err == nil {
		t.Fatal("unknown anchor accepted")
	}
	if err := c.do(ctx, http.MethodGet, "/api/doc?path="+url.QueryEscape("../../etc/passwd"), nil, &doc); err == nil {
		t.Fatal("path outside the folder served")
	}
}

func TestEventStream(t *testing.T) {
	w := newWorld(t)
	a := w.node("alice@corp.com")
	req, _ := http.NewRequest(http.MethodGet, "http://"+a.d.APIAddr()+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+a.d.Token())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal(resp.Header)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Scan() // hello
	for i := 0; i < 50 && a.d.bus.Clients() == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	a.write("new.md", plan)
	for sc.Scan() {
		if sc.Text() == "event: folder" {
			sc.Scan()
			if !strings.Contains(sc.Text(), "new.md") {
				t.Fatalf("data %q", sc.Text())
			}
			return
		}
	}
	t.Fatal("no folder event", sc.Err())
}

func TestSetupHandler(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, filepath.Join(dir, "cfg"))
	done := make(chan *config.Config, 1)
	var port string
	hs := httptest.NewServer(SetupHandler("tok", func() string { return port }, done))
	defer hs.Close()
	u, _ := url.Parse(hs.URL)
	port = u.Port()
	auth := map[string]string{"Authorization": "Bearer tok"}

	r := get(t, http.DefaultClient, hs.URL+"/api/status", auth)
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	post := func(body string) int {
		req, _ := http.NewRequest(http.MethodPost, hs.URL+"/api/setup", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer tok")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(`{"email":"bad","rendezvous":"peerjs-cloud","folder":"/x"}`); code != 400 {
		t.Fatalf("bad email: %d", code)
	}
	if code := post(`{"email":"a@corp.com","rendezvous":"peerjs-cloud","folder":"relative"}`); code != 400 {
		t.Fatalf("relative folder: %d", code)
	}
	folder := filepath.Join(dir, "bp")
	if code := post(`{"email":"a@corp.com","rendezvous":"peerjs-cloud","folder":"` + filepath.ToSlash(folder) + `"}`); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	cfg := <-done
	if cfg.Email != "a@corp.com" {
		t.Fatal(cfg)
	}
	if st, err := os.Stat(folder); err != nil || !st.IsDir() {
		t.Fatal("folder not created")
	}
	if c, err := config.Load(); err != nil || c.Folder != filepath.ToSlash(folder) {
		t.Fatal(c, err)
	}
}
