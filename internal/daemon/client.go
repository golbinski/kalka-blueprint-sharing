package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/plugin"
	"github.com/golbinski/kalka/internal/teamlog"
)

// ErrNotRunning means no daemon answers.
var ErrNotRunning = fmt.Errorf("%s is not running: start it with `%[1]s`", name.Binary)

// Client talks to the local API.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// Connect reads daemon.json from the config directory.
func Connect(configDir string) (*Client, error) {
	raw, err := os.ReadFile(filepath.Join(configDir, DaemonFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotRunning
	}
	if err != nil {
		return nil, err
	}
	var e Endpoint
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, fmt.Errorf("%s: %w", DaemonFile, err)
	}
	return NewClient(e.Addr, e.Token), nil
}

// NewClient addresses a daemon directly.
func NewClient(addr, token string) *Client {
	return &Client{base: "http://" + addr, token: token, http: &http.Client{}}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrNotRunning
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e APIError
		raw, _ := io.ReadAll(resp.Body)
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return &RemoteError{APIError: e}
		}
		return fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(raw))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// RemoteError is an error reported by the daemon.
type RemoteError struct{ APIError }

func (e *RemoteError) Error() string { return e.APIError.Error }

// Status fetches /api/status.
func (c *Client) Status(ctx context.Context) (core.Status, error) {
	var s core.Status
	return s, c.do(ctx, http.MethodGet, "/api/status", nil, &s)
}

// Shutdown asks the daemon to stop and returns its PID. The daemon
// finishes running rounds first; use Wait to see it gone.
func (c *Client) Shutdown(ctx context.Context) (int, error) {
	var r struct {
		PID int `json:"pid"`
	}
	return r.PID, c.do(ctx, http.MethodPost, "/api/shutdown", struct{}{}, &r)
}

// Summary fetches the tray's one-line state.
func (c *Client) Summary(ctx context.Context) (core.Summary, error) {
	var s core.Summary
	return s, c.do(ctx, http.MethodGet, "/api/summary", nil, &s)
}

// Settings fetches the settings.
func (c *Client) Settings(ctx context.Context) (Settings, error) {
	var s Settings
	return s, c.do(ctx, http.MethodGet, "/api/settings", nil, &s)
}

// ApplySettings changes the settings given.
func (c *Client) ApplySettings(ctx context.Context, in Settings) (Settings, error) {
	var s Settings
	return s, c.do(ctx, http.MethodPut, "/api/settings", in, &s)
}

// UpdateStatus fetches the update state; with check it asks GitHub first.
func (c *Client) UpdateStatus(ctx context.Context, check bool) (UpdateStatus, error) {
	var s UpdateStatus
	if check {
		return s, c.do(ctx, http.MethodPost, "/api/update/check", struct{}{}, &s)
	}
	return s, c.do(ctx, http.MethodGet, "/api/update", nil, &s)
}

// InstallUpdate downloads the update if need be, installs it and restarts
// kalka; the daemon stops as it answers.
func (c *Client) InstallUpdate(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/update/install", struct{}{}, nil)
}

// SetRendezvous switches the rendezvous server of the running daemon;
// without force a server that does not answer is refused.
func (c *Client) SetRendezvous(ctx context.Context, server string, force bool) (core.Status, error) {
	var s core.Status
	return s, c.do(ctx, http.MethodPost, "/api/rendezvous", RendezvousRequest{Rendezvous: server, Force: force}, &s)
}

// SetPaused pauses or resumes sync.
func (c *Client) SetPaused(ctx context.Context, paused bool) (Settings, error) {
	var s Settings
	path := "/api/resume"
	if paused {
		path = "/api/pause"
	}
	return s, c.do(ctx, http.MethodPost, path, struct{}{}, &s)
}

// ErrNoShell means no desktop shell is attached to the daemon.
var ErrNoShell = errors.New("no desktop window is attached")

// ShowUI asks the attached desktop shell to show its window, on path if
// given (spec 22.2). ErrNoShell: there is none; open the browser instead.
func (c *Client) ShowUI(ctx context.Context, path string) error {
	err := c.do(ctx, http.MethodPost, "/api/ui/show", map[string]string{"path": path}, nil)
	var re *RemoteError
	if errors.As(err, &re) && re.Kind == core.KindNoShell {
		return ErrNoShell
	}
	return err
}

