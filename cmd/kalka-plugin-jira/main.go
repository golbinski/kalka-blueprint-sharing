// kalka-plugin-jira links blueprints to Jira issues (spec 10.5): it
// resolves jira: references, answers issue_meta with an issue's title,
// status and parents, and exports a released blueprint to its issue as an
// attachment and one comment. It uses the user's own Jira credentials,
// set up through kalka's settings ops (Settings, Plugins, or `kalka plugin
// set jira`); nothing it reads is written into a blueprint.
//
// kalka runs it with no arguments, one JSON request on stdin and one JSON
// answer on stdout (spec 10.3).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	protocol = 2
	version  = "0.1.0"
)

// Budgets inside kalka's timeouts (10 s, and 60 s for export), so the
// plugin answers with a message instead of being killed.
var (
	metaBudget   = 8 * time.Second
	exportBudget = 55 * time.Second
)

type request struct {
	Protocol int      `json:"protocol"`
	Op       string   `json:"op"`
	Issues   []string `json:"issues"`
	Refs     []struct {
		Ref     string `json:"ref"`
		Payload string `json:"payload"`
	} `json:"refs"`
	Issue    string `json:"issue"`
	Document *struct {
		Title    string `json:"title"`
		Version  int64  `json:"version"`
		FileName string `json:"file_name"`
		Markdown string `json:"markdown"`
	} `json:"document"`
	SummaryMarkdown string `json:"summary_markdown"`
	Previous        *struct {
		AttachmentID string `json:"attachment_id"`
		CommentID    string `json:"comment_id"`
	} `json:"previous"`
	// settings ops
	Check   bool              `json:"check"`
	Values  map[string]any    `json:"values"`
	Secrets map[string]string `json:"secrets"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, nil))
}

// run is the plugin; newClient, when set, replaces the HTTP client
// (tests).
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, tweak func(*client)) int {
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if len(args) > 0 {
		if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
			usage(stdout)
			return 0
		}
		usage(stderr)
		return 2
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, 16<<20))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		fmt.Fprintln(stderr, "bad request:", err)
		return 1
	}
	enc := json.NewEncoder(stdout)
	if req.Op == "describe" {
		_ = enc.Encode(map[string]any{"protocol": protocol, "name": "jira", "version": version, "schemes": []string{"jira"},
			"capabilities": map[string]any{"resolve": true, "issue": map[string]any{
				"meta": true, "hierarchy": true,
				"fields": []string{"status", "type", "assignee", "labels", "sprint"},
				"export": map[string]bool{"attachment": true, "comment": true},
			}, "settings": map[string]bool{"apply": true, "clear": true}}})
		return 0
	}
	switch req.Op {
	case "settings":
		_ = enc.Encode(settingsAnswer(st, req.Check, tweak))
		return 0
	case "settings_apply":
		_ = enc.Encode(applySettings(st, req.Values, req.Secrets, tweak))
		return 0
	case "settings_clear":
		_ = enc.Encode(clearSettings(st))
		return 0
	}
	c, cerr := connect(st)
	if c != nil && tweak != nil {
		tweak(c)
	}
	switch req.Op {
	case "issue_meta":
		if cerr != nil {
			_ = enc.Encode(map[string]any{"protocol": protocol, "error": cerr.Error()})
			return 0
		}
		ctx, cancel := context.WithTimeout(context.Background(), metaBudget)
		defer cancel()
		res, err := c.issueMeta(ctx, st, req.Issues)
		if err != nil {
			_ = enc.Encode(map[string]any{"protocol": protocol, "error": err.Error()})
			return 0
		}
		_ = enc.Encode(map[string]any{"protocol": protocol, "results": res})
		return 0
	case "resolve":
		_ = enc.Encode(map[string]any{"protocol": protocol, "results": resolve(c, cerr, st, req)})
		return 0
	case "export":
		if cerr != nil {
			_ = enc.Encode(map[string]any{"ok": false, "error": cerr.Error()})
			return 0
		}
		ctx, cancel := context.WithTimeout(context.Background(), exportBudget)
		defer cancel()
		_ = enc.Encode(export(ctx, c, req, stderr))
		return 0
	}
	fmt.Fprintln(stderr, "unknown op", req.Op)
	return 1
}

func connect(st *store) (*client, error) {
	cfg, err := st.load()
	if err != nil {
		return nil, err
	}
	tok, err := st.token(cfg)
	if err != nil {
		return nil, err
	}
	return newClient(cfg, tok), nil
}

// resolve renders a jira:DEV-123 reference in a blueprint's body with the
// issue's title and status.
func resolve(c *client, cerr error, st *store, req request) []map[string]any {
	out := make([]map[string]any, len(req.Refs))
	var keys []string
	for _, r := range req.Refs {
		keys = append(keys, r.Payload)
	}
	var metas []issueMeta
	err := cerr
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), metaBudget)
		defer cancel()
		metas, err = c.issueMeta(ctx, st, keys)
	}
	for i, r := range req.Refs {
		if err != nil {
			out[i] = map[string]any{"ref": r.Ref, "ok": false, "error": err.Error()}
			continue
		}
		m := metas[i]
		if !m.OK {
			out[i] = map[string]any{"ref": r.Ref, "ok": false, "error": m.Error}
			continue
		}
		status := ""
		if m.Status != nil {
			status = m.Status.Name
		}
		out[i] = map[string]any{"ref": r.Ref, "ok": true, "title": m.Issue + " · " + m.Title, "url": m.URL, "status": status}
	}
	return out
}

// export attaches the file and writes or updates the comment. The new
// attachment goes up first and the old one is removed only once the
// comment is written, so a failure leaves the issue as it was.
func export(ctx context.Context, c *client, req request, stderr io.Writer) map[string]any {
	fail := func(msg string) map[string]any { return map[string]any{"ok": false, "error": msg} }
	if !validKey(req.Issue) {
		return fail(fmt.Sprintf("%q is not a Jira issue key (such as DEV-123)", req.Issue))
	}
	if req.Document == nil || req.Document.Markdown == "" {
		return fail("nothing to export")
	}
	name := safeFileName(req.Document.FileName)
	var prevAtt, prevComment string
	if p := req.Previous; p != nil {
		if idRe.MatchString(p.AttachmentID) {
			prevAtt = p.AttachmentID
		}
		if idRe.MatchString(p.CommentID) {
			prevComment = p.CommentID
		}
	}
	att, err := c.upload(ctx, req.Issue, name, []byte(req.Document.Markdown))
	if err != nil {
		return fail("attaching the blueprint: " + err.Error())
	}
	var body any = toADF(req.SummaryMarkdown)
	if c.flavor == DataCenter {
		body = req.SummaryMarkdown // Data Center takes text; its wiki markup reads Markdown well enough
	}
	comment, err := c.comment(ctx, req.Issue, prevComment, body)
	if err != nil {
		if derr := c.deleteAttachment(ctx, att); derr != nil {
			fmt.Fprintf(stderr, "could not remove the new attachment %s after the comment failed: %v\n", att, derr)
		}
		return fail("writing the comment: " + err.Error())
	}
	if prevAtt != "" && prevAtt != att {
		if err := c.deleteAttachment(ctx, prevAtt); err != nil {
			fmt.Fprintf(stderr, "the old attachment %s was not removed: %v\n", prevAtt, err)
		}
	}
	return map[string]any{"ok": true, "url": c.browse(req.Issue) + "?focusedCommentId=" + comment, "attachment_id": att, "comment_id": comment}
}

// safeFileName keeps an attachment name to letters, digits and . _ -.
func safeFileName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	n := strings.Trim(b.String(), "-.")
	if n == "" {
		n = "blueprint"
	}
	if len(n) > 120 {
		n = n[:120]
	}
	if !strings.HasSuffix(n, ".md") {
		n += ".md"
	}
	return n
}

func usage(w io.Writer) {
	fmt.Fprint(w, `kalka-plugin-jira links kalka blueprints to Jira issues.

kalka runs it by itself. Set it up in kalka's Settings, under Plugins, or:

  kalka plugin set jira site=https://example.atlassian.net email=you@example.org --secret-stdin token
  kalka plugin settings jira --check
  kalka plugin clear jira

Jira Cloud needs your email and an API token (id.atlassian.com, Security, API
tokens); Data Center needs a personal access token (flavor=datacenter).
`)
}

func (c *client) myself(ctx context.Context) error {
	var me struct {
		AccountID string `json:"accountId"`
		Name      string `json:"name"`
	}
	return c.do(ctx, http.MethodGet, "/myself", nil, &me)
}
