package core

// Team replication (spec 20): every member keeps a replica of the team's
// published documents. Sessions run between members over key ids, with no
// prompts: log heads and a digest first, then missing log operations, then
// inventories, then the wanted documents.

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/store"
	"github.com/golbinski/kalka/internal/syncproto"
	"github.com/golbinski/kalka/internal/teamlog"
	"github.com/golbinski/kalka/internal/transport"
)

// Replication timing and limits (spec 20.3).
var (
	ReplicateEvery = 10 * time.Minute
	// RoundPeers is how many online members a scheduled round reaches;
	// PushPeers how many a local change is pushed to.
	RoundPeers = 3
	PushPeers  = 5
	// RoundGap is the least time between two rounds of one team that were
	// not caused by a local change.
	RoundGap = 15 * time.Second
	// PushDelay gathers local changes before a push.
	PushDelay = 2 * time.Second
)

// logTeam is a replayed team log as the merge needs it.
type logTeam struct {
	st *teamlog.State
}

func (t logTeam) Term(ref string) (string, ed25519.PublicKey, bool) {
	for h, tm := range t.st.Terms {
		if bp.TermOf(h) != ref {
			continue
		}
		if pk, err := teamlog.DecodeKey(tm.PubKey); err == nil {
			return tm.Email, pk, true
		}
	}
	return "", nil, false
}

// TermIn is the term reference under which email signs in st with key
// pub: the operation that made it a member with that key, or "".
func TermIn(st *teamlog.State, email string, pub ed25519.PublicKey) string {
	if st == nil {
		return ""
	}
	m, ok := memberOf(st, email)
	if !ok || m.PubKey != teamlog.EncodeKey(pub) {
		return ""
	}
	return bp.TermOf(m.Since)
}

// TermNear is TermIn for the team whose folder holds path: the nearest
// folder above it with a team log. It is for writers without a running
// node, such as `kalka comment`; "" outside a team.
func TermNear(path, email string, pub ed25519.PublicKey) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	for dir := filepath.Dir(abs); ; {
		logs, _ := filepath.Glob(filepath.Join(dir, "*-team.log"))
		for _, lp := range logs {
			ops, _, err := teamlog.ReadFile(lp)
			if err != nil {
				continue
			}
			for _, o := range ops {
				if o.Supported() && o.Kind == teamlog.CreateTeam && o.Team == "" && teamlog.LogName(o.Args.Name) == filepath.Base(lp) {
					st, _ := teamlog.Replay(o.Hash(), ops)
					return TermIn(st, email, pub)
				}
			}
		}
		up := filepath.Dir(dir)
		if up == dir {
			return ""
		}
		dir = up
	}
}

// Anchors are the document's effective reassignments and accepted
// transfer (spec 20.4).
func (t logTeam) Anchors(id string) []merge.Anchor {
	var out []merge.Anchor
	for _, a := range t.st.Reassigned[strings.ToLower(id)] {
		out = append(out, merge.Anchor{Owner: a.Owner, After: a.After})
	}
	return out
}

// Accepted is the document's effective accept-transfer (spec 19.6).
func (t logTeam) Accepted(id string) (merge.Acceptance, bool) {
	id = strings.ToLower(id)
	for tr, dec := range t.st.Transfers {
		if dec.Accepted && dec.Doc == id {
			return merge.Acceptance{Transfer: tr, By: dec.By, Owner: dec.Owner}, true
		}
	}
	return merge.Acceptance{}, false
}

// held is a team log held locally, replayed.
type held struct {
	dir teamDir
	st  *teamlog.State
	ops []teamlog.Op
}

// team is the team as the merge sees it.
func (h *held) team() logTeam { return logTeam{st: h.st} }

// memberKey returns the key the log binds to a current member.
func (h *held) memberKey(email string) (ed25519.PublicKey, bool) {
	m, ok := memberOf(h.st, email)
	if !ok {
		return nil, false
	}
	k, err := teamlog.DecodeKey(m.PubKey)
	return k, err == nil
}

func (h *held) isMember(email string, key ed25519.PublicKey) bool {
	k, ok := h.memberKey(email)
	return ok && k.Equal(key)
}

func (d *Node) heldTeam(team string) (*held, error) {
	t, ok := d.teamByHash(team)
	if !ok {
		return nil, fmt.Errorf("team %s is not held here", short(team))
	}
	st, ops, _, _, err := d.load(t)
	if err != nil {
		return nil, err
	}
	return &held{dir: t, st: st, ops: ops}, nil
}

