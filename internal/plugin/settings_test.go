package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// settingsFake plays the plugins of the settings tests:
//
//	form      a full form kept in the file $KALKA_FAKE_STATE
//	status    status and hint only; the state from $KALKA_FAKE_STATUS
//	leaky     sends a secret's value and echoes what it is given
//	newer     fields this kalka does not know or that break the rules
//	slowform  takes too long to apply
func settingsFake(mode, op string, raw []byte) bool {
	switch mode {
	case "form", "status", "leaky", "newer", "slowform":
	default:
		return false
	}
	var req struct {
		Check   bool
		Values  map[string]any
		Secrets map[string]string
	}
	_ = json.Unmarshal(raw, &req)
	write := func(v map[string]any) {
		v["protocol"] = 2
		b, _ := json.Marshal(v)
		os.Stdout.Write(b)
	}
	if op == "describe" {
		caps := map[string]any{"apply": true, "clear": true}
		if mode == "status" {
			caps = map[string]any{}
		}
		write(map[string]any{"name": mode, "version": "0.1", "schemes": []string{mode}, "capabilities": map[string]any{"settings": caps}})
		return true
	}
	switch mode {
	case "form":
		formFake(op, req.Values, req.Secrets, write)
	case "status":
		write(map[string]any{"status": map[string]any{"state": os.Getenv("KALKA_FAKE_STATUS"), "message": "Uses another tool's login.\nsecond line"},
			"hint": map[string]any{"text": "Log in with the other tool.", "command": "fake auth login", "url": "javascript:alert(1)"}})
	case "leaky":
		if op == "settings" {
			write(map[string]any{"status": map[string]any{"state": "ready", "message": "ok"},
				"fields": []map[string]any{{"id": "token", "kind": "secret", "label": "Token", "value": "stored-secret-value", "set": true}}})
			return true
		}
		tok := req.Secrets["token"]
		fmt.Fprintf(os.Stderr, "debug: got token %s (%s)\n", tok, strings.ToUpper(tok))
		write(map[string]any{"ok": false, "error": "token " + tok + " is invalid", "field_errors": map[string]string{"token": "not " + tok}})
	case "newer":
		fields := []map[string]any{
			{"id": "fingerprint", "kind": "passkey", "label": "Passkey", "required": true},
			{"id": "site", "kind": "url", "label": "Site"},
			{"id": "site", "kind": "text", "label": "Again"},
			{"id": "extra", "kind": "text", "label": "Extra", "when": map[string]any{"field": "site", "is": []string{"x"}}},
			{"id": "long", "kind": "text", "label": strings.Repeat("L", 300)},
		}
		for i := 0; i < 20; i++ {
			fields = append(fields, map[string]any{"id": fmt.Sprintf("f%d", i), "kind": "text", "label": "F"})
		}
		write(map[string]any{"status": map[string]any{"state": "needs-setup", "message": "m"}, "fields": fields})
	case "slowform":
		if op == "settings_apply" {
			time.Sleep(5 * time.Second)
		}
		write(map[string]any{"status": map[string]any{"state": "needs-setup", "message": "m"},
			"fields": []map[string]any{{"id": "token", "kind": "secret", "label": "Token"}}})
	}
	return true
}

type formState struct {
	Values  map[string]any `json:"values"`
	Token   string         `json:"token"`
	Applies int            `json:"applies"`
}

func formFake(op string, values map[string]any, secrets map[string]string, write func(map[string]any)) {
	path := os.Getenv("KALKA_FAKE_STATE")
	var st formState
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	if st.Values == nil {
		st.Values = map[string]any{"flavor": "cloud"}
	}
	view := func() map[string]any {
		state, msg := "needs-setup", "Not connected."
		if st.Token != "" {
			state, msg = "ready", fmt.Sprint(st.Values["site"])
		}
		return map[string]any{"status": map[string]any{"state": state, "message": msg},
			"fields": []map[string]any{
				{"id": "flavor", "kind": "choice", "label": "Kind", "required": true, "value": st.Values["flavor"],
					"choices": []map[string]string{{"value": "cloud", "label": "Cloud"}, {"value": "dc", "label": "Data Center"}}},
				{"id": "site", "kind": "url", "label": "Site", "required": true, "value": st.Values["site"]},
				{"id": "email", "kind": "email", "label": "Email", "value": st.Values["email"], "when": map[string]any{"field": "flavor", "is": []string{"cloud"}}},
				{"id": "verbose", "kind": "bool", "label": "Verbose", "value": st.Values["verbose"]},
				{"id": "token", "kind": "secret", "label": "Token", "required": true, "set": st.Token != ""},
			}}
	}
	save := func() { b, _ := json.Marshal(st); os.WriteFile(path, b, 0o600) }
	switch op {
	case "settings":
		write(view())
	case "settings_apply":
		st.Applies++
		if strings.HasPrefix(secrets["token"], "wrong-") {
			save()
			write(map[string]any{"ok": false, "error": "The service did not accept the credentials.",
				"field_errors": map[string]string{"token": "Check the token.", "nope": "dropped"}})
			return
		}
		for k, v := range values {
			st.Values[k] = v
		}
		if t, ok := secrets["token"]; ok {
			st.Token = t
		}
		save()
		v := view()
		v["ok"] = true
		write(v)
	case "settings_clear":
		st = formState{Applies: st.Applies}
		save()
		v := view()
		v["ok"] = true
		write(v)
	}
}

