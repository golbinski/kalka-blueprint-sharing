package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

// fakeGH answers fixed endpoints and records what was asked.
type fakeGH struct {
	mu    sync.Mutex
	calls []string
	resp  map[string]string // endpoint -> "status body"
}

func (f *fakeGH) get(endpoint, accept string) (int, []byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, endpoint)
	f.mu.Unlock()
	r, ok := f.resp[endpoint]
	if !ok {
		return 404, []byte(`{"message":"Not Found"}`), nil
	}
	var st int
	fmt.Sscanf(r, "%d", &st)
	_, body, _ := strings.Cut(r, " ")
	return st, []byte(body), nil
}

const parsePy = "line1\nline2\nline3\nline4\nline5\n"

func newFake() *fakeGH {
	return &fakeGH{resp: map[string]string{
		"repos/org/doc-service":                 `200 {"default_branch":"main"}`,
		"repos/org/doc-service/commits/3f9c2a1": `200 {"sha":"3f9c2a1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
		"repos/org/doc-service/compare/3f9c2a1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa...main":                   `200 {"ahead_by":3,"files":[{"filename":"src/notify/batch.py"}]}`,
		"repos/org/doc-service/contents/src/import/parse.py?ref=3f9c2a1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": "200 " + parsePy,
		"repos/org/doc-service/contents/src/notify/batch.py?ref=3f9c2a1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": "200 x = 1\n",
		"repos/org/doc-service/contents/src/my%20dir/a.go?ref=3f9c2a1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa":   "200 package a\n",
		"repos/org/doc-service/contents/src?ref=3f9c2a1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa":                 `200 [{"name":"import"}]`,
		"repos/org/doc-service/contents/src/import/parse.py?ref=main":                                     "200 " + parsePy,
	}}
}

func call(t *testing.T, gh *fakeGH, fm map[string]any, payloads ...string) []result {
	t.Helper()
	req := map[string]any{"protocol": 1, "op": "resolve", "context": map[string]any{"frontmatter": fm}}
	var refs []map[string]string
	for _, p := range payloads {
		refs = append(refs, map[string]string{"ref": "gh:" + p, "payload": p})
	}
	req["refs"] = refs
	in, _ := json.Marshal(req)
	var out, errb bytes.Buffer
	if code := run(bytes.NewReader(in), &out, &errb, func(host string) api {
		if host != "" && host != "ghe.corp.com" {
			t.Errorf("host %q", host)
		}
		return gh
	}); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var resp struct {
		Protocol int
		Results  []result
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil || resp.Protocol != 1 || len(resp.Results) != len(payloads) {
		t.Fatalf("response %s %v", out.String(), err)
	}
	return resp.Results
}

var fm = map[string]any{"repo": "org/doc-service", "base_sha": "3f9c2a1"}

func TestResolve(t *testing.T) {
	gh := newFake()
	res := call(t, gh, fm, "src/import/parse.py#L2-L4", "src/notify/batch.py", "src/import/parse.py#L4-L9", "src/my dir/a.go", "src", "src/missing.py")

	p := res[0]
	if !p.OK || p.Title != "src/import/parse.py · L2–4" || p.Snippet.StartLine != 2 || p.Snippet.Text != "line2\nline3\nline4\n" || p.Snippet.Lang != "python" ||
		p.URL != "https://github.com/org/doc-service/blob/3f9c2a1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/src/import/parse.py#L2-L4" ||
		p.Status != "unchanged on main since 3f9c2a1" || len(p.Warnings) != 0 {
		t.Fatalf("parse.py %+v", p)
	}
	if b := res[1]; !b.OK || len(b.Warnings) != 1 || b.Warnings[0].Text != "changed on main since 3f9c2a1 (main is 3 commits ahead)" || b.Snippet.StartLine != 1 {
		t.Fatalf("batch.py %+v", b)
	}
	if r := res[2]; !r.OK || r.Snippet.Text != "line4\nline5\n" || r.Warnings[0].Text != "the file has only 5 lines" {
		t.Fatalf("past the end %+v", r)
	}
	if r := res[3]; !r.OK || !strings.Contains(r.URL, "/src/my%20dir/a.go") {
		t.Fatalf("space %+v", r)
	}
	if r := res[4]; !r.OK || r.Title != "src/" || r.Snippet != nil || !strings.Contains(r.URL, "/tree/") {
		t.Fatalf("dir %+v", r)
	}
	if r := res[5]; r.OK || r.Error != "src/missing.py does not exist at 3f9c2a1" {
		t.Fatalf("missing %+v", r)
	}
	// One compare per batch; one contents call per path.
	n := 0
	for _, c := range gh.calls {
		if strings.Contains(c, "/compare/") {
			n++
		}
		if strings.Contains(c, "parse.py") {
			n += 10
		}
	}
	if n != 11 {
		t.Fatalf("calls %v", gh.calls)
	}
}

func TestPayloadValidation(t *testing.T) {
	gh := newFake()
	bad := []string{"../etc/passwd", "/abs.py", "src/../x", "a.py#L5-L2", "a.py#line5", "a.py#L0", "src/", "", "a;rm -rf.py", "a\nb"}
	res := call(t, gh, fm, bad...)
	for i, r := range res {
		if r.OK || r.Error == "" {
			t.Errorf("%q accepted: %+v", bad[i], r)
		}
	}
	for _, c := range gh.calls {
		if strings.Contains(c, "contents") {
			t.Errorf("fetched for an invalid payload: %s", c)
		}
	}
}

func TestFrontmatter(t *testing.T) {
	for _, c := range []struct {
		fm   map[string]any
		want string
	}{
		{map[string]any{}, "no repo"},
		{map[string]any{"repo": "--help"}, "not owner/name"},
		{map[string]any{"repo": "org/x y"}, "not owner/name"},
		{map[string]any{"repo": "org/doc-service", "base_sha": "HEAD; ls"}, "not a commit hash"},
		{map[string]any{"repo": "org/doc-service", "host": "-oProxy"}, "not a host name"},
		{map[string]any{"repo": "org/nope"}, "not found"},
	} {
		r := call(t, newFake(), c.fm, "a.py")[0]
		if r.OK || !strings.Contains(r.Error, c.want) {
			t.Errorf("%v: %+v", c.fm, r)
		}
	}
	// Unknown base_sha: every result says so and shows the default branch.
	r := call(t, newFake(), map[string]any{"repo": "org/doc-service", "base_sha": "deadbee"}, "src/import/parse.py#L1")[0]
	if !r.OK || !strings.Contains(r.Warnings[0].Text, "base_sha deadbee is not on github.com") || r.Status != "on main" || !strings.Contains(r.URL, "/blob/main/") {
		t.Fatalf("unknown base %+v", r)
	}
	// No base_sha: the default branch, with a warning.
	r = call(t, newFake(), map[string]any{"repo": "org/doc-service"}, "src/import/parse.py")[0]
	if !r.OK || !strings.Contains(r.Warnings[0].Text, "no base_sha") {
		t.Fatalf("no base %+v", r)
	}
	// GitHub Enterprise: the host is used for links and handed to gh.
	gh := newFake()
	r = call(t, gh, map[string]any{"repo": "org/doc-service", "base_sha": "3f9c2a1", "host": "ghe.corp.com"}, "src/import/parse.py")[0]
	if !strings.HasPrefix(r.URL, "https://ghe.corp.com/org/doc-service/") {
		t.Fatalf("ghe %+v", r)
	}
}

func TestDescribeAndSplit(t *testing.T) {
	var out bytes.Buffer
	if run(strings.NewReader(`{"protocol":1,"op":"describe"}`), &out, &bytes.Buffer{}, nil) != 0 || !strings.Contains(out.String(), `"schemes":["gh"]`) {
		t.Fatal(out.String())
	}
	st, body, ok := splitResponse([]byte("HTTP/2.0 404 Not Found\nX-A: b\r\n\r\n{\"message\":\"Not Found\"}"))
	if !ok || st != 404 || string(body) != `{"message":"Not Found"}` {
		t.Fatalf("%d %q %v", st, body, ok)
	}
	if _, _, ok := splitResponse([]byte("gh: not logged in")); ok {
		t.Fatal("garbage parsed")
	}
}

// TestLiveGitHub resolves against a public repository with the real gh
// login. Opt-in: KALKA_GH_LIVE=1.
func TestLiveGitHub(t *testing.T) {
	if os.Getenv("KALKA_GH_LIVE") == "" {
		t.Skip("set KALKA_GH_LIVE=1 to call GitHub through gh")
	}
	req := `{"protocol":1,"op":"resolve","context":{"frontmatter":{"repo":"cli/cli","base_sha":"` + os.Getenv("KALKA_GH_SHA") + `"}},` +
		`"refs":[{"ref":"gh:README.md#L1-L5","payload":"README.md#L1-L5"},{"ref":"gh:go.mod#L1-L3","payload":"go.mod#L1-L3"},{"ref":"gh:nope.txt","payload":"nope.txt"}]}`
	var out, errb bytes.Buffer
	if run(strings.NewReader(req), &out, &errb, func(h string) api { return ghCLI{host: h} }) != 0 {
		t.Fatal(errb.String())
	}
	t.Log(out.String())
	var resp struct{ Results []result }
	_ = json.Unmarshal(out.Bytes(), &resp)
	if !resp.Results[0].OK || resp.Results[0].Snippet == nil || resp.Results[2].OK {
		t.Fatalf("%+v", resp.Results)
	}
}
