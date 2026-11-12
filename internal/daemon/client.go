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
func (c *Client) Status(ctx context.Context) (Status, error) {
	var s Status
	return s, c.do(ctx, http.MethodGet, "/api/status", nil, &s)
}

// Peers lists peers.
func (c *Client) Peers(ctx context.Context) ([]PeerView, error) {
	var p []PeerView
	return p, c.do(ctx, http.MethodGet, "/api/peers", nil, &p)
}

// AddPeer adds an address for this session and probes it.
func (c *Client) AddPeer(ctx context.Context, email string) (PeerView, error) {
	var p PeerView
	return p, c.do(ctx, http.MethodPost, "/api/peers", map[string]string{"email": email}, &p)
}

// Forget removes a pinned key.
func (c *Client) Forget(ctx context.Context, email string) error {
	return c.do(ctx, http.MethodDelete, "/api/peers/"+url.PathEscape(email), nil, nil)
}

// Code returns the verification code for a peer.
func (c *Client) Code(ctx context.Context, email string) (CodeView, error) {
	var v CodeView
	return v, c.do(ctx, http.MethodGet, "/api/verify/"+url.PathEscape(email), nil, &v)
}

// Confirm marks a peer verified.
func (c *Client) Confirm(ctx context.Context, email string) (CodeView, error) {
	var v CodeView
	return v, c.do(ctx, http.MethodPost, "/api/verify/"+url.PathEscape(email), struct{}{}, &v)
}

// Answer answers a prompt.
func (c *Client) Answer(ctx context.Context, id string, accept bool) error {
	return c.do(ctx, http.MethodPost, "/api/prompts/"+url.PathEscape(id), map[string]bool{"accept": accept}, nil)
}

// Sync starts a sync and calls on for each event until the stream ends.
func (c *Client) Sync(ctx context.Context, req SyncRequest, on func(Event)) error {
	b, _ := json.Marshal(req)
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/sync", bytes.NewReader(b))
	if err != nil {
		return err
	}
	hr.Header.Set("Authorization", "Bearer "+c.token)
	hr.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(hr)
	if err != nil {
		return ErrNotRunning
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e APIError
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return &RemoteError{APIError: e}
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		var ev Event
		if json.Unmarshal(sc.Bytes(), &ev) == nil {
			on(ev)
		}
	}
	return sc.Err()
}
