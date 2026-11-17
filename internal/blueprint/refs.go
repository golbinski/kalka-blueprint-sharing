package blueprint

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// References (spec 10.1) are Markdown links whose target has a non-web
// scheme: [label](scheme:payload). Plugins resolve them; the file only
// ever carries the reference.

var refRe = regexp.MustCompile(`^([a-z][a-z0-9+.-]*):(.*)$`)

// notDispatched are schemes never sent to plugins (spec 10.1), plus ones
// a browser would act on.
var notDispatched = map[string]bool{
	"http": true, "https": true, "mailto": true, "file": true,
	"javascript": true, "vbscript": true, "data": true, "blob": true, "about": true,
}

// PluginRef splits a link target into scheme and payload if it is a
// reference for a plugin.
func PluginRef(target string) (scheme, payload string, ok bool) {
	m := refRe.FindStringSubmatch(target)
	if m == nil || notDispatched[m[1]] || strings.IndexFunc(target, unicode.IsSpace) >= 0 {
		return "", "", false
	}
	return m[1], m[2], true
}

// References lists the plugin references in the body, the review log
// entries and the trace blocks, each once, in order of appearance.
func (d *Document) References() []string {
	var out []string
	seen := map[string]bool{}
	add := func(r string) {
		if _, _, ok := PluginRef(r); ok && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	links := func(src string) {
		root := goldmark.DefaultParser().Parse(text.NewReader([]byte(src)))
		_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			switch l := n.(type) {
			case *ast.Link:
				add(string(l.Destination))
			case *ast.AutoLink:
				add(string(l.URL([]byte(src))))
			}
			return ast.WalkContinue, nil
		})
	}
	links(d.Body())
	for _, t := range d.traces {
		for _, n := range t.Nodes {
			add(n.Ref)
		}
	}
	for _, it := range d.items {
		if it.Entry != nil {
			links(it.Entry.Body)
		}
	}
	return out
}