// ShowRoute is ShowUI on a route of the page, such as the join screen.
func (c *Client) ShowRoute(ctx context.Context, route string) error {
	err := c.do(ctx, http.MethodPost, "/api/ui/show", map[string]string{"route": route}, nil)
	var re *RemoteError
	if errors.As(err, &re) && re.Kind == core.KindNoShell {
		return ErrNoShell
	}
	return err
}

// ValidRoute: a route the shell may navigate to is "#/" and a path of
// URL-safe characters, so nothing else reaches the page's location.
func ValidRoute(r string) bool {
	if !strings.HasPrefix(r, "#/") || len(r) > 8<<10 {
		return false
	}
	for _, c := range r[2:] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("/-_.~%?=&", c)) {
			return false
		}
	}
	return true
}

// JoinRoute is the page's join screen for a link or token as given; an
// input that does not parse opens the join screen empty, which says so.
func JoinRoute(link string) string {
	t, err := teamlog.NormalizeJoin(link)
	if err != nil {
		return "#/join/"
	}
	if _, err := teamlog.ParseToken(t); err != nil {
		return "#/join/"
	}
	return "#/join/" + t
}

// ShellOpen is the event ShellEvents gives once the stream is open.
const ShellOpen = "open"

// ShellEvents subscribes as a desktop shell (spec 22): while the stream
// is open the daemon leaves prompts and notices to the shell. It calls on
// for each bus event until ctx ends or the daemon goes away, and first
// with ShellOpen once the daemon has subscribed it, so that a caller
// reading the state then misses nothing that happens after.
func (c *Client) ShellEvents(ctx context.Context, on func(typ string, data json.RawMessage)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/events?shell=1", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrNotRunning
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("events: %s", resp.Status)
	}
	// The daemon subscribes before it answers (serveEvents).
	on(ShellOpen, nil)
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	var typ string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			typ = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			on(typ, json.RawMessage(strings.TrimPrefix(line, "data: ")))
			typ = ""
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return sc.Err()
}

// URL is the UI's address with the session token, for a window or a
// browser.
func (c *Client) URL() string { return c.base + "/?token=" + c.token }

// Plugins lists the installed plugins (GET /api/plugins).
func (c *Client) Plugins(ctx context.Context) (plugin.List, error) {
	var l plugin.List
	return l, c.do(ctx, http.MethodGet, "/api/plugins", nil, &l)
}

// PluginSettings shows a plugin's state and form; check asks it to try
// its credentials.
func (c *Client) PluginSettings(ctx context.Context, scheme string, check bool) (*plugin.Answer, error) {
	var a plugin.Answer
	q := ""
	if check {
		q = "?check=1"
	}
	if err := c.do(ctx, http.MethodGet, "/api/plugins/"+url.PathEscape(scheme)+"/settings"+q, nil, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// PluginRefresh tells the daemon a plugin's settings changed, and returns
// its state as the daemon sees it.
func (c *Client) PluginRefresh(ctx context.Context, scheme string) (*plugin.Answer, error) {
	var a plugin.Answer
	if err := c.do(ctx, http.MethodPost, "/api/plugins/"+url.PathEscape(scheme)+"/refresh", nil, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// PluginSurvey is each installed plugin's checked state as the daemon
// sees it, for doctor.
func (c *Client) PluginSurvey(ctx context.Context) ([]plugin.Survey, error) {
	l, err := c.Plugins(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]plugin.Survey, len(l.Plugins))
	var wg sync.WaitGroup
	for i, p := range l.Plugins {
		out[i] = plugin.Survey{Scheme: p.Scheme, Problem: p.Problem}
		if p.Problem != "" || !p.Capabilities.CanSettings() {
			continue
		}
		out[i].Settings = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := c.PluginSettings(ctx, p.Scheme, true)
			if err != nil {
				out[i].Problem = err.Error()
				return
			}
			out[i].Status, out[i].Hint = &a.Status, a.Hint
		}()
	}
	wg.Wait()
	return out, nil
}
