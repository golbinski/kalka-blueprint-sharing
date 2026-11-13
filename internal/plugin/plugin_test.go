package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as the plugins: copied as kalka-plugin-<mode>,
// it behaves according to <mode>.
func TestMain(m *testing.M) {
	base := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if mode, ok := strings.CutPrefix(base, Prefix); ok {
		fake(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fake(mode string) {
	raw, _ := io.ReadAll(os.Stdin)
	var req struct {
		Op      string
		Context struct{ Frontmatter map[string]any }
		Refs    []Ref
	}
	_ = json.Unmarshal(raw, &req)
	if len(os.Args) > 1 {
		fmt.Println(`{"protocol":1,"results":[]}`) // arguments are never passed
		return
	}
	switch mode {
	case "crash":
		fmt.Fprintln(os.Stderr, "boom: segfault in parser")
		os.Exit(2)
	case "sleep":
		time.Sleep(8 * time.Second)
	case "garbage":
		fmt.Println("hello, I am not JSON")
	case "flood":
		os.Stdout.Write([]byte(strings.Repeat("x", maxStdout+10)))
	}
	if req.Op == "describe" {
		fmt.Println(`{"protocol":1,"name":"fake","version":"0.1","schemes":["ok"]}`)
		return
	}
	var res []map[string]any
	for i, r := range req.Refs {
		if mode == "partial" && i == 1 {
			continue
		}
		res = append(res, map[string]any{
			"ref": r.Ref, "ok": true, "title": "T " + r.Payload + strings.Repeat("!", 2000),
			"url":      map[bool]string{true: "javascript:alert(1)", false: "https://example.com/" + r.Payload}[strings.Contains(r.Payload, "evil")],
			"status":   fmt.Sprint(req.Context.Frontmatter["repo"]),
			"snippet":  map[string]any{"start_line": 3, "text": "<b>x</b>\n"},
			"warnings": []map[string]string{{"text": "changed"}},
		})
	}
	out, _ := json.Marshal(map[string]any{"protocol": 1, "results": res})
	os.Stdout.Write(out)
}

func install(t *testing.T, dir string, modes ...string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	for _, m := range modes {
		if err := os.WriteFile(filepath.Join(dir, Prefix+m+ext), bin, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func refs(payloads ...string) []Ref {
	var out []Ref
	for _, p := range payloads {
		out = append(out, Ref{Ref: "x:" + p, Payload: p})
	}
	return out
}

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	install(t, dir, "ok", "crash", "sleep", "garbage", "partial", "flood")
	var log strings.Builder
	h := &Host{Dir: dir, Log: &log}
	ctx := context.Background()
	fm := map[string]any{"repo": "org/doc-service"}

	got := h.Resolve(ctx, "ok", fm, refs("a.py", "evil.py"))
	if len(got) != 2 || !got[0].OK || got[0].Status != "org/doc-service" || got[0].URL != "https://example.com/a.py" || got[0].Snippet.Text != "<b>x</b>\n" {
		t.Fatalf("ok: %+v", got)
	}
	// Plugin output is capped and a non-web URL is dropped.
	if got[1].URL != "" || len(got[0].Title) > maxText+4 || !strings.HasSuffix(got[0].Title, "…") {
		t.Fatalf("not cleaned: %q %d", got[1].URL, len(got[0].Title))
	}

	old := Timeout
	Timeout = 2 * time.Second
	defer func() { Timeout = old }()
	for mode, want := range map[string]string{
		"missing": "no plugin installed for missing:",
		"crash":   "exited with an error",
		"sleep":   "timed out",
		"garbage": "not the expected JSON",
		"flood":   "more output than allowed",
	} {
		start := time.Now()
		got := h.Resolve(ctx, mode, fm, refs("a.py", "b.py"))
		if len(got) != 2 || got[0].OK || got[1].OK || !strings.Contains(got[0].Error, want) || got[1].Ref != "x:b.py" {
			t.Errorf("%s: %+v", mode, got)
		}
		if time.Since(start) > 4*time.Second {
			t.Errorf("%s took %v", mode, time.Since(start))
		}
	}
	if !strings.Contains(log.String(), "boom: segfault") {
		t.Errorf("stderr not logged: %q", log.String())
	}
	// A ref the plugin skipped gets its own error; the others resolve.
	got = h.Resolve(ctx, "partial", fm, refs("a.py", "b.py", "c.py"))
	if !got[0].OK || got[1].OK || !strings.Contains(got[1].Error, "no result") || !got[2].OK {
		t.Fatalf("partial: %+v", got)
	}
}

func TestFindAndDescribe(t *testing.T) {
	dir := t.TempDir()
	install(t, dir, "ok")
	h := &Host{Dir: dir}
	p, err := h.Find("ok")
	if err != nil || filepath.Dir(p) != dir {
		t.Fatal(p, err)
	}
	for _, bad := range []string{"../ok", "OK", "", "o k"} {
		if _, err := h.Find(bad); err == nil {
			t.Errorf("found %q", bad)
		}
	}
	d, err := h.Describe(context.Background(), p)
	if err != nil || d.Name != "fake" || d.Schemes[0] != "ok" {
		t.Fatal(d, err)
	}
	// Plugins on PATH are found after the config dir.
	other := t.TempDir()
	install(t, other, "onpath")
	t.Setenv("PATH", other+string(os.PathListSeparator)+os.Getenv("PATH"))
	if p, err := h.Find("onpath"); err != nil || filepath.Dir(p) != other {
		t.Fatal(p, err)
	}
}
