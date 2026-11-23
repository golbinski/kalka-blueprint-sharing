package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeJira is a Jira Cloud site with issues, comments and attachments.
type fakeJira struct {
	t           *testing.T
	mu          sync.Mutex
	issues      map[string]map[string]any // key -> fields
	comments    map[string]any            // id -> body
	attachments map[string]string         // id -> file content
	next        int
	limited     int // answer 429 to this many requests first
	searches    int
	requests    []string
	token       string
}

func newFakeJira(t *testing.T) (*fakeJira, *httptest.Server) {
	f := &fakeJira{t: t, token: "atl-token-0123456789", comments: map[string]any{}, attachments: map[string]string{},
		issues: map[string]map[string]any{
			"DEV-12": {"summary": "Last-mile reliability", "issuetype": map[string]any{"name": "Initiative"}},
			"DEV-90": {"summary": "Failed-delivery handling", "issuetype": map[string]any{"name": "Epic"}, "parent": map[string]any{"key": "DEV-12"}},
			"DEV-123": {"summary": "Redelivery after a failed attempt", "issuetype": map[string]any{"name": "Story"},
				"status":   map[string]any{"name": "In Progress", "statusCategory": map[string]any{"key": "indeterminate"}},
				"assignee": map[string]any{"displayName": "Ewa Nowak"}, "labels": []string{"delivery"},
				"parent": map[string]any{"key": "DEV-90"}, "updated": "2026-11-19T15:02:00.000+0100",
				"customfield_10020": []map[string]any{{"name": "Sprint 40", "state": "closed"}, {"name": "Sprint 41", "state": "active"}}},
			"DEV-140": {"summary": "Locker pickup", "issuetype": map[string]any{"name": "Task"},
				"status": map[string]any{"name": "Done", "statusCategory": map[string]any{"key": "done"}}},
		}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeJira) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	if r.Header.Get("Authorization") != "Basic "+b64("ewa@parcels.example:"+f.token) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if f.limited > 0 {
		f.limited--
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	reply := func(v any) { json.NewEncoder(w).Encode(v) }
	issueJSON := func(k string) map[string]any { return map[string]any{"key": k, "fields": f.issues[k]} }
	p := strings.TrimPrefix(r.URL.Path, "/rest/api/3")
	switch {
	case r.Method == "GET" && p == "/myself":
		reply(map[string]string{"accountId": "a1"})
	case r.Method == "GET" && p == "/field":
		reply([]map[string]any{{"id": "summary", "schema": map[string]any{}}, {"id": "customfield_10020", "schema": map[string]any{"custom": "com.pyxis.greenhopper.jira:gh-sprint"}}})
	case r.Method == "POST" && p == "/search/jql":
		f.searches++
		var q struct{ JQL string }
		json.NewDecoder(r.Body).Decode(&q)
		keys := strings.Split(strings.TrimSuffix(strings.TrimPrefix(q.JQL, "key in ("), ")"), ",")
		var out []map[string]any
		for _, k := range keys {
			if _, ok := f.issues[k]; !ok {
				w.WriteHeader(http.StatusBadRequest)
				reply(map[string]any{"errorMessages": []string{"An issue with key '" + k + "' does not exist for field 'key'."}})
				return
			}
			out = append(out, issueJSON(k))
		}
		reply(map[string]any{"issues": out})
	case r.Method == "GET" && strings.HasPrefix(p, "/issue/") && strings.Count(p, "/") == 2:
		k := strings.TrimPrefix(p, "/issue/")
		if _, ok := f.issues[k]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		reply(issueJSON(k))
	case r.Method == "POST" && strings.HasSuffix(p, "/attachments"):
		if r.Header.Get("X-Atlassian-Token") != "no-check" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		b, _ := io.ReadAll(file)
		f.next++
		id := fmt.Sprint(10045 + f.next)
		f.attachments[id] = hdr.Filename + "\n" + string(b)
		reply([]map[string]string{{"id": id}})
	case r.Method == "DELETE" && strings.HasPrefix(p, "/attachment/"):
		id := strings.TrimPrefix(p, "/attachment/")
		if _, ok := f.attachments[id]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(f.attachments, id)
		w.WriteHeader(http.StatusNoContent)
	case strings.Contains(p, "/comment"):
		var q struct{ Body any }
		json.NewDecoder(r.Body).Decode(&q)
		if r.Method == "PUT" {
			id := p[strings.LastIndex(p, "/")+1:]
			if _, ok := f.comments[id]; !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			f.comments[id] = q.Body
			reply(map[string]string{"id": id})
			return
		}
		f.next++
		id := fmt.Sprint(20310 + f.next)
		f.comments[id] = q.Body
		reply(map[string]string{"id": id})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func b64(s string) string {
	const enc = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var b strings.Builder
	in := []byte(s)
	for len(in) > 0 {
		var n uint32
		k := min(len(in), 3)
		for i := 0; i < 3; i++ {
			n <<= 8
			if i < k {
				n |= uint32(in[i])
			}
		}
		for i := 0; i < 4; i++ {
			if i <= k {
				b.WriteByte(enc[(n>>(18-6*i))&63])
			} else {
				b.WriteByte('=')
			}
		}
		in = in[k:]
	}
	return b.String()
}

// setup logs the plugin in to the fake site through its login command,
// with the token in a file (no credential store in tests).
func setup(t *testing.T, srv *httptest.Server) {
	t.Helper()
	t.Setenv("KALKA_PLUGIN_JIRA_DIR", t.TempDir())
	t.Setenv("KALKA_PLUGIN_JIRA_NO_KEYRING", "1")
	var out, errs bytes.Buffer
	in := strings.NewReader("atl-token-0123456789\n")
	if code := run([]string{"login", "--site", srv.URL, "--email", "ewa@parcels.example", "--token-stdin"}, in, &out, &errs, nil); code != 0 {
		t.Fatalf("login: %s %s", out.String(), errs.String())
	}
	st, _ := os.Stat(filepath.Join(os.Getenv("KALKA_PLUGIN_JIRA_DIR"), "config.json"))
	if st == nil || (st.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/') {
		t.Fatalf("config.json: %v", st)
	}
}

func call(t *testing.T, req any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(req)
	var out, errs bytes.Buffer
	noWait := func(c *client) { c.sleep = func(context.Context, time.Duration) error { return nil } }
	if code := run(nil, bytes.NewReader(raw), &out, &errs, noWait); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	return m
}

func TestDescribe(t *testing.T) {
	t.Setenv("KALKA_PLUGIN_JIRA_DIR", t.TempDir())
	d := call(t, map[string]any{"protocol": 2, "op": "describe"})
	caps, _ := json.Marshal(d["capabilities"])
	if d["protocol"] != float64(2) || !strings.Contains(string(caps), `"hierarchy":true`) || !strings.Contains(string(caps), `"export":{"attachment":true,"comment":true}`) {
		t.Fatalf("%v", d)
	}
}

func TestIssueMeta(t *testing.T) {
	f, srv := newFakeJira(t)
	setup(t, srv)
	res := call(t, map[string]any{"protocol": 2, "op": "issue_meta", "issues": []string{"DEV-123", "DEV-140", "DEV-999", "dev-1"}})
	var got struct {
		Results []issueMeta `json:"results"`
	}
	raw, _ := json.Marshal(res)
	json.Unmarshal(raw, &got)
	if len(got.Results) != 4 {
		t.Fatalf("%s", raw)
	}
	m := got.Results[0]
	if !m.OK || m.Title != "Redelivery after a failed attempt" || m.Status.Category != "in_progress" || m.Assignee != "Ewa Nowak" ||
		m.Sprint != "Sprint 41" || m.Type != "Story" || m.Updated != "2026-11-19T14:02:00Z" || m.URL != srv.URL+"/browse/DEV-123" {
		t.Fatalf("meta: %+v", m)
	}
	if len(m.Parents) != 2 || m.Parents[0].Issue != "DEV-90" || m.Parents[0].Type != "Epic" || m.Parents[1].Title != "Last-mile reliability" {
		t.Fatalf("parents: %+v", m.Parents)
	}
	if got.Results[1].Status.Category != "done" || got.Results[2].OK || got.Results[2].Error != "not found or no permission" || got.Results[3].OK {
		t.Fatalf("others: %+v", got.Results[1:])
	}
	// The JQL failed for the unknown key, so issues were read one by one;
	// keys never reach Jira unvalidated.
	for _, r := range f.requests {
		if strings.Contains(r, "dev-1") {
			t.Fatalf("invalid key sent: %s", r)
		}
	}
}

func TestExport(t *testing.T) {
	f, srv := newFakeJira(t)
	setup(t, srv)
	req := map[string]any{"protocol": 2, "op": "export", "issue": "DEV-123",
		"document":         map[string]any{"title": "Redelivery", "version": 3, "file_name": "redelivery-v3.md", "markdown": "---\nkalka_issue: jira:DEV-123\n---\n# Redelivery\n"},
		"summary_markdown": "*Exported from kalka.*\n\n# Redelivery\n\n## Plan\n\nA **second** attempt, see [the rules](https://parcels.example/rules) and `retry()`.\n\n- one\n- two\n"}
	res := call(t, req)
	if res["ok"] != true || res["attachment_id"] == "" || res["comment_id"] == "" || !strings.Contains(res["url"].(string), "focusedCommentId=") {
		t.Fatalf("%v", res)
	}
	att, comment := res["attachment_id"].(string), res["comment_id"].(string)
	if !strings.HasPrefix(f.attachments[att], "redelivery-v3.md\n---\nkalka_issue") {
		t.Fatalf("attachment: %q", f.attachments[att])
	}
	adf, _ := json.Marshal(f.comments[comment])
	for _, want := range []string{`"type":"doc"`, `"type":"heading"`, `"type":"strong"`, `"href":"https://parcels.example/rules"`, `"type":"code"`, `"type":"bulletList"`} {
		if !strings.Contains(string(adf), want) {
			t.Errorf("comment lacks %s: %s", want, adf)
		}
	}

	// Again with previous: the comment is edited, the attachment replaced.
	req["previous"] = map[string]string{"attachment_id": att, "comment_id": comment}
	res = call(t, req)
	if res["ok"] != true || res["comment_id"] != comment || res["attachment_id"] == att || len(f.attachments) != 1 || len(f.comments) != 1 {
		t.Fatalf("re-export: %v %v %v", res, f.attachments, f.comments)
	}

	// The comment was deleted on the issue: a new one is written.
	delete(f.comments, comment)
	req["previous"] = map[string]string{"attachment_id": res["attachment_id"].(string), "comment_id": comment}
	res = call(t, req)
	if res["ok"] != true || res["comment_id"] == comment || len(f.comments) != 1 || len(f.attachments) != 1 {
		t.Fatalf("deleted comment: %v %v", res, f.comments)
	}

	// A bad key never reaches Jira.
	req["issue"] = "DEV-123/../../admin"
	if res = call(t, req); res["ok"] != false {
		t.Fatalf("bad key: %v", res)
	}
}

func TestAuthAndRateLimits(t *testing.T) {
	f, srv := newFakeJira(t)
	setup(t, srv)
	// Rate limited twice, then answered: the plugin waits and retries.
	f.limited = 2
	res := call(t, map[string]any{"protocol": 2, "op": "issue_meta", "issues": []string{"DEV-140"}})
	if res["error"] != nil {
		t.Fatalf("rate limit: %v", res)
	}
	// Limited for longer than it may wait: a clear error for the batch.
	f.limited = 100
	res = call(t, map[string]any{"protocol": 2, "op": "issue_meta", "issues": []string{"DEV-123"}})
	if e, _ := res["error"].(string); !strings.Contains(e, "limiting requests") {
		t.Fatalf("still limited: %v", res)
	}
	f.limited = 0
	// The token was revoked: the whole batch fails, with what to do.
	f.token = "another-token-000"
	res = call(t, map[string]any{"protocol": 2, "op": "issue_meta", "issues": []string{"DEV-123"}})
	if e, _ := res["error"].(string); !strings.Contains(e, "rejected the credentials") || !strings.Contains(e, "kalka-plugin-jira login") {
		t.Fatalf("auth: %v", res)
	}
	res = call(t, map[string]any{"protocol": 2, "op": "export", "issue": "DEV-123", "document": map[string]any{"markdown": "# x\n", "file_name": "x.md"}})
	if res["ok"] != false || !strings.Contains(res["error"].(string), "rejected the credentials") {
		t.Fatalf("export auth: %v", res)
	}
	// Not logged in at all.
	t.Setenv("KALKA_PLUGIN_JIRA_DIR", t.TempDir())
	res = call(t, map[string]any{"protocol": 2, "op": "issue_meta", "issues": []string{"DEV-123"}})
	if e, _ := res["error"].(string); !strings.Contains(e, "not logged in") {
		t.Fatalf("no login: %v", res)
	}
}

func TestLoginRefusesBadInput(t *testing.T) {
	_, srv := newFakeJira(t)
	t.Setenv("KALKA_PLUGIN_JIRA_DIR", t.TempDir())
	t.Setenv("KALKA_PLUGIN_JIRA_NO_KEYRING", "1")
	for _, args := range [][]string{
		{"login", "--site", "http://jira.example.org", "--email", "ewa@parcels.example", "--token-stdin"},
		{"login", "--site", srv.URL, "--email", "ewa@parcels.example", "--token-stdin"}, // wrong token
	} {
		var out, errs bytes.Buffer
		if run(args, strings.NewReader("wrong-token-0000\n"), &out, &errs, nil) == 0 {
			t.Errorf("%v logged in", args)
		}
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("KALKA_PLUGIN_JIRA_DIR"), "config.json")); err == nil {
		t.Fatal("a failed login stored something")
	}
}

func TestSprintNames(t *testing.T) {
	dc := json.RawMessage(`["com.atlassian.greenhopper.service.sprint.Sprint@1[id=1,state=CLOSED,name=Sprint 40]","com.atlassian.greenhopper.service.sprint.Sprint@2[id=2,state=ACTIVE,name=Sprint 41,goal=]"]`)
	if got := sprintName(dc); got != "Sprint 41" {
		t.Fatal(got)
	}
	if got := safeFileName("../../redelivery v3.md"); got != "redelivery-v3.md" {
		t.Fatal(got)
	}
}

// A lookup that read the configuration before a logout, and records the
// sprint field after it, leaves no login behind: the field cache is a file
// of its own and config.json is written only by login and logout.
func TestFieldCacheNeverWritesALoginBack(t *testing.T) {
	_, srv := newFakeJira(t)
	setup(t, srv)
	dir := os.Getenv("KALKA_PLUGIN_JIRA_DIR")
	lookup, _ := openStore()
	if _, err := lookup.load(); err != nil { // the issue_meta process
		t.Fatal(err)
	}
	other, _ := openStore()
	if err := other.logout(); err != nil { // a logout meanwhile
		t.Fatal(err)
	}
	lookup.saveField(srv.URL, "customfield_10020")
	if _, err := os.Stat(filepath.Join(dir, "config.json")); !os.IsNotExist(err) {
		t.Fatalf("config.json written back: %v", err)
	}
	fresh, _ := openStore()
	if id, ok := fresh.cachedField(srv.URL); !ok || id != "customfield_10020" {
		t.Fatalf("cache: %q %v", id, ok)
	}
	st, err := os.Stat(filepath.Join(dir, fieldsFile))
	if err != nil || (os.PathSeparator == '/' && st.Mode().Perm()&0o077 != 0) {
		t.Fatalf("fields.json: %v %v", st, err)
	}
	if _, err := fresh.load(); err != errNotLoggedIn {
		t.Fatalf("logged in again: %v", err)
	}
}

// The sprint field found by a lookup is kept apart from the configuration.
func TestSprintFieldCachedApart(t *testing.T) {
	_, srv := newFakeJira(t)
	setup(t, srv)
	call(t, map[string]any{"protocol": 2, "op": "issue_meta", "issues": []string{"DEV-123"}})
	dir := os.Getenv("KALKA_PLUGIN_JIRA_DIR")
	cfg, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	fields, _ := os.ReadFile(filepath.Join(dir, fieldsFile))
	if strings.Contains(string(cfg), "customfield") || !strings.Contains(string(fields), "customfield_10020") {
		t.Fatalf("config %s\nfields %s", cfg, fields)
	}
}
