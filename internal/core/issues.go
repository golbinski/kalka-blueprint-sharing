package core

// Issue metadata (spec 10.5). What a tracker says about an issue depends
// on the reader's own credentials, like a resolved reference: it is
// fetched by the reader's plugin, kept in a cache in the configuration
// folder, and never written into a blueprint, into anything replicated or
// into a message to a peer. The core asks a plugin only for what its
// describe answer declares, and never looks at its name or scheme.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/check"
	"github.com/golbinski/kalka/internal/plugin"
)

// Cache lifetimes for issue metadata (spec 10.5).
var (
	// IssueTTL: a fetched answer is used for this long before it is
	// fetched again; it is still shown, with its time, after that.
	IssueTTL = 15 * time.Minute
	// IssueFailTTL: after a failed fetch the plugin is asked again this
	// soon; the tracker's "not found" counts as an answer.
	IssueFailTTL = time.Minute
	// capsTTL: how long the capabilities of the installed plugins are
	// trusted before describe runs again (the host also notices a
	// changed executable at once).
	capsTTL = time.Minute
)

// issueCacheFile is the cache's file in the configuration folder, which
// is never replicated.
const issueCacheFile = "issue-cache.json"

// IssueView is what the reader knows about a blueprint's issue.
type IssueView struct {
	// Issue is the kalka_issue value; Scheme and Payload its parts.
	Issue   string `json:"issue"`
	Scheme  string `json:"scheme"`
	Payload string `json:"payload"`
	// Caps are the issue capabilities the scheme's plugin declares; nil
	// when no plugin for it is installed or it declares none.
	Caps *plugin.IssueCaps `json:"caps,omitempty"`
	// Meta is the last answer of the tracker, AsOf when it was fetched.
	Meta *plugin.IssueMeta `json:"meta,omitempty"`
	AsOf string            `json:"as_of,omitempty"`
	// Stale: Meta is older than IssueTTL or the last fetch failed.
	Stale bool `json:"stale,omitempty"`
	// Pending: a fetch is running.
	Pending bool `json:"pending,omitempty"`
	// Error is why there is no fresh answer: no plugin, the plugin
	// failed, or the tracker's own answer (not found, no permission).
	Error string `json:"error,omitempty"`
}

// IssueCapsSummary says which issue features the installed plugins make
// possible, so the page offers only those (spec 11.1).
type IssueCapsSummary struct {
	Meta      bool `json:"meta"`
	Hierarchy bool `json:"hierarchy"`
	Export    bool `json:"export"`
}

type issueEntry struct {
	Meta    *plugin.IssueMeta `json:"meta,omitempty"`
	Fetched time.Time         `json:"fetched,omitzero"`
	// Error and FailedAt record the last failure; Meta stays.
	Error    string    `json:"error,omitempty"`
	FailedAt time.Time `json:"failed_at,omitzero"`
}

type capsEntry struct {
	caps  *plugin.Capabilities
	until time.Time
	err   string
}

type issueCache struct {
	plugins Plugins
	now     func() time.Time
	file    string // "" keeps the cache in memory only
	ctx     context.Context
	logf    func(string, ...any)

	mu       sync.Mutex
	entries  map[string]*issueEntry // scheme + "\x00" + payload
	inflight map[string]bool
	caps     map[string]capsEntry
	capsBusy map[string]bool
	all      *IssueCapsSummary
	allUntil time.Time
	allBusy  bool
	// gen counts the changes of each scheme's plugin settings, allGen of
	// any: what was asked before a change is not stored (spec 10.6).
	gen    map[string]uint64
	allGen uint64
}

func newIssueCache(p Plugins, now func() time.Time, configDir string, logf func(string, ...any)) *issueCache {
	c := &issueCache{plugins: p, now: now, ctx: context.Background(), logf: logf,
		entries: map[string]*issueEntry{}, inflight: map[string]bool{}, caps: map[string]capsEntry{}, capsBusy: map[string]bool{}, gen: map[string]uint64{}}
	if configDir != "" {
		c.file = filepath.Join(configDir, issueCacheFile)
		if raw, err := os.ReadFile(c.file); err == nil {
			var saved map[string]*issueEntry
			if json.Unmarshal(raw, &saved) == nil {
				for k, e := range saved {
					if e != nil {
						c.entries[k] = e
					}
				}
			}
		}
	}
	return c
}

func issueKey(r bp.IssueRef) string { return r.Scheme + "\x00" + r.Payload }

