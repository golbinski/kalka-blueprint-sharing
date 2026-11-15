package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/store"
)

// The waiting list over the API (spec 11.3, 13): a handoff waits for the
// new owner, opening the document clears it and keeps the mark in
// state.yaml; the lists take ?team=.
func TestWaitingAPI(t *testing.T) {
	depot, courier := teamPair(t)
	depot.write("parcels/lockers.md", lockers)
	waitFor(t, "the index", func() bool { return len(depot.d.Store.List()) == 1 })
	if _, err := depot.d.Publish(depot.path("parcels/lockers.md"), "", ""); err != nil {
		t.Fatal(err)
	}
	depot.write(filepath.Join(store.PersonalDir, "notes.md"), "# Depot notes\n\nKeys at the desk.\n")
	waitFor(t, "the pushed document", func() bool { return holds(courier, "every depot") })
	if _, err := depot.d.Handoff(depot.path("parcels/lockers.md"), courier.email); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the handoff", func() bool {
		items, _ := courier.d.Waiting("")
		return len(items) == 1
	})

	c := NewClient(courier.d.APIAddr(), courier.d.Token())
	ctx := context.Background()
	items, err := c.Waiting(ctx, "parcels")
	if err != nil || len(items) != 1 || items[0].Kind != core.WaitHanded || items[0].Path == "" {
		t.Fatalf("waiting: %+v %v", items, err)
	}
	if items, _ := c.Waiting(ctx, "personal"); len(items) != 0 {
		t.Fatalf("Personal waits: %+v", items)
	}
	var re *RemoteError
	if _, err := c.Waiting(ctx, "lending"); !errors.As(err, &re) || re.Kind != core.KindUnknownTeam {
		t.Fatalf("an unknown team: %v", err)
	}
	if sum, _ := c.Summary(ctx); sum.Waiting.Total != 1 || sum.Waiting.Oldest == nil || sum.Waiting.Oldest.Target != "#/t/parcels/waiting" {
		t.Fatalf("summary: %+v", sum.Waiting)
	}
	if ts, _ := c.Teams(ctx); len(ts) != 1 || ts[0].Waiting != 1 {
		t.Fatalf("teams: %+v", ts)
	}

	// Opening the document clears it.
	rel := strings.TrimPrefix(items[0].Path, courier.folder+string(filepath.Separator))
	var doc core.DocView
	if err := c.do(ctx, http.MethodGet, "/api/doc?path="+url.QueryEscape(filepath.ToSlash(rel)), nil, &doc); err != nil {
		t.Fatal(err)
	}
	if items, _ := c.Waiting(ctx, ""); len(items) != 0 {
		t.Fatalf("still waiting after opening: %+v", items)
	}
	raw, _ := os.ReadFile(filepath.Join(courier.cfgDir, StateFile))
	if !strings.Contains(string(raw), "seen:") {
		t.Fatalf("state.yaml has no mark:\n%s", raw)
	}
	if again, err := c.MarkSeen(ctx, items[0].Path); err != nil || again {
		t.Fatalf("marked twice: %v", err)
	}

	// ?team= on the lists: the depot's Personal file is not the team's.
	d := NewClient(depot.d.APIAddr(), depot.d.Token())
	var team, personal, all []store.Info
	for q, out := range map[string]*[]store.Info{"?team=parcels": &team, "?team=personal": &personal, "": &all} {
		if err := d.do(ctx, http.MethodGet, "/api/blueprints"+q, nil, out); err != nil {
			t.Fatal(err)
		}
	}
	if len(team) != 1 || len(personal) != 1 || len(all) != 2 || !strings.Contains(personal[0].Path, store.PersonalDir) {
		t.Fatalf("scoped lists: team %d, personal %d, all %d", len(team), len(personal), len(all))
	}
	if rows, err := d.Domains(ctx, "personal"); err != nil || len(rows) != 0 {
		t.Fatalf("domains: %+v %v", rows, err)
	}
}
