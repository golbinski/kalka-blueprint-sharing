package daemon

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/reference"
	"github.com/golbinski/kalka/internal/store"
)

// One workflow of each family, through the endpoints and bodies the page
// sends (plan T7): the team with presence and Sync now, a Personal
// document published and handed off, a seed budded and promoted, the
// settings, and the version line with the reference.
func TestAppWorkflows(t *testing.T) {
	about := func(o *Options) { o.About = About{Version: "1.2.3", WireProtocol: 1, PluginProtocol: 1} }
	depot, courier := teamPair(t, about)
	c := NewClient(depot.d.APIAddr(), depot.d.Token())
	ctx := context.Background()
	call := func(method, path string, in, out any) {
		t.Helper()
		if err := c.do(ctx, method, path, in, out); err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
	}

	// Team: the roster with presence; Sync now.
	waitFor(t, "presence", func() bool {
		var ts []core.TeamView
		call(http.MethodGet, "/api/teams", nil, &ts)
		for _, m := range ts[0].Members {
			if m.Email == courier.email {
				return m.Presence == "online"
			}
		}
		return false
	})
	var ts []core.TeamView
	call(http.MethodGet, "/api/teams", nil, &ts)
	for _, m := range ts[0].Members {
		if m.You && m.Presence != "" {
			t.Fatalf("the local user has a presence: %+v", m)
		}
	}
	call(http.MethodPost, "/api/teams/"+ts[0].Team+"/replicate", struct{}{}, nil)

	// Document: Personal, published from the list, handed off.
	rel := filepath.ToSlash(filepath.Join(store.PersonalDir, "lockers.md"))
	depot.write(filepath.FromSlash(rel), lockers)
	waitFor(t, "the index", func() bool { return len(depot.d.Store.List()) == 1 })
	var pub core.PublishResult
	call(http.MethodPost, "/api/publish", map[string]string{"path": rel, "team": ts[0].Team, "note": ""}, &pub)
	if pub.Version != 1 {
		t.Fatalf("publish: %+v", pub)
	}
	waitFor(t, "the published document", func() bool { return holds(courier, "every depot") })
	call(http.MethodPost, "/api/handoff", map[string]string{"path": pub.Path, "email": courier.email}, nil)
	waitFor(t, "the handoff", func() bool {
		items, _ := courier.d.Waiting("")
		return len(items) == 1 && items[0].Kind == core.WaitHanded
	})

	// Seed: budded from the document, promoted.
	var bud struct {
		Seed string `json:"seed"`
	}
	call(http.MethodPost, "/api/seeds", map[string]string{"path": pub.Path, "title": "Locker sizes", "problem": "Some parcels do not fit."}, &bud)
	call(http.MethodPost, "/api/promote", map[string]string{"path": bud.Seed, "status": "draft"}, nil)

	// Settings.
	off := false
	var set Settings
	call(http.MethodPut, "/api/settings", Settings{Notifications: &off}, &set)
	if set.Notifications == nil || *set.Notifications {
		t.Fatalf("settings: %+v", set)
	}

	// The version line and the reference.
	var a About
	call(http.MethodGet, "/api/about", nil, &a)
	if a.Version != "1.2.3" || a.Reference != reference.Version {
		t.Fatalf("about: %+v", a)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://"+depot.d.APIAddr()+"/api/reference", nil)
	req.Header.Set("Authorization", "Bearer "+depot.d.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Disposition"), reference.FileName) || string(body) != string(reference.Bytes()) {
		t.Fatalf("reference: %d %q", resp.StatusCode, resp.Header.Get("Content-Disposition"))
	}
}
