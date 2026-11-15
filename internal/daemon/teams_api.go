package daemon

import (
	"context"
	"github.com/golbinski/kalka/internal/core"
	"net/http"
	"net/url"
	"time"
)

// TeamRequest is the body of the roster endpoints. Team is a name or a
// hash; "-" or empty means the only team held.
type TeamRequest struct {
	Name    string `json:"name,omitempty"`
	Email   string `json:"email,omitempty"`
	Owner   *bool  `json:"owner,omitempty"`
	Expires string `json:"expires,omitempty"` // a Go duration, e.g. 72h
	Token   string `json:"token,omitempty"`
}

func teamRef(r *http.Request) string {
	if t := r.PathValue("team"); t != "-" {
		return t
	}
	return ""
}

func (d *Daemon) mountTeams(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/teams", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, d.Teams()) })
	mux.HandleFunc("POST /api/teams", d.teamCall(func(_ *http.Request, q TeamRequest) (any, error) { return d.CreateTeam(q.Name) }))
	mux.HandleFunc("GET /api/teams/{team}", func(w http.ResponseWriter, r *http.Request) {
		v, err := d.Team(teamRef(r))
		if err != nil {
			fail(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	})
	mux.HandleFunc("POST /api/teams/{team}/invites", d.teamCall(func(r *http.Request, q TeamRequest) (any, error) {
		var ttl time.Duration
		if q.Expires != "" {
			var err error
			if ttl, err = time.ParseDuration(q.Expires); err != nil {
				return nil, err
			}
		}
		return d.Invite(teamRef(r), q.Email, ttl)
	}))
	mux.HandleFunc("DELETE /api/invites/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := d.CancelInvite(r.PathValue("id")); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, struct{}{})
	})
	mux.HandleFunc("GET /api/teams/{team}/removal", func(w http.ResponseWriter, r *http.Request) {
		docs, err := d.RemovalPreview(teamRef(r), r.URL.Query().Get("email"))
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, docs)
	})
	mux.HandleFunc("POST /api/teams/{team}/remove", d.teamCall(func(r *http.Request, q TeamRequest) (any, error) { return d.Remove(teamRef(r), q.Email) }))
	mux.HandleFunc("POST /api/teams/{team}/owners", d.teamCall(func(r *http.Request, q TeamRequest) (any, error) {
		return d.SetOwner(teamRef(r), q.Email, q.Owner == nil || *q.Owner)
	}))
	mux.HandleFunc("POST /api/handoff", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Path  string `json:"path"`
			Email string `json:"email"`
		}
		if err := readBody(r, &q); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		res, err := d.Handoff(q.Path, q.Email)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/reassign", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Path  string `json:"path"`
			Email string `json:"email"`
		}
		if err := readBody(r, &q); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		res, err := d.Reassign(q.Path, q.Email)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/publish", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Path string `json:"path"`
			Team string `json:"team"`
			Note string `json:"note"`
		}
		if err := readBody(r, &q); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		res, err := d.Publish(q.Path, q.Team, q.Note)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/release", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Path         string   `json:"path"`
			Note         string   `json:"note"`
			Incorporates []string `json:"incorporates"`
			TakeOver     bool     `json:"take_over"` // a team owner becomes the owner first (7.4)
		}
		if err := readBody(r, &q); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		release := d.Release
		if q.TakeOver {
			release = d.TakeOverRelease
		}
		res, err := release(q.Path, q.Note, q.Incorporates)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("GET /api/domains", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, d.Domains()) })
	mux.HandleFunc("POST /api/delete", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Path     string `json:"path"`
			TakeOver bool   `json:"take_over"`
		}
		if err := readBody(r, &q); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		del := d.Delete
		if q.TakeOver {
			del = d.TakeOverDelete
		}
		if err := del(q.Path); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, struct{}{})
	})
	mux.HandleFunc("POST /api/teams/{team}/replicate", func(w http.ResponseWriter, r *http.Request) {
		team, err := d.TeamHash(teamRef(r))
		if err != nil {
			fail(w, http.StatusNotFound, err)
			return
		}
		st, err := d.ReplicateNow(r.Context(), team)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("GET /api/joins", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, d.Joins()) })
	mux.HandleFunc("POST /api/joins", d.teamCall(func(r *http.Request, q TeamRequest) (any, error) { return d.Join(r.Context(), q.Token) }))
}