// heldByName finds a team the local user belongs to by the name a
// document's `kalka_team:` key carries.
func (d *Node) heldByName(name string) (*held, error) {
	var found []*held
	for _, t := range d.teamDirs() {
		if t.Name != name {
			continue
		}
		h, err := d.heldTeam(t.Team)
		if err == nil && h.isMember(d.self.Email, d.pub) {
			found = append(found, h)
		}
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("you are not a member of a team named %s", name)
	case 1:
		return found[0], nil
	}
	return nil, fmt.Errorf("two teams you belong to are named %s", name)
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// teamDoc is one published document of the replication set.
type teamDoc struct {
	item    syncproto.InvItem
	path    string // the live file, the tombstone, or the pending file
	tomb    bool
	pending bool // a pending transfer, or its stub when tomb (spec 20.7)
	doc     *bp.Document
	raw     []byte
	chain   merge.ReleaseChain
}

func entriesOf(doc *bp.Document) []*bp.Entry {
	var out []*bp.Entry
	for _, it := range doc.Items() {
		if it.Entry != nil {
			out = append(out, it.Entry)
		}
	}
	return out
}

// describe computes a document's inventory line, or false when it has no
// valid release under this log and so does not replicate (spec 20.1).
func describe(h *held, doc *bp.Document) (syncproto.InvItem, merge.ReleaseChain, bool) {
	id := doc.KalkaID()
	ch := merge.Releases(entriesOf(doc), id, h.team())
	head, ok := ch.Head()
	if !ok {
		return syncproto.InvItem{}, ch, false
	}
	var ids []string
	trust := merge.Trusted(h.team(), id, entriesOf(doc))
	for _, e := range entriesOf(doc) {
		if e.Valid() && trust(e) {
			ids = append(ids, e.ID)
		}
	}
	slices.Sort(ids)
	return syncproto.InvItem{ID: id, Version: head.Release.Version, Plan: head.Release.Plan,
		Entries: idsDigest(ids), Deleted: head.Release.Deleted}, ch, true
}

// idsDigest is the inventory digest of a sorted list of entry ids.
func idsDigest(ids []string) string {
	sum := sha256.Sum256([]byte(strings.Join(slices.Compact(ids), ",")))
	return hex.EncodeToString(sum[:16])
}

func tombDir(h *held) string  { return filepath.Join(h.dir.Dir, ".kalka", "tombstones") }
func trashDir(h *held) string { return filepath.Join(h.dir.Dir, ".kalka", "trash") }

// replicaSet lists the team's documents held here: the team documents in
// its folder (isTeamDoc), the tombstones, and the pending transfers with
// the stubs of those declined or withdrawn (spec 14, 20.1, 20.6, 20.7).
// Nothing else ever enters an inventory; no file outside the team's
// folder is even read.
func (d *Node) replicaSet(h *held) map[string]teamDoc {
	set := map[string]teamDoc{}
	for _, in := range d.Store.List() {
		if in.KalkaID == "" || in.Duplicate || !inDir(h.dir.Dir, in.Path) {
			continue
		}
		f, err := d.Store.Load(in.Path)
		if err != nil || isTeamDoc(h, f.Path, f.Doc, false) != "" {
			continue
		}
		if item, ch, ok := describe(h, f.Doc); ok {
			set[item.ID] = teamDoc{item: item, path: f.Path, doc: f.Doc, raw: f.Raw, chain: ch}
		}
	}
	for _, p := range d.pendingDocs(h) {
		if _, released := set[p.id]; released || p.err != nil {
			continue
		}
		set[p.id] = teamDoc{item: pendingItem(p.doc, false), path: p.path, pending: true, doc: p.doc, raw: p.raw}
	}
	tombs, _ := filepath.Glob(filepath.Join(tombDir(h), "*.md"))
	for _, p := range tombs {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		doc, err := bp.Parse(raw)
		if err != nil || doc.KalkaID() == "" {
			continue
		}
		if item, ch, ok := describe(h, doc); ok && item.Deleted {
			set[item.ID] = teamDoc{item: item, path: p, tomb: true, doc: doc, raw: raw, chain: ch}
			continue
		}
		if _, _, err := merge.CheckStub(doc, h.team()); err == nil {
			if td, ok := set[doc.KalkaID()]; !ok || td.pending {
				set[doc.KalkaID()] = teamDoc{item: pendingItem(doc, true), path: p, tomb: true, pending: true, doc: doc, raw: raw}
			}
		}
	}
	return set
}

func digest(set map[string]teamDoc) string {
	lines := make([]string, 0, len(set))
	for _, td := range set {
		it := td.item
		lines = append(lines, fmt.Sprintf("%s %d %s %s %t %t", it.ID, it.Version, it.Plan, it.Entries, it.Deleted, it.Pending))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:16])
}

