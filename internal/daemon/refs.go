package daemon

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// RefView is a reference as the reader's plugins resolved it (spec 10.3).
// Resolved content is shown, never written to the file or sent to peers.
type RefView struct {
	Ref      string       `json:"ref"`
	Scheme   string       `json:"scheme,omitempty"`
	Payload  string       `json:"payload,omitempty"`
	OK       bool         `json:"ok"`
	Error    string       `json:"error,omitempty"`
	Title    string       `json:"title,omitempty"`
	URL      string       `json:"url,omitempty"`
	Status   string       `json:"status,omitempty"`
	Snippet  *Snippet     `json:"snippet,omitempty"`
	Warnings []RefWarning `json:"warnings,omitempty"`
}

// Snippet is source text from a plugin; the UI shows it as text only.
type Snippet struct {
	Lang      string `json:"lang,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	Text      string `json:"text"`
}

// RefWarning is a plugin's warning about a reference, e.g. staleness.
type RefWarning struct {
	Text string `json:"text"`
}

// Resolver resolves the references of one blueprint with the reader's
// plugins (M4). It returns results keyed by ref; a ref it does not return
// is reported as having no plugin.
type Resolver func(ctx context.Context, doc *bp.Document, refs []string) map[string]RefView

var schemeRe = regexp.MustCompile(`^([a-z][a-z0-9+.-]*):(.*)$`)

// notDispatched are schemes never sent to plugins (spec 10.1).
var notDispatched = map[string]bool{"http": true, "https": true, "mailto": true, "file": true}

// splitRef returns a reference's scheme and payload; ok is false for text
// that is not a plugin reference.
func splitRef(ref string) (scheme, payload string, ok bool) {
	m := schemeRe.FindStringSubmatch(strings.TrimSpace(ref))
	if m == nil || notDispatched[m[1]] {
		return "", "", false
	}
	return m[1], m[2], true
}

// TraceView is a trace block with the divergences that depend on the
// reader's plugins added: Unverified and Stale (spec 6.10).
type TraceView struct {
	bp.Trace
	// Graph counts the divergences computed from the graph alone; the rest
	// of Divergences are Unverified and Stale.
	Graph int `json:"graph"`
}

// traceViews resolves the blocks' references and adds plugin-dependent
// divergences. refs gets every reference seen.
func (d *Daemon) traceViews(ctx context.Context, doc *bp.Document) ([]TraceView, map[string]RefView) {
	traces := doc.Traces()
	var want []string
	seen := map[string]bool{}
	for _, t := range traces {
		for _, n := range t.Nodes {
			if n.Ref != "" && !seen[n.Ref] {
				seen[n.Ref] = true
				want = append(want, n.Ref)
			}
		}
	}
	var got map[string]RefView
	var plugin []string
	for _, r := range want {
		if _, _, ok := splitRef(r); ok {
			plugin = append(plugin, r)
		}
	}
	if d.opts.Resolver != nil && len(plugin) > 0 {
		got = d.opts.Resolver(ctx, doc, plugin)
	}
	refs := map[string]RefView{}
	for _, r := range want {
		scheme, payload, ok := splitRef(r)
		v, found := got[r]
		switch {
		case !ok:
			v = RefView{Error: "not a plugin reference"}
		case !found:
			v = RefView{Error: fmt.Sprintf("no plugin installed for %s:", scheme)}
		}
		v.Ref, v.Scheme, v.Payload = r, scheme, payload
		refs[r] = v
	}

	out := make([]TraceView, 0, len(traces))
	for _, t := range traces {
		tv := TraceView{Trace: t, Graph: len(t.Divergences)}
		tv.Divergences = append([]bp.Divergence(nil), t.Divergences...)
		if t.Valid() {
			last := t.Lanes[len(t.Lanes)-1]
			for _, n := range t.Nodes {
				r, has := refs[n.Ref]
				switch {
				case n.Lane == last && n.Ref == "":
					tv.Divergences = append(tv.Divergences, bp.Divergence{Class: bp.DivUnverified, Node: n.ID, Detail: "no reference"})
				case n.Lane == last && !r.OK:
					tv.Divergences = append(tv.Divergences, bp.Divergence{Class: bp.DivUnverified, Node: n.ID, Detail: r.Error})
				}
				if has && r.OK && len(r.Warnings) > 0 {
					tv.Divergences = append(tv.Divergences, bp.Divergence{Class: bp.DivStale, Node: n.ID, Detail: r.Warnings[0].Text})
				}
			}
		}
		if tv.Divergences == nil {
			tv.Divergences = []bp.Divergence{}
		}
		out = append(out, tv)
	}
	return out, refs
}
