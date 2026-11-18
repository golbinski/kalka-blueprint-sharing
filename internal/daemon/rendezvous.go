package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/doctor"
	"github.com/golbinski/kalka/internal/peerjs"
)

// RendezvousRequest is the body of POST /api/rendezvous.
type RendezvousRequest struct {
	Rendezvous string `json:"rendezvous"`
	// Force switches although the server did not answer.
	Force bool `json:"force,omitempty"`
}

// CheckRendezvous validates a server to switch to and, unless force, asks
// it to answer. It returns the value to store. An invalid value is a plain
// error; a silent server is a core.KindRendezvousSilent one.
func CheckRendezvous(ctx context.Context, server string, force bool) (string, error) {
	server = strings.TrimSpace(server)
	if _, err := peerjs.ParseRendezvous(server); err != nil {
		return "", err
	}
	if force {
		return server, nil
	}
	pctx, cancel := context.WithTimeout(ctx, doctor.Timeout)
	defer cancel()
	if _, err := doctor.AskRendezvous(pctx, server); err != nil {
		return "", &core.Error{Kind: core.KindRendezvousSilent, Msg: fmt.Sprintf("%s did not answer (%v)", server, err)}
	}
	return server, nil
}

// apiRendezvous switches the rendezvous server while running (spec 13):
// it saves config.yaml, then moves the node.
func (d *Daemon) apiRendezvous(w http.ResponseWriter, r *http.Request) {
	var q RendezvousRequest
	if err := readBody(r, &q); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	server, err := CheckRendezvous(r.Context(), q.Rendezvous, true)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if peerjs.SameServer(server, d.State().Rendezvous) {
		writeJSON(w, http.StatusOK, d.State())
		return
	}
	if !q.Force {
		if _, err := CheckRendezvous(r.Context(), server, false); err != nil {
			fail(w, http.StatusConflict, err)
			return
		}
	}
	// Only the server changes in the daemon's own config.yaml.
	d.mu.Lock()
	err = config.SetRendezvous(d.dir, server)
	if errors.Is(err, config.ErrMissing) && d.opts.Config != nil {
		// Started with settings given, not read (tests, embedding).
		c := *d.opts.Config
		c.Rendezvous = server
		err = config.SaveIn(d.dir, &c)
	}
	d.mu.Unlock()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	if err := d.SetRendezvous(server); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, d.State())
}
