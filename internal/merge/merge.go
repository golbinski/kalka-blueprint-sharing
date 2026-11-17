// Package merge merges two copies of a team document (spec 20.4): the plan
// follows the release chain, the entry set only grows.
package merge

import (
	"fmt"
	"sort"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// Reason is why a merge was rejected (spec 9.5).
type Reason string

const (
	Malformed Reason = "malformed"
	// NotOwner: the incoming plan has no valid release.
	NotOwner Reason = "not-owner"
)

// Outcome of a merge.
type Outcome int

const (
	// Rejected: the incoming file was not accepted; Reason says why.
	Rejected Outcome = iota
	// New: no local file existed; Doc is the incoming file.
	New
	// Unchanged: the local file already holds the merge result.
	Unchanged
	// Updated: Doc differs from the local file and should be written.
	Updated
)

func (o Outcome) String() string {
	return [...]string{"rejected", "new", "unchanged", "updated"}[o]
}

// Result of Merge.
type Result struct {
	Outcome Outcome
	Doc     *bp.Document // nil when Rejected
	Reason  Reason
	Detail  string // human-readable explanation of a rejection

	// PlanFromIncoming is true when the frontmatter and body were taken
	// from the incoming file.
	PlanFromIncoming bool
	// Notes are warnings to show the user: dropped invalid entries,
	// replaced unparsed content, entries that stay local.
	Notes []string
}

func reject(r Reason, detail string) Result {
	return Result{Outcome: Rejected, Reason: r, Detail: detail}
}

// malformed reports why a received file cannot be merged.
func malformed(d *bp.Document) string {
	if d.KalkaID() == "" {
		return "file has no kalka_id"
	}
	if d.Owner() == "" {
		return "file has no owner"
	}
	for _, p := range d.Problems() {
		switch p.Rule {
		case "F001", "F002", "F004", "B001", "B012":
			return p.Message
		}
	}
	return ""
}

// unionWith returns the valid entries of local and in that keep accepts,
// de-duplicated by id, in canonical order. Of two copies of one id the
// first wins, unless only a later one is verified.
func unionWith(local, in *bp.Document, keep func(e *bp.Entry, fromLocal bool) bool, verified func(*bp.Entry) bool) []*bp.Entry {
	seen := map[string]*bp.Entry{}
	for _, d := range []*bp.Document{local, in} {
		for _, it := range d.Items() {
			e := it.Entry
			if !e.Valid() || !keep(e, d == local) {
				continue
			}
			if cur := seen[e.ID]; cur == nil || (!verified(cur) && verified(e)) {
				seen[e.ID] = e
			}
		}
	}
	out := make([]*bp.Entry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Less(out[j]) })
	return out
}

// slot is one entry of the merged log with the unparsed lines after it.
type slot struct {
	e        *bp.Entry
	trailing []string
}

// assemble builds the merged log. Entries come from both sides; unparsed
// content comes from the plan side (spec 20.4, "Entries"). An invalid entry
// of the local file is replaced by a valid copy of the same id if either
// side holds one, and otherwise kept as unparsed content so nothing typed
// locally is lost; invalid incoming entries are dropped with a note.
func assembleWith(plan, other, local *bp.Document, keep func(e *bp.Entry, fromLocal bool) bool, verified func(*bp.Entry) bool) (lead []string, items []*bp.Item, notes []string) {
	slots := map[string]*slot{}
	var order []*slot
	intact := map[string]bool{} // ids some side holds a valid copy of
	for _, d := range []*bp.Document{plan, other} {
		for _, it := range d.Items() {
			if it.Entry.Valid() && keep(it.Entry, d == local) {
				intact[it.Entry.ID] = true
			}
		}
	}

	add := func(d *bp.Document, keepUnparsed bool) {
		var last *slot
		for _, it := range d.Items() {
			e := it.Entry
			if !e.Valid() {
				if d == local && intact[e.ID] {
					notes = append(notes, fmt.Sprintf("entry %s in your copy was edited; the team's copy replaces it (entries cannot change: write a new comment instead)", e.ID))
				} else if d == local {
					raw := entryLines(e)
					if last != nil {
						last.trailing = append(last.trailing, raw...)
					} else {
						lead = append(lead, raw...)
					}
					notes = append(notes, fmt.Sprintf("entry %s in your copy has an invalid id; it stays in your file but is not merged or sent as an entry", e.ID))
				} else {
					notes = append(notes, fmt.Sprintf("dropped incoming entry %s: its id does not match its content", e.ID))
				}
				continue
			}
			if !keep(e, d == local) {
				continue
			}
			s := slots[e.ID]
			if s == nil {
				s = &slot{e: e}
				slots[e.ID] = s
				order = append(order, s)
			} else if !verified(s.e) && verified(e) {
				s.e = e
			}
			last = s
			if keepUnparsed {
				s.trailing = append(s.trailing, it.Trailing...)
			}
		}
	}

	lead = append(lead, plan.Lead()...)
	add(plan, true)
	add(other, false)
	if other == local && hasUnparsed(local) {
		notes = append(notes, "unparsed review-log content in your copy was replaced by the owner's")
	}

	sort.SliceStable(order, func(i, j int) bool { return order[i].e.Less(order[j].e) })
	for _, s := range order {
		items = append(items, &bp.Item{Entry: s.e, Trailing: s.trailing})
	}
	return lead, items, notes
}

func countValid(d *bp.Document) int {
	n := 0
	for _, it := range d.Items() {
		if it.Entry.Valid() {
			n++
		}
	}
	return n
}

func entryLines(e *bp.Entry) []string {
	out := []string{e.Header() + "\n"}
	if e.Body != "" {
		for _, l := range strings.Split(e.Body, "\n") {
			out = append(out, l+"\n")
		}
	}
	return out
}

func hasUnparsed(d *bp.Document) bool {
	for _, l := range d.Lead() {
		if !bp.IsBlank(l) {
			return true
		}
	}
	for _, it := range d.Items() {
		for _, l := range it.Trailing {
			if !bp.IsBlank(l) {
				return true
			}
		}
	}
	return false
}
