package reference

import (
	"strings"
	"testing"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

func TestVersionAndShape(t *testing.T) {
	if Version != bp.HintsVersion || declared() != Version {
		t.Fatalf("versions: package %s, hints %s, file %s", Version, bp.HintsVersion, declared())
	}
	// Usable as an agent skill file: frontmatter with name and description.
	d, err := bp.Parse(Bytes())
	if err != nil || d.HasErrors() {
		t.Fatalf("parse: %v %v", err, d.Problems())
	}
	for _, k := range []string{"name", "description"} {
		if v, ok := d.Frontmatter().Get(k); !ok || v == "" {
			t.Fatalf("frontmatter %s missing", k)
		}
	}
	// The six recommended sections, in order; trace blocks are explained
	// in a part of their own.
	var heads []string
	in := false
	for _, h := range d.Headings() {
		if h.Level == 2 {
			in = h.Text == "The sections"
		}
		if in && h.Level == 3 {
			heads = append(heads, h.Text)
		}
	}
	want := "Domain reference,Scope,Verification plan,Critical sections,Operations,Deviation log"
	if strings.Join(heads, ",") != want {
		t.Fatalf("sections %v", heads)
	}
	if n := strings.Count(string(Bytes()), "*Why agents benefit:*"); n != 7 {
		t.Fatalf("%d rationales", n)
	}
}
