package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/identity"
	"github.com/golbinski/kalka/internal/store"
	"github.com/golbinski/kalka/internal/syncproto"
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
	mux.HandleFunc("GET /api/peers", d.apiPeers)
	mux.HandleFunc("POST /api/peers", d.apiAddPeer)
	mux.HandleFunc("DELETE /api/peers/{email}", d.apiForget)
	mux.HandleFunc("GET /api/verify/{email}", d.apiCode)
	mux.HandleFunc("POST /api/verify/{email}", d.apiConfirm)
	mux.HandleFunc("POST /api/sync", d.apiSync)
	mux.HandleFunc("GET /api/prompts", d.apiPrompts)
	mux.HandleFunc("POST /api/prompts/{id}", d.apiAnswer)
	mux.HandleFunc("GET /api/doc", d.apiDoc)
	mux.HandleFunc("POST /api/entries", d.apiEntry)
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) { serveEvents(d.bus, w, r) })
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		fail(w, http.StatusNotFound, errors.New("no such API endpoint"))
	})
	return gate{token: d.token, port: d.port}.wrap(mux)
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
		off *ErrOffline
		kc  *identity.KeyChangedError
		rej *syncproto.Rejected
		dv  *syncproto.Diverged
		dup *store.ErrDuplicate
	)
	switch {
	case errors.As(err, &off):
		return "offline"
	case errors.As(err, &kc):
		return "key-changed"
	case errors.As(err, &dv):
		return "diverged"
	case errors.As(err, &rej):
		return "rejected-" + string(rej.Reason)
	case errors.As(err, &dup):
		return "duplicate"
	case errors.Is(err, syncproto.ErrNotVerified):
		return "not-verified"
	}
	return ""
}

func (d *Daemon) apiStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.State())
}

func (d *Daemon) apiBlueprints(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.Store.List())
}

func (d *Daemon) apiPeers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.PeerList())
}

func readBody(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return fmt.Errorf("bad request body: %w", err)
	}
	return nil
}

func (d *Daemon) apiAddPeer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := readBody(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if !bp.ValidEmail(req.Email) {
		fail(w, http.StatusBadRequest, fmt.Errorf("%q is not an email address", req.Email))
		return
	}
	d.AddAddress(req.Email)
	d.Probe(r.Context(), req.Email)
	for _, p := range d.PeerList() {
		if bp.SameEmail(p.Email, req.Email) {
			writeJSON(w, http.StatusOK, p)
			return
		}
	}
}

func (d *Daemon) apiForget(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	ok, err := d.Peers.Forget(email)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		fail(w, http.StatusNotFound, fmt.Errorf("no key is pinned for %s", email))
		return
	}
	d.logf("removed the pinned key for %s", email)
	writeJSON(w, http.StatusOK, map[string]bool{"forgotten": true})
}

// CodeView is the verification state of one peer.
type CodeView struct {
	Email      string `json:"email"`
	Code       string `json:"code"`
	Verified   bool   `json:"verified"`
	KeyChanged bool   `json:"key_changed,omitempty"`
}

func (d *Daemon) codeView(email string) (CodeView, error) {
	p, ok := d.Peers.Get(email)
	if !ok || p.Key() == nil {
		return CodeView{}, fmt.Errorf("no key is pinned for %s yet: it is pinned on first contact, so sync with them first", email)
	}
	return CodeView{Email: p.Email, Code: identity.Code(d.pub, p.Key()), Verified: p.Verified, KeyChanged: p.KeyChanged != nil}, nil
}

func (d *Daemon) apiCode(w http.ResponseWriter, r *http.Request) {
	v, err := d.codeView(r.PathValue("email"))
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (d *Daemon) apiConfirm(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	p, ok := d.Peers.Get(email)
	if !ok || p.Key() == nil {
		_, err := d.codeView(email)
		fail(w, http.StatusNotFound, err)
		return
	}
	if err := d.Peers.SetVerified(email, p.Key()); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	v, _ := d.codeView(email)
	writeJSON(w, http.StatusOK, v)
}

// Event is one line of the /api/sync stream.
type Event struct {
	Type    string            `json:"type"` // progress | prompt | prompt-closed | result | error
	Message string            `json:"message,omitempty"`
	Prompt  *Pending          `json:"prompt,omitempty"`
	Result  *syncproto.Result `json:"result,omitempty"`
	Error   string            `json:"error,omitempty"`
	Kind    string            `json:"kind,omitempty"`
}

// apiSync runs a sync and streams its progress and prompts as NDJSON; the
// client answers prompts with POST /api/prompts/{id}.
func (d *Daemon) apiSync(w http.ResponseWriter, r *http.Request) {
	var req SyncRequest
	if err := readBody(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	scope := "op-" + hex.EncodeToString(b)

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	events := make(chan Event, 32)
	notices, stop := d.Prompts.Subscribe(scope)
	defer stop()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	emit := func(ev Event) {
		select {
		case events <- ev:
		case <-ctx.Done():
		}
	}
	go func() {
		defer close(events)
		res, err := d.Sync(ctx, req, scope, func(m string) { emit(Event{Type: "progress", Message: m}) })
		if err != nil {
			emit(Event{Type: "error", Error: err.Error(), Kind: errKind(err), Result: &res})
		} else {
			emit(Event{Type: "result", Result: &res})
		}
	}()
	enc := json.NewEncoder(w)
	for {
		select {
		case n := <-notices:
			ev := Event{Type: "prompt", Prompt: n.Prompt}
			if n.Closed {
				ev.Type = "prompt-closed"
			}
			_ = enc.Encode(ev)
		case ev, ok := <-events:
			if !ok {
				return
			}
			_ = enc.Encode(ev)
		case <-r.Context().Done():
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func (d *Daemon) apiPrompts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.Prompts.List())
}

func (d *Daemon) apiAnswer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Accept bool `json:"accept"`
	}
	if err := readBody(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := d.Prompts.Answer(r.PathValue("id"), req.Accept); err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"answered": true})
}

func (d *Daemon) apiDoc(w http.ResponseWriter, r *http.Request) {
	v, err := d.Document(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (d *Daemon) apiEntry(w http.ResponseWriter, r *http.Request) {
	var req EntryRequest
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
