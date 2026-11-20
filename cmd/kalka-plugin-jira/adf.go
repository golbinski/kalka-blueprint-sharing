package main

// Markdown to the Atlassian Document Format, for comments on Jira Cloud.
// It covers what a blueprint's summary uses: headings, paragraphs,
// emphasis, code, links, lists, quotes, code blocks and rules. Anything
// else becomes its text, so nothing is lost and nothing is interpreted.

import (
	"net/url"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type adfNode = map[string]any

// toADF converts Markdown into an ADF document.
func toADF(md string) adfNode {
	src := []byte(md)
	doc := goldmark.New().Parser().Parse(text.NewReader(src))
	content := blocks(doc, src)
	if len(content) == 0 {
		content = []adfNode{{"type": "paragraph", "content": []adfNode{}}}
	}
	return adfNode{"version": 1, "type": "doc", "content": content}
}

func blocks(n ast.Node, src []byte) []adfNode {
	out := []adfNode{}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, block(c, src)...)
	}
	return out
}

func block(n ast.Node, src []byte) []adfNode {
	switch n := n.(type) {
	case *ast.Heading:
		return []adfNode{{"type": "heading", "attrs": adfNode{"level": n.Level}, "content": inlines(n, src, nil)}}
	case *ast.Paragraph, *ast.TextBlock:
		in := inlines(n, src, nil)
		if len(in) == 0 {
			return nil
		}
		return []adfNode{{"type": "paragraph", "content": in}}
	case *ast.List:
		kind := "bulletList"
		attrs := adfNode(nil)
		if n.IsOrdered() {
			kind = "orderedList"
			attrs = adfNode{"order": max(n.Start, 1)}
		}
		var items []adfNode
		for it := n.FirstChild(); it != nil; it = it.NextSibling() {
			inner := blocks(it, src)
			if len(inner) == 0 {
				inner = []adfNode{{"type": "paragraph", "content": []adfNode{}}}
			}
			items = append(items, adfNode{"type": "listItem", "content": inner})
		}
		if len(items) == 0 {
			return nil
		}
		l := adfNode{"type": kind, "content": items}
		if attrs != nil {
			l["attrs"] = attrs
		}
		return []adfNode{l}
	case *ast.Blockquote:
		inner := blocks(n, src)
		if len(inner) == 0 {
			return nil
		}
		return []adfNode{{"type": "blockquote", "content": inner}}
	case *ast.FencedCodeBlock:
		return []adfNode{codeBlock(string(n.Language(src)), lines(n, src))}
	case *ast.CodeBlock:
		return []adfNode{codeBlock("", lines(n, src))}
	case *ast.ThematicBreak:
		return []adfNode{{"type": "rule"}}
	case *ast.HTMLBlock:
		t := strings.TrimSpace(lines(n, src))
		if t == "" {
			return nil
		}
		return []adfNode{{"type": "paragraph", "content": []adfNode{textNode(t, nil)}}}
	}
	return blocks(n, src)
}

func codeBlock(lang, body string) adfNode {
	body = strings.TrimRight(body, "\n")
	cb := adfNode{"type": "codeBlock", "content": []adfNode{}}
	if body != "" {
		cb["content"] = []adfNode{{"type": "text", "text": body}}
	}
	if lang != "" {
		cb["attrs"] = adfNode{"language": lang}
	}
	return cb
}

func lines(n ast.Node, src []byte) string {
	var b strings.Builder
	ls := n.Lines()
	for i := 0; i < ls.Len(); i++ {
		seg := ls.At(i)
		b.Write(seg.Value(src))
	}
	return b.String()
}

func inlines(n ast.Node, src []byte, marks []adfNode) []adfNode {
	out := []adfNode{}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, inline(c, src, marks)...)
	}
	return out
}

func with(marks []adfNode, m adfNode) []adfNode {
	return append(append([]adfNode(nil), marks...), m)
}

func inline(n ast.Node, src []byte, marks []adfNode) []adfNode {
	switch n := n.(type) {
	case *ast.Text:
		var out []adfNode
		if t := string(n.Segment.Value(src)); t != "" {
			out = append(out, textNode(t, marks))
		}
		switch {
		case n.HardLineBreak():
			out = append(out, adfNode{"type": "hardBreak"})
		case n.SoftLineBreak():
			out = append(out, textNode(" ", marks))
		}
		return out
	case *ast.String:
		if t := string(n.Value); t != "" {
			return []adfNode{textNode(t, marks)}
		}
		return nil
	case *ast.CodeSpan:
		var b strings.Builder
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if t, ok := c.(*ast.Text); ok {
				b.Write(t.Segment.Value(src))
			}
		}
		if b.Len() == 0 {
			return nil
		}
		// ADF allows no other mark with code but a link.
		var keep []adfNode
		for _, m := range marks {
			if m["type"] == "link" {
				keep = append(keep, m)
			}
		}
		return []adfNode{textNode(b.String(), with(keep, adfNode{"type": "code"}))}
	case *ast.Emphasis:
		m := adfNode{"type": "em"}
		if n.Level >= 2 {
			m = adfNode{"type": "strong"}
		}
		return inlines(n, src, with(marks, m))
	case *ast.Link:
		if href := safeHref(string(n.Destination)); href != "" {
			return inlines(n, src, with(marks, adfNode{"type": "link", "attrs": adfNode{"href": href}}))
		}
		return inlines(n, src, marks)
	case *ast.AutoLink:
		u := string(n.URL(src))
		if href := safeHref(u); href != "" {
			return []adfNode{textNode(u, with(marks, adfNode{"type": "link", "attrs": adfNode{"href": href}}))}
		}
		return []adfNode{textNode(u, marks)}
	case *ast.Image:
		return inlines(n, src, marks)
	case *ast.RawHTML:
		var b strings.Builder
		for i := 0; i < n.Segments.Len(); i++ {
			s := n.Segments.At(i)
			b.Write(s.Value(src))
		}
		if b.Len() == 0 {
			return nil
		}
		return []adfNode{textNode(b.String(), marks)}
	}
	return inlines(n, src, marks)
}

func textNode(t string, marks []adfNode) adfNode {
	n := adfNode{"type": "text", "text": t}
	if len(marks) > 0 {
		n["marks"] = marks
	}
	return n
}

// safeHref keeps web and mail links; kalka: and plugin references stay
// text, since they mean nothing in Jira.
func safeHref(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	switch u.Scheme {
	case "http", "https":
		if u.Host != "" {
			return s
		}
	case "mailto":
		return s
	}
	return ""
}
