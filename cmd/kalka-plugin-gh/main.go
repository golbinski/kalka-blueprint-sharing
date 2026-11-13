// kalka-plugin-gh resolves gh: references (spec 10.4) with the user's own
// `gh` CLI login; it keeps no token of its own.
//
//	gh:<path>[#L<a>[-L<b>]]
//
// Frontmatter keys it reads: repo (owner/name), base_sha, optional host
// (GitHub Enterprise). Results link to the file at base_sha and show the
// lines; a path changed on the default branch since base_sha gets a
// warning, found with one compare call per batch.
//
// Everything taken from a blueprint is validated and passed to gh as API
// path or query parameters of a fixed endpoint, never through a shell.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// version is set at release build time with -ldflags "-X main.version=...".
var version = "dev"

const (
	protocol = 1
	// maxLines is the most lines a snippet shows.
	maxLines = 200
	// headLines is shown when a reference names no lines.
	headLines = 30
)

type ref struct {
	Ref     string `json:"ref"`
	Payload string `json:"payload"`
}

type request struct {
	Protocol int    `json:"protocol"`
	Op       string `json:"op"`
	Context  struct {
		Frontmatter map[string]any `json:"frontmatter"`
	} `json:"context"`
	Refs []ref `json:"refs"`
}

type snippet struct {
	Lang      string `json:"lang,omitempty"`
	StartLine int    `json:"start_line"`
	Text      string `json:"text"`
}

type warning struct {
	Text string `json:"text"`
}

type result struct {
	Ref      string    `json:"ref"`
	OK       bool      `json:"ok"`
	Error    string    `json:"error,omitempty"`
	Title    string    `json:"title,omitempty"`
	URL      string    `json:"url,omitempty"`
	Status   string    `json:"status,omitempty"`
	Snippet  *snippet  `json:"snippet,omitempty"`
	Warnings []warning `json:"warnings,omitempty"`
}

// api is GitHub as seen through `gh api`; tests replace it.
type api interface {
	// get returns the HTTP status and body; accept "" means JSON.
	get(endpoint, accept string) (int, []byte, error)
}

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr, func(host string) api { return ghCLI{host: host} }))
}

func run(stdin io.Reader, stdout, stderr io.Writer, newAPI func(host string) api) int {
	raw, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
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
	switch req.Op {
	case "describe":
		_ = enc.Encode(map[string]any{"protocol": protocol, "name": "github", "version": version, "schemes": []string{"gh"}})
		return 0
	case "resolve":
		_ = enc.Encode(map[string]any{"protocol": protocol, "results": resolve(req, newAPI, stderr)})
		return 0
	}
	fmt.Fprintln(stderr, "unknown op", req.Op)
	return 1
}

var (
	repoRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`)
	shaRe     = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
	hostRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,252}(:[0-9]{1,5})?$`)
	segmentRe = regexp.MustCompile(`^[A-Za-z0-9._@+=,~ -]+$`)
	linesRe   = regexp.MustCompile(`^L([0-9]{1,7})(?:-L?([0-9]{1,7}))?$`)
)

