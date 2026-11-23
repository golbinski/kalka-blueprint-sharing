package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/plugin"
)

// fakePluginMain plays kalka-plugin-demo: a site, a bool and a token,
// kept in $KALKA_FAKE_PLUGIN_STATE.
func fakePluginMain() {
	raw, _ := io.ReadAll(os.Stdin)
	var req struct {
		Op      string
		Values  map[string]any
		Secrets map[string]string
	}
	_ = json.Unmarshal(raw, &req)
	path := os.Getenv("KALKA_FAKE_PLUGIN_STATE")
	st := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	out := map[string]any{"protocol": 2}
	view := func() {
		state, msg := "needs-setup", "Not connected."
		if st["token"] != nil {
			state, msg = "ready", fmt.Sprint(st["site"])
		}
		out["status"] = map[string]any{"state": state, "message": msg}
		out["fields"] = []map[string]any{
			{"id": "site", "kind": "url", "label": "Site", "required": true, "value": st["site"]},
			{"id": "verbose", "kind": "bool", "label": "Verbose", "value": st["verbose"]},
			{"id": "token", "kind": "secret", "label": "Token", "required": true, "set": st["token"] != nil},
		}
		out["hint"] = map[string]any{"text": "Get a token from the demo site."}
	}
	switch req.Op {
	case "describe":
		out = map[string]any{"protocol": 2, "name": "demo", "version": "0.1", "schemes": []string{"demo"},
			"capabilities": map[string]any{"resolve": true, "settings": map[string]bool{"apply": true, "clear": true}}}
	case "settings":
		view()
	case "settings_apply":
		if strings.HasPrefix(req.Secrets["token"], "wrong") {
			out["ok"], out["error"], out["field_errors"] = false, "The demo site refused the token.", map[string]string{"token": "Check the token."}
			break
		}
		for k, v := range req.Values {
			st[k] = v
		}
		if t, ok := req.Secrets["token"]; ok {
			st["token"] = t
		}
		b, _ := json.Marshal(st)
		os.WriteFile(path, b, 0o600)
		out["ok"] = true
		view()
	case "settings_clear":
		st = map[string]any{}
		os.Remove(path)
		out["ok"] = true
		view()
	}
	b, _ := json.Marshal(out)
	os.Stdout.Write(b)
}

