package blueprint

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Typed blocks other than trace (spec 6.14): glossary and invariants. They
// share the trace language's restricted YAML and its fence rules; each
// language names its own rule ids.

// blockCodes are a language's rule ids for what all typed blocks share.
type blockCodes struct {
	parse, feature, dupKey, shape, version, noVersion, unknownKey string
}

// blockParser collects one block's diagnostics. Lines are file lines.
type blockParser struct {
	codes  blockCodes
	base   int // file line of the opening fence
	errors []TraceError
	warns  []TraceError
}

func (p *blockParser) fail(code, node string, yamlLine int, format string, a ...any) {
	p.errors = append(p.errors, TraceError{Code: code, Node: node, Line: p.base + yamlLine, Message: fmt.Sprintf(format, a...)})
}

func (p *blockParser) warn(code, node string, yamlLine int, format string, a ...any) {
	p.warns = append(p.warns, TraceError{Code: code, Node: node, Line: p.base + yamlLine, Message: fmt.Sprintf(format, a...)})
}

// version reads the fence: the language's version 1, or an error. name is
// the info word, such as glossary.
func (p *blockParser) version(info, name string) bool {
	words := strings.Fields(info)
	switch {
	case len(words) < 2:
		p.warn(p.codes.noVersion, "", 0, "the fence names no version and is read as v1; write ```%s v1", name)
	case words[1] == "v1":
	case traceVersionRe.MatchString(words[1]):
		p.fail(p.codes.version, "", 0, "%s %s: needs a newer kalka", name, words[1])
		return false
	default:
		p.fail(p.codes.version, "", 0, "%q after %s is not a version; write ```%s v1", words[1], name, name)
		return false
	}
	return true
}

// read parses the content in the restricted form: one document, no
// anchors, aliases, tags or merge keys, no key twice in a mapping. It
// returns the root, or nil after an error.
func (p *blockParser) read(src string) *yaml.Node {
	dec := yaml.NewDecoder(strings.NewReader(src))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		p.yamlError(err)
		return nil
	}
	var extra yaml.Node
	switch err := dec.Decode(&extra); {
	case errors.Is(err, io.EOF):
	case err != nil:
		p.yamlError(err)
		return nil
	default:
		p.fail(p.codes.feature, "", extra.Line, "the block holds more than one YAML document; remove the --- line")
		return nil
	}
	walkYAML(&doc, func(n *yaml.Node, key bool) {
		switch {
		case n.Kind == yaml.AliasNode:
			p.fail(p.codes.feature, "", n.Line, "aliases (*%s) are not allowed; write the value out", n.Value)
		case n.Anchor != "":
			p.fail(p.codes.feature, "", n.Line, "anchors (&%s) are not allowed; write the value out", n.Anchor)
		case key && n.Tag == "!!merge":
			p.fail(p.codes.feature, "", n.Line, "merge keys (<<) are not allowed; write the keys out")
		case n.Style&yaml.TaggedStyle != 0:
			p.fail(p.codes.feature, "", n.Line, "explicit tags (%s) are not allowed; remove the tag", n.Tag)
		}
	})
	walkYAML(&doc, func(n *yaml.Node, _ bool) {
		if n.Kind != yaml.MappingNode {
			return
		}
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if seen[k.Value] {
				p.fail(p.codes.dupKey, "", k.Line, "key %s appears twice in one mapping; keep one", k.Value)
			}
			seen[k.Value] = true
		}
	})
	if len(p.errors) > 0 || len(doc.Content) == 0 {
		return nil
	}
	return doc.Content[0]
}

func (p *blockParser) yamlError(err error) {
	msg := err.Error()
	line := 0
	if m := yamlLineRe.FindStringSubmatch(msg); m != nil {
		line, _ = strconv.Atoi(m[1])
		msg = msg[len(m[0]):]
	}
	msg = strings.TrimPrefix(msg, "yaml: ")
	p.fail(p.codes.parse, "", line, "the YAML does not parse: %s; put text values in double quotes", msg)
}

// unknownKeys warns about keys that are not known and do not start with
// x-, suggesting the nearest known key.
func (p *blockParser) unknownKeys(m *yaml.Node, known []string, node string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i].Value
		if slices.Contains(known, k) || strings.HasPrefix(k, "x-") {
			continue
		}
		msg := fmt.Sprintf("unknown key %s is ignored", k)
		if s := nearest(k, known); s != "" {
			msg += fmt.Sprintf("; did you mean %s?", s)
		} else {
			msg += "; a key of your own starts with x-"
		}
		p.warn(p.codes.unknownKey, node, m.Content[i].Line, "%s", msg)
	}
}

// text reads a single text value of key in m; ok is false (and an error
// recorded) when it is not one.
func (p *blockParser) text(m *yaml.Node, key, node string) (string, bool) {
	v := mapValue(m, key)
	s, ok := textOf(v)
	if !ok {
		p.fail(p.codes.shape, node, v.Line, "%s is one text; put it in double quotes", key)
	}
	return s, ok
}

// texts reads a text or a list of texts.
func (p *blockParser) texts(m *yaml.Node, key, node string) ([]string, bool) {
	v := mapValue(m, key)
	if absent(v) {
		return nil, true
	}
	if v.Kind == yaml.ScalarNode {
		s, _ := textOf(v)
		return []string{s}, true
	}
	if v.Kind == yaml.SequenceNode {
		var out []string
		for _, it := range v.Content {
			s, ok := textOf(it)
			if !ok || absent(it) {
				p.fail(p.codes.shape, node, it.Line, "%s is a list of texts, such as %s: [\"one\", \"two\"]", key, key)
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	p.fail(p.codes.shape, node, v.Line, "%s is a list of texts, such as %s: [\"one\", \"two\"]", key, key)
	return nil, false
}

// blockProblems reports a typed block's diagnostics on the document.
func (d *Document) blockProblems(what string, errs, warns []TraceError) {
	for _, e := range append(append([]TraceError(nil), errs...), warns...) {
		msg := what + ": " + e.Message
		if e.Node != "" {
			msg = what + ", " + e.Node + ": " + e.Message
		}
		p := NewProblem(e.Code, e.Line, msg)
		p.Node = e.Node
		d.problems = append(d.problems, p)
	}
}