func str(fm map[string]any, k string) string {
	v, ok := fm[k]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// target is a parsed gh: payload.
type target struct {
	path     string
	from, to int // 0 when the reference names no lines
}

func parsePayload(p string) (target, error) {
	file, frag, _ := strings.Cut(p, "#")
	var t target
	if file == "" || len(file) > 400 || strings.HasPrefix(file, "/") || strings.HasSuffix(file, "/") {
		return t, errors.New("expected gh:<path>[#L<a>[-L<b>]] with a path relative to the repository")
	}
	for _, s := range strings.Split(file, "/") {
		if s == "." || s == ".." || !segmentRe.MatchString(s) {
			return t, fmt.Errorf("path segment %q is not allowed", s)
		}
	}
	t.path = file
	if frag != "" {
		m := linesRe.FindStringSubmatch(frag)
		if m == nil {
			return t, fmt.Errorf("line range %q: expected L<a> or L<a>-L<b>", frag)
		}
		t.from, _ = strconv.Atoi(m[1])
		t.to = t.from
		if m[2] != "" {
			t.to, _ = strconv.Atoi(m[2])
		}
		if t.from < 1 || t.to < t.from {
			return t, fmt.Errorf("line range %q is empty", frag)
		}
	}
	return t, nil
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

func resolve(req request, newAPI func(string) api, stderr io.Writer) []result {
	fm := req.Context.Frontmatter
	repo, base, host := str(fm, "repo"), str(fm, "base_sha"), str(fm, "host")
	out := make([]result, len(req.Refs))
	failAll := func(msg string) []result {
		for i, r := range req.Refs {
			out[i] = result{Ref: r.Ref, Error: msg}
		}
		return out
	}
	switch {
	case repo == "":
		return failAll("the frontmatter has no repo: (owner/name)")
	case !repoRe.MatchString(repo):
		return failAll(fmt.Sprintf("repo %q is not owner/name", repo))
	case base != "" && !shaRe.MatchString(base):
		return failAll(fmt.Sprintf("base_sha %q is not a commit hash", base))
	case host != "" && !hostRe.MatchString(host):
		return failAll(fmt.Sprintf("host %q is not a host name", host))
	}
	web := "github.com"
	if host != "" {
		web = host
	}
	gh := newAPI(host)

	// Round 1, in parallel: the default branch (for staleness and as a
	// fallback) and whether the remote has base_sha.
	var info struct {
		DefaultBranch string `json:"default_branch"`
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	var infoSt, commitSt int
	var infoBody, commitBody []byte
	var infoErr, commitErr error
	var round sync.WaitGroup
	round.Add(1)
	go func() {
		defer round.Done()
		infoSt, infoBody, infoErr = gh.get("repos/"+repo, "")
	}()
	if base != "" {
		round.Add(1)
		go func() {
			defer round.Done()
			commitSt, commitBody, commitErr = gh.get("repos/"+repo+"/commits/"+url.PathEscape(base), "")
		}()
	}
	round.Wait()
	switch {
	case infoErr != nil:
		return failAll(infoErr.Error())
	case infoSt == 404:
		return failAll(fmt.Sprintf("repository %s not found on %s, or your gh login cannot see it", repo, web))
	case infoSt != 200 || json.Unmarshal(infoBody, &info) != nil || info.DefaultBranch == "":
		return failAll(fmt.Sprintf("GitHub answered %d for repository %s", infoSt, repo))
	}
	branch := info.DefaultBranch

	// Where to read: base_sha if the remote has it, else the default branch
	// with a warning on every result.
	at, short := branch, ""
	var common []warning
	switch {
	case base == "":
		common = append(common, warning{"the frontmatter has no base_sha: showing " + branch})
	case commitErr == nil && commitSt == 200 && json.Unmarshal(commitBody, &commit) == nil && len(commit.SHA) >= 7:
		at, short = commit.SHA, commit.SHA[:7]
	default:
		common = append(common, warning{fmt.Sprintf("base_sha %s is not on %s (an unpushed commit?): showing %s", base, web, branch)})
	}

	// Round 2, in parallel: one compare call for the batch (staleness) and
	// the files.
	changed := map[string]bool{}
	truncated, ahead, compared := false, 0, false
	var cmpDone sync.WaitGroup
	if short != "" {
		cmpDone.Add(1)
		go func() {
			defer cmpDone.Done()
			var cmp struct {
				AheadBy int `json:"ahead_by"`
				Files   []struct {
					Filename         string `json:"filename"`
					PreviousFilename string `json:"previous_filename"`
				} `json:"files"`
			}
			st, body, err := gh.get("repos/"+repo+"/compare/"+url.PathEscape(at)+"..."+url.PathEscape(branch), "")
			if err != nil || st != 200 || json.Unmarshal(body, &cmp) != nil {
				fmt.Fprintf(stderr, "compare %s...%s: status %d %v\n", short, branch, st, err)
				return
			}
			compared, ahead, truncated = true, cmp.AheadBy, len(cmp.Files) >= 300
			for _, f := range cmp.Files {
				changed[f.Filename] = true
				if f.PreviousFilename != "" {
					changed[f.PreviousFilename] = true
				}
			}
		}()
	}

	// File contents, once per path, a few at a time.
	type file struct {
		status int
		body   []byte
		dir    bool
		err    error
	}
	files := map[string]*file{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	targets := make([]target, len(req.Refs))
	errs := make([]error, len(req.Refs))
	for i, r := range req.Refs {
		targets[i], errs[i] = parsePayload(r.Payload)
		if errs[i] != nil {
			continue
		}
		p := targets[i].path
		mu.Lock()
		if _, ok := files[p]; ok {
			mu.Unlock()
			continue
		}
		f := &file{}
		files[p] = f
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			f.status, f.body, f.err = gh.get("repos/"+repo+"/contents/"+escapePath(p)+"?ref="+url.QueryEscape(at), "application/vnd.github.raw")
			// A directory answers with a JSON listing even when raw is asked.
			f.dir = f.status == 200 && bytes.HasPrefix(bytes.TrimSpace(f.body), []byte("[{"))
		}()
	}
	wg.Wait()
	cmpDone.Wait()

	where := branch
	if short != "" {
		where = short
	}
	for i, r := range req.Refs {
		res := result{Ref: r.Ref}
		t := targets[i]
		f := files[t.path]
		switch {
		case errs[i] != nil:
			res.Error = errs[i].Error()
		case f.err != nil:
			res.Error = f.err.Error()
		case f.status == 404:
			res.Error = fmt.Sprintf("%s does not exist at %s", t.path, where)
		case f.status != 200:
			res.Error = fmt.Sprintf("GitHub answered %d for %s", f.status, t.path)
		default:
			res.OK = true
			res.Title = t.path
			res.URL = "https://" + web + "/" + repo + "/blob/" + url.PathEscape(at) + "/" + escapePath(t.path)
			if f.dir {
				res.Title += "/"
				res.URL = "https://" + web + "/" + repo + "/tree/" + url.PathEscape(at) + "/" + escapePath(t.path)
			} else {
				res.Snippet, res.Warnings = cut(f.body, t)
				if t.from > 0 {
					res.Title += fmt.Sprintf(" · L%d–%d", t.from, t.to)
					res.URL += fmt.Sprintf("#L%d-L%d", t.from, t.to)
				}
				res.Snippet.Lang = lang(t.path)
			}
			res.Warnings = append(append([]warning{}, common...), res.Warnings...)
			switch {
			case !compared:
			case changed[t.path]:
				res.Warnings = append(res.Warnings, warning{fmt.Sprintf("changed on %s since %s (%s is %d commits ahead)", branch, short, branch, ahead)})
			case truncated:
				res.Warnings = append(res.Warnings, warning{fmt.Sprintf("could not tell whether this changed: more than 300 files differ between %s and %s", short, branch)})
			default:
				res.Status = fmt.Sprintf("unchanged on %s since %s", branch, short)
			}
			if short != "" && res.Status == "" {
				res.Status = "at " + short
			} else if short == "" {
				res.Status = "on " + branch
			}
		}
		out[i] = res
	}
	return out
}

// cut takes the referenced lines from a file.
func cut(body []byte, t target) (*snippet, []warning) {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	var warns []warning
	from, to := t.from, t.to
	if from == 0 {
		from, to = 1, min(len(lines), headLines)
	}
	if from > len(lines) {
		return &snippet{StartLine: from}, []warning{{fmt.Sprintf("the file has only %d lines", len(lines))}}
	}
	if to > len(lines) {
		warns = append(warns, warning{fmt.Sprintf("the file has only %d lines", len(lines))})
		to = len(lines)
	}
	if to-from+1 > maxLines {
		to = from + maxLines - 1
	}
	return &snippet{StartLine: from, Text: strings.Join(lines[from-1:to], "\n") + "\n"}, warns
}

var langs = map[string]string{
	".go": "go", ".py": "python", ".js": "javascript", ".ts": "typescript", ".tsx": "tsx", ".jsx": "jsx",
	".java": "java", ".kt": "kotlin", ".rb": "ruby", ".rs": "rust", ".c": "c", ".h": "c", ".cc": "cpp",
	".cpp": "cpp", ".cs": "csharp", ".php": "php", ".sh": "bash", ".sql": "sql", ".yaml": "yaml",
	".yml": "yaml", ".json": "json", ".md": "markdown", ".toml": "toml", ".swift": "swift", ".scala": "scala",
}

func lang(p string) string { return langs[strings.ToLower(path.Ext(p))] }

// ghCLI calls `gh api` without a shell; the endpoint is built only from
// validated parts and always starts with "repos/".
type ghCLI struct{ host string }

func (g ghCLI) get(endpoint, accept string) (int, []byte, error) {
	args := []string{"api", "--include", "--method", "GET"}
	if g.host != "" {
		args = append(args, "--hostname", g.host)
	}
	if accept != "" {
		args = append(args, "-H", "Accept: "+accept)
	}
	args = append(args, endpoint)
	cmd := exec.Command("gh", args...)
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "NO_COLOR=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		if errors.Is(err, exec.ErrNotFound) {
			return 0, nil, errors.New("the gh CLI is not installed: install it and run gh auth login")
		}
		return 0, nil, err
	}
	status, body, ok := splitResponse(out.Bytes())
	if !ok {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = "no response"
		}
		if strings.Contains(msg, "auth login") {
			msg = "gh is not logged in: run gh auth login"
		}
		return 0, nil, errors.New("gh: " + msg)
	}
	return status, body, nil
}

// splitResponse reads `gh api --include` output: a status line, headers,
// a blank line, the body.
func splitResponse(b []byte) (int, []byte, bool) {
	b = bytes.TrimLeft(b, "\r\n")
	line, rest, _ := bytes.Cut(b, []byte("\n"))
	f := strings.Fields(string(line))
	if len(f) < 2 || !strings.HasPrefix(f[0], "HTTP/") {
		return 0, nil, false
	}
	status, err := strconv.Atoi(f[1])
	if err != nil {
		return 0, nil, false
	}
	for {
		l, r, found := bytes.Cut(rest, []byte("\n"))
		if len(bytes.TrimSpace(l)) == 0 || !found {
			if !found {
				r = nil
			}
			return status, r, true
		}
		rest = r
	}
}