// capsOf returns the scheme's capabilities if known; otherwise it starts
// asking in the background and reports pending.
func (c *issueCache) capsOf(scheme string, done func()) (caps *plugin.Capabilities, pending bool, errText string) {
	if c.plugins == nil {
		return nil, false, fmt.Sprintf("no plugin installed for %s:", scheme)
	}
	c.mu.Lock()
	e, ok := c.caps[scheme]
	fresh := ok && c.now().Before(e.until)
	busy := c.capsBusy[scheme]
	if !fresh && !busy {
		c.capsBusy[scheme] = true
	}
	gen := c.gen[scheme]
	c.mu.Unlock()
	if !fresh && !busy {
		go func() {
			ce := capsEntry{until: c.now().Add(capsTTL)}
			if _, err := c.plugins.Find(scheme); err != nil {
				ce.err = fmt.Sprintf("no plugin installed for %s:", scheme)
			} else if d, err := c.plugins.Capabilities(c.ctx, scheme); err != nil {
				ce.err = fmt.Sprintf("the %s: plugin failed: %v", scheme, err)
			} else {
				caps := d.Capabilities
				ce.caps = &caps
			}
			c.mu.Lock()
			if c.gen[scheme] == gen {
				c.caps[scheme] = ce
			}
			delete(c.capsBusy, scheme)
			c.mu.Unlock()
			if done != nil {
				done()
			}
		}()
	}
	if !ok {
		return nil, true, ""
	}
	return e.caps, false, e.err
}

// lookup returns a view for every issue, fetching what is missing or
// expired in the background, one batch per scheme; done runs when more
// is known. It never waits for a plugin.
func (c *issueCache) lookup(refs []bp.IssueRef, done func()) map[string]IssueView {
	out := map[string]IssueView{}
	missing := map[string][]string{}
	now := c.now()
	for _, r := range refs {
		k := r.String()
		if _, seen := out[k]; seen {
			continue
		}
		v := IssueView{Issue: k, Scheme: r.Scheme, Payload: r.Payload}
		caps, pending, errText := c.capsOf(r.Scheme, done)
		c.mu.Lock()
		e := c.entries[issueKey(r)]
		if e != nil {
			v.Meta = e.Meta
			if e.Meta != nil && !e.Fetched.IsZero() {
				v.AsOf = e.Fetched.UTC().Format(time.RFC3339)
				v.Stale = now.Sub(e.Fetched) > IssueTTL
			}
			if e.Error != "" && (e.Meta == nil || e.FailedAt.After(e.Fetched)) {
				v.Error = e.Error
				v.Stale = v.Meta != nil
			}
		}
		switch {
		case pending:
			v.Pending = true
		case caps == nil || !caps.CanMeta():
			// No plugin declares metadata for this scheme (any more):
			// the raw key only, never an old answer.
			v.Meta, v.AsOf, v.Stale, v.Error = nil, "", false, errText
			if caps != nil && caps.Issue != nil {
				ic := *caps.Issue
				v.Caps = &ic
			}
		default:
			ic := *caps.Issue
			v.Caps = &ic
			due := e == nil || (e.FailedAt.After(e.Fetched) && now.Sub(e.FailedAt) > IssueFailTTL) ||
				(!e.FailedAt.After(e.Fetched) && now.Sub(e.Fetched) > IssueTTL)
			ik := issueKey(r)
			if c.inflight[ik] {
				v.Pending = true
			} else if due {
				c.inflight[ik] = true
				v.Pending = true
				missing[r.Scheme] = append(missing[r.Scheme], r.Payload)
			}
		}
		c.mu.Unlock()
		out[k] = v
	}
	for scheme, batch := range missing {
		c.mu.Lock()
		gen := c.gen[scheme]
		c.mu.Unlock()
		go c.fetch(scheme, batch, gen, done)
	}
	return out
}

func (c *issueCache) fetch(scheme string, batch []string, gen uint64, done func()) {
	res, err := c.plugins.IssueMeta(c.ctx, scheme, batch)
	at := c.now()
	c.mu.Lock()
	if c.gen[scheme] != gen {
		// The plugin's settings changed meanwhile: this answer was given
		// for the old ones.
		for _, payload := range batch {
			delete(c.inflight, scheme+"\x00"+payload)
		}
		c.mu.Unlock()
		if done != nil {
			done()
		}
		return
	}
	for i, payload := range batch {
		k := scheme + "\x00" + payload
		delete(c.inflight, k)
		e := c.entries[k]
		if e == nil {
			e = &issueEntry{}
			c.entries[k] = e
		}
		switch {
		case err != nil:
			// The plugin as a whole failed (offline, crashed): keep the
			// last answer and say why it is not fresh.
			e.Error, e.FailedAt = err.Error(), at
		case !res[i].OK:
			// The tracker's own answer: no such issue, or no access.
			e.Meta, e.Fetched, e.Error, e.FailedAt = nil, time.Time{}, res[i].Error, at
		default:
			m := res[i]
			e.Meta, e.Fetched, e.Error, e.FailedAt = &m, at, "", time.Time{}
		}
	}
	c.saveLocked()
	c.mu.Unlock()
	if err != nil && c.logf != nil {
		c.logf("issue metadata for %s: %v", scheme, err)
	}
	if done != nil {
		done()
	}
}

