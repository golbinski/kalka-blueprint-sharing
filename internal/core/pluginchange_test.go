package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/plugin"
)

// gatedTracker answers issue_meta only when let through, so a settings
// change can land while a fetch is in flight.
type gatedTracker struct {
	*fakeTracker
	gate    chan struct{}
	asked   chan struct{}
	forgets atomic.Int32
}

func (g *gatedTracker) IssueMeta(ctx context.Context, scheme string, issues []string) ([]plugin.IssueMeta, error) {
	g.asked <- struct{}{}
	<-g.gate
	return g.fakeTracker.IssueMeta(ctx, scheme, issues)
}

func (g *gatedTracker) Forget(string) { g.forgets.Add(1) }

func TestForgetSchemeDropsOnlyThatScheme(t *testing.T) {
	dir := t.TempDir()
	tr := newTracker(true)
	c := newIssueCache(tr, time.Now, dir, nil)
	c.mu.Lock()
	c.entries["other\x00X-1"] = &issueEntry{Meta: &plugin.IssueMeta{Issue: "X-1", OK: true, Title: "kept"}, Fetched: time.Now()}
	c.mu.Unlock()
	ref, _ := bp.ParseIssue("jira:DEV-123")
	waitFor(t, "the fetch", func() bool {
		c.lookup([]bp.IssueRef{ref}, nil)
		c.mu.Lock()
		defer c.mu.Unlock()
		e := c.entries["jira\x00DEV-123"]
		return e != nil && e.Meta != nil
	})
	c.summary(nil)
	waitFor(t, "the summary", func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.all != nil })

	c.forgetScheme("jira")
	c.mu.Lock()
	_, jira := c.entries["jira\x00DEV-123"]
	_, other := c.entries["other\x00X-1"]
	_, caps := c.caps["jira"]
	all := c.all
	c.mu.Unlock()
	if jira || !other || caps || all != nil {
		t.Fatalf("jira %v other %v caps %v summary %v", jira, other, caps, all)
	}
	raw, err := os.ReadFile(filepath.Join(dir, issueCacheFile))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]json.RawMessage
	_ = json.Unmarshal(raw, &saved)
	if _, ok := saved["jira\x00DEV-123"]; ok || saved["other\x00X-1"] == nil || strings.Contains(string(raw), "Redelivery") {
		t.Fatalf("on disk: %s", raw)
	}
}

func TestFetchAcrossAChangeStoresNothing(t *testing.T) {
	g := &gatedTracker{fakeTracker: newTracker(true), gate: make(chan struct{}), asked: make(chan struct{}, 4)}
	c := newIssueCache(g, time.Now, t.TempDir(), nil)
	ref, _ := bp.ParseIssue("jira:DEV-123")
	// Capabilities first, so the next lookup fetches.
	c.lookup([]bp.IssueRef{ref}, nil)
	waitFor(t, "capabilities", func() bool { c.mu.Lock(); defer c.mu.Unlock(); _, ok := c.caps["jira"]; return ok })
	done := make(chan struct{}, 4)
	c.lookup([]bp.IssueRef{ref}, func() { done <- struct{}{} })
	<-g.asked
	c.forgetScheme("jira") // the settings change while the tracker is asked
	close(g.gate)
	<-done
	c.mu.Lock()
	e := c.entries["jira\x00DEV-123"]
	inflight := c.inflight["jira\x00DEV-123"]
	c.mu.Unlock()
	if e != nil || inflight {
		t.Fatalf("stored %+v, inflight %v", e, inflight)
	}

	// The same for references.
	rc := newRefCache(newTracker(false), time.Now)
	doc, err := bp.Parse([]byte("# Plan\n\nSee jira:DEV-123.\n"))
	if err != nil {
		t.Fatal(err)
	}
	rdone := make(chan struct{}, 1)
	rc.lookup(doc, []string{"jira:DEV-123"}, func() { rdone <- struct{}{} })
	rc.forgetScheme("jira")
	<-rdone
	rc.mu.Lock()
	n := len(rc.entries)
	rc.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d references stored across a change", n)
	}
}

func TestPluginChangedTellsThePageOnce(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example")
	n := ns[0]
	g := &gatedTracker{fakeTracker: newTracker(true)}
	n.usePlugins(g)
	n.d.refs.entries["h\x00gh:x"] = refEntry{view: RefView{Ref: "gh:x", Scheme: "gh"}, until: time.Now().Add(time.Hour)}
	n.d.refs.entries["h\x00jira:x"] = refEntry{view: RefView{Ref: "jira:x", Scheme: "jira"}, until: time.Now().Add(time.Hour)}
	events, stop := n.d.bus.Subscribe()
	defer stop()
	n.d.PluginChanged("jira")
	if g.forgets.Load() != 1 {
		t.Fatalf("host forgot %d times", g.forgets.Load())
	}
	if _, ok := n.d.refs.entries["h\x00jira:x"]; ok {
		t.Fatal("jira reference kept")
	}
	if _, ok := n.d.refs.entries["h\x00gh:x"]; !ok {
		t.Fatal("gh reference dropped")
	}
	got := map[string]int{}
	for timeout := time.After(300 * time.Millisecond); ; {
		select {
		case e := <-events:
			if m, ok := e.Data.(map[string]string); ok && m["scheme"] == "jira" {
				got[e.Type]++
			}
			continue
		case <-timeout:
		}
		break
	}
	if got["issues"] != 1 || got["refs"] != 1 || got["plugins"] != 1 || len(got) != 3 {
		t.Fatalf("events %v", got)
	}
}