func (d *Daemon) teamCall(f func(*http.Request, TeamRequest) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var q TeamRequest
		if err := readBody(r, &q); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		v, err := f(r, q)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

func teamPath(team string) string {
	if team == "" {
		team = "-"
	}
	return "/api/teams/" + url.PathEscape(team)
}

// Teams lists the teams held.
func (c *Client) Teams(ctx context.Context) ([]core.TeamView, error) {
	var v []core.TeamView
	return v, c.do(ctx, http.MethodGet, "/api/teams", nil, &v)
}

// Team shows one team; empty means the only one.
func (c *Client) Team(ctx context.Context, team string) (core.TeamView, error) {
	var v core.TeamView
	return v, c.do(ctx, http.MethodGet, teamPath(team), nil, &v)
}

// CreateTeam creates a team.
func (c *Client) CreateTeam(ctx context.Context, name string) (core.TeamView, error) {
	var v core.TeamView
	return v, c.do(ctx, http.MethodPost, "/api/teams", TeamRequest{Name: name}, &v)
}

// Invite makes an invite token.
func (c *Client) Invite(ctx context.Context, team, email, expires string) (core.InviteResult, error) {
	var v core.InviteResult
	return v, c.do(ctx, http.MethodPost, teamPath(team)+"/invites", TeamRequest{Email: email, Expires: expires}, &v)
}

// RemovalPreview lists the documents a removal reassigns.
func (c *Client) RemovalPreview(ctx context.Context, team, email string) ([]core.Reassigned, error) {
	var v []core.Reassigned
	return v, c.do(ctx, http.MethodGet, teamPath(team)+"/removal?email="+url.QueryEscape(email), nil, &v)
}

// Remove removes a member.
func (c *Client) Remove(ctx context.Context, team, email string) (core.TeamView, error) {
	var v core.TeamView
	return v, c.do(ctx, http.MethodPost, teamPath(team)+"/remove", TeamRequest{Email: email}, &v)
}

// SetOwner adds or removes an owner.
func (c *Client) SetOwner(ctx context.Context, team, email string, owner bool) (core.TeamView, error) {
	var v core.TeamView
	return v, c.do(ctx, http.MethodPost, teamPath(team)+"/owners", TeamRequest{Email: email, Owner: &owner}, &v)
}

// Handoff makes another member the owner (spec 7.2).
func (c *Client) Handoff(ctx context.Context, path, email string) (core.PublishResult, error) {
	var v core.PublishResult
	return v, c.do(ctx, http.MethodPost, "/api/handoff", map[string]string{"path": path, "email": email}, &v)
}

// Reassign makes any member the owner, for team owners (spec 7.3).
func (c *Client) Reassign(ctx context.Context, path, email string) (core.ReassignResult, error) {
	var v core.ReassignResult
	return v, c.do(ctx, http.MethodPost, "/api/reassign", map[string]string{"path": path, "email": email}, &v)
}

// Join redeems a token.
func (c *Client) Join(ctx context.Context, token string) (core.JoinView, error) {
	var v core.JoinView
	return v, c.do(ctx, http.MethodPost, "/api/joins", TeamRequest{Token: token}, &v)
}

// Joins lists joins.
func (c *Client) Joins(ctx context.Context) ([]core.JoinView, error) {
	var v []core.JoinView
	return v, c.do(ctx, http.MethodGet, "/api/joins", nil, &v)
}

// Publish publishes a document, or an update of it.
func (c *Client) Publish(ctx context.Context, path, team, note string) (core.PublishResult, error) {
	var v core.PublishResult
	return v, c.do(ctx, http.MethodPost, "/api/publish", map[string]string{"path": path, "team": team, "note": note}, &v)
}

// Release releases the next version of a domain document.
func (c *Client) Release(ctx context.Context, path, note string, incorporates []string) (core.PublishResult, error) {
	var v core.PublishResult
	return v, c.do(ctx, http.MethodPost, "/api/release", map[string]any{"path": path, "note": note, "incorporates": incorporates}, &v)
}

// Domains lists the domain documents held by the daemon.
func (c *Client) Domains(ctx context.Context) ([]core.DomainRow, error) {
	var v []core.DomainRow
	return v, c.do(ctx, http.MethodGet, "/api/domains", nil, &v)
}

// Delete deletes a published document for the team.
func (c *Client) Delete(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodPost, "/api/delete", map[string]string{"path": path}, nil)
}

// Replicate runs a replication round now.
func (c *Client) Replicate(ctx context.Context, team string) (core.TeamStatus, error) {
	var v core.TeamStatus
	return v, c.do(ctx, http.MethodPost, teamPath(team)+"/replicate", struct{}{}, &v)
}
