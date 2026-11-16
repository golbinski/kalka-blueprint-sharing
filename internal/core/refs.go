package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/plugin"
)

// Plugins resolves references (spec 10); *plugin.Host implements it.
type Plugins interface {
	Find(scheme string) (string, error)
	Resolve(ctx context.Context, scheme string, frontmatter map[string]any, refs []plugin.Ref) []plugin.Result
}

// Cache lifetimes: spec 10.3 for results; failures are retried sooner.
var (
	RefTTL     = 5 * time.Minute
	RefFailTTL = time.Minute
)

// RefView is a reference as the reader's plugins resolved it. Resolved
// content is shown, never written to the file or sent to peers.
type RefView struct {
	Ref     string `json:"ref"`
	Scheme  string `json:"scheme,omitempty"`
	Payload string `json:"payload,omitempty"`
	// Plugin names the executable that answered.
	Plugin  string `json:"plugin,omitempty"`
	Pending bool   `json:"pending,omitempty"`
	plugin.Result
}

// refCache resolves references in the background and keeps the results,
// keyed by ref and frontmatter hash (spec 10.3). The document view never
// waits for a plugin: it shows what is known and a "resolving" state, and
// the UI is told when more is known. A slow or broken plugin therefore
// cannot affect the UI (spec 10).
type refCache struct {
	plugins Plugins
	now     func() time.Time
	ctx     context.Context

	mu       sync.Mutex
	entries  map[string]refEntry
	inflight map[string]bool
}

type refEntry struct {
	view  RefView
	until time.Time
}

func newRefCache(p Plugins, now func() time.Time) *refCache {
	return &refCache{plugins: p, now: now, ctx: context.Background(), entries: map[string]refEntry{}, inflight: map[string]bool{}}
}

func fmHash(doc *bp.Document) string {
	inner := ""
	if fm := doc.Frontmatter(); fm != nil {
		inner = fm.Inner()
	}
	sum := sha256.Sum256([]byte(inner))
	return hex.EncodeToString(sum[:8])
}

// lookup returns a view for every ref. Refs that need a plugin call are
// resolved in the background, one batch per scheme; done runs when a
// batch has finished.
func (c *refCache) lookup(doc *bp.Document, refs []string, done func()) map[string]RefView {
	out := map[string]RefView{}
	h := fmHash(doc)
	missing := map[string][]plugin.Ref{} // scheme -> refs to fetch
	pluginName := map[string]string{}
	now := c.now()

	c.mu.Lock()
	for _, r := range refs {
		scheme, payload, ok := bp.PluginRef(r)
		v := RefView{Ref: r, Scheme: scheme, Payload: payload}
		v.Result.Ref = r
		switch {
		case !ok:
			v.Error = "not a plugin reference"
		case c.plugins == nil:
			v.Error = fmt.Sprintf("no plugin installed for %s:", scheme)
		default:
			k := h + "\x00" + r
			if e, ok := c.entries[k]; ok && now.Before(e.until) {
				v = e.view
				break
			}
			name, found := pluginName[scheme]
			if !found {
				// Finding is cheap and not cached: a plugin installed
				// now is used on the next look.
				if p, err := c.plugins.Find(scheme); err == nil {
					name = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(p), ".exe"), ".cmd")
				}
				pluginName[scheme] = name
			}
			if name == "" {
				v.Error = fmt.Sprintf("no plugin installed for %s:", scheme)
				break
			}
			v.Plugin, v.Pending, v.Error = name, true, "resolving…"
			if !c.inflight[k] {
				c.inflight[k] = true
				missing[scheme] = append(missing[scheme], plugin.Ref{Ref: r, Payload: payload})
			}
		}
		out[r] = v
	}
	c.mu.Unlock()

	if len(missing) == 0 {
		return out
	}
	fm := doc.Frontmatter().Map()
	for scheme, batch := range missing {
		go func() {
			res := c.plugins.Resolve(c.ctx, scheme, fm, batch)
			at := c.now()
			c.mu.Lock()
			for i, r := range res {
				k := h + "\x00" + batch[i].Ref
				delete(c.inflight, k)
				ttl := RefTTL
				if !r.OK {
					ttl = RefFailTTL
				}
				_, payload, _ := bp.PluginRef(batch[i].Ref)
				r.Ref = batch[i].Ref
				c.entries[k] = refEntry{view: RefView{Ref: batch[i].Ref, Scheme: scheme, Payload: payload, Plugin: pluginName[scheme], Result: r}, until: at.Add(ttl)}
			}
			// Drop expired entries now and then.
			for k, e := range c.entries {
				if at.After(e.until) {
					delete(c.entries, k)
				}
			}
			c.mu.Unlock()
			if done != nil {
				done()
			}
		}()
	}
	return out
}

// TraceView is a trace block with the divergences that depend on the
// reader's plugins added: Unverified and Stale (spec 6.10).
type TraceView struct {
	bp.Trace
	// Graph counts the divergences computed from the graph alone; the rest
	// of Divergences are Unverified and Stale.
	Graph int `json:"graph"`
}

// refViews resolves the blueprint's references and builds the trace views.
// done runs when background resolution has more to show.
func (d *Node) refViews(doc *bp.Document, done func()) ([]TraceView, map[string]RefView) {
	refs := d.refs.lookup(doc, doc.References(), done)
	traces := doc.Traces()
	out := make([]TraceView, 0, len(traces))
	for _, t := range traces {
		tv := TraceView{Trace: t, Graph: len(t.Divergences)}
		tv.Divergences = append([]bp.Divergence(nil), t.Divergences...)
		if t.Valid() {
			last := t.Lanes[len(t.Lanes)-1]
			for _, n := range t.Nodes {
				r, has := refs[n.Ref]
				if !has && n.Ref != "" {
					r = RefView{Ref: n.Ref}
					r.Error = "not a plugin reference"
				}
				switch {
				case n.Lane == last && n.Ref == "":
					tv.Divergences = append(tv.Divergences, bp.Divergence{Class: bp.DivUnverified, Node: n.ID, Detail: "no reference"})
				case n.Lane == last && !r.OK:
					tv.Divergences = append(tv.Divergences, bp.Divergence{Class: bp.DivUnverified, Node: n.ID, Detail: r.Error})
				}
				if r.OK && len(r.Warnings) > 0 {
					tv.Divergences = append(tv.Divergences, bp.Divergence{Class: bp.DivStale, Node: n.ID, Detail: r.Warnings[0].Text})
				}
			}
		}
		if tv.Divergences == nil {
			tv.Divergences = []bp.Divergence{}
		}
		out = append(out, tv)
	}
	// Trace refs that are not plugin refs (an https link) still get a view.
	for _, t := range traces {
		for _, n := range t.Nodes {
			if _, ok := refs[n.Ref]; n.Ref != "" && !ok {
				v := RefView{Ref: n.Ref}
				v.Error = "not a plugin reference"
				refs[n.Ref] = v
			}
		}
	}
	return out, refs
}