func installDemo(t *testing.T) (dir, state string) {
	t.Helper()
	dir = t.TempDir()
	t.Setenv("KALKA_CONFIG_DIR", dir)
	t.Setenv("KALKA_NO_START", "1")
	state = filepath.Join(t.TempDir(), "demo.json")
	t.Setenv("KALKA_FAKE_PLUGIN_STATE", state)
	bin, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	os.MkdirAll(filepath.Join(dir, "plugins"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "plugins", plugin.Prefix+"demo"+ext), bin, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, state
}

func kalka(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errs strings.Builder
	code := run(args, strings.NewReader(stdin), &out, &errs, time.Now)
	return code, out.String(), errs.String()
}

func TestPluginCommandsWithoutDaemon(t *testing.T) {
	_, state := installDemo(t)
	const secret = "demo-secret-0123456789"

	code, out, errs := kalka(t, "", "plugins")
	if code != 0 || !strings.Contains(out, "demo") || !strings.Contains(out, "not set up: Not connected.") || !strings.Contains(out, "not running") {
		t.Fatalf("plugins %d: %s%s", code, out, errs)
	}
	code, out, _ = kalka(t, "", "plugins", "--json")
	var pv pluginsView
	if code != 0 || json.Unmarshal([]byte(out), &pv) != nil || pv.Daemon || len(pv.Plugins) != 1 || pv.Plugins[0].Status.State != plugin.StateNeedsSetup {
		t.Fatalf("plugins --json: %s", out)
	}

	// Usage errors: a secret on the command line is refused, not echoed.
	for _, args := range [][]string{
		{"plugin", "set", "demo", "token=" + secret},
		{"plugin", "set", "demo", "site"},
		{"plugin", "set", "demo", "--secret-stdin", "token", "--secret-stdin", "token"},
		{"plugin", "set", "demo"}, // nothing, and not a terminal
		{"plugin", "set"},
		{"plugin", "frobnicate"},
	} {
		code, out, errs := kalka(t, "", args...)
		if code != exitUsage || strings.Contains(out+errs, secret) {
			t.Errorf("%v: %d %s%s", args, code, out, errs)
		}
	}
	if _, err := os.Stat(state); err == nil {
		t.Fatal("a usage error stored something")
	}

	// A value that does not fit: plugin-value with fields; the plugin was not asked.
	code, out, _ = kalka(t, "", "plugin", "set", "demo", "site=ftp://demo.example", "--json")
	var e cliError
	if code != exitProblems || json.Unmarshal([]byte(out), &e) != nil || e.Kind != core.KindPluginValue || e.Fields["site"] == "" {
		t.Fatalf("bad value: %d %s", code, out)
	}
	// Refused by the plugin: plugin-rejected, nothing stored, the secret not shown.
	code, out, errs = kalka(t, "wrong-"+secret+"\n", "plugin", "set", "demo", "site=https://demo.example", "--secret-stdin", "token", "--json")
	if code != exitProblems || json.Unmarshal([]byte(out), &e) != nil || e.Kind != core.KindPluginRejected || e.Fields["token"] == "" || strings.Contains(out+errs, secret) {
		t.Fatalf("rejected: %d %s%s", code, out, errs)
	}
	// Applied.
	code, out, errs = kalka(t, secret+"\r\n", "plugin", "set", "demo", "site=https://demo.example", "verbose=on", "--secret-stdin", "token")
	if code != 0 || !strings.Contains(out, "ready: https://demo.example") || strings.Contains(out+errs, secret) {
		t.Fatalf("set: %d %s%s", code, out, errs)
	}
	raw, _ := os.ReadFile(state)
	if !strings.Contains(string(raw), `"token":"`+secret+`"`) || !strings.Contains(string(raw), `"verbose":true`) {
		t.Fatalf("stored %s", raw)
	}
	code, out, _ = kalka(t, "", "plugin", "settings", "demo")
	if code != 0 || !strings.Contains(out, "stored") || strings.Contains(out, secret) || !strings.Contains(out, "Get a token") {
		t.Fatalf("settings: %s", out)
	}
	code, out, _ = kalka(t, "", "plugin", "settings", "demo", "--json")
	var sv pluginSettingsView
	if code != 0 || json.Unmarshal([]byte(out), &sv) != nil || sv.Daemon || !sv.Can.Apply || strings.Contains(out, secret) {
		t.Fatalf("settings --json: %s", out)
	}
	for i := 0; i < 2; i++ {
		if code, out, errs := kalka(t, "", "plugin", "clear", "demo", "--json"); code != 0 || !strings.Contains(out, `"needs-setup"`) || !strings.Contains(out, `"refreshed": false`) {
			t.Fatalf("clear %d: %d %s%s", i, code, out, errs)
		}
	}
	if code, out, _ := kalka(t, "", "plugin", "settings", "nope", "--json"); code != exitProblems || !strings.Contains(out, core.KindNoPlugin) {
		t.Fatalf("no plugin: %s", out)
	}
}

// With a daemon running, the views ask it, and set tells it, warning
// when it sees the plugin otherwise.
func TestPluginCommandsWithDaemon(t *testing.T) {
	dir, _ := installDemo(t)
	var refreshed atomic.Int32
	daemonAnswer := plugin.Answer{Scheme: "demo", SettingsView: plugin.SettingsView{Status: plugin.Status{State: plugin.StateError, Message: "no credential store here"}, Fields: []plugin.Field{}}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{}`)) })
	mux.HandleFunc("GET /api/plugins", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(plugin.List{Plugins: []plugin.Listed{{Scheme: "demo", File: "/daemon/kalka-plugin-demo", Source: plugin.SourcePath, Shadowed: []string{},
			Capabilities: &plugin.Capabilities{Settings: &plugin.SettingsCaps{Apply: true}}}}})
	})
	mux.HandleFunc("GET /api/plugins/demo/settings", func(w http.ResponseWriter, _ *http.Request) { json.NewEncoder(w).Encode(daemonAnswer) })
	mux.HandleFunc("POST /api/plugins/demo/refresh", func(w http.ResponseWriter, _ *http.Request) {
		refreshed.Add(1)
		json.NewEncoder(w).Encode(daemonAnswer)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	u, _ := net.ResolveTCPAddr("tcp", strings.TrimPrefix(srv.URL, "http://"))
	ep, _ := json.Marshal(daemon.Endpoint{Addr: u.String(), Token: "t"})
	os.WriteFile(filepath.Join(dir, daemon.DaemonFile), ep, 0o600)

	code, out, _ := kalka(t, "", "plugins", "--json")
	if code != 0 || !strings.Contains(out, `"daemon": true`) || !strings.Contains(out, "/daemon/kalka-plugin-demo") || !strings.Contains(out, "no credential store here") {
		t.Fatalf("plugins via daemon: %s", out)
	}
	code, out, errs := kalka(t, "demo-token-0123\n", "plugin", "set", "demo", "site=https://demo.example", "--secret-stdin", "token", "--json")
	if code != 0 || refreshed.Load() != 1 || !strings.Contains(out, `"refreshed": true`) || !strings.Contains(errs, "sees this plugin as: error, no credential store here") {
		t.Fatalf("set with daemon: %d %s%s", code, out, errs)
	}
}

// Doctor reports each plugin.
func TestDoctorPluginRow(t *testing.T) {
	installDemo(t)
	code, out, _ := kalka(t, "", "doctor", "--json")
	if !strings.Contains(out, `"plugin:demo"`) || !strings.Contains(out, "not set up: Not connected.") {
		t.Fatalf("doctor %d: %s", code, out)
	}
}
