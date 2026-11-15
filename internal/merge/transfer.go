package merge

// Transfers between teams (spec 20.7): attested entries, and the checks
// on a pending document.

import (
	"errors"
	"fmt"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// Acceptance is an effective accept-transfer of a document (spec 19.6):
// Transfer is the digest of the offer's transfer entry, By the accepting
// team owner, Owner the owner of version 1.
type Acceptance struct {
	Transfer string
	By       string
	Owner    string
}

// Attestation is what an accepted (or pending) transfer attests of a
// document: the entries brought from the source, by id and signature
// text, and who vouches for them.
type Attestation struct {
	Offer    *bp.Entry
	Transfer bp.Transfer
	Entries  map[string]string // id -> sig as signed over
	Actor    string
	By       string // the accepting team owner; "" while pending
}

// Has reports whether e is the attested copy of an attested entry.
func (a *Attestation) Has(e *bp.Entry) bool {
	if a == nil {
		return false
	}
	sig, ok := a.Entries[e.ID]
	return ok && sig == e.Sig && e.Valid()
}

// offerIn finds the transfer entry with digest digest among entries, and
// checks that the history it lists is present and hashes to its digest.
// It does not check who signed it.
func offerIn(entries []*bp.Entry, digest string) (*Attestation, error) {
	var offer *bp.Entry
	for _, e := range entries {
		if e.Kind == bp.KindTransfer && e.Valid() && e.Digest() == digest {
			offer = e
		}
	}
	if offer == nil {
		return nil, errors.New("the transfer entry is missing")
	}
	t, ok := offer.Transfer()
	if !ok {
		return nil, errors.New("the transfer entry does not parse")
	}
	a, err := attest(entries, t)
	if err != nil {
		return nil, err
	}
	a.Offer, a.Actor = offer, offer.Author
	return a, nil
}

// attest matches the entries a transfer lists against those held. A copy
// with a signature is tried first, then one without: a relay may hold
// both (spec 20.4, "the copy whose sig verifies is kept").
func attest(entries []*bp.Entry, t bp.Transfer) (*Attestation, error) {
	byID := map[string][]*bp.Entry{}
	for _, e := range entries {
		if e.Valid() {
			byID[e.ID] = append(byID[e.ID], e)
		}
	}
	for _, signed := range []bool{true, false} {
		var chosen []*bp.Entry
		missing := ""
		for _, id := range t.Entries {
			cs := byID[id]
			if len(cs) == 0 {
				missing = id
				break
			}
			pick := cs[0]
			for _, c := range cs {
				if (c.Sig != "") == signed {
					pick = c
					break
				}
			}
			chosen = append(chosen, pick)
		}
		if missing != "" {
			return nil, fmt.Errorf("attested entry %s is missing", missing)
		}
		if bp.HistoryDigest(chosen) == t.History {
			a := &Attestation{Transfer: t, Entries: map[string]string{}}
			for _, e := range chosen {
				a.Entries[e.ID] = e.Sig
			}
			return a, nil
		}
	}
	return nil, errors.New("the attested entries do not match the history the transfer signed")
}

// Attested returns the attestation of an accepted transfer of kalkaID over
// entries, or nil: none was accepted, or the entries held do not match
// (then they count as entries of nobody and are dropped, spec 20.7).
func Attested(team Team, kalkaID string, entries []*bp.Entry) *Attestation {
	acc, ok := team.Accepted(kalkaID)
	if !ok {
		return nil
	}
	a, err := offerIn(entries, acc.Transfer)
	if err != nil || !VerifiedBy(team, kalkaID, a.Offer) {
		return nil
	}
	a.By = acc.By
	return a
}

// Trusted is the entry filter of a team document: signed by a key the
// team bound to the author, or attested by an accepted transfer.
func Trusted(team Team, kalkaID string, entries []*bp.Entry) func(*bp.Entry) bool {
	a := Attested(team, kalkaID, entries)
	return func(e *bp.Entry) bool { return a.Has(e) || VerifiedBy(team, kalkaID, e) }
}

// Offer is a pending document that passed the checks of spec 20.7.
type Offer struct {
	*Attestation
	// Withdrawn is set when the actor signed a transfer-withdrawn entry.
	Withdrawn bool
}

// CheckOffer checks a pending document against the destination team
// (spec 20.7): exactly one transfer entry signed under its kalka_id by a
// key the log binds to its author; `to` is this team; `plan` is the
// document's plan; the listed entries are present and hash to `history`;
// no release; every other entry signed by a member. Whether the author
// is a current member is the caller's check.
func CheckOffer(doc *bp.Document, team Team, teamHash string) (*Offer, error) {
	id := doc.KalkaID()
	if id == "" || doc.Owner() == "" {
		return nil, errors.New("the offer has no kalka_id or no owner")
	}
	entries := allEntries(doc)
	var offers []*bp.Entry
	for _, e := range entries {
		if e.Kind == bp.KindTransfer && e.Valid() && VerifiedBy(team, id, e) {
			offers = append(offers, e)
		}
	}
	switch len(offers) {
	case 0:
		return nil, errors.New("the transfer entry is not signed by a key this team binds to its author")
	case 1:
	default:
		return nil, errors.New("the document carries two transfer entries of its own")
	}
	a, err := offerIn(entries, offers[0].Digest())
	if err != nil {
		return nil, err
	}
	t := a.Transfer
	if t.To != teamHash {
		return nil, errors.New("the transfer is addressed to another team")
	}
	if t.Plan != doc.PlanHash() {
		return nil, errors.New("the plan is not the plan the transfer signed")
	}
	o := &Offer{Attestation: a}
	for _, e := range entries {
		if !e.Valid() {
			return nil, fmt.Errorf("entry %s has an invalid id", e.ID)
		}
		if a.Has(e) || e == a.Offer {
			continue
		}
		if !VerifiedBy(team, id, e) {
			return nil, fmt.Errorf("entry %s is neither attested nor signed by a member", e.ID)
		}
		if e.Kind == bp.KindRelease {
			return nil, errors.New("a pending document has no release")
		}
		if e.Withdraws() == a.Offer.ID && bp.SameEmail(e.Author, a.Actor) {
			o.Withdrawn = true
		}
	}
	return o, nil
}

// Pending merges two copies of a pending document (spec 20.7): the plan
// is fixed by the offer, the entries are the union. Both must carry the
// same offer. local may be nil.
func Pending(local, in *bp.Document, team Team, teamHash string) (*bp.Document, *Offer, error) {
	oi, err := CheckOffer(in, team, teamHash)
	if err != nil {
		return nil, nil, err
	}
	if local == nil {
		return in, oi, nil
	}
	ol, err := CheckOffer(local, team, teamHash)
	if err != nil || ol.Offer.ID != oi.Offer.ID {
		return nil, nil, errors.New("the offer differs from the one held")
	}
	keep := func(e *bp.Entry, _ bool) bool { return true }
	verified := func(e *bp.Entry) bool { return oi.Has(e) || VerifiedBy(team, in.KalkaID(), e) }
	lead, items, _ := assembleWith(local, in, local, keep, verified)
	doc, err := bp.Compose(local, local.EOL(), lead, items)
	if err != nil {
		return nil, nil, err
	}
	o, err := CheckOffer(doc, team, teamHash)
	return doc, o, err
}

// CheckStub checks the stub of a declined or withdrawn offer: exactly one
// transfer entry signed under the document's kalka_id by a key of the
// team, and no release. withdrawn is set when its author also signed a
// transfer-withdrawn answering it.
func CheckStub(doc *bp.Document, team Team) (offer *bp.Entry, withdrawn bool, err error) {
	id := doc.KalkaID()
	if id == "" {
		return nil, false, errors.New("no kalka_id")
	}
	for _, e := range allEntries(doc) {
		if e.Kind == bp.KindRelease {
			return nil, false, errors.New("a stub of an offer has no release")
		}
		if e.Kind == bp.KindTransfer && e.Valid() && VerifiedBy(team, id, e) {
			if offer != nil {
				return nil, false, errors.New("two transfer entries")
			}
			offer = e
		}
	}
	if offer == nil {
		return nil, false, errors.New("no transfer entry signed by a member")
	}
	for _, e := range allEntries(doc) {
		if e.Withdraws() == offer.ID && bp.SameEmail(e.Author, offer.Author) && VerifiedBy(team, id, e) {
			withdrawn = true
		}
	}
	return offer, withdrawn, nil
}

func allEntries(doc *bp.Document) []*bp.Entry {
	var out []*bp.Entry
	for _, it := range doc.Items() {
		if it.Entry != nil {
			out = append(out, it.Entry)
		}
	}
	return out
}
