package rendezvous

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Options configure `kalka rendezvous` (spec 9.6).
type Options struct {
	// Listen is the HTTP(S) address for signaling and /kalka/ice.
	Listen string
	// Path is where signaling is mounted ("/" serves <origin>/peerjs).
	// /kalka/ice is always at the root of the origin (spec 9.4).
	Path string
	Key  string
	// AllowOrigins are extra Origin host patterns accepted from browsers.
	AllowOrigins []string
	// TLS, if set, serves HTTPS and TURN over TLS.
	TLS *tls.Certificate
	// PlainHTTP keeps signaling on plain HTTP even with TLS set: TLS ends
	// at a reverse proxy and the certificate is for TURN over TLS only.
	PlainHTTP bool

	// TURN is off when TURNIP is nil.
	TURNIP      net.IP // the address peers reach the relay at
	TURNHost    string // the name in TURN URLs (default TURNIP)
	TURNPort    int    // UDP and TCP (default 3478)
	TURNTLSPort int    // TURN over TLS when TLS is set; 0 = off
	TURNListen  string // host the TURN listeners bind (default all)
	RelayBind   string // where relay sockets are opened (default 0.0.0.0)
	// RelayMin..RelayMax bound relay ports (0 = the OS picks).
	RelayMin, RelayMax uint16
	Secret             string // signs TURN credentials (default random per run)
	TTL                time.Duration
	// STUN URLs handed out besides the TURN ones (default: the TURN
	// address as STUN, when TURN is on).
	STUN []string
}

// Running is a started server.
type Running struct {
	// Addr is the HTTP(S) listen address actually bound.
	Addr string
	// URL is what clients put in `rendezvous:`.
	URL string
	// ICE is the TURN/STUN URLs handed out, empty when TURN is off.
	ICE []string

	srv     *Server
	http    *http.Server
	closers []io.Closer
	done    chan error
}

// Start binds every listener before returning, so a port conflict is
// reported at once, then serves until Close.
func Start(o Options) (*Running, error) {
	if o.Path == "" {
		o.Path = "/"
	}
	if !strings.HasPrefix(o.Path, "/") {
		return nil, fmt.Errorf("--path %q must start with /", o.Path)
	}
	if !strings.HasSuffix(o.Path, "/") {
		o.Path += "/"
	}
	if o.TTL == 0 {
		o.TTL = 12 * time.Hour
	}
	if o.TURNPort == 0 {
		o.TURNPort = 3478
	}
	if o.RelayBind == "" {
		o.RelayBind = "0.0.0.0"
	}
	r := &Running{done: make(chan error, 1)}
	fail := func(err error) (*Running, error) {
		r.closeAll()
		return nil, err
	}

	if o.TURNIP != nil {
		if o.Secret == "" {
			b := make([]byte, 32)
			_, _ = rand.Read(b)
			o.Secret = hex.EncodeToString(b)
		}
		host := o.TURNHost
		if host == "" {
			host = o.TURNIP.String()
		}
		hp := net.JoinHostPort(host, strconv.Itoa(o.TURNPort))
		udp, err := net.ListenPacket("udp", net.JoinHostPort(o.TURNListen, strconv.Itoa(o.TURNPort)))
		if err != nil {
			return fail(fmt.Errorf("TURN UDP: %w", err))
		}
		r.closers = append(r.closers, udp)
		tcp, err := net.Listen("tcp", net.JoinHostPort(o.TURNListen, strconv.Itoa(o.TURNPort)))
		if err != nil {
			return fail(fmt.Errorf("TURN TCP: %w", err))
		}
		r.closers = append(r.closers, tcp)
		cfg := TURNConfig{Secret: o.Secret, RelayIP: o.TURNIP, RelayBind: o.RelayBind, MinPort: o.RelayMin, MaxPort: o.RelayMax, UDP: udp, TCP: tcp}
		r.ICE = []string{"turn:" + hp + "?transport=udp", "turn:" + hp + "?transport=tcp"}
		if o.TLS != nil && o.TURNTLSPort != 0 {
			l, err := net.Listen("tcp", net.JoinHostPort(o.TURNListen, strconv.Itoa(o.TURNTLSPort)))
			if err != nil {
				return fail(fmt.Errorf("TURN TLS: %w", err))
			}
			cfg.TLS = TLSListener(l, *o.TLS)
			r.closers = append(r.closers, cfg.TLS)
			r.ICE = append(r.ICE, "turns:"+net.JoinHostPort(host, strconv.Itoa(o.TURNTLSPort))+"?transport=tcp")
		}
		ts, err := StartTURN(cfg)
		if err != nil {
			return fail(fmt.Errorf("TURN: %w", err))
		}
		// The TURN server closes the listeners it was given.
		r.closers = []io.Closer{ts}
		if o.STUN == nil {
			o.STUN = []string{"stun:" + hp}
		}
	}

	r.srv = &Server{Key: o.Key, AllowOrigins: o.AllowOrigins}
	mux := http.NewServeMux()
	r.srv.Mount(mux, o.Path)
	mux.Handle("GET "+o.Path+"{$}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "PeerJS Server", "description": "kalka rendezvous"})
	}))
	// The PeerJS client library asks for an id when none is given.
	mux.Handle("GET "+o.Path+r.srv.Key+"/id", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, hex.EncodeToString(b))
	}))
	if o.TURNIP != nil {
		mux.Handle("/kalka/ice", ICEHandler(o.Secret, r.ICE, o.STUN, o.TTL))
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok\n") })

	ln, err := net.Listen("tcp", o.Listen)
	if err != nil {
		return fail(err)
	}
	scheme := "http"
	if o.TLS != nil && !o.PlainHTTP {
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{*o.TLS}, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
		scheme = "https"
	}
	r.Addr = ln.Addr().String()
	r.URL = scheme + "://" + r.Addr + o.Path
	if o.Key != "" && o.Key != "peerjs" {
		r.URL += "?key=" + o.Key
	}
	r.http = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { r.done <- r.http.Serve(ln) }()
	return r, nil
}

// Wait blocks until the HTTP server stops or ctx ends, then closes all.
func (r *Running) Wait(ctx context.Context) error {
	var err error
	select {
	case err = <-r.done:
	case <-ctx.Done():
	}
	r.Close()
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}

// Close stops everything.
func (r *Running) Close() {
	if r.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = r.http.Shutdown(ctx)
		cancel()
		r.http.Close()
	}
	if r.srv != nil {
		r.srv.Close()
	}
	r.closeAll()
}

func (r *Running) closeAll() {
	for _, c := range r.closers {
		_ = c.Close()
	}
	r.closers = nil
}
