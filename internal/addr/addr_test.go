package addr

import "testing"

func TestCanonical(t *testing.T) {
	for in, want := range map[string]string{
		"bob@example.org":   "bob@example.org",
		"Bob@Example.ORG":   "bob@example.org",
		"Émile@Example.org": "Émile@example.org", // only ASCII is lowered
		"ſam@example.org":   "ſam@example.org",
	} {
		if got := Canonical(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if !Same("Bob@example.org", "bob@EXAMPLE.org") || Same("émile@x", "Émile@x") || IsCanonical("Bob@x") || !IsCanonical("bob@x") {
		t.Fatal("Same or IsCanonical")
	}
}
