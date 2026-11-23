package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/doctor"
	"github.com/golbinski/kalka/internal/plugin"
)

// fakeHost is a plugin host with one tracker, "jira", whose settings live
// in memory, and "gh", which has nothing to set.
type fakeHost struct {
	mu      sync.Mutex
	token   string
	site    string
	forgets atomic.Int32
	active  atomic.Int32
	overlap atomic.Bool
	slow    time.Duration
}

func (f *fakeHost) Find(scheme string) (string, error) {
	if scheme == "jira" || scheme == "gh" {
		return "/x/kalka-plugin-" + scheme, nil
	}
	return "", plugin.ErrNotFound
}
func (f *fakeHost) Installed() []string { return []string{"gh", "jira"} }
func (f *fakeHost) Capabilities(_ context.Context, scheme string) (*plugin.Description, error) {
	if _, err := f.Find(scheme); err != nil {
		return nil, err
	}
	return &plugin.Description{Protocol: 2, Name: scheme, Capabilities: plugin.Capabilities{Resolve: true}}, nil
}
func (f *fakeHost) Resolve(_ context.Context, _ string, _ map[string]any, refs []plugin.Ref) []plugin.Result {
	return make([]plugin.Result, len(refs))
}
func (f *fakeHost) IssueMeta(context.Context, string, []string) ([]plugin.IssueMeta, error) {
	return nil, nil
}
func (f *fakeHost) Export(context.Context, string, plugin.ExportRequest) (*plugin.ExportResult, error) {
	return nil, nil
}
func (f *fakeHost) Forget(string) { f.forgets.Add(1) }

func (f *fakeHost) List(context.Context) plugin.List {
	return plugin.List{Plugins: []plugin.Listed{
		{Scheme: "gh", File: "/x/kalka-plugin-gh", Source: plugin.SourceBundled, Shadowed: []string{}, Capabilities: &plugin.Capabilities{Resolve: true}},
		{Scheme: "jira", File: "/x/kalka-plugin-jira", Source: plugin.SourceBundled, Shadowed: []string{}, Capabilities: &plugin.Capabilities{Settings: &plugin.SettingsCaps{Apply: true, Clear: true}}},
	}}
}

func (f *fakeHost) answer() *plugin.Answer {
	set := f.token != ""
	st := plugin.Status{State: plugin.StateNeedsSetup, Message: "Not connected."}
	if set {
		st = plugin.Status{State: plugin.StateReady, Message: f.site}
	}
	return &plugin.Answer{Scheme: "jira", Can: plugin.Can{Apply: true, Clear: true}, SettingsView: plugin.SettingsView{Status: st, Fields: []plugin.Field{
		{ID: "site", Kind: plugin.KindURL, Label: "Site", Value: f.site},
		{ID: "token", Kind: plugin.KindSecret, Label: "Token", Set: &set},
	}}}
}

func (f *fakeHost) Settings(_ context.Context, scheme string, _ bool) (*plugin.Answer, error) {
	switch scheme {
	case "jira":
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.answer(), nil
	case "gh":
		return nil, plugin.ErrNoSettings
	}
	return nil, plugin.ErrNotFound
}