func readForm(t *testing.T) formState {
	t.Helper()
	var st formState
	b, _ := os.ReadFile(os.Getenv("KALKA_FAKE_STATE"))
	_ = json.Unmarshal(b, &st)
	return st
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func formHost(t *testing.T, modes ...string) (*Host, *syncBuf) {
	t.Helper()
	dir := t.TempDir()
	install(t, dir, modes...)
	t.Setenv("KALKA_FAKE_STATE", filepath.Join(t.TempDir(), "state.json"))
	log := &syncBuf{}
	return &Host{Dir: dir, Log: log}, log
}

func TestSettingsForm(t *testing.T) {
	h, _ := formHost(t, "form")
	ctx := context.Background()
	v, err := h.Settings(ctx, "form", false)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status.State != StateNeedsSetup || len(v.Fields) != 5 || v.Incomplete {
		t.Fatalf("%+v", v)
	}
	tok, _ := v.Field("token")
	if tok.Set == nil || *tok.Set || tok.Value != nil {
		t.Fatalf("token %+v", tok)
	}
	if em, _ := v.Field("email"); em.When == nil || em.When.Field != "flavor" {
		t.Fatalf("email %+v", em)
	}

	res, err := h.Apply(ctx, "form", map[string]any{"site": "https://parcels.example.net", "email": "ewa@parcels.example", "verbose": true},
		map[string]Secret{"token": " good-token-123\n"})
	if err != nil || !res.OK || res.View.Status.State != StateReady || res.View.Status.Message != "https://parcels.example.net" {
		t.Fatalf("%+v %v", res, err)
	}
	if st := readForm(t); st.Token != "good-token-123" || st.Applies != 1 {
		t.Fatalf("stored %+v", st)
	}
	v, _ = h.Settings(ctx, "form", false)
	if tok, _ := v.Field("token"); tok.Set == nil || !*tok.Set || tok.Value != nil {
		t.Fatalf("token after apply %+v", tok)
	}

	// A refusal: field errors for fields of the form only, nothing stored.
	res, err = h.Apply(ctx, "form", nil, map[string]Secret{"token": "wrong-token"})
	if err != nil || res.OK || res.Error == "" || res.FieldErrors["token"] != "Check the token." || res.FieldErrors["nope"] != "" {
		t.Fatalf("%+v %v", res, err)
	}
	if st := readForm(t); st.Token != "good-token-123" {
		t.Fatalf("refusal changed the token: %+v", st)
	}

	// Values that do not fit never reach the plugin.
	before := readForm(t).Applies
	for _, c := range []struct {
		values  map[string]any
		secrets map[string]Secret
		field   string
	}{
		{map[string]any{"nope": "x"}, nil, "nope"},
		{map[string]any{"token": "x"}, nil, "token"},
		{nil, map[string]Secret{"site": "x"}, "site"},
		{map[string]any{"flavor": "dc", "email": "ewa@parcels.example"}, nil, "email"},
		{map[string]any{"site": "ftp://parcels.example.net"}, nil, "site"},
		{map[string]any{"site": "https://ewa:pw@parcels.example.net"}, nil, "site"},
		{map[string]any{"verbose": "yes"}, nil, "verbose"},
		{map[string]any{"flavor": "server"}, nil, "flavor"},
		{map[string]any{"email": "not-an-address"}, nil, "email"},
		{map[string]any{"site": "https://x.example/\u0007"}, nil, "site"},
		{nil, map[string]Secret{"token": "  \n"}, "token"},
		{nil, map[string]Secret{"token": Secret(strings.Repeat("t", MaxSecret+1))}, "token"},
	} {
		_, err := h.Apply(ctx, "form", c.values, c.secrets)
		var ve *ValueError
		if !errors.As(err, &ve) || ve.Fields[c.field] == "" {
			t.Errorf("%v %v: %v", c.values, c.secrets, err)
		}
	}
	if readForm(t).Applies != before {
		t.Fatal("the plugin was asked to apply values that do not fit")
	}

	// A field shown only for another choice may be set with that choice.
	if res, err := h.Apply(ctx, "form", map[string]any{"flavor": "dc"}, nil); err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}

	res, err = h.Clear(ctx, "form")
	if err != nil || !res.OK || res.View.Status.State != StateNeedsSetup {
		t.Fatalf("%+v %v", res, err)
	}
}

