package reference

import (
	"regexp"
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
	// The six recommended sections, in order.
	var heads []string
	for _, h := range d.Headings() {
		if h.Level == 3 {
			heads = append(heads, h.Text)
		}
	}
	want := "Domain reference,Scope,Verification plan,Critical sections,Operations,Deviation log"
	if strings.Join(heads, ",") != want {
		t.Fatalf("sections %v", heads)
	}
	if n := strings.Count(string(Bytes()), "*Why agents benefit:*"); n != 6 {
		t.Fatalf("%d rationales", n)
	}
}

// The reference must not use financial or trading examples (product rule).
func TestNoFinancialExamples(t *testing.T) {
	bad := regexp.MustCompile(`(?i)\b(payments?|invoices?|trad(e|es|ing)|stocks?|shares|banks?|bank account|prices?|money|loans?|credit card|portfolio|ledger|billing|fraud|purchase orders?|checkout|currency)\b`)
	if m := bad.FindAllString(string(Bytes()), -1); m != nil {
		t.Fatalf("financial terms: %v", m)
	}
}
