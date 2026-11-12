// Package syncproto runs one sync over an authenticated channel (spec 7,
// 8.3, 9.5): code verification on first contact, the offer/reply/ack
// exchange with sync or handoff intent, user prompts kept alive with
// keep-alives, and convergence after a not-owner rejection.
package syncproto

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/store"
	"github.com/golbinski/kalka/internal/transport"
)

// Timing (spec 9.5). Variables so tests can shorten them.
var (
	// Silence is how long either side waits for the next message.
	Silence = 30 * time.Second
	// PromptTimeout bounds one user prompt.
	PromptTimeout = 5 * time.Minute
	// KeepaliveEvery is how often a side waiting on its user says so.
	KeepaliveEvery = 10 * time.Second
	// ByeWait is how long the initiator waits for the closing bye.
	ByeWait = 5 * time.Second
	// LockWait is how long a responder waits for a blueprint busy in
	// another sync.
	LockWait = 10 * time.Second
)

// Intents of a sync_offer.
const (
	IntentSync    = "sync"
	IntentHandoff = "handoff"
)

// Prompt kinds.
const (
	PromptVerify   = "verify-code"
	PromptNew      = "accept-new"
	PromptHandoff  = "accept-handoff"
	PromptConverge = "converge"
)

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

// Bye ends a session; Reason and Detail say why when it ends early.
type Bye struct {
	Type   string `json:"type"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Verify carries one side's code verification state: verified, pending
// (its user is being asked) or declined.
type Verify struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type keepalive struct {
	Type string `json:"type"`
}

var known = []string{"sync_offer", "sync_reply", "sync_ack", "verify", "hello", "auth"}

// Prompt is a question for the local user.
type Prompt struct {
	Kind    string `json:"kind"`
	Peer    string `json:"peer"`
	KalkaID string `json:"kalka_id,omitempty"`
	Title   string `json:"title,omitempty"`
	Size    int    `json:"size,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// Prompter asks the user; it returns when answered or ctx ends.
type Prompter func(ctx context.Context, p Prompt) (bool, error)

// Store is the folder as the protocol needs it; *store.Store implements it.
type Store interface {
	Find(kalkaID string) (*store.File, error)
	Load(path string) (*store.File, error)
	Create(doc *bp.Document, filename string) (string, error)
	Update(f *store.File, doc *bp.Document) error
	SaveConflict(f *store.File, at time.Time) (string, error)
	Lock(kalkaID string, wait time.Duration) (func(), bool)
}

// Session is one authenticated connection, seen from one side.
type Session struct {
	Ch    Channel
	Self  string // local email
	Peer  string // authenticated peer email
	Keys  merge.Keys
	Store Store
	Ask   Prompter
	Now   func() time.Time
	// Progress, if set, receives human-readable steps.
	Progress func(string)

	lastOwner string // owner of the local copy in the last not-owner rejection
}

// Result describes what one sync did locally.
type Result struct {
	KalkaID  string        `json:"kalka_id"`
	Title    string        `json:"title,omitempty"`
	Path     string        `json:"path,omitempty"`
	Peer     string        `json:"peer"`
	Intent   string        `json:"intent"`
	Outcome  merge.Outcome `json:"-"`
	Status   string        `json:"outcome"` // new | unchanged | updated | rejected
	Reason   merge.Reason  `json:"reason,omitempty"`
	Detail   string        `json:"detail,omitempty"`
	Conflict string        `json:"conflict,omitempty"`
	Notes    []string      `json:"notes,omitempty"`
}

func (r *Result) set(o merge.Outcome) {
	r.Outcome = o
	r.Status = strings.ToLower(o.String())
}

// Rejected is returned by Initiate when the responder refused the offer.
type Rejected struct {
	Reason merge.Reason
	Detail string
}

func (r *Rejected) Error() string {
	return fmt.Sprintf("the peer rejected the sync (%s): %s", r.Reason, r.Detail)
}

// Diverged means neither side can accept the other's plan (spec 7.5).
type Diverged struct{ Mine, Theirs string }

func (d *Diverged) Error() string {
	if bp.SameEmail(d.Mine, d.Theirs) || d.Theirs == "" {
		return fmt.Sprintf("plans diverged; owner is %s — sync with the owner", d.Mine)
	}
	return fmt.Sprintf("plans diverged; your copy names %s as owner, theirs names %s — sync with the owner", d.Mine, d.Theirs)
}

// PeerEnded means the peer closed the session with a bye.
type PeerEnded struct{ Reason, Detail string }

func (e *PeerEnded) Error() string {
	if e.Detail == "" {
		return "the peer ended the session"
	}
	return "the peer ended the session: " + e.Detail
}

// ErrNotVerified means a side did not confirm the verification code.
var ErrNotVerified = errors.New("the verification code was not confirmed")

func (s *Session) progress(format string, a ...any) {
	if s.Progress != nil {
		s.Progress(fmt.Sprintf(format, a...))
	}
}

func (s *Session) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// End sends a bye that tells the peer why this side is stopping.
func (s *Session) End(reason string, err error) {
	b := Bye{Type: "bye", Reason: reason}
	if err != nil {
		b.Detail = err.Error()
	}
	_ = s.Ch.Send(b)
}

// recv returns the next message of one of the wanted types, skipping
// keep-alives and unknown types.
func (s *Session) recv(timeout time.Duration, want ...string) (transport.Envelope, error) {
	for {
		env, err := s.Ch.Receive(timeout)
		if errors.Is(err, io.EOF) {
			return env, fmt.Errorf("%s closed the connection", s.Peer)
		}
		if err != nil {
			return env, fmt.Errorf("no answer from %s: %w", s.Peer, err)
		}
		switch {
		case env.Type == "keepalive":
			continue
		case env.Type == "bye":
			var b Bye
			_ = env.Decode(&b)
			if b.Reason == "diverged" {
				return env, &Diverged{Theirs: b.Detail}
			}
			return env, &PeerEnded{Reason: b.Reason, Detail: b.Detail}
		case slices.Contains(want, env.Type):
			return env, nil
		case slices.Contains(known, env.Type):
			return env, fmt.Errorf("%s sent %q out of turn", s.Peer, env.Type)
		}
	}
}

// ask runs a prompt, sending keep-alives so the peer keeps waiting.
func (s *Session) ask(p Prompt) (bool, error) {
	if s.Ask == nil {
		return false, errors.New("no way to ask the user")
	}
	p.Peer = s.Peer
	ctx, cancel := context.WithTimeout(context.Background(), PromptTimeout)
	defer cancel()
	stop := make(chan struct{})
	every := KeepaliveEvery
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if s.Ch.Send(keepalive{Type: "keepalive"}) != nil {
					cancel()
					return
				}
			}
		}
	}()
	ok, err := s.Ask(ctx, p)
	close(stop)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		err = fmt.Errorf("no answer within %v", PromptTimeout)
	}
	return ok, err
}

