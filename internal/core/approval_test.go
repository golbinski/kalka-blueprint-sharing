package core

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// approvalText is the approval state of the team document id as n's
// document view and list row show it.
func approvalText(t *testing.T, n *node, id string) string {
	t.Helper()
	p, _ := n.teamFile(id)
	v, err := n.d.Document(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if v.Approval == nil {
		return ""
	}
	return v.Approval.Text()
}

func rowApproval(t *testing.T, n *node, id string) string {
	t.Helper()
	sc, _ := n.d.ResolveScope("parcels")
	for _, in := range n.d.BlueprintsIn(sc) {
		if strings.EqualFold(in.KalkaID, id) {
			return bp.Approval{State: in.Approval, Version: in.ApprovedVersion}.Text()
		}
	}
	t.Fatalf("%s lists no %s", n.email, id)
	return ""
}

// Approve (spec 6.18): releases unreleased edits and approves them in one
// step, approves the latest release without edits, appends nothing when
// it is approved already, and the approval holds across a handoff until
// a release changes the body.
func TestApprove(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	courier.write("redelivery.md", issueDoc)
	waitIndexed(t, courier, 1)
	id := courier.share("redelivery.md", depot)
	path := courier.path("redelivery.md")
	if got := approvalText(t, courier, id); got != "draft" {
		t.Fatalf("published: %q", got)
	}

	// A member who is not the owner gets the release error.
	if _, err := depot.d.Approve(must(depot.teamFile(id)), "", nil); KindOf(err) != KindNotOwner {
		t.Fatalf("a team owner approved without taking over: %v", err)
	}

	// Unreleased edits: one confirmation releases v2 and approves it.
	courier.edit("redelivery.md", "the next day", "the same evening")
	r, err := courier.d.Approve(path, "Second attempt the same evening.", nil)
	if err != nil || !r.Released || !r.Changed || r.Version != 2 || r.State != "approved v2" {
		t.Fatalf("approve with edits: %+v %v", r, err)
	}
	if got := approvalText(t, courier, id); got != "approved v2" {
		t.Fatalf("header: %q", got)
	}
	ap := courier.doc("redelivery.md")
	var approved *bp.Entry
	for _, e := range entriesOf(ap) {
		if e.Kind == bp.KindApproved {
			approved = e
		}
	}
	if x, ok := approved.Approved(); !ok || x.Version != 2 || x.Plan != courier.head(id).Release.Plan || approved.Sig == "" {
		t.Fatalf("approved entry %q", approved.Header()+"\n"+approved.Body)
	}

	// Again with no edits: nothing appended.
	before := courier.read("redelivery.md")
	if r, err := courier.d.Approve(path, "", nil); err != nil || r.Changed || r.Version != 2 {
		t.Fatalf("approve again: %+v %v", r, err)
	}
	if courier.read("redelivery.md") != before {
		t.Fatal("approving an approved document changed the file")
	}
	if _, err := courier.d.Approve(path, "", []string{approved.ID}); err == nil {
		t.Fatal("--incorporates with nothing to release")
	}

	// The approval replicates, and a handoff (frontmatter only) keeps it.
	courier.replicate(depot)
	if got := approvalText(t, depot, id); got != "approved v2" {
		t.Fatalf("depot sees %q", got)
	}
	if _, err := courier.d.Handoff(path, depot.email); err != nil {
		t.Fatal(err)
	}
	courier.replicate(depot)
	if got, row := approvalText(t, depot, id), rowApproval(t, depot, id); got != "approved v2" || row != "approved v2" {
		t.Fatalf("after a handoff: %q, row %q", got, row)
	}
	// Local unreleased edits do not change the state.
	dp := must(depot.teamFile(id))
	rel, err := filepath.Rel(depot.d.Store.Dir(), dp)
	if err != nil {
		t.Fatal(err)
	}
	depot.write(rel, strings.Replace(depot.read(rel), "same evening", "same afternoon", 1))
	waitStored(t, depot, dp, "same afternoon")
	if got := approvalText(t, depot, id); got != "approved v2" {
		t.Fatalf("with unreleased edits: %q", got)
	}
	// A release that changes the body.
	if _, err := depot.d.Release(dp, "", nil); err != nil {
		t.Fatal(err)
	}
	depot.replicate(courier)
	for _, n := range []*node{depot, courier} {
		if got, row := approvalText(t, n, id), rowApproval(t, n, id); got != "changed since approved v2" || row != got {
			t.Fatalf("%s after a body change: %q, row %q", n.email, got, row)
		}
	}
	// Approving it again approves the latest release.
	if r, err := depot.d.Approve(dp, "", nil); err != nil || r.Released || !r.Changed || r.Version != 4 {
		t.Fatalf("approve v4: %+v %v", r, err)
	}
	depot.replicate(courier)
	if got := approvalText(t, courier, id); got != "approved v4" {
		t.Fatalf("courier sees %q", got)
	}
}

// An approved entry by someone other than the owner of the release it
// cites is ignored, with the reason, on every peer; a team owner approves
// someone else's document by taking it over first (spec 7.4).
func TestApprovalByNonOwner(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	depot, courier := ns[0], ns[1]
	courier.write("redelivery.md", issueDoc)
	waitIndexed(t, courier, 1)
	id := courier.share("redelivery.md", depot)

	// Depot signs an approval of courier's v1 by hand.
	dp, doc := depot.teamFile(id)
	h := depot.held(t, depot.team())
	head := depot.head(id)
	e, err := bp.NewApproval(doc, depot.email, depot.d.now(), 1, head.Release.Plan, "", depot.d.signerIn(h))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	f, _ := depot.d.Store.Load(dp)
	if err := depot.d.Store.Update(f, doc); err != nil {
		t.Fatal(err)
	}
	depot.replicate(courier)
	for _, n := range []*node{depot, courier} {
		p, _ := n.teamFile(id)
		v, _ := n.d.Document(context.Background(), p)
		var why string
		for _, ev := range v.Entries {
			if ev.ID == e.ID && ev.Approved != nil {
				why = ev.Approved.Ignored
			}
		}
		if v.Approval.Text() != "draft" || !strings.Contains(why, courier.email) {
			t.Fatalf("%s: %q, entry ignored for %q", n.email, v.Approval.Text(), why)
		}
	}

	// The team owner takes it over, then approves.
	r, err := depot.d.TakeOverApprove(dp, "", nil)
	if err != nil || !r.Changed || r.Version != 2 {
		t.Fatalf("take over and approve: %+v %v", r, err)
	}
	if got := approvalText(t, depot, id); got != "approved v2" {
		t.Fatalf("after taking over: %q", got)
	}
}

// Approval exists for team documents only: Personal, seeds and domain
// documents are refused with a named kind. Approval changes no
// diagnostic: an unclaimed goal is P030 before and after.
func TestApproveScope(t *testing.T) {
	_, ns := teamOfN(t, "depot@parcels.example", "courier@parcels.example")
	courier := ns[1]
	courier.write("personal/draft.md", issueDoc)
	courier.write("epic.md", strings.ReplaceAll(planEpic, "alice@library.example", courier.email))
	waitIndexed(t, courier, 2)
	if _, err := courier.d.Approve(courier.path("personal/draft.md"), "", nil); KindOf(err) != KindInPersonal {
		t.Fatalf("personal: %v", err)
	}
	id := courier.share("epic.md")
	path := courier.path("epic.md")
	p030 := func() bool {
		v, _ := courier.d.Document(context.Background(), path)
		for _, p := range v.Problems {
			if p.Rule == "P030" {
				return true
			}
		}
		return false
	}
	if !p030() {
		t.Fatal("no P030 before approving")
	}
	if _, err := courier.d.Approve(path, "", nil); err != nil {
		t.Fatal(err)
	}
	if got := approvalText(t, courier, id); got != "approved v1" || !p030() {
		t.Fatalf("after approving: %q, P030 %v", got, p030())
	}
	// The graph card and the agent context name the state.
	g, _, err := courier.d.Graph(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if n.Focus && (n.Approval == nil || n.Approval.Text() != "approved v1") {
			t.Fatalf("graph card %+v", n.Approval)
		}
	}
	if c, err := courier.d.AgentContext(path, false); err != nil || !strings.Contains(c.Markdown(), "\n\nApproved v1, released v1 at ") {
		t.Fatalf("context %v %s", err, c.Markdown())
	}

	courier.write("idea.md", "---\nkalka_status: seed\n---\n# An idea\n\n## Problem\n\nx\n")
	waitIndexed(t, courier, 3)
	courier.share("idea.md")
	if _, err := courier.d.Approve(courier.path("idea.md"), "", nil); KindOf(err) != KindNoApproval {
		t.Fatalf("seed: %v", err)
	}
	courier.write("lockers-domain.md", "---\nkalka_kind: domain\nkalka_domain: lockers\n---\n# Lockers\n")
	waitIndexed(t, courier, 4)
	courier.share("lockers-domain.md")
	if _, err := courier.d.Approve(courier.path("lockers-domain.md"), "", nil); KindOf(err) != KindNoApproval {
		t.Fatalf("domain: %v", err)
	}
	// kalka_status: closed is a label: a draft.
	courier.write("old.md", "---\nkalka_status: closed\n---\n# Old\n")
	waitIndexed(t, courier, 5)
	oid := courier.share("old.md")
	if got := approvalText(t, courier, oid); got != "draft" {
		t.Fatalf("closed: %q", got)
	}
}

// A document copied to another team starts there as a draft; the source's
// approval is history (spec 20.7).
func TestApprovalAfterCopy(t *testing.T) {
	x := twoTeams(t)
	_, path := x.lockersIn(t)
	if _, err := x.ewa.d.Approve(path, "", nil); err != nil {
		t.Fatal(err)
	}
	r, err := x.ewa.d.Copy(path, "library-desk")
	if err != nil {
		t.Fatal(err)
	}
	roundIn(t, x.ewa, x.jan, x.lib)
	if _, err := x.jan.d.AcceptTransfer(r.ID, ""); err != nil {
		t.Fatal(err)
	}
	jp, jd := x.jan.fileIn(t, x.lib, r.ID)
	v, err := x.jan.d.Document(context.Background(), jp)
	if err != nil {
		t.Fatal(err)
	}
	history := ""
	for _, ev := range v.Entries {
		if ev.Approved != nil {
			history = ev.Approved.Ignored
		}
	}
	if v.Approval == nil || v.Approval.Text() != "draft" || !strings.Contains(history, "history") {
		t.Fatalf("copy: %+v, source approval %q", v.Approval, history)
	}
	if got := jd.FileApproval().Text(); got != "draft" {
		t.Fatalf("as the file says: %q", got)
	}
}