func inventory(set map[string]teamDoc) []syncproto.InvItem {
	out := make([]syncproto.InvItem, 0, len(set))
	for _, td := range set {
		out = append(out, td.item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// wants lists what to ask for from the other side's inventory: documents
// not held, newer versions, and differing entry sets. A local tombstone is
// final and wants nothing. A pending transfer is wanted while none is
// held, again for a withdrawal, and gives way to the released document
// (spec 20.7).
func wants(mine map[string]teamDoc, theirs []syncproto.InvItem) []string {
	var out []string
	for _, t := range theirs {
		m, ok := mine[t.ID]
		switch {
		case !ok:
			out = append(out, t.ID)
		case m.item.Deleted:
		case t.Pending && !m.pending:
		case t.Pending:
			if t.Deleted || t.Entries != m.item.Entries {
				out = append(out, t.ID)
			}
		case m.pending:
			out = append(out, t.ID)
		case t.Deleted, t.Version > m.item.Version, t.Entries != m.item.Entries, t.Version == m.item.Version && t.Plan != m.item.Plan:
			out = append(out, t.ID)
		}
	}
	return out
}

// missingOps are the operations of h not among their heads or the heads'
// ancestors as far as h knows them.
func missingOps(h *held, theirHeads []string) []json.RawMessage {
	byHash := map[string]teamlog.Op{}
	for _, o := range h.ops {
		byHash[o.Hash()] = o
	}
	have := map[string]bool{}
	var walk func(string)
	walk = func(x string) {
		if have[x] {
			return
		}
		have[x] = true
		if o, ok := byHash[x]; ok {
			for _, p := range o.Prev {
				walk(p)
			}
		}
	}
	for _, x := range theirHeads {
		walk(x)
	}
	out := []json.RawMessage{}
	for _, o := range h.ops {
		if !have[o.Hash()] {
			out = append(out, json.RawMessage(o.Encode()))
		}
	}
	return out
}

// mergeOps adds the valid operations of one team from a peer to its log,
// and returns how many were new. Badly signed operations are not written,
// nor operations whose signer no add-member or create-team among their
// ancestors binds: those can never count, whatever arrives later, so
// refusing them cannot make two peers differ, and someone who was never a
// member cannot make members store and replay their operations.
func (d *Node) mergeOps(h *held, raws []json.RawMessage) (int, error) {
	var in []teamlog.Op
	for _, r := range raws {
		o, err := teamlog.Decode(r)
		if err != nil || !o.VerifySig() {
			continue
		}
		if o.Team != h.dir.Team && !(o.Kind == teamlog.CreateTeam && o.Hash() == h.dir.Team) {
			continue
		}
		in = append(in, o)
	}
	ops := boundOps(h.ops, in)
	if len(ops) == 0 {
		return 0, nil
	}
	before := len(h.ops)
	d.teams.mu.Lock()
	all, err := teamlog.AppendFile(h.dir.Log, ops)
	d.teams.mu.Unlock()
	if err != nil {
		return 0, err
	}
	st, _ := teamlog.Replay(h.dir.Team, all)
	h.ops, h.st = all, st
	if n := len(all) - before; n > 0 {
		d.bus.Publish(BusEvent{Type: "teams"})
		return n, nil
	}
	return 0, nil
}

// boundOps keeps the operations of in whose ancestors, among held and in,
// are all present and bind their signer: the create-team or an add-member
// names its key, or an operation of a newer rule version, itself bound,
// stands among them (that version may bind keys in ways this kalka cannot
// read). Held operations count as bound. An operation whose ancestors are
// missing comes again with them in a later session.
func boundOps(held, in []teamlog.Op) []teamlog.Op {
	byHash := map[string]*teamlog.Op{}
	ok := map[string]bool{}
	for i := range held {
		h := held[i].Hash()
		byHash[h], ok[h] = &held[i], true
	}
	for i := range in {
		byHash[in[i].Hash()] = &in[i]
	}
	var bound func(o *teamlog.Op, depth int) bool
	bound = func(o *teamlog.Op, depth int) bool {
		h := o.Hash()
		if v, done := ok[h]; done {
			return v
		}
		if o.Supported() && o.Kind == teamlog.CreateTeam {
			ok[h] = true
			return true
		}
		ok[h] = false // until shown otherwise; also ends any loop
		found := false
		seen := map[string]bool{}
		stack := append([]string(nil), o.Prev...)
		for len(stack) > 0 {
			x := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[x] {
				continue
			}
			seen[x] = true
			a, held := byHash[x]
			if !held {
				return false // an ancestor is missing
			}
			switch {
			case !a.Supported():
				found = found || depth < 64 && bound(a, depth+1)
			case a.Kind == teamlog.CreateTeam && a.Signer == o.Signer,
				a.Kind == teamlog.AddMember && a.Args.PubKey == o.Signer:
				found = true
			}
			stack = append(stack, a.Prev...)
		}
		ok[h] = found
		return found
	}
	var out []teamlog.Op
	for i := range in {
		if bound(&in[i], 0) {
			out = append(out, in[i])
		}
	}
	return out
}

// maxStrangerOps is the most operations taken from a caller the log does
// not count as a member: the few that add them, after a join.
const maxStrangerOps = 64

// Refusal is a team document that was not written, and why (spec 20.2).
type Refusal struct {
	KalkaID string    `json:"kalka_id"`
	Title   string    `json:"title,omitempty"`
	From    string    `json:"from"`
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
}

// SessionResult is one replication session.
type SessionResult struct {
	Peer        string    `json:"peer"`
	Equal       bool      `json:"equal"`
	OpsIn       int       `json:"ops_in"`
	Received    []string  `json:"received"` // kalka_ids written
	Sent        []string  `json:"sent"`
	Refused     []Refusal `json:"refused,omitempty"`
	Unavailable int       `json:"unavailable,omitempty"`
}

// exchange runs a session for one team on an authenticated channel.
// first is the peer's team_sync when this side responds.
func (d *Node) exchange(ch syncproto.Channel, peer transport.Peer, team string, first *transport.Envelope) (SessionResult, error) {
	res := SessionResult{Peer: peer.Email, Received: []string{}, Sent: []string{}}
	if d.wrapChan != nil {
		ch = d.wrapChan(ch)
	}
	responder := first != nil
	// end says why this side stops. The initiator then waits for the
	// responder to close, so nothing still in flight is cut off; a
	// responder that refuses gives its bye a moment to leave.
	end := func(reason string) {
		_ = ch.Send(syncproto.Bye{Type: "bye", Reason: reason})
		if responder {
			time.Sleep(200 * time.Millisecond)
			return
		}
		for {
			if _, err := ch.Receive(syncproto.ByeWait); err != nil {
				return
			}
		}
	}
	recv := func(typ string, v any) error {
		for {
			env, err := ch.Receive(syncproto.Silence)
			if err != nil {
				return fmt.Errorf("no answer from %s: %w", peer.Email, err)
			}
			switch env.Type {
			case "keepalive":
				continue
			case "bye":
				var b syncproto.Bye
				_ = env.Decode(&b)
				if b.Reason == syncproto.ByeNotMember {
					return fmt.Errorf("%s does not count you as a member of the team yet", peer.Email)
				}
				return &syncproto.PeerEnded{Reason: b.Reason, Detail: b.Detail}
			case typ:
				return env.Decode(v)
			}
			if !syncproto.Known(env.Type) {
				continue // a message of a newer kalka (spec 9.5)
			}
			return fmt.Errorf("%s sent %q, expected %s", peer.Email, env.Type, typ)
		}
	}

	h, err := d.heldTeam(team)
	if err != nil || !h.isMember(d.self.Email, d.pub) {
		if responder {
			end(syncproto.ByeNotMember) // the same answer for an unknown team
		}
		return res, errors.New("not a member of that team")
	}
	// Nothing about the documents is read for a caller the log does not
	// count yet (spec 8.2): its team_sync answer carries the log heads
	// only, so the initiator can bring the operations that add it.
	known := h.isMember(peer.Email, peer.Key)
	var set map[string]teamDoc
	dg := ""
	if known {
		set = d.replicaSet(h)
		dg = digest(set)
	}
	var theirs syncproto.TeamSync
	mine := func() syncproto.TeamSync {
		return syncproto.TeamSync{Type: "team_sync", Team: team, Heads: h.st.Heads, Digest: dg}
	}
	if responder {
		if err := first.Decode(&theirs); err != nil {
			return res, err
		}
		if err := ch.Send(mine()); err != nil {
			return res, err
		}
	} else {
		if !h.isMember(peer.Email, peer.Key) {
			return res, fmt.Errorf("%s is not a member of %s under this key", peer.Email, h.dir.Name)
		}
		if err := ch.Send(mine()); err != nil {
			return res, err
		}
		if err := recv("team_sync", &theirs); err != nil {
			return res, err
		}
	}
	if theirs.Team != team {
		end("invalid")
		return res, errors.New("the peer answered for another team")
	}
	if !known && slices.Equal(theirs.Heads, h.st.Heads) {
		end(syncproto.ByeNotMember) // its operations cannot add it
		return res, fmt.Errorf("%s is not a member of %s", peer.Email, h.dir.Name)
	}
	if known && slices.Equal(theirs.Heads, h.st.Heads) && theirs.Digest == dg {
		res.Equal = true
		if responder {
			_ = recv("bye", &struct{}{})
		} else {
			end(syncproto.ByeDone)
		}
		return res, nil
	}

	// Log operations first. Each side sends only to someone it counts as
	// a member; the responder checks the initiator after merging, so a
	// newly added member can bring the operation that adds them.
	var tl syncproto.TeamLog
	sendLog := func() error {
		return ch.Send(syncproto.TeamLog{Type: "team_log", Team: team, Ops: missingOps(h, theirs.Heads)})
	}
	takeLog := func() error {
		if err := recv("team_log", &tl); err != nil {
			return err
		}
		if !known && len(tl.Ops) > maxStrangerOps {
			end(syncproto.ByeNotMember)
			return fmt.Errorf("%s, not a member, sent %d log operations", peer.Email, len(tl.Ops))
		}
		n, err := d.mergeOps(h, tl.Ops)
		res.OpsIn = n
		return err
	}
	if responder {
		if err := takeLog(); err != nil {
			return res, err
		}
		if !h.isMember(peer.Email, peer.Key) {
			end(syncproto.ByeNotMember)
			return res, fmt.Errorf("%s is not a member of %s", peer.Email, h.dir.Name)
		}
		if err := sendLog(); err != nil {
			return res, err
		}
	} else {
		if err := sendLog(); err != nil {
			return res, err
		}
		if err := takeLog(); err != nil {
			return res, err
		}
		if !h.isMember(peer.Email, peer.Key) {
			end(syncproto.ByeNotMember)
			return res, fmt.Errorf("%s is no longer a member of %s", peer.Email, h.dir.Name)
		}
	}
	if res.OpsIn > 0 {
		d.afterLogChange(h)
	}
	set = d.replicaSet(h) // the log may have changed what counts

	// Inventories, then wants, initiator first in each pair.
	var inv syncproto.TeamInv
	var tw syncproto.TeamWant
	myInv := syncproto.TeamInv{Type: "team_inv", Team: team, Items: inventory(set)}
	steps := []func() error{
		func() error { return ch.Send(myInv) },
		func() error { return recv("team_inv", &inv) },
	}
	if responder {
		steps[0], steps[1] = steps[1], steps[0]
	}
	for _, f := range steps {
		if err := f(); err != nil {
			return res, err
		}
	}
	myWants := wants(set, inv.Items)
	steps = []func() error{
		func() error { return ch.Send(syncproto.TeamWant{Type: "team_want", Team: team, IDs: myWants}) },
		func() error { return recv("team_want", &tw) },
	}
	if responder {
		steps[0], steps[1] = steps[1], steps[0]
	}
	for _, f := range steps {
		if err := f(); err != nil {
			return res, err
		}
	}
	if len(tw.IDs) > 1000 {
		end("invalid")
		return res, errors.New("the peer asked for too many documents")
	}

	give := func() error {
		for _, id := range tw.IDs {
			doc := syncproto.TeamDoc{Type: "team_doc", Team: team, KalkaID: id}
			if td, ok := set[strings.ToLower(id)]; ok && td.pending {
				// A pending document travels as signed (spec 20.7).
				doc.Filename, doc.Content, doc.Pending = filepath.Base(td.path), string(td.raw), true
				res.Sent = append(res.Sent, td.item.ID)
			} else if ok {
				if rep, err := merge.Replica(td.doc, h.team()); err == nil {
					doc.Filename, doc.Content = filepath.Base(td.path), string(rep.Bytes())
					res.Sent = append(res.Sent, td.item.ID)
				}
			}
			// Anything else, a Personal document included, gets the
			// answer for a document that does not exist.
			if err := ch.Send(doc); err != nil {
				return err
			}
		}
		return nil
	}
	take := func() error {
		for range myWants {
			var off syncproto.TeamDoc
			if err := recv("team_doc", &off); err != nil {
				return err
			}
			if off.Team != team || !slices.Contains(myWants, off.KalkaID) {
				return fmt.Errorf("%s sent a document that was not asked for", peer.Email)
			}
			if off.Content == "" {
				res.Unavailable++
				continue
			}
			apply := d.applyReplica
			if off.Pending {
				apply = d.applyPending
			}
			written, ref := apply(h, peer.Email, off)
			if ref != nil {
				res.Refused = append(res.Refused, *ref)
			}
			if written {
				res.Received = append(res.Received, off.KalkaID)
			}
		}
		return nil
	}
	steps = []func() error{give, take}
	if responder {
		steps = []func() error{take, give}
	}
	for _, f := range steps {
		if err := f(); err != nil {
			return res, err
		}
	}
	if responder {
		_ = recv("bye", &struct{}{})
	} else {
		end(syncproto.ByeDone)
	}
	// A reassignment to the local user waits for the releases up to its
	// cut-off (spec 7.3); a document received after the log can complete
	// them.
	for _, id := range res.Received {
		if as := h.st.Reassigned[strings.ToLower(id)]; len(as) > 0 && bp.SameEmail(as[len(as)-1].Owner, d.self.Email) {
			d.takeReassigned(h, strings.ToLower(id), "")
		}
	}
	return res, nil
}

// applyReplica merges one received document (spec 20.4) and writes it, or
// reports why it was refused. Nothing that fails a check is written. When
// the entries are taken but the incoming plan is not, because its release
// does not count, it writes and also reports why (spec 20.4: each peer
// says why it did not apply a release).
func (d *Node) applyReplica(h *held, from string, off syncproto.TeamDoc) (bool, *Refusal) {
	refuse := func(title, reason string) (bool, *Refusal) {
		return false, &Refusal{KalkaID: off.KalkaID, Title: title, From: from, Reason: reason, At: d.now().UTC()}
	}
	in, err := bp.Parse([]byte(off.Content))
	if err != nil {
		return refuse("", "damaged file: "+err.Error())
	}
	if in.KalkaID() != strings.ToLower(off.KalkaID) {
		return refuse(in.Title(), "the file's kalka_id is not the one asked for")
	}
	if in.Team() != h.dir.Name {
		return refuse(in.Title(), "the file is not published to this team")
	}
	id := in.KalkaID()
	unlock, ok := d.Store.Lock(id, syncproto.LockWait)
	if !ok {
		return refuse(in.Title(), "the document is busy in another sync; it comes again in the next round")
	}
	defer unlock()
	for attempt := 0; attempt < 3; attempt++ {
		set := d.replicaSet(h)
		var local *bp.Document
		var live *store.File
		if td, ok := set[id]; ok && td.pending {
			// A pending copy: the released document replaces it.
		} else if ok {
			local = td.doc
			if !td.tomb {
				live, _ = d.Store.Load(td.path)
			}
		} else if f, err := d.Store.FindIn(h.dir.Dir, id); err == nil && f != nil {
			local, live = f.Doc, f
		}
		if live != nil {
			local = live.Doc
		}
		team := h.team()
		res := merge.Docs(local, in, team)
		var planRef *Refusal
		if res.Outcome != merge.Rejected && local != nil {
			if why := voidPlan(local, in, res.Doc, team); why != "" {
				_, planRef = refuse(in.Title(), "the plan was not taken: "+why)
			}
		}
		switch res.Outcome {
		case merge.Rejected:
			return refuse(in.Title(), res.Detail)
		case merge.Unchanged:
			return false, planRef
		}
		doc := res.Doc
		if doc.Deleted() {
			if err := d.writeTombstone(h, doc, live); err != nil {
				return refuse(in.Title(), err.Error())
			}
			d.logf("%s was deleted for team %s (from %s)", in.Title(), h.dir.Name, from)
			return true, nil
		}
		if td, ok := set[id]; ok && td.tomb && !td.pending {
			return false, nil // a tombstone is final
		}
		var path string
		if live != nil {
			path = live.Path
			var kept string
			if res.PlanFromIncoming {
				kept = d.keepDisplaced(h, live.Doc, doc, path)
			}
			err = d.Store.Update(live, doc)
			if errors.Is(err, fsutil.ErrChanged) {
				if kept != "" {
					_ = os.Remove(kept) // taken again on the next attempt
				}
				continue
			}
		} else {
			if err = os.MkdirAll(h.dir.Dir, 0o755); err == nil {
				path, err = d.Store.CreateIn(h.dir.Dir, doc, strings.TrimSuffix(off.Filename, ".md"))
			}
		}
		if err != nil {
			return refuse(in.Title(), err.Error())
		}
		d.snapshotHead(h, path, doc)
		d.notePushed(id)
		d.dropPending(h, id)
		var before *bp.Document
		if live != nil {
			before = live.Doc
			d.commentNotice(live.Doc, doc, path)
		}
		d.handoffNotice(h, before, doc, path)
		d.domainNotice(h, doc)
		return true, planRef
	}
	return refuse(in.Title(), "the file kept changing while it was merged")
}

// voidPlan explains why an incoming plan that differs from the local one
// was not taken: the reason its release does not count. It is "" when the
// plan was taken, or when it simply is not newer.
func voidPlan(local, in, merged *bp.Document, team merge.Team) string {
	ih := in.PlanHash()
	if ih == local.PlanHash() || merged == nil || merged.PlanHash() == ih {
		return ""
	}
	ch := merge.Releases(entriesOf(merged), in.KalkaID(), team)
	for _, e := range entriesOf(in) {
		if r, ok := e.Release(); ok && r.Plan == ih {
			if why, void := ch.Void[e.ID]; void {
				return why
			}
		}
	}
	return ""
}

// snapshotHead keeps the released version of a domain document beside it
// (spec 6.12) when the file holds the head's plan.
func (d *Node) snapshotHead(h *held, path string, doc *bp.Document) {
	if !doc.IsDomain() {
		return
	}
	ch := merge.Releases(entriesOf(doc), doc.KalkaID(), h.team())
	if head, ok := ch.Head(); ok && head.Release.Plan == doc.PlanHash() {
		if _, err := d.Store.SaveSnapshot(path, head.Release.Version, doc); err != nil {
			d.logf("snapshot of %s v%d: %v", path, head.Release.Version, err)
		}
	}
}

// writeTombstone stores a deletion stub and moves the last full copy to
// the team's trash (spec 20.6).
func (d *Node) writeTombstone(h *held, stub *bp.Document, live *store.File) error {
	if err := os.MkdirAll(tombDir(h), 0o755); err != nil {
		return err
	}
	if err := fsutil.WriteAtomic(filepath.Join(tombDir(h), stub.KalkaID()+".md"), stub.Bytes(), nil); err != nil {
		return err
	}
	if live != nil {
		if err := os.MkdirAll(trashDir(h), 0o755); err != nil {
			return err
		}
		base := strings.TrimSuffix(filepath.Base(live.Path), ".md")
		dst := filepath.Join(trashDir(h), fmt.Sprintf("%s.%s.md", base, stub.KalkaID()[:8]))
		if err := fsutil.WriteAtomic(dst, live.Raw, nil); err != nil {
			return err
		}
		if err := os.Remove(live.Path); err != nil {
			return err
		}
		d.Store.Forget(live.Path)
	}
	d.bus.Publish(BusEvent{Type: "teams"})
	return nil
}

// --- scheduling (spec 20.3) ---

// TeamStatus is the replication status line of one team.
type TeamStatus struct {
	LastRound time.Time `json:"last_round,omitzero"`
	Reached   []string  `json:"reached"`
	Tried     int       `json:"tried"`
	Error     string    `json:"error,omitempty"`
	Refused   []Refusal `json:"refused"`
	Received  int       `json:"received"`
}

type replication struct {
	mu      sync.Mutex
	status  map[string]*TeamStatus
	lastRun map[string]time.Time
	push    map[string]bool // teams with a pending push
	kick    chan string
	pushed  map[string]time.Time // documents written by replication, to not push them back at once
	running map[string]bool
	ended   map[string]chan struct{} // closed when the team's running round ends
}

func newReplication() *replication {
	return &replication{status: map[string]*TeamStatus{}, lastRun: map[string]time.Time{}, push: map[string]bool{},
		kick: make(chan string, 64), pushed: map[string]time.Time{}, running: map[string]bool{},
		ended: map[string]chan struct{}{}}
}

func (d *Node) notePushed(id string) {
	d.repl.mu.Lock()
	d.repl.pushed[id] = time.Now()
	d.repl.mu.Unlock()
}

// kickTeam asks for a round of team (or "" for all), soon.
func (d *Node) kickTeam(team string) {
	select {
	case d.repl.kick <- team:
	default:
	}
}

// pushTeam asks for a push of a local change.
func (d *Node) pushTeam(team string) {
	d.repl.mu.Lock()
	d.repl.push[team] = true
	d.repl.mu.Unlock()
	time.AfterFunc(PushDelay, func() { d.kickTeam(team) })
}

// replicateLoop runs rounds at start, on kicks, and every ReplicateEvery
// with jitter.
func (d *Node) replicateLoop(ctx context.Context) {
	folder, stop := d.Store.Subscribe()
	defer stop()
	next := func() <-chan time.Time {
		j := time.Duration(rand.Int64N(int64(ReplicateEvery/5) + 1))
		return time.After(ReplicateEvery - ReplicateEvery/10 + j)
	}
	tick := next()
	start := time.After(2 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-start:
			d.background(func() { d.replicateAll(d.work, false) })
		case <-tick:
			tick = next()
			d.background(func() { d.replicateAll(d.work, false) })
		case team := <-d.repl.kick:
			if team == "" {
				d.background(func() { d.replicateAll(d.work, false) })
			} else {
				d.background(func() { d.round(d.work, team, false) })
			}
		case ev := <-folder:
			d.localChange(ev.Path)
		}
	}
}

// localChange pushes a change to a published document, unless replication
// itself just wrote it.
func (d *Node) localChange(path string) {
	f, err := d.Store.Load(path)
	if err != nil || f.Doc.Team() == "" {
		return
	}
	d.repl.mu.Lock()
	at, recent := d.repl.pushed[f.Doc.KalkaID()]
	d.repl.mu.Unlock()
	if recent && time.Since(at) < 5*time.Second {
		return
	}
	if h, err := d.heldAt(f.Path); err == nil && f.Doc.Team() == h.dir.Name {
		d.pushTeam(h.dir.Team)
	}
}

func (d *Node) replicateAll(ctx context.Context, force bool) {
	for _, t := range d.teamDirs() {
		d.round(ctx, t.Team, force)
	}
}

// ReplicateNow runs one round of team now, for tests and the UI's
// "Replicate now".
func (d *Node) ReplicateNow(ctx context.Context, team string) (TeamStatus, error) {
	if team == "" {
		return TeamStatus{}, errors.New("no team given")
	}
	if d.Paused() {
		return TeamStatus{}, ErrPaused
	}
	// A round of the team already running (a push, the periodic round)
	// would make this one a no-op with no status yet: wait for it to end,
	// then run.
	for {
		busy := d.round(ctx, team, true)
		if busy == nil {
			break
		}
		select {
		case <-busy:
		case <-ctx.Done():
			return TeamStatus{}, ctx.Err()
		}
	}
	d.repl.mu.Lock()
	defer d.repl.mu.Unlock()
	st := d.repl.status[team]
	if st == nil {
		return TeamStatus{}, errors.New("no round ran")
	}
	return *st, nil
}

// endRound marks the team's round finished. A push asked for while it
// ran found it running and returned; it is kicked again here, or it would
// wait for the next periodic round (ReplicateEvery).
func (d *Node) endRound(ctx context.Context, team string) {
	r := d.repl
	r.mu.Lock()
	r.running[team] = false
	if ch := r.ended[team]; ch != nil {
		close(ch)
		delete(r.ended, team)
	}
	again := r.push[team]
	r.mu.Unlock()
	if again && ctx.Err() == nil && !d.isStopping() {
		d.kickTeam(team)
	}
}

// round reaches up to RoundPeers online members (PushPeers for a push).
// When a round of the team is already running it does nothing and
// returns a channel closed when that round ends; otherwise nil.
func (d *Node) round(ctx context.Context, team string, force bool) (busy <-chan struct{}) {
	if d.Paused() {
		return nil // a queued push stays queued for the resume
	}
	r := d.repl
	r.mu.Lock()
	push := r.push[team]
	if r.running[team] {
		ch := r.ended[team]
		r.mu.Unlock()
		return ch
	}
	if !force && !push && time.Since(r.lastRun[team]) < RoundGap {
		r.mu.Unlock()
		return nil
	}
	r.running[team], r.push[team], r.lastRun[team] = true, false, time.Now()
	r.ended[team] = make(chan struct{})
	r.mu.Unlock()
	defer d.endRound(ctx, team)

	h, err := d.heldTeam(team)
	if err != nil || !h.isMember(d.self.Email, d.pub) {
		return nil
	}
	limit := RoundPeers
	if push {
		limit = PushPeers
	}
	var others []string
	for email := range h.st.Members {
		if !bp.SameEmail(email, d.self.Email) {
			others = append(others, email)
		}
	}
	rand.Shuffle(len(others), func(i, j int) { others[i], others[j] = others[j], others[i] })
	st := TeamStatus{Reached: []string{}, Refused: []Refusal{}}
	var lastErr error
	for _, email := range others {
		if len(st.Reached) >= limit || ctx.Err() != nil {
			break
		}
		key, ok := h.memberKey(email)
		if !ok {
			continue
		}
		st.Tried++
		if d.isStopping() {
			break // the peers left are tried at the next start
		}
		res, err := d.replicateWith(ctx, h, email, key)
		if err != nil {
			if d.isStopping() {
				break
			}
			var off *ErrOffline
			if !errors.As(err, &off) {
				lastErr = err
				d.logf("replication of %s with %s: %v", h.dir.Name, email, err)
			}
			continue
		}
		d.noteSync()
		st.Reached = append(st.Reached, email)
		st.Refused = append(st.Refused, res.Refused...)
		st.Received += len(res.Received)
	}
	st.LastRound = d.now().UTC()
	if lastErr != nil && len(st.Reached) == 0 {
		st.Error = lastErr.Error()
	}
	r.mu.Lock()
	if old := r.status[team]; old != nil && len(st.Refused) == 0 {
		st.Refused = old.Refused // keep the last refusals visible until a round has new ones
	}
	if len(st.Refused) > 20 {
		st.Refused = st.Refused[len(st.Refused)-20:]
	}
	r.status[team] = &st
	r.mu.Unlock()
	d.bus.Publish(BusEvent{Type: "teams"})
	return nil
}

// replicateWith runs one session with a member, dialing the key the log
// binds to them (spec 8.2, 9.2).
func (d *Node) replicateWith(ctx context.Context, h *held, email string, key ed25519.PublicKey) (SessionResult, error) {
	ep, err := d.endpoint()
	if err != nil {
		return SessionResult{}, err
	}
	dctx, cancel := context.WithTimeout(ctx, DialTimeout)
	if d.stopping != nil {
		context.AfterFunc(d.stopping, cancel) // a stop drains sessions, not dials
	}
	c, err := ep.Dial(dctx, transport.KeyPeerID(key))
	cancel()
	if errors.Is(err, transport.ErrOffline) {
		d.markPresence(email, transport.Offline)
		return SessionResult{}, &ErrOffline{Email: email}
	}
	if err != nil {
		return SessionResult{}, err
	}
	defer c.Close()
	d.markPresence(email, transport.Online)
	verify := func(e string, k ed25519.PublicKey) error {
		if !bp.SameEmail(e, email) || !k.Equal(key) {
			return fmt.Errorf("the peer is not %s with the key the team log names", email)
		}
		return nil
	}
	peer, err := transport.Handshake(c, d.self, verify)
	if err != nil {
		return SessionResult{}, err
	}
	return d.exchange(c, peer, h.dir.Team, nil)
}

// replicaRespond answers an incoming team_sync.
func (d *Node) replicaRespond(c *transport.Conn, peer transport.Peer, first transport.Envelope) {
	var ts syncproto.TeamSync
	_ = first.Decode(&ts)
	res, err := d.exchange(c, peer, ts.Team, &first)
	if err != nil {
		d.logf("replication from %s: %v", peer.Email, err)
		return
	}
	d.noteSync()
	if len(res.Received) > 0 || len(res.Refused) > 0 {
		d.logf("replication from %s: %d received, %d refused", peer.Email, len(res.Received), len(res.Refused))
		d.repl.mu.Lock()
		st := d.repl.status[ts.Team]
		if st == nil {
			st = &TeamStatus{Reached: []string{}, Refused: []Refusal{}}
			d.repl.status[ts.Team] = st
		}
		st.Refused = append(st.Refused, res.Refused...)
		st.Received += len(res.Received)
		d.repl.mu.Unlock()
		d.bus.Publish(BusEvent{Type: "teams"})
	}
}

// replStatus returns the status line of team.
func (d *Node) replStatus(team string) *TeamStatus {
	d.repl.mu.Lock()
	defer d.repl.mu.Unlock()
	if st := d.repl.status[team]; st != nil {
		c := *st
		return &c
	}
	return nil
}
