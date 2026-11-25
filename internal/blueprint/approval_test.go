package blueprint

import (
	"strings"
	"testing"
	"time"
)

// approvalDoc is a team document released and approved by its owner.
type approvalDoc struct {
	t   *testing.T
	d   *Document
	v   int64
	now time.Time
}

func newApprovalDoc(t *testing.T) *approvalDoc {
	return &approvalDoc{t: t, d: mustParse(t, relDoc), now: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)}
}

func (a *approvalDoc) tick() time.Time { a.now = a.now.Add(time.Minute); return a.now }

func (a *approvalDoc) release(by string) *Entry {
	a.t.Helper()
	a.v++
	e, err := NewRelease(a.d, by, a.tick(), a.v, false, nil, "", signerOf(testKey(by)))
	if err != nil {
		a.t.Fatal(err)
	}
	if err := a.d.AddEntry(e); err != nil {
		a.t.Fatal(err)
	}
	return e
}

func (a *approvalDoc) approve(by string, v int64, plan string) *Entry {
	a.t.Helper()
	e, err := NewApproval(a.d, by, a.tick(), v, plan, "", signerOf(testKey(by)))
	if err != nil {
		a.t.Fatal(err)
	}
	if err := a.d.AddEntry(e); err != nil {
		a.t.Fatal(err)
	}
	return e
}

func (a *approvalDoc) state() string {
	a.t.Helper()
	again := mustParse(a.t, string(a.d.Bytes()))
	return again.FileApproval().Text()
}

func planOf(e *Entry) string { r, _ := e.Release(); return r.Plan }

// The approval holds while the body is unchanged: a release that changes
// only frontmatter keeps it, one that changes the body gives "changed
// since approved vN", and approving again approves the latest release.
func TestApprovalLifecycle(t *testing.T) {
	a := newApprovalDoc(t)
	if got := a.d.FileApproval().Text(); got != ApprovalDraft {
		t.Fatalf("before any release: %q", got)
	}
	r1 := a.release("dana@example.org")
	if got := a.state(); got != "draft" {
		t.Fatalf("released, not approved: %q", got)
	}
	ap := a.approve("dana@example.org", 1, planOf(r1))
	if x, ok := ap.Approved(); !ok || x.Version != 1 || x.Plan != planOf(r1) {
		t.Fatalf("approved body %q", ap.Body)
	}
	if got := a.state(); got != "approved v1" {
		t.Fatalf("after approving: %q", got)
	}
	// A handoff: frontmatter only.
	a.d.SetFrontmatter("kalka_owner", "marek@example.org")
	a.release("dana@example.org")
	if got := a.state(); got != "approved v1" {
		t.Fatalf("after a handoff: %q", got)
	}
	// The new owner changes the body.
	d2 := mustParse(t, strings.Replace(string(a.d.Bytes()), "The redelivery module.", "The redelivery module, with lockers.", 1))
	a.d = d2
	r3 := a.release("marek@example.org")
	if got := a.state(); got != "changed since approved v1" {
		t.Fatalf("after a body change: %q", got)
	}
	// The former owner cannot approve v3: only the owner v3 names.
	bad := a.approve("dana@example.org", 3, planOf(r3))
	ap3 := a.d.FileApproval()
	if ap3.Text() != "changed since approved v1" || !strings.Contains(ap3.Ignored[bad.ID], "marek@example.org") {
		t.Fatalf("a non-owner's approval: %+v", ap3)
	}
	a.approve("marek@example.org", 3, planOf(r3))
	if got := a.state(); got != "approved v3" {
		t.Fatalf("approved again: %q", got)
	}
}

// Approvals that do not cite a counting release by version and plan hash
// are ignored, with the reason.
func TestApprovalIgnored(t *testing.T) {
	a := newApprovalDoc(t)
	r1 := a.release("dana@example.org")
	wrongPlan := a.approve("dana@example.org", 1, strings.Repeat("0", 64))
	noRelease := a.approve("dana@example.org", 4, planOf(r1))
	ap := a.d.FileApproval()
	if ap.State != ApprovalDraft || len(ap.Ignored) != 2 {
		t.Fatalf("%+v", ap)
	}
	if !strings.Contains(ap.Ignored[wrongPlan.ID], "plan") || !strings.Contains(ap.Ignored[noRelease.ID], "version 4") {
		t.Fatalf("reasons %v", ap.Ignored)
	}
	// Edited after signing: the id no longer matches.
	e := a.approve("dana@example.org", 1, planOf(r1))
	e.Body += "\nedited"
	if got := a.d.FileApproval(); got.State != ApprovalDraft || got.Ignored[e.ID] == "" {
		t.Fatalf("edited approval counts: %+v", got)
	}
}

