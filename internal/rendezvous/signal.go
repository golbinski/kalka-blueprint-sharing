// Package rendezvous is kalka's PeerJS-compatible signaling server (spec
// 9.6). State is in memory only. It follows peerjs-server 1.0.2 behaviour:
// OPEN on registration, ID-TAKEN for a held id with another token, src set
// by the server, queueing for absent ids and EXPIRE after ExpireTimeout.
package rendezvous

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/golbinski/kalka/internal/peerjs"
)

// Server is a signaling server. Mount it with Handler.
type Server struct {
	Key           string
	ExpireTimeout time.Duration // default 5 s
	AliveTimeout  time.Duration // default 90 s
	// Intercept, if set, sees every relayed message and returns what to
	// deliver (nil drops it). Tests use it to play a malicious server.
	Intercept func(m peerjs.Message) *peerjs.Message
	// AllowOrigins lists Origin header patterns accepted for browser
	// clients; non-browser clients send no Origin and are always accepted.
	AllowOrigins []string

	mu      sync.Mutex
	clients map[string]*client
	queue   map[string][]queued
	stop    chan struct{}
	once    sync.Once
}

type client struct {
	token string
	conn  *websocket.Conn
	send  chan []byte
	seen  time.Time
}

type queued struct {
	m  peerjs.Message
	at time.Time
}

func (s *Server) init() {
	s.once.Do(func() {
		if s.Key == "" {
			s.Key = "peerjs"
		}
		if s.ExpireTimeout == 0 {
			s.ExpireTimeout = 5 * time.Second
		}
		if s.AliveTimeout == 0 {
			s.AliveTimeout = 90 * time.Second
		}
		s.clients = map[string]*client{}
		s.queue = map[string][]queued{}
		s.stop = make(chan struct{})
		go s.sweep()
	})
}

// Close stops the background sweep and drops all clients.
func (s *Server) Close() {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	for id, c := range s.clients {
		c.conn.CloseNow()
		delete(s.clients, id)
	}
}

// Handler serves the WebSocket endpoint; mount it at <path>peerjs.
func (s *Server) Handler() http.Handler {
	s.init()
	return http.HandlerFunc(s.serveWS)
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.AllowOrigins})
	if err != nil {
		return
	}
	conn.SetReadLimit(1 << 20)
	ctx := r.Context()
	q := r.URL.Query()
	id, token, key := q.Get("id"), q.Get("token"), q.Get("key")
	if id == "" || token == "" || key == "" {
		s.refuse(ctx, conn, peerjs.Error, "No id, token, or key supplied to websocket server")
		return
	}
	if key != s.Key {
		s.refuse(ctx, conn, peerjs.Error, "Invalid key provided")
		return
	}

	s.mu.Lock()
	if old, ok := s.clients[id]; ok && old.token != token {
		s.mu.Unlock()
		s.refuse(ctx, conn, peerjs.IDTaken, "ID is taken")
		return
	}
	c := &client{token: token, conn: conn, send: make(chan []byte, 64), seen: time.Now()}
	s.clients[id] = c
	pending := s.queue[id]
	delete(s.queue, id)
	s.mu.Unlock()

	// Unregister before closing c.send: Deliver sends under s.mu, so once c
	// is out of the map nothing can send on the closed channel.
	defer func() {
		s.mu.Lock()
		if s.clients[id] == c {
			delete(s.clients, id)
		}
		s.mu.Unlock()
		close(c.send)
		conn.CloseNow()
	}()

	go func() {
		for data := range c.send {
			wctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := conn.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				conn.CloseNow()
				return
			}
		}
	}()

	open, _ := json.Marshal(peerjs.Message{Type: peerjs.Open})
	c.send <- open
	for _, p := range pending {
		data, _ := json.Marshal(p.m)
		c.send <- data
	}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		s.mu.Lock()
		c.seen = time.Now()
		s.mu.Unlock()
		var m peerjs.Message
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		m.Src = id
		switch m.Type {
		case peerjs.Heartbeat:
			continue
		case peerjs.Leave:
			if m.Dst == "" {
				return
			}
		}
		s.relay(m)
	}
}

func (s *Server) refuse(ctx context.Context, conn *websocket.Conn, typ, msg string) {
	p, _ := json.Marshal(map[string]string{"msg": msg})
	data, _ := json.Marshal(peerjs.Message{Type: typ, Payload: p})
	_ = conn.Write(ctx, websocket.MessageText, data)
	conn.Close(websocket.StatusNormalClosure, "")
}

func (s *Server) relay(m peerjs.Message) {
	if s.Intercept != nil {
		out := s.Intercept(m)
		if out == nil {
			return
		}
		m = *out
	}
	s.Deliver(m)
}

// Deliver sends m to its destination as the server would, queueing it if
// the destination is absent. Exposed for tests that inject messages.
func (s *Server) Deliver(m peerjs.Message) {
	s.init()
	data, _ := json.Marshal(m)
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[m.Dst]; ok {
		select {
		case c.send <- data:
		default:
			c.conn.CloseNow()
		}
		return
	}
	if m.Dst != "" && m.Type != peerjs.Leave && m.Type != peerjs.Expire {
		s.queue[m.Dst] = append(s.queue[m.Dst], queued{m: m, at: time.Now()})
	}
}

// sweep expires queued messages and drops silent clients, like
// peerjs-server's messagesExpire and checkBrokenConnections.
func (s *Server) sweep() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-t.C:
			var expired []peerjs.Message
			s.mu.Lock()
			for dst, q := range s.queue {
				if len(q) == 0 || now.Sub(q[0].at) < s.ExpireTimeout {
					continue
				}
				seen := map[string]bool{}
				for _, item := range q {
					if !seen[item.m.Src] {
						seen[item.m.Src] = true
						expired = append(expired, peerjs.Message{Type: peerjs.Expire, Src: dst, Dst: item.m.Src})
					}
				}
				delete(s.queue, dst)
			}
			for id, c := range s.clients {
				if now.Sub(c.seen) > s.AliveTimeout {
					c.conn.CloseNow()
					delete(s.clients, id)
				}
			}
			s.mu.Unlock()
			for _, m := range expired {
				s.Deliver(m)
			}
		}
	}
}

// Mount registers the handler on mux under path (e.g. "/").
func (s *Server) Mount(mux *http.ServeMux, path string) {
	mux.Handle(strings.TrimSuffix(path, "/")+"/peerjs", s.Handler())
}
