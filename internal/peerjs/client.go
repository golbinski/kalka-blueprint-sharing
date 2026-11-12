package peerjs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// HeartbeatInterval matches the PeerJS client library's default.
const HeartbeatInterval = 5 * time.Second

// ErrIDTaken means another connection holds this id: the same email is
// online elsewhere (spec 9.2).
var ErrIDTaken = errors.New("this email is already online elsewhere (peer id taken)")

// Client is a connection to a PeerJS server.
type Client struct {
	id   string
	conn *websocket.Conn
	in   chan Message

	mu     sync.Mutex
	closed bool
	err    error
	cancel context.CancelFunc
}

// Dial registers id with the server and waits for OPEN.
func Dial(ctx context.Context, srv Server, id string) (*Client, error) {
	if !ValidID(id) {
		return nil, fmt.Errorf("peer id %q is not valid for PeerJS", id)
	}
	tok := make([]byte, 8)
	_, _ = rand.Read(tok)
	q := url.Values{"key": {srv.Key}, "id": {id}, "token": {hex.EncodeToString(tok)}, "version": {"1.5.4"}}
	u := srv.URL + "?" + q.Encode()

	hdr := http.Header{}
	hdr.Set("User-Agent", "kalka")
	conn, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		return nil, fmt.Errorf("rendezvous %s: %w", srv.URL, err)
	}
	conn.SetReadLimit(1 << 20)

	var first Message
	if err := readJSON(ctx, conn, &first); err != nil {
		conn.CloseNow()
		return nil, fmt.Errorf("rendezvous %s: %w", srv.URL, err)
	}
	switch first.Type {
	case Open:
	case IDTaken:
		conn.CloseNow()
		return nil, ErrIDTaken
	case Error:
		conn.CloseNow()
		return nil, fmt.Errorf("rendezvous %s refused: %s", srv.URL, payloadMsg(first.Payload))
	default:
		conn.CloseNow()
		return nil, fmt.Errorf("rendezvous %s: unexpected first message %s", srv.URL, first.Type)
	}

	loop, cancel := context.WithCancel(context.Background())
	c := &Client{id: id, conn: conn, in: make(chan Message, 64), cancel: cancel}
	go c.readLoop(loop)
	go c.heartbeat(loop)
	return c, nil
}

func payloadMsg(p json.RawMessage) string {
	var v struct {
		Msg string `json:"msg"`
	}
	if json.Unmarshal(p, &v) == nil && v.Msg != "" {
		return v.Msg
	}
	return string(p)
}

func readJSON(ctx context.Context, c *websocket.Conn, v any) error {
	_, data, err := c.Read(ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// ID returns the registered peer id.
func (c *Client) ID() string { return c.id }

// Messages delivers messages from the server. It is closed when the
// connection ends; Err then says why.
func (c *Client) Messages() <-chan Message { return c.in }

// Err returns the error that ended the connection, if any.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Send sends a message to dst.
func (c *Client) Send(ctx context.Context, typ, dst string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(Message{Type: typ, Dst: dst, Payload: raw})
	return c.conn.Write(ctx, websocket.MessageText, data)
}

// Close leaves the server.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	data, _ := json.Marshal(Message{Type: Leave})
	_ = c.conn.Write(ctx, websocket.MessageText, data)
	c.cancel()
	return c.conn.Close(websocket.StatusNormalClosure, "")
}

func (c *Client) readLoop(ctx context.Context) {
	defer close(c.in)
	for {
		var m Message
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			c.mu.Lock()
			if !c.closed {
				c.err = err
			}
			c.mu.Unlock()
			c.cancel()
			return
		}
		if json.Unmarshal(data, &m) != nil || m.Type == Heartbeat {
			continue
		}
		select {
		case c.in <- m:
		case <-ctx.Done():
			return
		}
	}
}

func (c *Client) heartbeat(ctx context.Context) {
	t := time.NewTicker(HeartbeatInterval)
	defer t.Stop()
	data, _ := json.Marshal(Message{Type: Heartbeat})
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = c.conn.Write(wctx, websocket.MessageText, data)
			cancel()
		}
	}
}
