package core

import (
	"context"
	"strings"
	"testing"
)

// The document view carries reference hints apart from problems (spec
// 6.13), and hints do not stop a publish.
func TestDocumentHints(t *testing.T) {
	_, ns := teamOfN(t, "alice@library.example", "bob@library.example")
	alice, bob := ns[0], ns[1]
	alice.write("plan.md", "---\nkalka_reference: kalka-blueprint@v1\n---\n# Locker pickup\n\n## Scope\nThe pickup module.\n")
	alice.write("plain.md", "# Locker pickup\n\n## Plan\nNo opt-in.\n")

	ctx := context.Background()
	v, err := alice.d.Document(ctx, "plan.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Hints) != 4 {
		t.Fatalf("hints %+v", v.Hints)
	}
	for _, p := range v.Problems {
		if strings.HasPrefix(p.Code, "hint-") {
			t.Fatalf("hint among problems: %+v", p)
		}
	}
	if v, _ := alice.d.Document(ctx, "plain.md"); v == nil || v.Hints == nil || len(v.Hints) != 0 {
		t.Fatalf("no opt-in: %+v", v)
	}
	waitIndexed(t, alice, 2)
	if _, doc := bob.teamFile(alice.share("plan.md", bob)); doc == nil {
		t.Fatal("the document with hints did not replicate")
	}
}
