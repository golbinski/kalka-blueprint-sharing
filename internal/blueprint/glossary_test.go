package blueprint

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type glossaryCorpus struct {
	Terms []Term `json:"terms"`
	Cases []struct {
		Text    string  `json:"text"`
		Matches [][]any `json:"matches"`
	} `json:"cases"`
}

// The web tests run the same corpus through the page's matcher.
func TestGlossaryCorpus(t *testing.T) {
	data, err := os.ReadFile("../../testdata/glossary/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var c glossaryCorpus
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	m := NewMatcher(c.Terms)
	for _, k := range c.Cases {
		var got [][]any
		for _, x := range m.Match(k.Text) {
			got = append(got, []any{k.Text[x.Start:x.End], float64(x.Term), x.Alias})
		}
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(k.Matches)
		if len(got) == 0 && len(k.Matches) == 0 {
			continue
		}
		if string(g) != string(w) {
			t.Errorf("%q: %s, want %s", k.Text, g, w)
		}
	}
}

const loansDomain = "---\nkalka_kind: domain\nkalka_domain: loans\nkalka_team: lending\nkalka_version: 2\n---\n# Loans\n\n"

func rulesOf(d *Document, prefix string) string {
	var ids []string
	for _, p := range d.Problems() {
		if strings.HasPrefix(p.Rule, prefix) {
			ids = append(ids, p.Rule)
		}
	}
	return strings.Join(ids, " ")
}

func TestGlossaryBlocks(t *testing.T) {
	g := func(body string) string { return "```glossary v2\n" + body + "```\n" }
	for _, c := range []struct {
		name, body string
		terms      int
		rules      string
	}{
		{"two blocks are one glossary", g("term loan: One item lent\n") + "\n## Members\n\n" + g("term member: Someone with a card\n  see loan\n"), 2, ""},
		{"a term with spaces", g("term queue head: The member first in the queue, as it stands: now\n  form queue heads\n"), 1, ""},
		{"term twice across blocks", g("term loan: a\n") + "\n" + g("term Loan: b\n"), 1, "G012"},
		{"alias names another entry", g("term loan: a\nterm lending: b\n  alias loan\n"), 1, "G012"},
		{"missing definition", g("term loan:\n"), 0, ""},
		{"unknown keyword and see", g("term loan: a\n  defintion b\n  see fine\n"), 1, "G016"},
		{"too long", g("term " + strings.Repeat("a", 61) + ": x\n"), 0, "G013"},
		{"context once", g("term loan: a\n  context lending\n"), 1, ""},
	} {
		d, err := Parse([]byte(loansDomain + c.body))
		if err != nil {
			t.Fatal(err)
		}
		if got := rulesOf(d, "G"); got != c.rules || len(d.Glossary()) != c.terms {
			t.Errorf("%s: rules %q terms %d, want %q %d", c.name, got, len(d.Glossary()), c.rules, c.terms)
		}
	}
	// The shared lexical rules report the rest.
	for _, c := range []struct{ name, body, rules string }{
		{"missing definition", g("term loan:\n"), "S005"},
		{"no version", "```glossary\nterm loan: a\n```\n", "S013"},
		{"newer version", "```glossary v3\nterm loan: a\n```\n", "S012"},
		{"retired version", "```glossary v1\n- term: \"loan\"\n```\n", "S012"},
		{"yaml", g("- term: \"loan\"\n  definition: \"a\"\n"), "S010"},
		{"unknown keyword", g("term loan: a\n  defintion b\n"), "S020"},
		{"context twice", g("term loan: a\n  context x\n  context y\n"), "S021"},
		{"empty", g(""), "S031"},
	} {
		d, _ := Parse([]byte(loansDomain + c.body))
		if got := rulesOf(d, "S"); got != c.rules {
			t.Errorf("%s: rules %q, want %q", c.name, got, c.rules)
		}
	}
	// The parts of an entry.
	d, _ := Parse([]byte(loansDomain + g("term member: A person registered with the library\n  alias patron\n  alias reader\n  form members\n  see loan\n  context desk\nterm loan: a\n")))
	if m := d.Glossary()[0]; m.Term != "member" || strings.Join(m.Aliases, ",") != "patron,reader" || strings.Join(m.Forms, ",") != "members" || m.Context != "desk" || m.See[0] != "loan" || m.Line != 10 {
		t.Errorf("entry %+v", m)
	}
	// A blueprint that names no domain keeps its own glossary; one that
	// names a domain is linked against the domain's, and its own is code.
	block := "# Plan\n\n" + g("term loan: a\n")
	d, _ = Parse([]byte(block))
	if got := rulesOf(d, "G"); got != "" || len(d.Glossary()) != 1 {
		t.Errorf("glossary in a blueprint with no domain: %q, %d terms", got, len(d.Glossary()))
	}
	d, _ = Parse([]byte("---\nkalka_domain: lending/loans@v2\n---\n" + block))
	if got := rulesOf(d, "G"); got != "G008" || d.Glossary() != nil {
		t.Errorf("glossary in a blueprint that names a domain: %q", got)
	}
}

// Terms are matched in prose, headings and link text, never in code, link
// targets, the frontmatter or the review log; positions are file lines
// and character columns.
func TestTermUses(t *testing.T) {
	src := loansDomain + "```glossary v2\nterm loan: a\n  form loans\n  alias borrowing\n```\n\n" +
		"## Loans and borrowing\n\nA *loan* ends; see [a borrowing](gh:x#borrowing) and `loan`.\n\n" +
		"```text\nloan\n```\n\nÜber borrowing.\n\n## Review log\n\n### 2026-11-01T10:00:00Z · c-x · comment · a@b.example\n\nloan\n"
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, u := range d.TermUses(NewMatcher(d.Glossary())) {
		got = append(got, u.Surface+"@"+itoa(u.Line)+":"+itoa(u.Column)+"-"+itoa(u.EndColumn))
	}
	want := "Loans@7:3-8 Loans@15:4-9 borrowing@15:14-23 loan@17:4-8 borrowing@17:23-32 borrowing@23:6-15"
	if strings.Join(got, " ") != want {
		t.Errorf("uses %v\nwant %s", got, want)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
