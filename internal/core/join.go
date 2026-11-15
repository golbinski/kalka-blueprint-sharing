package core

// The join handshake of spec 19.7, both sides. The joiner dials the
// inviting owner's key id with an invite proof in hello; the owner checks
// it, appends add-member and sends the log on the same connection. Neither
// side compares a verification code: the invite is the verification.

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/syncproto"
	"github.com/golbinski/kalka/internal/teamlog"
	"github.com/golbinski/kalka/internal/transport"
)

// JoinEvery is how often a pending join dials the owner (spec 19.7 step 3).
var JoinEvery = time.Minute

// JoinView is a join as the UI and `kalka join` show it.
type JoinView struct {
	ID      string    `json:"id"`
	Team    string    `json:"team"`
	Name    string    `json:"name"`
	Owner   string    `json:"owner,omitempty"` // the owner's email, once met
	Created time.Time `json:"created"`
	Expires time.Time `json:"expires"`
	State   string    `json:"state"`
	Detail  string    `json:"detail,omitempty"`
	Tries   int       `json:"tries"`
	LastTry time.Time `json:"last_try,omitzero"`
	// Warning is set when the token names another rendezvous server.
	Warning string `json:"warning,omitempty"`
}

func joinView(r joinRecord) JoinView {
	return JoinView{ID: r.ID, Team: r.Team, Name: r.Name, Owner: r.Owner, Created: r.Created, Expires: r.Expires,
		State: r.State, Detail: r.Detail, Tries: r.Tries, LastTry: r.LastTry}
}

// Joins lists the joins recorded in joins.yaml, newest first.
func (d *Node) Joins() []JoinView {
	out := []JoinView{}
	l := d.teams.joins.list()
	for i := len(l) - 1; i >= 0; i-- {
		out = append(out, joinView(l[i]))
	}
	return out
}

// Join redeems an invite token (spec 19.7 step 2): it stores the pending
// join and dials the owner now and then once a minute until the invite
// expires.
func (d *Node) Join(ctx context.Context, token string) (JoinView, error) {
	if d.Paused() {
		return JoinView{}, ErrPaused
	}
	iv, err := teamlog.ParseToken(token)
	if err != nil {
		return JoinView{}, fmt.Errorf("%w: copy the whole line the team owner sent", err)
	}
	if !bp.SameEmail(iv.Email, d.self.Email) {
		return JoinView{}, fmt.Errorf("this invite is for %s, but this kalka is set up as %s", iv.Email, d.self.Email)
	}
	if iv.Owner == teamlog.EncodeKey(d.pub) {
		return JoinView{}, errors.New("this is an invite you made: pass it on to the person it is for")
	}
	now := d.now()
	if !now.Before(iv.Expires) {
		return JoinView{}, fmt.Errorf("%w (on %s): ask the team owner for a new one", teamlog.ErrInviteExpired, iv.Expires.Local().Format("2 Jan 15:04"))
	}
	if t, ok := d.teamByHash(iv.Team); ok {
		if st, _, _, _, err := d.load(t); err == nil {
			if m, ok := memberOf(st, d.self.Email); ok && m.PubKey == teamlog.EncodeKey(d.pub) {
				return JoinView{}, fmt.Errorf("you are already a member of %s", iv.TeamName)
			}
		}
	}
	id := iv.ID()
	var rec joinRecord
	err = d.teams.joins.update(func(l []joinRecord) ([]joinRecord, bool) {
		for i := range l {
			if l[i].ID == id {
				if l[i].State == JoinFailed {
					l[i].State, l[i].Detail = JoinPending, ""
				}
				rec = l[i]
				return l, true
			}
		}
		rec = joinRecord{ID: id, Team: iv.Team, Name: iv.TeamName, Token: iv.Token(), Created: now.UTC(), Expires: iv.Expires, State: JoinPending}
		return append(l, rec), true
	})
	if err != nil {
		return JoinView{}, err
	}
	v := joinView(rec)
	if iv.Rendezvous != d.cfg.Rendezvous {
		v.Warning = fmt.Sprintf("the invite was made on the rendezvous server %s, and this kalka uses %s: the owner can only be reached if you both use the same one", iv.Rendezvous, d.cfg.Rendezvous)
	}
	d.bus.Publish(BusEvent{Type: "teams"})
	go d.tryJoin(context.WithoutCancel(ctx), id)
	return v, nil
}