// Confirm runs code verification (spec 8.3) after the handshake. verified
// says whether the peer's pinned key is already confirmed locally; mark is
// called when the local user confirms. Both sides must end verified.
func (s *Session) Confirm(verified bool, code string, mark func() error) error {
	status := "verified"
	if !verified {
		status = "pending"
	}
	if err := s.Ch.Send(Verify{Type: "verify", Status: status}); err != nil {
		return err
	}
	if !verified {
		s.progress("first sync with %s: compare the verification code %s", s.Peer, code)
		ok, err := s.ask(Prompt{Kind: PromptVerify, Code: code,
			Message: fmt.Sprintf("First sync with %s. Compare this code with them, on a call or in person: %s. Does it match?", s.Peer, code)})
		if err != nil {
			_ = s.Ch.Send(Verify{Type: "verify", Status: "declined"})
			return err
		}
		if !ok {
			_ = s.Ch.Send(Verify{Type: "verify", Status: "declined"})
			return fmt.Errorf("%w: you declined the code for %s", ErrNotVerified, s.Peer)
		}
		if err := mark(); err != nil {
			return err
		}
		if err := s.Ch.Send(Verify{Type: "verify", Status: "verified"}); err != nil {
			return err
		}
	}
	for {
		env, err := s.recv(Silence, "verify")
		if err != nil {
			return err
		}
		var v Verify
		_ = env.Decode(&v)
		switch v.Status {
		case "verified":
			return nil
		case "declined":
			return fmt.Errorf("%w: %s did not confirm the code", ErrNotVerified, s.Peer)
		}
	}
}