// forgetScheme drops everything known about the scheme's issues and its
// plugin's capabilities, on disk too, and what is being asked for it.
func (c *issueCache) forgetScheme(scheme string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen[scheme]++
	c.allGen++
	delete(c.caps, scheme)
	c.all, c.allUntil = nil, time.Time{}
	prefix := scheme + "\x00"
	for k := range c.entries {
		if strings.HasPrefix(k, prefix) {
			delete(c.entries, k)
		}
	}
	c.saveLocked()
}

// saveLocked writes the cache to the configuration folder. A failure
// only costs the offline fallback after a restart.
func (c *issueCache) saveLocked() {
	if c.file == "" {
		return
	}
	raw, err := json.Marshal(c.entries)
	if err != nil {
		return
	}
	tmp := c.file + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, c.file)
	}
}

// summary is what the installed plugins make possible, refreshed in the
// background; done runs when it changes.
func (c *issueCache) summary(done func()) IssueCapsSummary {
	if c.plugins == nil {
		return IssueCapsSummary{}
	}
	c.mu.Lock()
	cur := c.all
	due := cur == nil || c.now().After(c.allUntil)
	start := due && !c.allBusy
	if start {
		c.allBusy = true
	}
	allGen := c.allGen
	c.mu.Unlock()
	if start {
		go func() {
			var s IssueCapsSummary
			for _, scheme := range c.plugins.Installed() {
				d, err := c.plugins.Capabilities(c.ctx, scheme)
				if err != nil {
					continue
				}
				s.Meta = s.Meta || d.Capabilities.CanMeta()
				s.Hierarchy = s.Hierarchy || d.Capabilities.CanHierarchy()
				s.Export = s.Export || d.Capabilities.CanExport()
			}
			c.mu.Lock()
			if c.allGen != allGen {
				c.allBusy = false
				c.mu.Unlock()
				if done != nil {
					done()
				}
				return
			}
			changed := c.all == nil || *c.all != s
			c.all, c.allUntil, c.allBusy = &s, c.now().Add(capsTTL), false
			c.mu.Unlock()
			if changed && done != nil {
				done()
			}
		}()
	}
	if cur == nil {
		return IssueCapsSummary{}
	}
	return *cur
}

// IssuesView answers GET /api/issues: what the installed plugins make
// possible and what is known about the issues of a scope's documents.
type IssuesView struct {
	Caps   IssueCapsSummary     `json:"caps"`
	Issues map[string]IssueView `json:"issues"`
}

// IssuesIn looks up the issues the scope's documents name. The answer
// never waits for a plugin; an "issues" event says when more is known.
func (d *Node) IssuesIn(sc Scope) IssuesView {
	notify := func() { d.bus.Publish(BusEvent{Type: "issues"}) }
	var refs []bp.IssueRef
	seen := map[string]bool{}
	for _, in := range d.Store.List() {
		if in.Issue == "" || seen[in.Issue] || !d.Holds(sc, in.Path) {
			continue
		}
		if r, ok := bp.ParseIssue(in.Issue); ok {
			seen[in.Issue] = true
			refs = append(refs, r)
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].String() < refs[j].String() })
	return IssuesView{Caps: d.issues.summary(notify), Issues: d.issues.lookup(refs, notify)}
}

// GraphIssues is what the reader's plugins know about the issues of a
// graph's cards: the tracker status the canvas shows on a card (spec
// 11.1). It is an overlay of the API only, from the reader's cache, so
// kalka graph stays the same with and without one; kalka never reads
// anything into the status.
func (d *Node) GraphIssues(g check.Graph) map[string]IssueView {
	var refs []bp.IssueRef
	seen := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Card != "" || n.Issue == "" || seen[n.Issue] {
			continue
		}
		if r, ok := bp.ParseIssue(n.Issue); ok {
			seen[n.Issue] = true
			refs = append(refs, r)
		}
	}
	if len(refs) == 0 {
		return map[string]IssueView{}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].String() < refs[j].String() })
	return d.issues.lookup(refs, func() { d.bus.Publish(BusEvent{Type: "issues"}) })
}

// issueView is the open document's issue, or nil.
func (d *Node) issueView(doc *bp.Document, rel string) *IssueView {
	r, ok := doc.Issue()
	if !ok {
		return nil
	}
	v := d.issues.lookup([]bp.IssueRef{r}, func() {
		d.bus.Publish(BusEvent{Type: "issues", Data: map[string]string{"path": rel}})
	})[r.String()]
	return &v
}

// PluginChanged is called after the scheme's plugin settings changed (or
// may have): what was fetched with the old ones is dropped, not marked
// stale, and the page is told once each to ask again (spec 10.6).
func (d *Node) PluginChanged(scheme string) {
	if d.opts.Plugins != nil {
		d.opts.Plugins.Forget(scheme)
	}
	d.refs.forgetScheme(scheme)
	d.issues.forgetScheme(scheme)
	for _, t := range []string{"issues", "refs", "plugins"} {
		d.bus.Publish(BusEvent{Type: t, Data: map[string]string{"scheme": scheme}})
	}
}
