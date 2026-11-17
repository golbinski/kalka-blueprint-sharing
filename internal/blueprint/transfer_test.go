package blueprint

import (
	"strings"
	"testing"
	"time"
)

func TestTransferEntryRoundTrip(t *testing.T) {
	k := testKey("dana")
	d := mustParse(t, relDoc)
	at := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	c1, err := Append(d, KindComment, "dana@example.org", at, "scope", "", "", "Three attempts at most.", signerOf(k))
	if err != nil {
		t.Fatal(err)
	}
	c2, err := Append(d, KindComment, "olek@example.org", at.Add(time.Minute), "", "", "", "Unsigned remark.", Signer{})
	if err != nil {
		t.Fatal(err)
	}
	hist := []*Entry{c2, c1} // any order: the digest sorts
	tr := Transfer{Mode: ModeMove, FromTeam: "parcels", FromID: "4be1c0ffee01", Source: strings.Repeat("a", 64), ToTeam: "library-desk", To: strings.Repeat("b", 64),
		Plan: d.PlanHash(), History: HistoryDigest(hist), Entries: EntryIDs(hist)}
	e, err := NewTransfer(d, "dana@example.org", at.Add(2*time.Minute), tr, signerOf(k))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	again := mustParse(t, string(d.Bytes()))
	got, ok := again.Items()[2].Entry.Transfer()
	if !ok || got.Mode != ModeMove || got.From() != "parcels/4be1c0ffee01" || got.History != tr.History || len(got.Entries) != 2 || got.Entries[0] != c1.ID {
		t.Fatalf("parsed %+v", got)
	}
	// The digest covers the signature text and the full content.
	stripped := *c1
	stripped.Sig = ""
	if HistoryDigest([]*Entry{&stripped, c2}) == tr.History || HistoryDigest([]*Entry{c1}) == tr.History {
		t.Fatal("digest ignores a stripped signature or a missing entry")
	}
	// No entries: the list is left out.
	empty := Transfer{Mode: ModeCopy, FromTeam: "parcels", FromID: "4be1c0ffee01", Source: tr.Source, ToTeam: tr.ToTeam, To: tr.To, Plan: tr.Plan, History: HistoryDigest(nil)}
	e2, err := NewTransfer(d, "dana@example.org", at, empty, signerOf(k))
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := e2.Transfer(); !ok || len(p.Entries) != 0 || p.Mode != ModeCopy {
		t.Fatalf("empty: %+v %q", p, e2.Body)
	}
	w, err := NewWithdrawal(d, "dana@example.org", at, e.ID, "withdrawn", signerOf(k))
	if err != nil || w.Withdraws() != e.ID {
		t.Fatalf("withdrawal %v %q", err, w.Withdraws())
	}
}

func TestMoveTombstoneRelease(t *testing.T) {
	d := mustParse(t, relDoc)
	e, err := NewMoveTombstone(d, "dana@example.org", time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC), 4, "library-desk/9c1d2e3f-0a4b-4c5d-8e6f-7a8b9c0d1e2f", signerOf(testKey("dana")))
	if err != nil {
		t.Fatal(err)
	}
	r, ok := e.Release()
	if !ok || !r.Deleted || r.Moved != "library-desk/9c1d2e3f-0a4b-4c5d-8e6f-7a8b9c0d1e2f" || r.Version != 4 {
		t.Fatalf("%+v %q", r, e.Body)
	}
	if !strings.HasSuffix(strings.Split(e.Body, "\n")[0], "· deleted · moved:library-desk/9c1d2e3f-0a4b-4c5d-8e6f-7a8b9c0d1e2f") {
		t.Fatalf("first line %q", e.Body)
	}
}
