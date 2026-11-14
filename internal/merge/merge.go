// Package merge implements the sync merge of spec 7.2.
package merge

import (
	"crypto/ed25519"
	"fmt"
	"sort"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// Reason is why a merge was rejected (spec 9.5).
type Reason string

const (
	Malformed Reason = "malformed"
	Declined  Reason = "declined"
	NotOwner  Reason = "not-owner"
	Diverged  Reason = "diverged"
)

// Outcome of a merge.
type Outcome int

const (
	// Rejected: the incoming file was not accepted; Reason says why.
	Rejected Outcome = iota
	// New: no local file existed; Doc is the incoming file, to be written
	// only after the user accepts it (spec 7.2 step 2).
	New
	// Unchanged: the local file already holds the merge result.
	Unchanged
	// Updated: Doc differs from the local file and should be written.
	Updated
)

func (o Outcome) String() string {
	return [...]string{"rejected", "new", "unchanged", "updated"}[o]
}

// Keys returns the public key pinned for an email. It must also know the
// local user's own key, because a chain can pass through them.
type Keys func(email string) (ed25519.PublicKey, bool)

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
	// replaced unparsed content, handoffs whose author has no pinned key.
	Notes []string
	// UnknownKeys lists handoff authors whose signature could not be
	// checked because no key is pinned for them.
	UnknownKeys []string
}

// Merge merges incoming bytes, received from sender, into local. local is
// nil when no file with the incoming kalka_id exists.
func Merge(local *bp.Document, incoming []byte, sender string, keys Keys) Result {
	in, err := bp.Parse(incoming)
	if err != nil {
		return reject(Malformed, err.Error())
	}
	return Docs(local, in, sender, keys)
}

// Docs is Merge for an already parsed incoming document.
func Docs(local, in *bp.Document, sender string, keys Keys) Result {
	if msg := malformed(in); msg != "" {
		return reject(Malformed, msg)
	}
	if local == nil {
		return Result{Outcome: New, Doc: in, PlanFromIncoming: true}
	}
	if local.KalkaID() != in.KalkaID() {
		return reject(Malformed, "kalka_id differs from the local file")
	}

	var res Result
	// Of two copies of one entry, the one whose signature verifies against
	// the author's pinned key wins, so a relay cannot strip a signature.
	verified := func(e *bp.Entry) bool {
		pub, ok := keys(e.Author)
		return ok && e.VerifySig(in.KalkaID(), pub)
	}
	all := func(*bp.Entry, bool) bool { return true }
	union := unionWith(local, in, all, verified)
	switch {
	case local.Plan() == in.Plan():
		// Rule 3: same plan.
	case bp.SameEmail(sender, local.Owner()):
		// Rule 4, first branch: the owner changed the plan.
		res.PlanFromIncoming = true
	case bp.SameEmail(sender, in.Owner()):
		ok, unknown := Chain(union, local.Owner(), sender, in.KalkaID(), keys)
		res.UnknownKeys = unknown
		if !ok {
			r := reject(NotOwner, fmt.Sprintf("%s is not the owner and no verified handoff from %s leads to them", sender, local.Owner()))
			if len(unknown) > 0 {
				r.Detail += fmt.Sprintf("; cannot verify handoffs by %s: sync with them first", strings.Join(unknown, ", "))
			}
			r.UnknownKeys = unknown
			return r
		}
		res.PlanFromIncoming = true
	default:
		return reject(NotOwner, fmt.Sprintf("only the owner (%s) can change the plan", local.Owner()))
	}

	plan, other := local, in
	if res.PlanFromIncoming {
		plan, other = in, local
	}
	lead, items, notes := assembleWith(plan, other, local, all, verified)
	res.Notes = append(res.Notes, notes...)

	doc, err := bp.Compose(plan, local.EOL(), lead, items)
	if err != nil {
		return reject(Malformed, "merged file: "+err.Error())
	}
	if doc.Plan() != plan.Plan() || countValid(doc) != len(items) {
		return reject(Malformed, bp.ErrOpenBlock.Error())
	}
	res.Doc = doc
	if bp.Normalize(string(doc.Bytes())) == bp.Normalize(string(local.Bytes())) {
		res.Outcome, res.Doc = Unchanged, local
	} else {
		res.Outcome = Updated
	}
	return res
}

func reject(r Reason, detail string) Result {
	return Result{Outcome: Rejected, Reason: r, Detail: detail}
}

// malformed reports why a received file cannot take part in a sync.
func malformed(d *bp.Document) string {
	if d.KalkaID() == "" {
		return "file has no kalka_id"
	}
	if d.Owner() == "" {
		return "file has no owner"
	}
	for _, p := range d.Problems() {
		switch p.Code {
		case "id-invalid", "owner-invalid", "entry-too-large", "frontmatter-unterminated", "body-open-block":
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

// Chain reports whether verified handoff entries lead from owner to target
// (spec 7.2, "Handoff chain"). entries must be in canonical order. unknown
// lists authors of would-be links whose key is not pinned.
func Chain(entries []*bp.Entry, owner, target, kalkaID string, keys Keys) (ok bool, unknown []string) {
	current := owner
	if bp.SameEmail(current, target) {
		return true, nil
	}
	for _, e := range entries {
		to, isHandoff := e.HandoffTarget()
		if !isHandoff || !bp.SameEmail(e.Author, current) || !e.Valid() {
			continue
		}
		pub, has := keys(e.Author)
		if !has {
			unknown = appendOnce(unknown, strings.ToLower(e.Author))
			continue
		}
		if !e.VerifySig(kalkaID, pub) {
			continue
		}
		current = to
		if bp.SameEmail(current, target) {
			return true, unknown
		}
	}
	return false, unknown
}

func appendOnce(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

// slot is one entry of the merged log with the unparsed lines after it.
type slot struct {
	e        *bp.Entry
	trailing []string
}

// assemble builds the merged log. Entries come from both sides; unparsed
// content comes from the plan side (spec 7.2 step 6). Invalid entries of the
// local file are kept as unparsed content so nothing typed locally is lost;
// invalid incoming entries are dropped with a note.
func assembleWith(plan, other, local *bp.Document, keep func(e *bp.Entry, fromLocal bool) bool, verified func(*bp.Entry) bool) (lead []string, items []*bp.Item, notes []string) {
	slots := map[string]*slot{}
	var order []*slot

	add := func(d *bp.Document, keepUnparsed bool) {
		var last *slot
		for _, it := range d.Items() {
			e := it.Entry
			if !e.Valid() {
				if d == local {
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
