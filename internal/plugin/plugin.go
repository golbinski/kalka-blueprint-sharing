// Package plugin runs reference plugins (spec 10): executables named
// kalka-plugin-<scheme>, one process per request, one JSON object on
// stdin and one on stdout. Nothing from a blueprint is ever put on the
// command line; plugin output is data, length-capped and checked here.
package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golbinski/kalka/internal/name"
)

// Protocol is the plugin protocol version (spec 10.3).
const Protocol = 1

// Prefix names plugin executables: Prefix + scheme.
const Prefix = name.Binary + "-plugin-"

// Timeout is how long one plugin call may take (spec 10.3).
var Timeout = 10 * time.Second

// Output limits: a plugin is trusted to run, not to be well behaved.
const (
	maxStdout   = 8 << 20
	maxStderr   = 64 << 10
	maxText     = 500
	maxSnippet  = 64 << 10
	maxWarnings = 20
)

var schemeRe = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)

// Ref is one reference to resolve.
type Ref struct {
	Ref     string `json:"ref"`
	Payload string `json:"payload"`
}

// Snippet is source text; it is shown as text, never as HTML.
type Snippet struct {
	Lang      string `json:"lang,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	Text      string `json:"text"`
}

// Warning is a note about a resolved reference, e.g. that it is stale.
type Warning struct {
	Text string `json:"text"`
}

// Result is a plugin's answer for one reference.
type Result struct {
	Ref      string    `json:"ref"`
	OK       bool      `json:"ok"`
	Error    string    `json:"error,omitempty"`
	Title    string    `json:"title,omitempty"`
	URL      string    `json:"url,omitempty"`
	Status   string    `json:"status,omitempty"`
	Snippet  *Snippet  `json:"snippet,omitempty"`
	Warnings []Warning `json:"warnings,omitempty"`
}

// Description is the answer to "describe".
type Description struct {
	Protocol int      `json:"protocol"`
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Schemes  []string `json:"schemes"`
}

type request struct {
	Protocol int         `json:"protocol"`
	Op       string      `json:"op"`
	Context  *reqContext `json:"context,omitempty"`
	Refs     []Ref       `json:"refs,omitempty"`
}

type reqContext struct {
	Frontmatter map[string]any `json:"frontmatter"`
}

type response struct {
	Protocol int      `json:"protocol"`
	Results  []Result `json:"results"`
}

// Host finds and runs plugins.
type Host struct {
	// Dir is searched before PATH (<config>/plugins).
	Dir string
	// Log receives each plugin's stderr and failures.
	Log io.Writer
}

// ErrNotFound means no plugin is installed for a scheme.
var ErrNotFound = errors.New("no plugin installed")

// Find returns the executable for a scheme.
func (h *Host) Find(scheme string) (string, error) {
	if !schemeRe.MatchString(scheme) {
		return "", ErrNotFound
	}
	base := Prefix + scheme
	if h.Dir != "" {
		exts := []string{""}
		if runtime.GOOS == "windows" {
			exts = []string{".exe", ".cmd"}
		}
		for _, ext := range exts {
			p := filepath.Join(h.Dir, base+ext)
			if st, err := os.Stat(p); err == nil && !st.IsDir() && (runtime.GOOS == "windows" || st.Mode()&0o111 != 0) {
				return p, nil
			}
		}
	}
	// LookPath tries PATHEXT on Windows (.exe, .cmd, ...).
	p, err := exec.LookPath(base)
	if err != nil {
		return "", ErrNotFound
	}
	return p, nil
}

// Describe asks a plugin what it is.
func (h *Host) Describe(ctx context.Context, path string) (*Description, error) {
	var d Description
	if err := h.call(ctx, path, request{Protocol: Protocol, Op: "describe"}, &d); err != nil {
		return nil, err
	}
	if d.Protocol != Protocol {
		return nil, fmt.Errorf("plugin speaks protocol %d, kalka speaks %d", d.Protocol, Protocol)
	}
	return &d, nil
}

// Resolve runs one batch for one scheme. It returns a result for every
// ref, in order: failures of the plugin as a whole (missing, crash,
// timeout, garbage) become an error on each ref, so one plugin cannot
// break anything else.
func (h *Host) Resolve(ctx context.Context, scheme string, frontmatter map[string]any, refs []Ref) []Result {
	fail := func(msg string) []Result {
		out := make([]Result, len(refs))
		for i, r := range refs {
			out[i] = Result{Ref: r.Ref, Error: msg}
		}
		return out
	}
	path, err := h.Find(scheme)
	if err != nil {
		return fail(fmt.Sprintf("no plugin installed for %s:", scheme))
	}
	if frontmatter == nil {
		frontmatter = map[string]any{}
	}
	var resp response
	if err := h.call(ctx, path, request{Protocol: Protocol, Op: "resolve", Context: &reqContext{Frontmatter: frontmatter}, Refs: refs}, &resp); err != nil {
		h.logf("%s: %v", filepath.Base(path), err)
		return fail(fmt.Sprintf("%s failed: %v", filepath.Base(path), err))
	}
	if resp.Protocol != Protocol {
		return fail(fmt.Sprintf("%s answered with protocol %d", filepath.Base(path), resp.Protocol))
	}
	byRef := map[string]Result{}
	for _, r := range resp.Results {
		if _, dup := byRef[r.Ref]; !dup {
			byRef[r.Ref] = clean(r)
		}
	}
	out := make([]Result, len(refs))
	for i, r := range refs {
		res, ok := byRef[r.Ref]
		if !ok {
			res = Result{Ref: r.Ref, Error: filepath.Base(path) + " returned no result for this reference"}
		}
		out[i] = res
	}
	return out
}

func (h *Host) logf(format string, a ...any) {
	if h.Log != nil {
		fmt.Fprintf(h.Log, "plugin: "+format+"\n", a...)
	}
}

// call runs the plugin once: no arguments, the request on stdin, a hard
// timeout, and output read with limits.
func (h *Host) call(ctx context.Context, path string, req request, out any) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	in, err := json.Marshal(req)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path)
	cmd.Stdin = bytes.NewReader(in)
	cmd.Dir = os.TempDir()
	hideWindow(cmd)
	stdout := &capped{max: maxStdout}
	stderr := &capped{max: maxStderr}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	// Children the plugin started (gh) may hold the pipes open after it
	// is killed; stop waiting for them shortly after.
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if s := strings.TrimSpace(stderr.String()); s != "" {
		h.logf("%s: %s", filepath.Base(path), s)
	}
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return fmt.Errorf("timed out after %s", Timeout)
	case err != nil:
		return fmt.Errorf("exited with an error (%v)", err)
	case stdout.over:
		return errors.New("wrote more output than allowed")
	}
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("wrote output that is not the expected JSON (%v)", err)
	}
	return nil
}

// clean caps a result's text and drops what the UI must not get: a URL
// that is not http(s), invalid UTF-8.
func clean(r Result) Result {
	r.Error = text(r.Error, maxText)
	r.Title = text(r.Title, maxText)
	r.Status = text(r.Status, maxText)
	if u, err := url.Parse(r.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		r.URL = ""
	}
	if r.Snippet != nil {
		r.Snippet.Text = text(r.Snippet.Text, maxSnippet)
		r.Snippet.Lang = text(r.Snippet.Lang, 32)
		if r.Snippet.StartLine < 0 {
			r.Snippet.StartLine = 0
		}
	}
	if len(r.Warnings) > maxWarnings {
		r.Warnings = r.Warnings[:maxWarnings]
	}
	for i := range r.Warnings {
		r.Warnings[i].Text = text(r.Warnings[i].Text, maxText)
	}
	if !r.OK && r.Error == "" {
		r.Error = "not resolved"
	}
	return r
}

func text(s string, max int) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}

// capped is a buffer that stops growing at max bytes. The buffer is a
// field, not embedded: an embedded bytes.Buffer would promote ReadFrom,
// and io.Copy would use it and skip the limit.
type capped struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); len(p) > room {
		c.over = true
		if room > 0 {
			c.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *capped) Bytes() []byte  { return c.buf.Bytes() }
func (c *capped) String() string { return c.buf.String() }