// Request is what the initiator syncs.
type Request struct {
	File   *store.File  // the local file as read for this operation
	Offer  *bp.Document // what to send: File.Doc, or a handoff candidate
	Intent string
}

// Initiate runs the initiator's side. For a handoff, the candidate is never
// written before the recipient's merged reply arrives (spec 7.4).
func (s *Session) Initiate(req Request) (Result, error) {
	local, offer, intent := req.File, req.Offer, req.Intent
	if offer == nil {
		offer = local.Doc
	}
	if intent == "" {
		intent = IntentSync
	}
	id := local.Doc.KalkaID()
	res := Result{KalkaID: id, Title: local.Doc.Title(), Path: local.Path, Peer: s.Peer, Intent: intent}
	if id == "" {
		return res, errors.New("blueprint has no kalka_id yet: it must be prepared for its first send")
	}
	for round := 0; round < 2; round++ {
		s.progress("offering %q to %s", res.Title, s.Peer)
		if err := s.Ch.Send(Offer{Type: "sync_offer", KalkaID: id, Filename: baseName(local.Path), Intent: intent, Content: string(offer.Bytes())}); err != nil {
			return res, err
		}
		env, err := s.recv(Silence, "sync_reply")
		if err != nil {
			return res, err
		}
		var rep Reply
		if err := env.Decode(&rep); err != nil || rep.KalkaID != id {
			return res, fmt.Errorf("%s sent a reply for another blueprint", s.Peer)
		}
		if rep.Status == "merged" {
			m, f, err := s.apply(local, []byte(rep.Content))
			if err != nil {
				s.End("error", err)
				return res, err
			}
			res.set(m.Outcome)
			res.Notes, res.Path = m.Notes, f.Path
			if err := s.Ch.Send(Ack{Type: "sync_ack", KalkaID: id, Rev: m.Doc.Rev()}); err != nil {
				return res, err
			}
			// Wait for the bye before the caller closes: closing at once can
			// abort the association with the ack still queued. A missing
			// bye is harmless (spec 7.4 step 4).
			_, _ = s.Ch.Receive(ByeWait)
			return res, nil
		}

		res.set(merge.Rejected)
		res.Reason, res.Detail = rep.Reason, rep.Detail
		switch {
		case rep.Reason == merge.Diverged:
			return res, &Diverged{Mine: local.Doc.Owner(), Theirs: theirOwner(rep.Content)}
		case rep.Reason == merge.NotOwner && rep.Content != "" && round == 0:
			m := merge.Merge(local.Doc, []byte(rep.Content), s.Peer, s.Keys)
			if m.Outcome == merge.Rejected {
				d := &Diverged{Mine: local.Doc.Owner(), Theirs: theirOwner(rep.Content)}
				s.End("diverged", errors.New(local.Doc.Owner()))
				return res, d
			}
			ok, err := s.ask(Prompt{Kind: PromptConverge, KalkaID: id, Title: res.Title,
				Message: fmt.Sprintf("Your copy of %q differs from the owner's plan (%s). Replace your plan with theirs? Your entries are kept and your current file is saved as a conflict copy.", res.Title, m.Doc.Owner())})
			if err != nil || !ok {
				s.End("declined", errors.New("kept their own copy"))
				if err == nil {
					err = &Rejected{Reason: rep.Reason, Detail: rep.Detail}
				}
				return res, err
			}
			conflict, err := s.Store.SaveConflict(local, s.now())
			if err != nil {
				s.End("error", err)
				return res, err
			}
			res.Conflict = conflict
			m, f, err := s.apply(local, []byte(rep.Content))
			if err != nil {
				s.End("error", err)
				return res, err
			}
			res.Notes = m.Notes
			local, offer, intent = f, f.Doc, IntentSync
			if fresh, err := s.Store.Load(f.Path); err == nil {
				local, offer = fresh, fresh.Doc
			}
			s.progress("replaced the plan with the owner's; your file was saved as %s", conflict)
			continue
		}
		s.End("", nil)
		return res, &Rejected{Reason: rep.Reason, Detail: rep.Detail}
	}
	return res, errors.New("the sync did not converge")
}