// A plugin that sends a secret's value, echoes it in its answer or on
// stderr gets none of it past the host.
func TestSettingsSecretsNeverLeak(t *testing.T) {
	h, log := formHost(t, "leaky")
	ctx := context.Background()
	v, err := h.Settings(ctx, "leaky", false)
	if err != nil {
		t.Fatal(err)
	}
	if tok, _ := v.Field("token"); tok.Value != nil || tok.Set == nil || !*tok.Set {
		t.Fatalf("%+v", tok)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "stored-secret-value") {
		t.Fatalf("stored secret passed on: %s", raw)
	}
	const secret = "abc123-very-secret"
	res, err := h.Apply(ctx, "leaky", nil, map[string]Secret{"token": secret})
	if err != nil || res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	all, _ := json.Marshal(res)
	if strings.Contains(string(all), secret) || !strings.Contains(res.Error, "[secret]") || !strings.Contains(res.FieldErrors["token"], "[secret]") {
		t.Fatalf("answer: %s", all)
	}
	l := log.String()
	if strings.Contains(l, secret) || strings.Contains(l, strings.ToUpper(secret)) || strings.Contains(l, "stored-secret-value") {
		t.Fatalf("log holds the secret: %s", l)
	}
	if !strings.Contains(l, "withheld") || !strings.Contains(l, "sent the value of secret field token") {
		t.Fatalf("log: %s", l)
	}
}

func TestSettingsNewerPlugin(t *testing.T) {
	h, _ := formHost(t, "newer")
	ctx := context.Background()
	v, err := h.Settings(ctx, "newer", false)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Incomplete || len(v.Fields) > MaxFields {
		t.Fatalf("incomplete %v, %d fields", v.Incomplete, len(v.Fields))
	}
	for _, f := range v.Fields {
		if f.ID == "fingerprint" || f.ID == "extra" || len(f.Label) > maxLabel+len("…") {
			t.Errorf("kept %+v", f)
		}
	}
	if _, err := h.Apply(ctx, "newer", map[string]any{"f1": "x"}, nil); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("applied an incomplete form: %v", err)
	}
}

func TestSettingsStatusOnly(t *testing.T) {
	h, _ := formHost(t, "status", "ok")
	ctx := context.Background()
	t.Setenv("KALKA_FAKE_STATUS", "weird")
	v, err := h.Settings(ctx, "status", false)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status.State != StateError || strings.Contains(v.Status.Message, "\n") || len(v.Fields) != 0 {
		t.Fatalf("%+v", v)
	}
	if v.Hint == nil || v.Hint.Command != "fake auth login" || v.Hint.URL != "" {
		t.Fatalf("hint %+v", v.Hint)
	}
	if _, err := h.Apply(ctx, "status", nil, nil); !errors.Is(err, ErrNoSettings) {
		t.Fatal(err)
	}
	if _, err := h.Clear(ctx, "status"); !errors.Is(err, ErrNoSettings) {
		t.Fatal(err)
	}
	if _, err := h.Settings(ctx, "ok", false); !errors.Is(err, ErrNoSettings) {
		t.Fatal(err)
	}
	if _, err := h.Settings(ctx, "missing", false); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSettingsTimeout(t *testing.T) {
	h, _ := formHost(t, "slowform")
	defer func(d time.Duration) { SettingsTimeout = d }(SettingsTimeout)
	SettingsTimeout = time.Second
	_, err := h.Apply(context.Background(), "slowform", nil, map[string]Secret{"token": "slow-secret-1"})
	if err == nil || !strings.Contains(err.Error(), "timed out") || strings.Contains(err.Error(), "slow-secret-1") {
		t.Fatal(err)
	}
}

func TestSecretPrints(t *testing.T) {
	s := Secret("hunter2-token")
	for _, f := range []string{"%v", "%s", "%q", "%#v", "%+v", "%x"} {
		if out := fmt.Sprintf(f, s); strings.Contains(out, "hunter2") {
			t.Errorf("%s: %s", f, out)
		}
	}
	if out := fmt.Sprintf("%v", map[string]Secret{"token": s}); strings.Contains(out, "hunter2") {
		t.Error(out)
	}
	b, _ := json.Marshal(map[string]any{"token": s})
	if strings.Contains(string(b), "hunter2") {
		t.Error(string(b))
	}
}

func TestLocate(t *testing.T) {
	cfg, beside, onPath := t.TempDir(), t.TempDir(), t.TempDir()
	install(t, cfg, "ok")
	install(t, beside, "ok", "both")
	install(t, onPath, "ok", "both", "pathonly")
	t.Setenv("PATH", onPath+string(os.PathListSeparator)+os.Getenv("PATH"))
	h := &Host{Dir: cfg, ExeDir: beside}
	f, err := h.Locate("ok")
	if err != nil || f.Source != SourceConfig || filepath.Dir(f.File) != cfg || len(f.Shadowed) != 2 {
		t.Fatalf("%+v %v", f, err)
	}
	if f, _ := h.Locate("both"); f.Source != SourceBundled || len(f.Shadowed) != 1 || filepath.Dir(f.Shadowed[0]) != onPath {
		t.Fatalf("%+v", f)
	}
	if f, _ := h.Locate("pathonly"); f.Source != SourcePath || len(f.Shadowed) != 0 {
		t.Fatalf("%+v", f)
	}
}
