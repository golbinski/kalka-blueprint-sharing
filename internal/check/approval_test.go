package check

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// kalka graph and kalka context, reading files with no team log, name a
// team document's approval state as the file says (spec 6.18), the same
// on every run.
func TestApprovalInGraphAndContext(t *testing.T) {
	dir := t.TempDir()
	planDoc(t, dir, "epic.md", epicID, "", "kalka_owner: ewa@example.org\nkalka_team: parcels\n", "Holds", goals(epicGoals))
	planDoc(t, dir, "leaf.md", leafID, epicID, "kalka_owner: ewa@example.org\nkalka_team: parcels\n", "Hold record", goals(criterion))
	// Release and approve the epic, as its owner.
	seed := sha256.Sum256([]byte("ewa"))
	key := ed25519.NewKeyFromSeed(seed[:])
	sign := bp.Signer{Fn: func(m []byte) []byte { return ed25519.Sign(key, m) }}
	path := filepath.Join(dir, "epic.md")
	raw, _ := os.ReadFile(path)
	d, err := bp.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	r, err := bp.NewRelease(d, "ewa@example.org", at, 1, false, nil, "", sign)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AddEntry(r); err != nil {
		t.Fatal(err)
	}
	a, err := bp.NewApproval(d, "ewa@example.org", at.Add(time.Minute), 1, d.PlanHash(), "", sign)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AddEntry(a); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, d.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	f := NewFolder(dir)
	lraw, _ := os.ReadFile(filepath.Join(dir, "leaf.md"))
	leaf, _ := bp.Parse(lraw)
	o := Options{Domains: f.Tree(), Docs: f}
	g := BuildGraph("leaf.md", leaf, GraphOptions{Options: o})
	got := map[string]string{}
	for _, n := range g.Nodes {
		if n.Card == "" && n.Approval != nil {
			got[n.Title] = n.Approval.Text()
		}
	}
	if got["Holds"] != "approved v1" || got["Hold record"] != "draft" {
		t.Fatalf("cards %v", got)
	}
	first, _ := json.Marshal(g)
	again, _ := json.Marshal(BuildGraph("leaf.md", leaf, GraphOptions{Options: o}))
	if string(first) != string(again) {
		t.Fatal("kalka graph is not deterministic")
	}
	c := Context("leaf.md", leaf, o, false)
	if c.Goals[0].Doc.State != "approved v1" || c.Goals[0].Doc.Released != 1 || c.Focus.State != "draft" || c.Focus.Released != 0 {
		t.Fatalf("context %+v %+v", c.Goals[0].Doc, c.Focus)
	}
	if md := c.Markdown(); !strings.Contains(md, "### Holds (parent)\n\nApproved v1, released v1 at 2026-11-25T09:00:00Z.") || !strings.Contains(md, "## Focus: Hold record\n\nDraft, unreleased.") {
		t.Fatalf("context markdown:\n%s", md)
	}
	// Approval changes no diagnostic: the unclaimed G2 is P030 still.
	if got, _ := (planCase{dir, f}).check(t, "epic.md", "P"); got != "P030" {
		t.Fatalf("epic diagnostics %q", got)
	}
}