// apply merges content from the peer into the local file and writes it,
// merging again if the file changed on disk meanwhile (spec 14).
func (s *Session) apply(local *store.File, content []byte) (merge.Result, *store.File, error) {
	for attempt := 0; ; attempt++ {
		m := merge.Merge(local.Doc, content, s.Peer, s.Keys)
		if m.Outcome == merge.Rejected {
			return m, local, fmt.Errorf("cannot merge the reply from %s (%s): %s", s.Peer, m.Reason, m.Detail)
		}
		if m.Outcome != merge.Updated {
			return m, local, nil
		}
		err := s.Store.Update(local, m.Doc)
		if err == nil {
			return m, local, nil
		}
		if !errors.Is(err, fsutil.ErrChanged) || attempt == 2 {
			return m, local, err
		}
		if local, err = s.Store.Load(local.Path); err != nil {
			return m, local, err
		}
	}
}

func theirOwner(content string) string {
	if d, err := bp.Parse([]byte(content)); err == nil {
		return d.Owner()
	}
	return ""
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// Respond runs the responder's side: it answers offers until the
// initiator acknowledges a merge or leaves. A rejection with convergence
// content may be followed by a second offer on the same channel.
func (s *Session) Respond() ([]Result, error) {
	var out []Result
	for round := 0; round < 3; round++ {
		env, err := s.recv(Silence, "sync_offer")
		if err != nil {
			var pe *PeerEnded
			var dv *Diverged
			if round > 0 && (errors.As(err, &pe) || strings.Contains(err.Error(), "closed the connection")) {
				return out, nil
			}
			if errors.As(err, &dv) {
				dv.Mine = s.lastOwner
			}
			return out, err
		}
		var off Offer
		if err := env.Decode(&off); err != nil {
			return out, err
		}
		off.KalkaID = strings.ToLower(off.KalkaID)
		res, done, err := s.respondOne(off)
		out = append(out, res)
		if err != nil || done {
			return out, err
		}
	}
	return out, nil
}

func (s *Session) reply(r Reply) error {
	r.Type = "sync_reply"
	return s.Ch.Send(r)
}

func (s *Session) respondOne(off Offer) (res Result, done bool, err error) {
	res = Result{KalkaID: off.KalkaID, Peer: s.Peer, Intent: off.Intent}
	reject := func(r merge.Reason, detail, content string) (Result, bool, error) {
		res.set(merge.Rejected)
		res.Reason, res.Detail = r, detail
		return res, false, s.reply(Reply{KalkaID: off.KalkaID, Status: "rejected", Reason: r, Detail: detail, Content: content})
	}
	if off.KalkaID == "" {
		return reject(merge.Malformed, "the offer has no kalka_id", "")
	}
	unlock, ok := s.Store.Lock(off.KalkaID, LockWait)
	if !ok {
		return reject(merge.Declined, "the blueprint is busy in another sync on the recipient's side; try again", "")
	}
	defer unlock()

	local, err := s.Store.Find(off.KalkaID)
	if err != nil {
		return reject(merge.Declined, err.Error(), "")
	}
	accepted := false
	for attempt := 0; ; attempt++ {
		var ldoc *bp.Document
		if local != nil {
			ldoc = local.Doc
		}
		m := merge.Merge(ldoc, []byte(off.Content), s.Peer, s.Keys)
		res.Notes = m.Notes
		if m.Doc != nil {
			res.Title = m.Doc.Title()
		}
		switch m.Outcome {
		case merge.Rejected:
			if m.Reason == merge.NotOwner && local != nil {
				res.Title, res.Path = local.Doc.Title(), local.Path
				s.lastOwner = local.Doc.Owner()
				// Could the initiator take our plan? That is its merge of
				// our file into its own, with us as sender.
				if in, perr := bp.Parse([]byte(off.Content)); perr == nil {
					if back := merge.Docs(in, local.Doc, s.Self, s.Keys); back.Outcome == merge.Rejected {
						return reject(merge.Diverged, (&Diverged{Mine: local.Doc.Owner(), Theirs: in.Owner()}).Error(), "")
					}
				}
				return reject(merge.NotOwner, m.Detail, string(local.Doc.Bytes()))
			}
			return reject(m.Reason, m.Detail, "")

		case merge.New:
			if !accepted {
				p := Prompt{Kind: PromptNew, KalkaID: off.KalkaID, Title: m.Doc.Title(), Size: len(off.Content),
					Message: fmt.Sprintf("%s sends you a new blueprint: %q (%d bytes). Accept it?", s.Peer, m.Doc.Title(), len(off.Content))}
				if off.Intent == IntentHandoff && bp.SameEmail(m.Doc.Owner(), s.Self) {
					p.Kind = PromptHandoff
					p.Message = fmt.Sprintf("%s wants to hand %q over to you (a blueprint you do not have yet, %d bytes). Accept ownership?", s.Peer, m.Doc.Title(), len(off.Content))
				}
				ok, err := s.ask(p)
				if err != nil {
					return reject(merge.Declined, "the recipient did not answer: "+err.Error(), "")
				}
				if !ok {
					return reject(merge.Declined, s.Self+" declined", "")
				}
				accepted = true
			}
			path, err := s.Store.Create(m.Doc, off.Filename)
			if err != nil {
				s.End("error", errors.New("the recipient could not store the file"))
				return res, true, err
			}
			res.Path = path

		case merge.Updated:
			res.Path = local.Path
			if !accepted && off.Intent == IntentHandoff && !bp.SameEmail(local.Doc.Owner(), s.Self) && bp.SameEmail(m.Doc.Owner(), s.Self) {
				ok, err := s.ask(Prompt{Kind: PromptHandoff, KalkaID: off.KalkaID, Title: m.Doc.Title(),
					Message: fmt.Sprintf("%s wants to hand %q over to you. Accept ownership?", s.Peer, m.Doc.Title())})
				if err != nil {
					return reject(merge.Declined, "the recipient did not answer: "+err.Error(), "")
				}
				if !ok {
					return reject(merge.Declined, s.Self+" declined the handoff", "")
				}
				accepted = true
			}
			err := s.Store.Update(local, m.Doc)
			if errors.Is(err, fsutil.ErrChanged) && attempt < 2 {
				if local, err = s.Store.Find(off.KalkaID); err != nil {
					return reject(merge.Declined, err.Error(), "")
				}
				continue
			}
			if err != nil {
				s.End("error", errors.New("the recipient could not store the file"))
				return res, true, err
			}

		case merge.Unchanged:
			res.Path = local.Path
		}

		res.set(m.Outcome)
		if err := s.reply(Reply{KalkaID: off.KalkaID, Status: "merged", Content: string(m.Doc.Bytes())}); err != nil {
			return res, true, err
		}
		if _, err := s.recv(Silence, "sync_ack"); err != nil {
			return res, true, fmt.Errorf("waiting for sync_ack: %w", err)
		}
		_ = s.Ch.Send(Bye{Type: "bye"})
		return res, true, nil
	}
}
