package blueprint

import (
	"regexp"
	"strings"
)

// HintsVersion is the blueprint reference version whose hints this library
// gives (spec 6.13). internal/reference embeds the matching text; a test
// keeps the two equal.
const HintsVersion = "kalka-blueprint@v1"

// ReferenceKey is the frontmatter key a document opts into hints with.
const ReferenceKey = "reference"

// hintSections are the recommended body sections of v1 with the heading
// slugs that count as present, in the reference's order. The deviation log
// lives in the review log and gets no hint.
var hintSections = []struct {
	code, slug, message string
}{
	{"hint-no-domain-reference", "domain-reference", "no domain reference: name the domain version the plan relies on (`domain:` in the frontmatter, or a “Domain reference” section)"},
	{"hint-no-scope", "scope", "no scope: list the modules the change will touch, so work beyond them becomes a seed"},
	{"hint-no-verification-plan", "verification-plan", "no verification plan: invariants, edge cases and what a reviewer must check"},
	{"hint-no-critical-sections", "critical-sections", "no critical sections: places that deliberately depart from the clean design, so agents leave them alone"},
	{"hint-no-operations", "operations", "no operations: failure modes, what to watch, how to roll back"},
}

// domainRefRe is the shape of `domain:` on a blueprint (spec 6.2).
var domainRefRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9-]*@v[1-9][0-9]*$`)

// trailingDup strips the -1, -2 suffix the slug rule adds to repeated
// headings, so a second "Scope" heading still counts.
var trailingDup = regexp.MustCompile(`-[0-9]+$`)

// Hints returns the soft hints for a document that opted into the
// blueprint reference (spec 6.13), as information-level problems with codes
// starting "hint-". A document without `reference:` gets none. Hints are
// never part of Problems(): they do not affect validity, `kalka check`'s
// exit code, or a sync.
func Hints(d *Document) []Problem {
	if d == nil {
		return nil
	}
	ref, ok := d.Frontmatter().Get(ReferenceKey)
	if !ok {
		return nil
	}
	if ref != HintsVersion {
		if !strings.HasPrefix(ref, "kalka-blueprint@v") {
			return nil
		}
		return []Problem{{Severity: Info, Code: "hint-unknown-reference",
			Message: "this document follows " + ref + "; this kalka knows " + HintsVersion + " and gives no hints for it"}}
	}
	have := map[string]bool{}
	for _, h := range d.Headings() {
		have[h.Slug] = true
		have[trailingDup.ReplaceAllString(h.Slug, "")] = true
	}
	if v, ok := d.Frontmatter().Get("domain"); ok && domainRefRe.MatchString(v) {
		have["domain-reference"] = true
	}
	var out []Problem
	for _, s := range hintSections {
		if !have[s.slug] {
			out = append(out, Problem{Severity: Info, Code: s.code, Message: s.message})
		}
	}
	return out
}