func (f *fakeHost) Apply(ctx context.Context, scheme string, values map[string]any, secrets map[string]plugin.Secret) (*plugin.Answer, error) {
	if scheme != "jira" {
		return f.Settings(ctx, scheme, false)
	}
	if f.active.Add(1) > 1 {
		f.overlap.Store(true)
	}
	defer f.active.Add(-1)
	time.Sleep(f.slow)
	f.mu.Lock()
	form := f.answer()
	f.mu.Unlock()
	sec, err := plugin.CheckValues(&form.SettingsView, values, secrets)
	if err != nil {
		return nil, err
	}
	if t, ok := sec["token"]; ok && strings.HasPrefix(fmtSecret(t), "wrong") {
		return nil, &plugin.RejectedError{Msg: "The service did not accept the credentials.", Fields: map[string]string{"token": "Check the token."}}
	}
	if t, ok := sec["token"]; ok && strings.HasPrefix(fmtSecret(t), "crash") {
		return nil, &plugin.FailedError{Msg: "kalka-plugin-jira failed: exited with an error"}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := values["site"].(string); ok {
		f.site = s
	}
	if t, ok := sec["token"]; ok {
		f.token = fmtSecret(t)
	}
	return f.answer(), nil
}

// fmtSecret reads a secret's bytes, as only the request to a plugin does.
func fmtSecret(s plugin.Secret) string { return string(s) }

func (f *fakeHost) Clear(_ context.Context, scheme string) (*plugin.Answer, error) {
	if scheme != "jira" {
		return nil, plugin.ErrNoSettings
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.token, f.site = "", ""
	return f.answer(), nil
}

func (f *fakeHost) SurveyAll(context.Context) []plugin.Survey {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.answer().Status
	return []plugin.Survey{{Scheme: "gh"}, {Scheme: "jira", Settings: true, Status: &st}}
}

type rawResp struct {
	code int
	body string
}

func raw(t *testing.T, n *node, method, path string, body any) rawResp {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, "http://"+n.d.APIAddr()+path, rd)
	req.Header.Set("Authorization", "Bearer "+n.d.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return rawResp{resp.StatusCode, string(b)}
}

func TestPluginSettingsAPI(t *testing.T) {
	w := newWorld(t)
	fh := &fakeHost{}
	var logBuf syncLog
	n := w.node("ewa@parcels.example", func(o *Options) { o.Plugins = fh; o.Log = &logBuf })
	const secret = "atl-very-secret-0123"

	if r := raw(t, n, "GET", "/api/plugins", nil); r.code != 200 || !strings.Contains(r.body, `"scheme":"jira"`) {
		t.Fatalf("list: %+v", r)
	}
	if r := raw(t, n, "GET", "/api/plugins/jira/settings?check=1", nil); r.code != 200 || !strings.Contains(r.body, `"needs-setup"`) || !strings.Contains(r.body, `"can":{"apply":true,"clear":true}`) {
		t.Fatalf("get: %+v", r)
	}
	events, stop := n.d.Bus().Subscribe()
	defer stop()
	r := raw(t, n, "PUT", "/api/plugins/jira/settings", map[string]any{"values": map[string]any{"site": "https://parcels.example.net"}, "secrets": map[string]string{"token": secret}})
	if r.code != 200 || !strings.Contains(r.body, `"ready"`) || strings.Contains(r.body, secret) {
		t.Fatalf("put: %+v", r)
	}
	if fh.forgets.Load() != 1 {
		t.Fatalf("forgets %d", fh.forgets.Load())
	}
	sawPlugins := false
	for timeout := time.After(2 * time.Second); !sawPlugins; {
		select {
		case e := <-events:
			sawPlugins = e.Type == "plugins"
		case <-timeout:
			t.Fatal("no plugins event")
		}
	}

	for _, c := range []struct {
		method, path string
		body         any
		code         int
		kind         string
	}{
		{"GET", "/api/plugins/nope/settings", nil, 404, core.KindNoPlugin},
		{"GET", "/api/plugins/gh/settings", nil, 409, core.KindNoPluginSettings},
		{"DELETE", "/api/plugins/gh/settings", nil, 409, core.KindNoPluginSettings},
		{"POST", "/api/plugins/nope/refresh", nil, 404, core.KindNoPlugin},
		{"PUT", "/api/plugins/jira/settings", map[string]any{"values": map[string]any{"token": secret}}, 400, core.KindPluginValue},
		{"PUT", "/api/plugins/jira/settings", map[string]any{"values": map[string]any{"site": "ftp://" + secret}}, 400, core.KindPluginValue},
		{"PUT", "/api/plugins/jira/settings", map[string]any{"secrets": map[string]string{"token": "wrong-" + secret}}, 422, core.KindPluginRejected},
		{"PUT", "/api/plugins/jira/settings", map[string]any{"secrets": map[string]string{"token": "crash-" + secret}}, 502, core.KindPluginFailed},
		{"PUT", "/api/plugins/jira/settings", "{" + secret, 400, ""},
	} {
		r := raw(t, n, c.method, c.path, c.body)
		var e APIError
		_ = json.Unmarshal([]byte(r.body), &e)
		if r.code != c.code || (c.kind != "" && e.Kind != c.kind) || strings.Contains(r.body, secret) {
			t.Errorf("%s %s: %d %s", c.method, c.path, r.code, r.body)
		}
		if (c.code == 400 && c.kind != "") || c.code == 422 {
			if len(e.Fields) == 0 {
				t.Errorf("%s %s: no fields: %s", c.method, c.path, r.body)
			}
		}
	}
	if fh.forgets.Load() != 1 {
		t.Fatalf("a failed change invalidated: %d", fh.forgets.Load())
	}
	if r := raw(t, n, "POST", "/api/plugins/jira/refresh", nil); r.code != 200 || fh.forgets.Load() != 2 {
		t.Fatalf("refresh: %+v", r)
	}
	for i := 0; i < 2; i++ {
		if r := raw(t, n, "DELETE", "/api/plugins/jira/settings", nil); r.code != 200 || !strings.Contains(r.body, `"needs-setup"`) {
			t.Fatalf("delete: %+v", r)
		}
	}
	if strings.Contains(logBuf.String(), secret) {
		t.Fatal("the log holds the secret")
	}

	// Doctor: a plugin not set up is fine.
	var rep doctor.Report
	if err := NewClient(n.d.APIAddr(), n.d.Token()).do(context.Background(), http.MethodGet, "/api/doctor", nil, &rep); err != nil {
		t.Fatal(err)
	}
	rows := map[string]doctor.Result{}
	for _, x := range rep.Results {
		rows[x.Check] = x
	}
	if rows["plugin:jira"].State != doctor.OK || !strings.HasPrefix(rows["plugin:jira"].Detail, "not set up") || rows["plugin:gh"].State != doctor.OK {
		t.Fatalf("doctor: %+v", rep.Results)
	}
}

func TestPluginChangesSerialise(t *testing.T) {
	w := newWorld(t)
	fh := &fakeHost{slow: 200 * time.Millisecond}
	n := w.node("ewa@parcels.example", func(o *Options) { o.Plugins = fh })
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw(t, n, "PUT", "/api/plugins/jira/settings", map[string]any{"secrets": map[string]string{"token": "good-token"}})
		}()
	}
	wg.Wait()
	if fh.overlap.Load() {
		t.Fatal("two applies ran at once")
	}
}

// The gate guards these routes like every other.
func TestPluginRoutesNeedTheToken(t *testing.T) {
	w := newWorld(t)
	n := w.node("ewa@parcels.example", func(o *Options) { o.Plugins = &fakeHost{} })
	req, _ := http.NewRequest("PUT", "http://"+n.d.APIAddr()+"/api/plugins/jira/settings", strings.NewReader(`{"secrets":{"token":"x"}}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no token: %d", resp.StatusCode)
	}
}

type syncLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncLog) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncLog) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }
