package blueprint

import (
	"reflect"
	"testing"
)

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
