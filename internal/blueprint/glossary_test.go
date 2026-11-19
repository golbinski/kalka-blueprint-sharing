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
	for _, c := range []struct {
		name, body string
		terms      int
		rules      string
	}{
		{"two blocks are one glossary", "```glossary v1\n- term: \"loan\"\n  definition: \"One item lent\"\n```\n\n## Members\n\n```glossary v1\n- term: \"member\"\n  definition: \"Someone with a card\"\n  see: [\"loan\"]\n```\n", 2, ""},
		{"term twice across blocks", "```glossary v1\n- term: \"loan\"\n  definition: \"a\"\n```\n\n```glossary v1\n- term: \"Loan\"\n  definition: \"b\"\n```\n", 1, "G012"},
		{"alias names another entry", "```glossary v1\n- term: \"loan\"\n  definition: \"a\"\n- term: \"lending\"\n  definition: \"b\"\n  aliases: [\"loan\"]\n```\n", 1, "G012"},
		{"missing definition", "```glossary v1\n- term: \"loan\"\n```\n", 0, "G011"},
		{"missing term", "```glossary v1\n- definition: \"x\"\n```\n", 0, "G010"},
		{"not a list", "```glossary v1\nterm: \"loan\"\n```\n", 0, "G004"},
		{"unknown key and see", "```glossary v1\n- term: \"loan\"\n  definition: \"a\"\n  defintion: \"b\"\n  see: [\"fine\"]\n```\n", 1, "G007 G016"},
		{"no version", "```glossary\n- term: \"loan\"\n  definition: \"a\"\n```\n", 1, "G006"},
		{"newer version", "```glossary v2\n- term: \"loan\"\n  definition: \"a\"\n```\n", 0, "G005"},
		{"bad yaml", "```glossary v1\n- term: \"loan\n```\n", 0, "G001"},
		{"anchor", "```glossary v1\n- &a term: \"loan\"\n  definition: \"a\"\n```\n", 0, "G002"},
	} {
		d, err := Parse([]byte(loansDomain + c.body))
		if err != nil {
			t.Fatal(err)
		}
		if got := rulesOf(d, "G"); got != c.rules || len(d.Glossary()) != c.terms {
			t.Errorf("%s: rules %q terms %d, want %q %d", c.name, got, len(d.Glossary()), c.rules, c.terms)
		}
	}
	// A blueprint that names no domain keeps its own glossary; one that
	// names a domain is linked against the domain's, and its own is code.
	block := "# Plan\n\n```glossary v1\n- term: \"loan\"\n  definition: \"a\"\n```\n"
	d, _ := Parse([]byte(block))
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
	src := loansDomain + "```glossary v1\n- term: \"loan\"\n  definition: \"a\"\n  forms: [\"loans\"]\n  aliases: [\"borrowing\"]\n```\n\n" +
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
	want := "Loans@7:3-8 Loans@16:4-9 borrowing@16:14-23 loan@18:4-8 borrowing@18:23-32 borrowing@24:6-15"
	if strings.Join(got, " ") != want {
		t.Errorf("uses %v\nwant %s", got, want)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
