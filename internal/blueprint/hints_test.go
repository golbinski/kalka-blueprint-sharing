package blueprint

import (
	"strings"
	"testing"
)

func hintCodes(ps []Problem) string {
	var c []string
	for _, p := range ps {
		if p.Severity != Info {
			c = append(c, "NOT-INFO:"+p.Code)
		}
		c = append(c, p.Code)
	}
	return strings.Join(c, ",")
}

const allSections = "# Redelivery\n\n## Domain reference\nparcels/delivery@v7\n\n## Scope\nx\n\n## Verification plan\nx\n\n## Critical sections\nx\n\n## Operations\nx\n"

func TestHintsOptIn(t *testing.T) {
	// No reference: no hints, however bare the document.
	if h := Hints(mustParse(t, "# Plan\n\nDo it.\n")); len(h) != 0 {
		t.Fatalf("hints without opt-in: %v", h)
	}
	// Another tool's `reference:` value is not ours.
	if h := Hints(mustParse(t, "---\nreference: ISO-9001\n---\n# Plan\n")); len(h) != 0 {
		t.Fatalf("foreign reference: %v", h)
	}
	// Opted in, nothing present: every section, in order.
	got := hintCodes(Hints(mustParse(t, "---\nreference: kalka-blueprint@v1\n---\n# Plan\n")))
	want := "hint-no-domain-reference,hint-no-scope,hint-no-verification-plan,hint-no-critical-sections,hint-no-operations"
	if got != want {
		t.Fatalf("got %s", got)
	}
	// All present, at any heading level: none.
	if h := Hints(mustParse(t, "---\nreference: kalka-blueprint@v1\n---\n"+strings.ReplaceAll(allSections, "## Scope", "#### Scope"))); len(h) != 0 {
		t.Fatalf("all present: %v", h)
	}
	// An unknown version: one information line.
	if got := hintCodes(Hints(mustParse(t, "---\nreference: kalka-blueprint@v9\n---\n# Plan\n"))); got != "hint-unknown-reference" {
		t.Fatalf("v9: %s", got)
	}
}

// Each hint appears alone when only its section is missing.
func TestHintsEach(t *testing.T) {
	for _, s := range hintSections {
		body := allSections
		for _, h := range []string{"Domain reference", "Scope", "Verification plan", "Critical sections", "Operations"} {
			if BaseSlug(h) == s.slug {
				body = strings.Replace(body, "## "+h, "## Something else", 1)
			}
		}
		got := hintCodes(Hints(mustParse(t, "---\nreference: kalka-blueprint@v1\n---\n"+body)))
		if got != s.code {
			t.Errorf("%s missing: got %q", s.slug, got)
		}
	}
	// The domain reference may be the frontmatter key instead of a section.
	d := mustParse(t, "---\nreference: kalka-blueprint@v1\ndomain: parcels/delivery@v7\n---\n"+strings.Replace(allSections, "## Domain reference", "## Background", 1))
	if h := Hints(d); len(h) != 0 {
		t.Fatalf("domain key: %v", h)
	}
	// A malformed key does not count.
	d = mustParse(t, "---\nreference: kalka-blueprint@v1\ndomain: delivery\n---\n"+strings.Replace(allSections, "## Domain reference", "## Background", 1))
	if got := hintCodes(Hints(d)); got != "hint-no-domain-reference" {
		t.Fatalf("malformed domain key: %s", got)
	}
}

// Hints never make a document invalid.
func TestHintsAreNotProblems(t *testing.T) {
	d := mustParse(t, "---\nreference: kalka-blueprint@v1\n---\n# Plan\n")
	if d.HasErrors() {
		t.Fatalf("errors: %v", d.Problems())
	}
	for _, p := range d.Problems() {
		if strings.HasPrefix(p.Code, "hint-") {
			t.Fatalf("hint among problems: %v", p)
		}
	}
}
