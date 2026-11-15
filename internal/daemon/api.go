package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/golbinski/kalka/internal/core"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/store"
)

// DaemonFile tells the CLI where the API is and holds its token (spec 13).
const DaemonFile = "daemon.json"

// CookieName holds the token in a browser.
const CookieName = "kalka_token"

// Endpoint is the content of daemon.json.
type Endpoint struct {
	Addr  string `json:"addr"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

func (d *Daemon) writeDaemonFile() error {
	data, _ := json.Marshal(Endpoint{Addr: d.apiAddr, Token: d.token, PID: os.Getpid()})
	return fsutil.WritePrivate(filepath.Join(d.dir, DaemonFile), data)
}

func (d *Daemon) removeDaemonFile() {
	p := filepath.Join(d.dir, DaemonFile)
	if raw, err := os.ReadFile(p); err == nil {
		var e Endpoint
		if json.Unmarshal(raw, &e) == nil && e.Token == d.token {
			_ = os.Remove(p)
		}
	}
}

// Handler is the local API with its guards.
func (d *Daemon) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", d.apiStatus)
	mux.HandleFunc("GET /api/blueprints", d.apiBlueprints)
	mux.HandleFunc("GET /api/doc", d.apiDoc)
	mux.HandleFunc("POST /api/entries", d.apiEntry)
	mux.HandleFunc("GET /api/seeds", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, d.Seeds()) })
	mux.HandleFunc("POST /api/seeds", d.apiBud)
	mux.HandleFunc("POST /api/backlink", d.apiBacklink)
	mux.HandleFunc("POST /api/promote", d.apiPromote)
	d.mountTeams(mux)
	mux.HandleFunc("POST /api/shutdown", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]int{"pid": os.Getpid()})
		// Send the answer before the server starts closing, or the caller
		// can see a dropped connection and take kalka for not running.
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		go d.Shutdown()
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) { serveEvents(d.Bus(), w, r) })
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		fail(w, http.StatusNotFound, errors.New("no such API endpoint"))
	})
	mux.HandleFunc("POST /api/open-external", d.apiOpenExternal)
	mux.HandleFunc("POST /api/ui/show", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Path string `json:"path"`
		}
		_ = readBody(r, &q)
		if d.Bus().Shells() == 0 {
			writeJSON(w, http.StatusConflict, APIError{Error: "no desktop window is attached", Kind: "no-shell"})
			return
		}
		d.Bus().Publish(core.BusEvent{Type: "ui-show", Data: map[string]string{"path": q.Path}})
		writeJSON(w, http.StatusOK, map[string]bool{"shown": true})
	})
	mux.HandleFunc("GET /api/summary", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, d.Summary()) })
	mux.HandleFunc("GET /api/settings", d.apiSettings)
	mux.HandleFunc("PUT /api/settings", d.apiSettings)
	mux.HandleFunc("POST /api/pause", d.apiPause(true))
	mux.HandleFunc("POST /api/resume", d.apiPause(false))
	return gate{token: d.token, port: d.port, noUI: d.opts.NoUI}.wrap(mux)
}

func (d *Daemon) port() string {
	_, p, _ := net.SplitHostPort(d.apiAddr)
	return p
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// APIError is the body of a failed request.
type APIError struct {
	Error string `json:"error"`
	Kind  string `json:"kind,omitempty"`
}

func fail(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, APIError{Error: err.Error(), Kind: errKind(err)})
}

// errKind classifies an error for agents.
func errKind(err error) string {
	var (
		off *core.ErrOffline
		dup *store.ErrDuplicate
	)
	switch {
	case errors.As(err, &off):
		return "offline"
	case errors.As(err, &dup):
		return "duplicate"
	case errors.Is(err, core.ErrPaused):
		return "paused"
	}
	return ""
}

func (d *Daemon) apiStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.State())
}

func (d *Daemon) apiBlueprints(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.Store.List())
}

func readBody(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return fmt.Errorf("bad request body: %w", err)
	}
	return nil
}

func (d *Daemon) apiDoc(w http.ResponseWriter, r *http.Request) {
	var ver int64
	if s := r.URL.Query().Get("v"); s != "" {
		if _, err := fmt.Sscanf(s, "%d", &ver); err != nil {
			fail(w, http.StatusBadRequest, errors.New("v must be a version number"))
			return
		}
	}
	v, err := d.DocumentAt(r.Context(), r.URL.Query().Get("path"), ver)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (d *Daemon) apiEntry(w http.ResponseWriter, r *http.Request) {
	var req core.EntryRequest
	if err := readBody(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	e, err := d.AddEntry(req)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": e.ID, "anchor": e.Anchor, "kind": e.Kind})
}

func (d *Daemon) apiBud(w http.ResponseWriter, r *http.Request) {
	var req core.BudRequest
	if err := readBody(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	res, err := d.Bud(req)
	if err != nil && res.Seed == "" {
		fail(w, http.StatusBadRequest, err)
		return
	}
	// A seed written without its backlink is still a success: the UI
	// offers "add the backlink" from res.BacklinkError.
	writeJSON(w, http.StatusOK, res)
}

func (d *Daemon) apiBacklink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Seed string `json:"seed"`
	}
	if err := readBody(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	e, err := d.Backlink(req.Seed)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	id := ""
	if e != nil {
		id = e.ID
	}
	writeJSON(w, http.StatusOK, map[string]string{"entry": id})
}

func (d *Daemon) apiPromote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path   string `json:"path"`
		Status string `json:"status"`
	}
	if err := readBody(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := d.Promote(req.Path, req.Status); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ExternalURL checks a link the page asks to open outside (spec 13): an
// absolute http or https URL with a host, nothing else (no file:, no
// javascript:, no custom schemes that start other programs).
func ExternalURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", errors.New("only http and https links open outside kalka")
	}
	return u.String(), nil
}

// apiOpenExternal opens a link in the system browser, for a page shown in
// the desktop window, which has no tabs.
func (d *Daemon) apiOpenExternal(w http.ResponseWriter, r *http.Request) {
	var q struct {
		URL string `json:"url"`
	}
	if err := readBody(r, &q); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	u, err := ExternalURL(q.URL)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if d.opts.OpenExternal == nil {
		fail(w, http.StatusNotImplemented, errors.New("this kalka cannot open a browser"))
		return
	}
	if err := d.opts.OpenExternal(u); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"opened": true})
}
