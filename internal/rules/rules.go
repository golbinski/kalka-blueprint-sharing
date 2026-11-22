// Package rules is the registry of every diagnostic kalka reports (spec
// 6.17): one declaration per rule, used by the checker, the app,
// `kalka check --explain` and the generated rule lists.
package rules

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Severity of a rule's diagnostics.
type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
	Info    Severity = "info"
)

// Newest is the newest rule set this kalka knows. Set 2 goes with the
// kalka language v2; set 1 went with the retired v1 blocks and is not
// run any more (kalka is 0.x: no compatibility between versions).
const (
	Newest = 2
	Oldest = 2
)

// Rule is one registered rule.
type Rule struct {
	ID       string   `json:"id"`
	Severity Severity `json:"severity"`
	// Title names the rule in a few words; Meaning says when it fires and
	// Why why it exists. Wrong and Right are short examples.
	Title   string `json:"title"`
	Meaning string `json:"meaning"`
	Why     string `json:"why"`
	Wrong   string `json:"wrong,omitempty"`
	Right   string `json:"right,omitempty"`
	// Set is the first rule set the rule belongs to.
	Set int `json:"set"`
	// Blocks marks the structural errors that make a file one that cannot
	// be signed or merged safely: publishing refuses such a file.
	Blocks bool `json:"blocks,omitempty"`
}

// Groups are the id letters, in the order the lists show them.
var Groups = []struct{ Letter, Name string }{
	{"F", "Frontmatter"},
	{"B", "Structure and the review log"},
	{"S", "The kalka language v2"},
	{"T", "Trace blocks"},
	{"G", "Glossary"},
	{"I", "Invariants"},
	{"P", "Plan graph and goals"},
	{"D", "Domain tree"},
	{"R", "References to other documents"},
	{"H", "Hints"},
}

var idRe = regexp.MustCompile(`^[FBSTGIPDRH][0-9]{3}$`)

var byID = map[string]*Rule{}

func init() {
	for i := range all {
		r := &all[i]
		if !idRe.MatchString(r.ID) {
			panic("rules: bad id " + r.ID)
		}
		if byID[r.ID] != nil {
			panic("rules: " + r.ID + " registered twice")
		}
		if r.Set == 0 {
			r.Set = Oldest
		}
		byID[r.ID] = r
	}
	sort.SliceStable(all, func(i, j int) bool { return less(all[i].ID, all[j].ID) })
}

// less orders ids by group, then number.
func less(a, b string) bool {
	ga, gb := groupIndex(a[:1]), groupIndex(b[:1])
	if ga != gb {
		return ga < gb
	}
	return a < b
}

func groupIndex(l string) int {
	for i, g := range Groups {
		if g.Letter == l {
			return i
		}
	}
	return len(Groups)
}

// Lookup returns the rule with this id.
func Lookup(id string) (Rule, bool) {
	r := byID[strings.ToUpper(id)]
	if r == nil {
		return Rule{}, false
	}
	return *r, true
}

// Must returns the rule with this id and panics for an unregistered one:
// emitting an id the registry does not know is a programming error.
func Must(id string) Rule {
	r, ok := byID[id]
	if !ok {
		panic("rules: unregistered rule " + id)
	}
	return *r
}

// All returns every rule, by group and number.
func All() []Rule { return append([]Rule(nil), all...) }

// ParseSet reads `v<N>`: the set, or an error saying why it cannot run.
func ParseSet(s string) (int, error) {
	if s == "" {
		return Newest, nil
	}
	n, err := strconv.Atoi(strings.TrimPrefix(s, "v"))
	if !strings.HasPrefix(s, "v") || err != nil || n < 1 {
		return 0, fmt.Errorf("%q is not a rule set; write v%d", s, Newest)
	}
	if n < Oldest {
		return 0, fmt.Errorf("rules v%d went with the kalka language v1, which this kalka no longer reads; run v%d", n, Newest)
	}
	if n > Newest {
		return 0, fmt.Errorf("rules v%d needs a newer kalka (this one knows v%d to v%d)", n, Oldest, Newest)
	}
	return n, nil
}

// InSet reports whether the rule runs in the set.
func InSet(id string, set int) bool {
	r := byID[id]
	return r != nil && r.Set <= set
}

// Explain is what `kalka check --explain` prints.
func Explain(r Rule) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s): %s\n\n%s\n\nWhy: %s\n", r.ID, r.Severity, r.Title, r.Meaning, r.Why)
	if r.Wrong != "" {
		fmt.Fprintf(&b, "\nWrong:\n%s\n", indent(r.Wrong))
	}
	if r.Right != "" {
		fmt.Fprintf(&b, "\nRight:\n%s\n", indent(r.Right))
	}
	return b.String()
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return strings.Join(lines, "\n")
}

// Markdown is the rule list for docs/rules.md and the blueprint reference:
// one table per group.
func Markdown(heading string) string {
	var b strings.Builder
	for _, g := range Groups {
		var rs []Rule
		for _, r := range all {
			if r.ID[:1] == g.Letter {
				rs = append(rs, r)
			}
		}
		if len(rs) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s %s (%s)\n\n| Rule | Severity | What it means |\n|---|---|---|\n", heading, g.Name, g.Letter)
		for _, r := range rs {
			fmt.Fprintf(&b, "| `%s` | %s | **%s.** %s |\n", r.ID, r.Severity, r.Title, strings.ReplaceAll(r.Meaning, "|", `\|`))
		}
		b.WriteString("\n")
	}
	return b.String()
}
