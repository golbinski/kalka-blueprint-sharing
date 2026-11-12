package daemon

import (
	"crypto/subtle"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/ui"
)

// CSP is sent with every response (spec 11.2): no inline script, no remote
// origins. React sets styles through the CSSOM, which style-src does not
// restrict, so no 'unsafe-inline' is needed.
const CSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; " +
	"connect-src 'self'; frame-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// MermaidPath holds the frame that draws ```mermaid blocks (spec 6.10).
// Mermaid is a large library working on untrusted text, so it runs in a
// sandboxed frame with an opaque origin: it cannot read the page, send the
// token cookie or reach the API, and talks to the page only by
// postMessage. The CSP sandbox directive keeps it sandboxed even when the
// frame is opened on its own. Mermaid's SVG carries <style> elements, which
// only this frame allows.
const MermaidPath = "/mermaid/"

func frameCSP(port string) string {
	self := "http://127.0.0.1:" + port + " http://localhost:" + port
	return "default-src 'none'; script-src 'self' " + self + "; style-src 'self' 'unsafe-inline' " + self +
		"; img-src data:; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'; sandbox allow-scripts"
}

// gate guards one listener (spec 13).
type gate struct {
	token string
	port  func() string
}

func (g gate) tokenOK(t string) bool {
	return t != "" && subtle.ConstantTimeCompare([]byte(t), []byte(g.token)) == 1
}

func (g gate) allowedHost(host string) bool {
	h, p, err := net.SplitHostPort(host)
	if err != nil || p != g.port() {
		return false
	}
	return h == "127.0.0.1" || h == "localhost"
}

func (g gate) allowedOrigin(origin string) bool {
	o, ok := strings.CutPrefix(origin, "http://")
	return ok && g.allowedHost(o)
}

// wrap serves the UI and the API behind the guards: every request needs a
// Host naming this loopback listener (defeats DNS rebinding); /api/ needs
// the token as bearer (CLI) or cookie (browser), and a browser request that
// changes state also needs this listener's Origin. The UI's static files
// carry no data and are served without a token, so the page can explain a
// missing one.
func (g gate) wrap(api http.Handler) http.Handler {
	static := staticHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		if strings.HasPrefix(r.URL.Path, MermaidPath) {
			h.Set("Content-Security-Policy", frameCSP(g.port()))
			h.Set("X-Frame-Options", "SAMEORIGIN")
		} else {
			h.Set("Content-Security-Policy", CSP)
			h.Set("X-Frame-Options", "DENY")
		}
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		if !g.allowedHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			// The URL kalka opens carries the token once; keep it as a
			// cookie and drop it from the address bar.
			if t := r.URL.Query().Get("token"); t != "" {
				if g.tokenOK(t) {
					http.SetCookie(w, &http.Cookie{Name: CookieName, Value: g.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
				}
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			static.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		bearer, isBearer := strings.CutPrefix(auth, "Bearer ")
		cookie, _ := r.Cookie(CookieName)
		switch {
		case isBearer && g.tokenOK(bearer):
			// CLI: a bearer header cannot be sent cross-site without CORS,
			// which this server never grants.
		case cookie != nil && g.tokenOK(cookie.Value):
			if r.Method != http.MethodGet && r.Method != http.MethodHead && !g.allowedOrigin(r.Header.Get("Origin")) {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
		default:
			writeJSON(w, http.StatusForbidden, APIError{Error: "missing or wrong token: open the UI with the link `kalka open` prints", Kind: "token"})
			return
		}
		api.ServeHTTP(w, r)
	})
}

const missingUI = `<!doctype html><meta charset="utf-8"><title>kalka</title>
<p>The web UI is not built into this binary. Build it with <code>make web</code> and rebuild <code>kalka</code>;
the CLI works without it.</p>`

// staticHandler serves the embedded UI; unknown paths get index.html so
// client-side routes work.
func staticHandler() http.Handler {
	if !ui.Built() {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(missingUI))
		})
	}
	files := ui.FS()
	fsrv := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if st, err := fs.Stat(files, p); err != nil || st.IsDir() {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		fsrv.ServeHTTP(w, r)
	})
}

// Bus fans daemon events out to the UI's event streams.
type Bus struct {
	mu   sync.Mutex
	subs map[chan BusEvent]struct{}
	n    atomic.Int32
}

// BusEvent is one server-sent event.
type BusEvent struct {
	Type string `json:"type"` // folder | prompt | prompt-closed | sync | status | peers
	Data any    `json:"data,omitempty"`
}

func newBus() *Bus { return &Bus{subs: map[chan BusEvent]struct{}{}} }

// Publish delivers e to every subscriber that keeps up.
func (b *Bus) Publish(e BusEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Subscribe adds a stream.
func (b *Bus) Subscribe() (<-chan BusEvent, func()) {
	ch := make(chan BusEvent, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	b.n.Add(1)
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
		b.n.Add(-1)
	}
}

// Clients is the number of connected UI streams.
func (b *Bus) Clients() int { return int(b.n.Load()) }

// serveEvents streams bus events as server-sent events.
func serveEvents(b *Bus, w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	ch, cancel := b.Subscribe()
	defer cancel()
	_, _ = w.Write([]byte(": hello\n\n"))
	fl.Flush()
	t := time.NewTicker(25 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
			_, _ = w.Write([]byte(": ping\n\n"))
		case e := <-ch:
			data, err := jsonMarshal(e.Data)
			if err != nil {
				continue
			}
			_, _ = w.Write([]byte("event: " + e.Type + "\ndata: " + string(data) + "\n\n"))
		}
		fl.Flush()
	}
}

// SetupDefaults are suggested values for the setup form.
type SetupDefaults struct {
	Folder     string `json:"folder"`
	Rendezvous string `json:"rendezvous"`
}

// SetupHandler serves the UI in setup mode (no config yet, spec 5): the
// status says so and POST /api/setup saves the three settings. done
// receives the saved config.
func SetupHandler(token string, port func() string, done chan<- *config.Config) http.Handler {
	home, _ := os.UserHomeDir()
	defaults := SetupDefaults{Folder: filepath.Join(home, "blueprints"), Rendezvous: config.PeerJSCloud}
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"setup": true, "defaults": defaults})
	})
	mux.HandleFunc("POST /api/setup", func(w http.ResponseWriter, r *http.Request) {
		var c config.Config
		if err := readBody(r, &c); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		cfg := &config.Config{Email: strings.TrimSpace(c.Email), Rendezvous: strings.TrimSpace(c.Rendezvous), Folder: strings.TrimSpace(c.Folder), Team: c.Team}
		if err := SaveSetup(cfg); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
		once.Do(func() { done <- cfg })
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		fail(w, http.StatusServiceUnavailable, errors.New("kalka is not set up yet"))
	})
	return gate{token: token, port: port}.wrap(mux)
}

// SaveSetup validates and saves the setup settings, creating the folder.
func SaveSetup(cfg *config.Config) error {
	if strings.HasPrefix(cfg.Folder, "~") {
		home, _ := os.UserHomeDir()
		cfg.Folder = filepath.Join(home, strings.TrimPrefix(cfg.Folder, "~"))
	}
	if cfg.Folder != "" && !filepath.IsAbs(cfg.Folder) {
		return errors.New("folder must be an absolute path")
	}
	if err := Validate(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.Folder, 0o755); err != nil {
		return err
	}
	return config.Save(cfg)
}
