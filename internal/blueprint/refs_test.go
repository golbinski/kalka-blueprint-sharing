package blueprint

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// The shared corpus: web/src/markdown.ts classifies the same targets the
// same way (its test reads this file too).
func TestPluginRefCorpus(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/refs/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Cases []struct {
			Target string  `json:"target"`
			Scheme *string `json:"scheme"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &c); err != nil || len(c.Cases) < 20 {
		t.Fatal(err, len(c.Cases))
	}
	for _, x := range c.Cases {
		s, _, ok := PluginRef(x.Target)
		if want := x.Scheme; ok != (want != nil) || (ok && s != *want) {
			t.Errorf("%q: %q %v", x.Target, s, ok)
		}
	}
}

func TestPluginRef(t *testing.T) {
	for in, want := range map[string]string{
		"gh:src/a.py#L1-L9": "gh", "jira:ABC-1": "jira", "c++:x": "c++",
		"https://x": "", "http://x": "", "mailto:a@b": "", "file:/etc/passwd": "", "javascript:alert(1)": "",
		"Gh:x": "", "gh x:y": "", "src/a.py": "", "#plan": "", "1x:y": "",
	} {
		s, _, ok := PluginRef(in)
		if s != want || ok != (want != "") {
			t.Errorf("%q: %q %v", in, s, ok)
		}
	} // A named repository (spec 10.4) stays in the payload.
	if s, p, ok := PluginRef("gh:api:src/a.go#L1"); !ok || s != "gh" || p != "api:src/a.go#L1" {
		t.Errorf("named: %q %q %v", s, p, ok)
	}
}

func TestReferences(t *testing.T) {
	src := "# T\n\n## Plan\n\nSee [parse](gh:src/parse.py#L1-L9), <jira:ABC-12>, [web](https://x) and [again](gh:src/parse.py#L1-L9).\n\n" +
		"[def]: gh:src/def.py\n\nUse [it][def].\n\n" + specTrace + "\n## Review log\n\n"
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewEntry(KindComment, "bob@corp.com", t0, "plan", d.Rev(), "Compare with [old](gh:src/old.py).")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	d, _ = d.Reparse()
	want := []string{"gh:src/parse.py#L1-L9", "jira:ABC-12", "gh:src/def.py",
		"gh:src/upload/handlers.py#L40-L120", "gh:src/import/size_guard.py#L12-L48", "gh:src/import/parse.py#L200-L310", "gh:src/notify/batch.py#L15-L90",
		"gh:src/old.py"}
	if got := d.References(); !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}
