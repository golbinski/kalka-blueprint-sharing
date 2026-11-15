package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/golbinski/kalka/internal/core"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/golbinski/kalka/internal/name"
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
	if errors.As(err, &re) && re.Kind == "no-shell" {
		return ErrNoShell
	}
	return err
}

// ShellEvents subscribes as a desktop shell (spec 22): while the stream
// is open the daemon leaves prompts and notices to the shell. It calls on
// for each bus event until ctx ends or the daemon goes away.
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