// joinLoop retries pending joins until they finish or expire; it runs for
// the daemon's life, so joins survive restarts.
func (d *Node) joinLoop(ctx context.Context) {
	t := time.NewTicker(min(5*time.Second, JoinEvery))
	defer t.Stop()
	for {
		for _, r := range d.teams.joins.list() {
			if r.State != JoinPending {
				continue
			}
			d.teams.mu.Lock()
			last := d.teams.lastTry[r.ID]
			d.teams.mu.Unlock()
			if time.Since(last) >= JoinEvery {
				go d.tryJoin(ctx, r.ID)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// setJoin edits one join record.
func (d *Node) setJoin(id string, f func(*joinRecord)) {
	_ = d.teams.joins.update(func(l []joinRecord) ([]joinRecord, bool) {
		for i := range l {
			if l[i].ID == id {
				f(&l[i])
				return l, true
			}
		}
		return l, false
	})
	d.bus.Publish(BusEvent{Type: "teams"})
}

// joinFailure is an attempt that ends the join, with the reason shown.
type joinFailure struct{ msg string }

func (e *joinFailure) Error() string { return e.msg }

// tryJoin makes one attempt at a pending join.
func (d *Node) tryJoin(ctx context.Context, id string) {
	d.teams.mu.Lock()
	if d.teams.trying[id] {
		d.teams.mu.Unlock()
		return
	}
	d.teams.trying[id] = true
	d.teams.lastTry[id] = time.Now()
	d.teams.mu.Unlock()
	defer func() {
		d.teams.mu.Lock()
		delete(d.teams.trying, id)
		d.teams.mu.Unlock()
	}()
	var rec joinRecord
	for _, r := range d.teams.joins.list() {
		if r.ID == id {
			rec = r
		}
	}
	if rec.State != JoinPending {
		return
	}
	if !d.now().Before(rec.Expires) {
		d.setJoin(id, func(r *joinRecord) {
			r.State, r.Detail = JoinFailed, "the invite expired before the owner was reached: ask them for a new one"
		})
		return
	}
	iv, err := teamlog.ParseToken(rec.Token)
	if err != nil {
		d.setJoin(id, func(r *joinRecord) { r.State, r.Detail = JoinFailed, err.Error() })
		return
	}
	owner, err := d.attemptJoin(ctx, iv)
	var fail *joinFailure
	d.setJoin(id, func(r *joinRecord) {
		r.Tries++
		r.LastTry = d.now().UTC()
		if owner != "" {
			r.Owner = owner
		}
		switch {
		case err == nil:
			r.State, r.Detail = JoinDone, ""
		case errors.As(err, &fail):
			r.State, r.Detail = JoinFailed, fail.msg
		default:
			r.Detail = err.Error()
		}
	})
	switch {
	case err == nil:
		d.logf("joined team %s", iv.TeamName)
		d.notice(Notice{Kind: NoticeJoinDone, Title: iv.TeamName, Team: iv.TeamName, Message: "you joined the team " + iv.TeamName})
	case errors.As(err, &fail):
		d.logf("joining %s failed: %v", iv.TeamName, err)
		d.notice(Notice{Kind: NoticeJoinFail, Title: iv.TeamName, Team: iv.TeamName, Message: "joining " + iv.TeamName + " failed: " + fail.msg})
	default:
		d.logf("joining %s: %v; trying again in %s", iv.TeamName, err, JoinEvery)
	}
}

// byeText explains an owner's refusal.
var byeText = map[string]string{
	syncproto.ByeInviteUnknown: "the owner's kalka has no such invite (it was made on another installation, or withdrawn)",
	syncproto.ByeInviteUsed:    "the invite was already used: each invite admits one person once",
	syncproto.ByeInviteExpired: "the invite has expired: ask the owner for a new one",
	syncproto.ByeInviteProof:   "the invite proof did not match: the token may be damaged",
	syncproto.ByeInviteTeam:    "the invite is for another team",
	syncproto.ByeInviteEmail:   "the invite is for another email address",
	syncproto.ByeInviteNoOwner: "the person who invited you is no longer an owner of the team",
}

// attemptJoin dials the owner and runs the joiner's side. A *joinFailure
// ends the join; other errors are retried. It returns the owner's email
// once the handshake succeeded.
func (d *Node) attemptJoin(ctx context.Context, iv teamlog.Invite) (string, error) {
	ownerKey, _ := teamlog.DecodeKey(iv.Owner)
	ep, err := d.endpoint()
	if err != nil {
		return "", err
	}
	dctx, cancel := context.WithTimeout(ctx, DialTimeout)
	c, err := ep.Dial(dctx, transport.KeyPeerID(ownerKey))
	cancel()
	if errors.Is(err, transport.ErrOffline) {
		return "", errors.New("the team owner is not online; kalka keeps trying once a minute until the invite expires")
	}
	if err != nil {
		return "", fmt.Errorf("connecting to the team owner: %w", err)
	}
	defer c.Close()
	claim := &transport.InviteClaim{ID: iv.ID(), Team: iv.Team, Proof: teamlog.Proof(iv.Secret, iv.Team, iv.Owner, teamlog.EncodeKey(d.pub))}
	// The key id already proves the key; the token is the trust anchor.
	verify := func(_ string, k ed25519.PublicKey) error {
		if !k.Equal(ownerKey) {
			return errors.New("the peer is not the owner named in the invite")
		}
		return nil
	}
	peer, err := transport.HandshakeInvite(c, d.self, verify, claim)
	if err != nil {
		return "", err
	}
	for {
		env, err := c.Receive(syncproto.Silence)
		if err != nil {
			return peer.Email, fmt.Errorf("waiting for the team log: %w", err)
		}
		switch env.Type {
		case "keepalive":
			continue
		case "bye":
			var b syncproto.Bye
			_ = env.Decode(&b)
			msg := byeText[b.Reason]
			if msg == "" {
				msg = "the owner's kalka refused the join"
				if b.Detail != "" {
					msg += ": " + b.Detail
				}
			} else if b.Detail != "" {
				msg += " (" + b.Detail + ")"
			}
			if b.Reason == "" || strings.HasPrefix(b.Reason, "invite-") || b.Reason == "refused" {
				return peer.Email, &joinFailure{msg}
			}
			return peer.Email, errors.New(msg)
		case "team_log":
			var m syncproto.TeamLog
			if err := env.Decode(&m); err != nil {
				return peer.Email, &joinFailure{"the owner sent a damaged team log"}
			}
			err := d.acceptJoinLog(iv, m)
			reason, detail := syncproto.ByeJoined, ""
			if err != nil {
				reason, detail = "invalid", err.Error()
			}
			_ = c.Send(syncproto.Bye{Type: "bye", Reason: reason, Detail: detail})
			time.Sleep(200 * time.Millisecond)
			if err != nil {
				return peer.Email, &joinFailure{err.Error()}
			}
			return peer.Email, nil
		}
	}
}

// acceptJoinLog checks the log the owner sent (spec 19.7 step 5): it starts
// at the team in the token and makes this key a member through this
// invite. Then it is written to the team's folder.
func (d *Node) acceptJoinLog(iv teamlog.Invite, m syncproto.TeamLog) error {
	if m.Team != iv.Team {
		return errors.New("the owner sent the log of another team")
	}
	var ops []teamlog.Op
	for _, raw := range m.Ops {
		o, err := teamlog.Decode(raw)
		if err != nil {
			return fmt.Errorf("the team log holds a damaged operation: %v", err)
		}
		ops = append(ops, o)
	}
	var root *teamlog.Op
	for i := range ops {
		if ops[i].Kind == teamlog.CreateTeam && ops[i].Hash() == iv.Team {
			root = &ops[i]
		}
	}
	if root == nil || root.Args.Name != iv.TeamName {
		return errors.New("the team log does not start at the team named in the invite")
	}
	st, _ := teamlog.Replay(iv.Team, ops)
	me, ok := memberOf(st, d.self.Email)
	if !ok || me.PubKey != teamlog.EncodeKey(d.pub) {
		return errors.New("the team log the owner sent does not make you a member")
	}
	if add := opByHash(ops, me.Since); add == nil || add.Args.Invite != iv.ID() {
		return errors.New("the team log adds you, but not through this invite")
	}
	d.teams.mu.Lock()
	defer d.teams.mu.Unlock()
	t, ok := d.teamByHash(iv.Team)
	if !ok {
		t = d.newTeamDir(iv.TeamName, iv.Team)
	}
	if _, err := teamlog.AppendFile(t.Log, ops); err != nil {
		return err
	}
	d.bus.Publish(BusEvent{Type: "teams"})
	d.kickTeam(iv.Team)
	return nil
}

func opByHash(ops []teamlog.Op, h string) *teamlog.Op {
	for i := range ops {
		if ops[i].Hash() == h {
			return &ops[i]
		}
	}
	return nil
}

// redeem is the owner's side, for an incoming connection whose hello
// carried an invite (spec 19.7 steps 4-6).
func (d *Node) redeem(c *transport.Conn, peer transport.Peer) {
	ops, reason, detail := d.admit(peer)
	if reason != "" {
		_ = c.Send(syncproto.Bye{Type: "bye", Reason: reason, Detail: detail})
		d.logf("refused a join by %s: %s %s", peer.Email, reason, detail)
		time.Sleep(200 * time.Millisecond)
		return
	}
	m := syncproto.TeamLog{Type: "team_log", Team: peer.Invite.Team}
	for _, o := range ops {
		m.Ops = append(m.Ops, json.RawMessage(o.Encode()))
	}
	if err := c.Send(m); err != nil {
		d.logf("sending the team log to %s: %v", peer.Email, err)
		return
	}
	for {
		env, err := c.Receive(syncproto.Silence)
		if err != nil {
			d.logf("join by %s: no answer after the team log: %v", peer.Email, err)
			return
		}
		if env.Type == "bye" {
			var b syncproto.Bye
			_ = env.Decode(&b)
			if b.Reason == syncproto.ByeJoined {
				d.logf("%s joined the team", peer.Email)
				team := peer.Invite.Team
				if h, err := d.heldTeam(team); err == nil {
					team = h.dir.Name
				}
				d.notice(Notice{Kind: NoticeJoined, Title: team, Team: team, Message: peer.Email + " joined " + team + " with your invite"})
			} else {
				d.logf("join by %s: their kalka refused the log: %s", peer.Email, b.Detail)
			}
			return
		}
	}
}

// admit checks an invite claim and, if it holds, adds the member. It
// returns the log to send, or the bye reason and detail.
func (d *Node) admit(peer transport.Peer) ([]teamlog.Op, string, string) {
	claim := peer.Invite
	d.teams.mu.Lock()
	defer d.teams.mu.Unlock()
	var rec *inviteRecord
	for _, r := range d.teams.invites.list() {
		if r.ID == claim.ID {
			rec = &r
		}
	}
	if rec == nil || rec.Canceled {
		return nil, syncproto.ByeInviteUnknown, ""
	}
	iv, err := teamlog.ParseToken(rec.Token)
	if err != nil {
		return nil, syncproto.ByeInviteUnknown, ""
	}
	joinerKey := teamlog.EncodeKey(peer.Key)
	if claim.Team != iv.Team {
		return nil, syncproto.ByeInviteTeam, ""
	}
	want := teamlog.Proof(iv.Secret, iv.Team, teamlog.EncodeKey(d.pub), joinerKey)
	if !hmac.Equal([]byte(want), []byte(claim.Proof)) {
		return nil, syncproto.ByeInviteProof, ""
	}
	used := !rec.UsedAt.IsZero()
	// A used invite presented again by the key that used it is a retry
	// after a lost connection: the log is sent again, nothing is added.
	if used && rec.UsedKey != joinerKey {
		return nil, syncproto.ByeInviteUsed, ""
	}
	if !used && !d.now().Before(iv.Expires) {
		return nil, syncproto.ByeInviteExpired, ""
	}
	if !bp.SameEmail(peer.Email, iv.Email) {
		return nil, syncproto.ByeInviteEmail, fmt.Sprintf("invited %s, connected as %s", iv.Email, peer.Email)
	}
	t, ok := d.teamByHash(iv.Team)
	if !ok {
		return nil, syncproto.ByeInviteNoOwner, "the team log is not in the owner's folder"
	}
	if used {
		_, ops, _, _, err := d.load(t)
		if err != nil {
			return nil, "error", err.Error()
		}
		return ops, "", ""
	}
	st, err := d.ownerOf(t)
	if err != nil {
		return nil, syncproto.ByeInviteNoOwner, ""
	}
	if _, ok := memberOf(st, iv.Email); ok {
		return nil, syncproto.ByeInviteUsed, iv.Email + " is already a member"
	}
	// The email is the one the owner invited, never the joiner's claim.
	add := d.teamSigner(t).AddMember(st.Heads, strings.ToLower(iv.Email), peer.Key, iv.ID(), d.now())
	if _, err := d.commit(t, []teamlog.Op{add}); err != nil {
		return nil, "error", err.Error()
	}
	_ = d.teams.invites.update(func(l []inviteRecord) ([]inviteRecord, bool) {
		for i := range l {
			if l[i].ID == rec.ID {
				l[i].UsedAt, l[i].UsedKey, l[i].AddedBy = d.now().UTC(), joinerKey, add.Hash()
			}
		}
		return l, true
	})
	_, ops, _, _, err := d.load(t)
	if err != nil {
		return nil, "error", err.Error()
	}
	return ops, "", ""
}
