// Package syncproto runs one sync over an authenticated channel (spec 7.1,
// 9.5): the initiator sends its file, the responder merges and replies with
// the merged file, the initiator merges the reply and acknowledges.
//
// This is the M1 version: plain sync with intent "sync". Handoff intent,
// user prompts and convergence offers arrive in M2.
package syncproto

import (
	"errors"
	"fmt"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/transport"
)

// ByeWait is how long the initiator waits for the closing bye.
const ByeWait = 5 * time.Second

// Silence is how long either side waits for the next message (spec 9.5).
const Silence = 30 * time.Second

// Channel is the part of transport.Conn the protocol uses.
type Channel interface {
	Send(v any) error
	Receive(timeout time.Duration) (transport.Envelope, error)
}

// Offer is sync_offer.
type Offer struct {
	Type     string `json:"type"`
	KalkaID  string `json:"kalka_id"`
	Filename string `json:"filename"`
	Intent   string `json:"intent"`
	Content  string `json:"content"`
}

// Reply is sync_reply.
type Reply struct {
	Type    string       `json:"type"`
	KalkaID string       `json:"kalka_id"`
	Status  string       `json:"status"` // merged | rejected
	Reason  merge.Reason `json:"reason,omitempty"`
	Detail  string       `json:"detail,omitempty"`
	Content string       `json:"content,omitempty"`
}

// Ack is sync_ack.
type Ack struct {
	Type    string `json:"type"`
	KalkaID string `json:"kalka_id"`
	Rev     string `json:"rev"`
}

// Store is the responder's view of its folder.
type Store interface {
	// Find returns the local document with this kalka_id, or nil.
	Find(kalkaID string) (*bp.Document, error)
	// Write stores the merged document. filename is the sender's
	// suggestion, used only for a new blueprint.
	Write(doc *bp.Document, filename string, isNew bool) error
	// AcceptNew asks the user whether to accept a new blueprint.
	AcceptNew(from string, doc *bp.Document) bool
}

// Rejected is returned by Initiate when the responder rejected the offer.
type Rejected struct {
	Reason merge.Reason
	Detail string
	// Content is the responder's file, present for not-owner so the
	// initiator can offer convergence (spec 7.5).
	Content string
}

func (r *Rejected) Error() string {
	return fmt.Sprintf("peer rejected the sync (%s): %s", r.Reason, r.Detail)
}

// Initiate syncs local with the peer and returns the merge result the
// caller must write (Outcome Updated) before Initiate sends the ack. The
// write callback is called between merging the reply and acknowledging.
func Initiate(ch Channel, local *bp.Document, filename string, peer string, keys merge.Keys, write func(*bp.Document) error) (merge.Result, error) {
	if local.KalkaID() == "" {
		return merge.Result{}, errors.New("blueprint has no kalka_id yet: it must be prepared for its first send")
	}
	if err := ch.Send(Offer{Type: "sync_offer", KalkaID: local.KalkaID(), Filename: filename, Intent: "sync", Content: string(local.Bytes())}); err != nil {
		return merge.Result{}, err
	}
	env, err := ch.Receive(Silence)
	if err != nil {
		return merge.Result{}, fmt.Errorf("waiting for the peer's reply: %w", err)
	}
	var rep Reply
	if env.Type != "sync_reply" || env.Decode(&rep) != nil || rep.KalkaID != local.KalkaID() {
		return merge.Result{}, fmt.Errorf("peer sent %q instead of sync_reply", env.Type)
	}
	if rep.Status != "merged" {
		return merge.Result{}, &Rejected{Reason: rep.Reason, Detail: rep.Detail, Content: rep.Content}
	}
	res := merge.Merge(local, []byte(rep.Content), peer, keys)
	if res.Outcome == merge.Rejected {
		return res, fmt.Errorf("cannot merge the peer's reply (%s): %s", res.Reason, res.Detail)
	}
	if res.Outcome == merge.Updated {
		if err := write(res.Doc); err != nil {
			return res, err
		}
	}
	if err := ch.Send(Ack{Type: "sync_ack", KalkaID: local.KalkaID(), Rev: res.Doc.Rev()}); err != nil {
		return res, err
	}
	// Wait for the responder's bye before the caller closes the
	// connection: closing at once can abort the association while the ack
	// is still queued. A missing bye is harmless (spec 7.4 step 4).
	_, _ = ch.Receive(ByeWait)
	return res, nil
}

// Respond handles one sync_offer from peer.
func Respond(ch Channel, st Store, peer string, keys merge.Keys) (merge.Result, error) {
	env, err := ch.Receive(Silence)
	if err != nil {
		return merge.Result{}, err
	}
	if env.Type != "sync_offer" {
		return merge.Result{}, fmt.Errorf("peer sent %q instead of sync_offer", env.Type)
	}
	var off Offer
	if err := env.Decode(&off); err != nil {
		return merge.Result{}, err
	}
	local, err := st.Find(off.KalkaID)
	if err != nil {
		return merge.Result{}, err
	}
	res := merge.Merge(local, []byte(off.Content), peer, keys)
	reject := func(r merge.Reason, detail string) (merge.Result, error) {
		rep := Reply{Type: "sync_reply", KalkaID: off.KalkaID, Status: "rejected", Reason: r, Detail: detail}
		if r == merge.NotOwner && local != nil {
			rep.Content = string(local.Bytes())
		}
		return res, ch.Send(rep)
	}
	switch res.Outcome {
	case merge.Rejected:
		return reject(res.Reason, res.Detail)
	case merge.New:
		if !st.AcceptNew(peer, res.Doc) {
			res.Outcome, res.Reason = merge.Rejected, merge.Declined
			return reject(merge.Declined, "the recipient declined the blueprint")
		}
		if err := st.Write(res.Doc, off.Filename, true); err != nil {
			return res, err
		}
	case merge.Updated:
		if err := st.Write(res.Doc, off.Filename, false); err != nil {
			return res, err
		}
	}
	if err := ch.Send(Reply{Type: "sync_reply", KalkaID: off.KalkaID, Status: "merged", Content: string(res.Doc.Bytes())}); err != nil {
		return res, err
	}
	env, err = ch.Receive(Silence)
	if err != nil {
		return res, fmt.Errorf("waiting for sync_ack: %w", err)
	}
	if env.Type != "sync_ack" {
		return res, fmt.Errorf("peer sent %q instead of sync_ack", env.Type)
	}
	_ = ch.Send(map[string]string{"type": "bye"})
	return res, nil
}