// The approval citing the highest version applies, never the latest by
// time, so entries merged in any order give the same state.
func TestApprovalOrder(t *testing.T) {
	a := newApprovalDoc(t)
	r1 := a.release("dana@example.org")
	d := mustParse(t, strings.Replace(string(a.d.Bytes()), "The redelivery module.", "Changed.", 1))
	a.d = d
	r2 := a.release("dana@example.org")
	high := a.approve("dana@example.org", 2, planOf(r2))
	low := a.approve("dana@example.org", 1, planOf(r1)) // later in time, lower version
	rels := []*Entry{r1, r2}
	for _, es := range [][]*Entry{{high, low}, {low, high}} {
		got := ApprovalOf(rels, es)
		if got.Text() != "approved v2" || got.Entry != high.ID {
			t.Fatalf("order %v: %+v", es, got)
		}
	}
	if got := ApprovalOf([]*Entry{r2, r1}, []*Entry{low}); got.Text() != "changed since approved v1" {
		t.Fatalf("only v1 approved: %+v", got)
	}
}

// No approval state outside a team document, and kalka_status: closed is
// an ordinary label: a draft.
func TestApprovalScope(t *testing.T) {
	for _, src := range []string{
		"# Local\n",
		"---\nkalka_id: 4be1c0ffee0100000000000000000000\nkalka_owner: dana@example.org\n---\n# Personal\n",
		"---\nkalka_id: 4be1c0ffee0100000000000000000000\nkalka_owner: dana@example.org\nkalka_team: parcels\nkalka_status: seed\n---\n# Seed\n",
		"---\nkalka_id: 4be1c0ffee0100000000000000000000\nkalka_owner: dana@example.org\nkalka_team: parcels\nkalka_kind: domain\nkalka_domain: parcels\n---\n# Domain\n",
	} {
		if got := mustParse(t, src).FileApproval(); got.State != "" {
			t.Errorf("%q: state %q", src, got.State)
		}
	}
	closed := mustParse(t, strings.Replace(relDoc, "kalka_team: parcels\n", "kalka_team: parcels\nkalka_status: closed\n", 1))
	if got := closed.FileApproval().Text(); got != ApprovalDraft {
		t.Errorf("closed: %q", got)
	}
}

// Approvals are made by approving, not written as comments.
func TestApprovalNotByHand(t *testing.T) {
	d := mustParse(t, relDoc)
	if _, err := Append(d, KindApproved, "dana@example.org", time.Now(), "", "", "", "approved: v1 · plan:"+strings.Repeat("a", 64), signerOf(testKey("dana"))); err == nil {
		t.Fatal("an approved entry was appended as a comment")
	}
}

// Entries a transfer brought from another team are history: a copy starts
// as a draft in its new team.
func TestApprovalAfterTransfer(t *testing.T) {
	a := newApprovalDoc(t)
	r1 := a.release("dana@example.org")
	ap := a.approve("dana@example.org", 1, planOf(r1))
	// The copy: a new kalka_id, the source entries listed by its transfer.
	src := string(a.d.Bytes())
	cp := mustParse(t, strings.Replace(src, "kalka_id: 4be1c0ffee0100000000000000000000", "kalka_id: 5ce1c0ffee0100000000000000000000", 1))
	tr := Transfer{Mode: "copy", FromTeam: "parcels", FromID: "4be1c0ffee0100000000000000000000", Source: strings.Repeat("1", 64),
		ToTeam: "lockers", To: strings.Repeat("2", 64), Plan: cp.PlanHash(), History: strings.Repeat("3", 64), Entries: []string{r1.ID, ap.ID}}
	e, err := NewEntry(KindTransfer, "dana@example.org", a.tick(), "", cp.Rev(), tr.body())
	if err != nil {
		t.Fatal(err)
	}
	if err := cp.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	if got := cp.FileApproval().Text(); got != ApprovalDraft {
		t.Fatalf("a copy: %q", got)
	}
	// In the source of a move the same entry lists the source's own
	// entries, which still count there.
	if err := a.d.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	if got := a.d.FileApproval().Text(); got != "approved v1" {
		t.Fatalf("the source of a move: %q", got)
	}
}
