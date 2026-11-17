package teamlog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
)

// State is the result of a replay: a pure function of the set of known
// operations (spec 19.4, docs/teamlog.md).
type State struct {
	Team string // the team hash
	Name string
	// Members are the current members by email.
	Members map[string]Member
	// Owners are the current owners' emails, most senior first.
	Owners []string
	// Keys lists every key the log has ever bound to an email, in replay
	// order, so entries of removed members can still be verified (20.4).
	Keys map[string][]string
	// Terms maps the hash of each create-team and add-member that bound a
	// key (each in Keys) to the email and key it bound: what an entry's
	// term: field names (spec 6.5).
	Terms map[string]Term
	// Reassigned maps a kalka_id to its anchors: every effective
	// reassign-document of it, and an effective accept-transfer at cut-off
	// 0, ordered by cut-off, one per cut-off (rule 8).
	Reassigned map[string][]Assignment
	// Transfers maps a transfer digest to its effective decision (19.6).
	Transfers map[string]Decision
	// Invites maps a used invite id to the add-member that used it.
	Invites map[string]string
	// Effective holds the hashes of the operations that count.
	Effective map[string]bool
	// Discarded lists the operations that do not count, with the reason,
	// in replay order.
	Discarded []Discard
	// Pending lists operations held but not replayed: a predecessor is
	// missing, or the rule version is not supported.
	Pending []Discard
	// Heads are the hashes no other known operation names in prev; a new
	// operation names them.
	Heads []string
}

// Term is one binding of a key to an email.
type Term struct {
	Email  string
	PubKey string
}

// Member is a current member.
type Member struct {
	Email  string `json:"email"`
	PubKey string `json:"pubkey"`
	Since  string `json:"since"` // the add-member (or create-team) hash
	Owner  bool   `json:"owner,omitempty"`
}

// Assignment is an effective reassign-document, or an accept-transfer
// (Transfer set, After 0): Owner is the only valid signer of release
// After+1 (spec 20.4).
type Assignment struct {
	Op       string `json:"op"`
	Owner    string `json:"owner"`
	From     string `json:"from,omitempty"`
	After    int64  `json:"after"`
	At       string `json:"at,omitempty"`
	Transfer string `json:"transfer,omitempty"`
	By       string `json:"by,omitempty"` // who signed the operation
}

// Decision is the effective decision on one transfer offer: an accept, or
// else the first decline (rule 8).
type Decision struct {
	Op       string `json:"op"`
	Doc      string `json:"doc"`
	Accepted bool   `json:"accepted"`
	Owner    string `json:"owner,omitempty"` // accept: the owner of version 1
	By       string `json:"by"`              // the deciding team owner
	At       string `json:"at,omitempty"`
}

// Discard explains why an operation does not count.
type Discard struct {
	Hash   string `json:"hash"`
	Kind   string `json:"op"`
	Reason string `json:"reason"`
}

// Rejected is an operation Replay would not consider at all.
type Rejected struct {
	Op     Op
	Reason string
}

// Replay verifies every signature and replays the operations of team.
// Operations of another team are skipped; operations with a bad signature
// are returned as rejected and do not count as known (19.1).
func Replay(team string, ops []Op) (*State, []Rejected) {
	var good []Op
	var bad []Rejected
	for _, o := range ops {
		if o.Team != team && !(o.Kind == CreateTeam && o.Team == "") {
			continue
		}
		if !o.VerifySig() {
			bad = append(bad, Rejected{o, "bad signature"})
			continue
		}
		good = append(good, o)
	}
	return replay(team, good, ""), bad
}

// replay is Replay over operations whose signatures were checked. salt,
// when set, changes only the order in which concurrent operations are
// visited; the harness uses it to show that no outcome beyond rule 8
// depends on that order.
func replay(team string, ops []Op, salt string) *State {
	r := newReplayer(team, ops, salt)
	return r.run()
}

// ---- graph ----

type node struct {
	op     Op
	hash   string
	depth  int
	prev   []int
	sorter string // hash, or salted hash
}

type bitset []uint64

func (b bitset) has(i int) bool { return b[i>>6]&(1<<(uint(i)&63)) != 0 }
func (b bitset) set(i int)      { b[i>>6] |= 1 << (uint(i) & 63) }
func (b bitset) or(c bitset) {
	for i := range b {
		b[i] |= c[i]
	}
}

type replayer struct {
	team    string
	nodes   []*node // topological: by depth, then sorter
	idx     map[string]int
	anc     []bitset // strict ancestors
	pending []Discard
	heads   []string

	// structural caches (independent of effectiveness)
	final *eval // the evaluation the state came from, for the harness

	skeyMemo map[int]*seniority
	grants   map[int][]int
	email    map[int]string
}

func newReplayer(team string, ops []Op, salt string) *replayer {
	r := &replayer{team: team, idx: map[string]int{}, skeyMemo: map[int]*seniority{}, grants: map[int][]int{}, email: map[int]string{}}
	byHash := map[string]Op{}
	for _, o := range ops {
		h := o.Hash()
		if o.Kind == CreateTeam && h != team {
			continue // another team's root
		}
		byHash[h] = o
	}
	// Heads: hashes named by nobody, among everything held.
	named := map[string]bool{}
	for _, o := range byHash {
		for _, p := range o.Prev {
			named[p] = true
		}
	}
	for h := range byHash {
		if !named[h] {
			r.heads = append(r.heads, h)
		}
	}
	sort.Strings(r.heads)

	// Depth by memoized recursion; ops reaching a missing or unsupported
	// predecessor are pending.
	depth := map[string]int{}
	state := map[string]int{} // 0 new, 1 visiting, 2 done-ok, 3 pending
	why := map[string]string{}
	var visit func(h string) bool
	visit = func(h string) bool {
		switch state[h] {
		case 1:
			return false // a cycle cannot happen with hashes; be safe
		case 2:
			return true
		case 3:
			return false
		}
		o, ok := byHash[h]
		if !ok {
			return false
		}
		state[h] = 1
		if o.V != RuleVersion {
			state[h], why[h] = 3, fmt.Sprintf("made by a newer kalka (rule version %d); update kalka to read it", o.V)
			return false
		}
		d := 0
		for _, p := range o.Prev {
			if !visit(p) {
				state[h] = 3
				if _, held := byHash[p]; held {
					why[h] = "waits for " + short(p) + ", which is pending"
				} else {
					why[h] = "waits for " + short(p)
				}
				return false
			}
			if depth[p]+1 > d {
				d = depth[p] + 1
			}
		}
		depth[h] = d
		state[h] = 2
		return true
	}
	var ready []*node
	for h, o := range byHash {
		if visit(h) {
			s := h
			if salt != "" {
				sum := sha256.Sum256([]byte(salt + h))
				s = hex.EncodeToString(sum[:])
			}
			ready = append(ready, &node{op: o, hash: h, depth: depth[h], sorter: s})
		} else {
			r.pending = append(r.pending, Discard{h, o.Kind, why[h]})
		}
	}
	sort.Slice(r.pending, func(i, j int) bool { return r.pending[i].Hash < r.pending[j].Hash })
	sort.Slice(ready, func(i, j int) bool {
		if ready[i].depth != ready[j].depth {
			return ready[i].depth < ready[j].depth
		}
		return ready[i].sorter < ready[j].sorter
	})
	r.nodes = ready
	words := (len(ready) + 63) / 64
	for i, n := range ready {
		r.idx[n.hash] = i
	}
	r.anc = make([]bitset, len(ready))
	for i, n := range ready {
		b := make(bitset, words)
		for _, p := range n.op.Prev {
			pi := r.idx[p]
			n.prev = append(n.prev, pi)
			b.set(pi)
			b.or(r.anc[pi])
		}
		r.anc[i] = b
	}
	return r
}

// before reports whether a is a strict ancestor of b.
func (r *replayer) before(a, b int) bool { return r.anc[b].has(a) }

// upTo reports whether a is b or an ancestor of b.
func (r *replayer) upTo(a, b int) bool { return a == b || r.anc[b].has(a) }

func isRemoval(k string) bool { return k == RemoveMember || k == RemoveOwner }

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// ---- structural seniority (docs/teamlog.md, "Seniority") ----

// seniority orders owners: lexicographic over the chain of owner grants
// from the creator down; a shorter chain that is a prefix comes first, so
// an owner always ranks above everyone they appointed.
type seniority struct {
	none  bool
	chain []step
}

type step struct {
	depth int
	hash  string
}

func (a *seniority) less(b *seniority) bool {
	if a.none != b.none {
		return !a.none
	}
	for i := 0; i < len(a.chain) && i < len(b.chain); i++ {
		x, y := a.chain[i], b.chain[i]
		if x.depth != y.depth {
			return x.depth < y.depth
		}
		if x.hash != y.hash {
			return x.hash < y.hash
		}
	}
	return len(a.chain) < len(b.chain)
}

// memberKeyAt is the key bound to email by the latest add-member (or the
// create-team) visible from i, structurally.
func (r *replayer) memberKeyAt(email string, i int) string {
	key, best := "", -1
	for j := 0; j <= i; j++ {
		if !r.upTo(j, i) {
			continue
		}
		o := r.nodes[j].op
		if (o.Kind == AddMember || o.Kind == CreateTeam) && o.Args.Email == email && j > best {
			best = j
			if o.Kind == AddMember {
				key = o.Args.PubKey
			} else {
				key = o.Signer
			}
		}
	}
	return key
}

// signerEmail is the email bound to i's signer key, structurally.
func (r *replayer) signerEmail(i int) string {
	if e, ok := r.email[i]; ok {
		return e
	}
	o := r.nodes[i].op
	e, best := "", -1
	for j := 0; j <= i; j++ {
		if !r.upTo(j, i) {
			continue
		}
		p := r.nodes[j].op
		if p.Kind == CreateTeam && p.Signer == o.Signer && j > best {
			e, best = p.Args.Email, j
		}
		if p.Kind == AddMember && p.Args.PubKey == o.Signer && j > best {
			e, best = p.Args.Email, j
		}
	}
	r.email[i] = e
	return e
}

// ownerGrants are the owner grants of i's signer visible from i and not
// followed by a removal of that email visible from i: the grants of the
// signer's current owner term in i's view, structurally.
func (r *replayer) ownerGrants(i int) []int {
	if g, ok := r.grants[i]; ok {
		return g
	}
	o := r.nodes[i].op
	var out []int
	if o.Kind == CreateTeam {
		out = []int{i}
	} else if e := r.signerEmail(i); e != "" {
		for j := 0; j < i; j++ {
			if !r.before(j, i) {
				continue
			}
			p := r.nodes[j].op
			grant := (p.Kind == CreateTeam && p.Args.Email == e && p.Signer == o.Signer) ||
				(p.Kind == AddOwner && p.Args.Email == e && r.memberKeyAt(e, j) == o.Signer)
			if !grant {
				continue
			}
			ended := false
			for k := j + 1; k < i && !ended; k++ {
				q := r.nodes[k].op
				ended = isRemoval(q.Kind) && q.Args.Email == e && r.before(j, k) && r.before(k, i)
			}
			if !ended {
				out = append(out, j)
			}
		}
	}
	r.grants[i] = out
	return out
}

// skey is the structural seniority of i's signer.
func (r *replayer) skey(i int) *seniority {
	if s, ok := r.skeyMemo[i]; ok {
		return s
	}
	best := &seniority{none: true}
	for _, g := range r.ownerGrants(i) {
		var k *seniority
		if r.nodes[g].op.Kind == CreateTeam {
			k = &seniority{}
		} else {
			parent := r.skey(g)
			if parent.none {
				continue
			}
			k = &seniority{chain: append(append([]step(nil), parent.chain...), step{r.nodes[g].depth, r.nodes[g].hash})}
		}
		if k.less(best) {
			best = k
		}
	}
	r.skeyMemo[i] = best
	return best
}

// prio orders removals for rule 3: the signer's seniority, then the
// removal's own depth and hash.
func (r *replayer) prioLess(a, b int) bool {
	ka, kb := r.skey(a), r.skey(b)
	if ka.less(kb) {
		return true
	}
	if kb.less(ka) {
		return false
	}
	if r.nodes[a].depth != r.nodes[b].depth {
		return r.nodes[a].depth < r.nodes[b].depth
	}
	return r.nodes[a].hash < r.nodes[b].hash
}

// blocks reports whether removal a ends the owner term that removal b's
// signer signed b under, without having seen b (rule 3).
func (r *replayer) blocks(a, b int) bool {
	if a == b || r.before(b, a) {
		return false
	}
	e := r.signerEmail(b)
	if e == "" || r.nodes[a].op.Args.Email != e {
		return false
	}
	for _, g := range r.ownerGrants(b) {
		if r.before(g, a) {
			return true
		}
	}
	return false
}

// ---- evaluation ----

type mterm struct {
	email, key string
	grant      int
	enders     []int
}

type oterm struct {
	mt     *mterm
	grants []int
	enders []int
}

// allEnders are the removals that ended the owner term, directly or by
// ending the member term under it.
func (ot *oterm) allEnders() []int {
	return append(append([]int(nil), ot.enders...), ot.mt.enders...)
}

type eval struct {
	eff     []bool
	reason  []string
	used    []*oterm // the owner term each effective op was signed under
	mterms  map[string][]*mterm
	oterms  map[*mterm][]*oterm
	invites map[string]int
	ends    map[int][]*oterm // owner terms each effective removal ended
}

func (r *replayer) run() *State {
	removals := []int{}
	for i, n := range r.nodes {
		if isRemoval(n.op.Kind) {
			removals = append(removals, i)
		}
	}
	sort.Slice(removals, func(a, b int) bool { return r.prioLess(removals[a], removals[b]) })

	lost := map[int]string{}
	for {
		// Rule 3: removals in order of their signers' seniority; one is
		// blocked by an accepted removal of its signer it did not see.
		decided := map[int]string{} // removal -> "" (effective) or reason
		var accepted []int
		for _, R := range removals {
			if why, ok := lost[R]; ok {
				decided[R] = why
				continue
			}
			blocked := ""
			for _, A := range accepted {
				if r.blocks(A, R) {
					blocked = "signer was removed by " + short(r.nodes[A].hash) + " (a more senior owner, concurrently)"
					break
				}
			}
			decided[R] = blocked
			if blocked == "" {
				accepted = append(accepted, R)
			}
		}

		kills := map[int]string{}
		var e *eval
		restart := false
		for {
			e = r.evaluate(decided, kills)
			// A removal decided effective must be valid; otherwise it is
			// lost, and the decision runs again.
			bad := -1
			for _, R := range accepted {
				if !e.eff[R] && kills[R] == "" {
					bad = R
					break
				}
			}
			if bad >= 0 {
				lost[bad] = e.reason[bad]
				restart = true
				break
			}
			// Rule 2: an effective removal discards the removed owner's
			// operations it did not see. Removals by that owner are exempt
			// only if they rank above it (rule 3).
			grew := false
			for i := range r.nodes {
				if !e.eff[i] || e.used[i] == nil {
					continue
				}
				for _, R := range e.used[i].allEnders() {
					if r.upTo(i, R) {
						continue
					}
					if isRemoval(r.nodes[i].op.Kind) {
						if r.prioLess(R, i) {
							lost[i] = "signer was removed by " + short(r.nodes[R].hash) + " (a more senior owner, concurrently)"
							restart = true
						}
						continue
					}
					kills[i] = "signer was removed by " + short(r.nodes[R].hash)
					grew = true
					break
				}
			}
			if restart {
				break
			}
			if !grew {
				break
			}
		}
		if restart {
			continue
		}
		// Rule 6: never without an owner. The least senior effective
		// removal that ended an owner term is lost.
		if !e.hasOwner() {
			worst := -1
			for i := len(removals) - 1; i >= 0; i-- {
				R := removals[i]
				if e.eff[R] && len(e.ends[R]) > 0 {
					worst = R
					break
				}
			}
			if worst < 0 {
				break // no removal to blame: an empty log
			}
			lost[worst] = "would leave the team without an owner"
			continue
		}
		r.final = e
		return r.state(e)
	}
	r.final = r.evaluate(map[int]string{}, map[int]string{})
	return r.state(r.final)
}

func (e *eval) hasOwner() bool {
	for _, ots := range e.oterms {
		for _, ot := range ots {
			if len(ot.enders) == 0 && len(ot.mt.enders) == 0 {
				return true
			}
		}
	}
	return false
}

func (e *eval) currentMember(email string) *mterm {
	for _, mt := range e.mterms[email] {
		if len(mt.enders) == 0 {
			return mt
		}
	}
	return nil
}

func (e *eval) currentOwner(mt *mterm) *oterm {
	for _, ot := range e.oterms[mt] {
		if len(ot.allEnders()) == 0 {
			return ot
		}
	}
	return nil
}

// evaluate visits every operation once, in topological order, given the
// rule-3 decisions and the rule-2 kills found so far.
func (r *replayer) evaluate(decided map[int]string, kills map[int]string) *eval {
	n := len(r.nodes)
	e := &eval{eff: make([]bool, n), reason: make([]string, n), used: make([]*oterm, n),
		mterms: map[string][]*mterm{}, oterms: map[*mterm][]*oterm{},
		invites: map[string]int{}, ends: map[int][]*oterm{}}

	// The term i's signer acts under: an owner term granted in i's view
	// and not ended in i's view.
	auth := func(i int) *oterm {
		k := r.nodes[i].op.Signer
		for _, mts := range e.mterms {
			for _, mt := range mts {
				if mt.key != k {
					continue
				}
				for _, ot := range e.oterms[mt] {
					seen := false
					for _, g := range ot.grants {
						seen = seen || r.upTo(g, i)
					}
					if !seen {
						continue
					}
					endedInView := false
					for _, x := range ot.allEnders() {
						endedInView = endedInView || r.before(x, i)
					}
					if !endedInView {
						return ot
					}
				}
			}
		}
		return nil
	}
	// The member term of email visible from i: granted in i's view and
	// not ended in i's view.
	memberInView := func(email string, i int) *mterm {
		var best *mterm
		for _, mt := range e.mterms[email] {
			if !r.before(mt.grant, i) {
				continue
			}
			endedInView := false
			for _, x := range mt.enders {
				endedInView = endedInView || r.before(x, i)
			}
			if !endedInView && (best == nil || mt.grant > best.grant) {
				best = mt
			}
		}
		return best
	}

	for i, nd := range r.nodes {
		o := nd.op
		fail := func(why string) { e.reason[i] = why }
		if why, ok := kills[i]; ok {
			fail(why)
			continue
		}
		if o.Kind == CreateTeam {
			mt := &mterm{email: o.Args.Email, key: o.Signer, grant: i}
			e.mterms[mt.email] = append(e.mterms[mt.email], mt)
			ot := &oterm{mt: mt, grants: []int{i}}
			e.oterms[mt] = append(e.oterms[mt], ot)
			e.eff[i], e.used[i] = true, ot
			continue
		}
		ot := auth(i)
		if ot == nil {
			fail("signer is not an owner")
			continue
		}
		self := ot.mt.email
		a := o.Args
		switch o.Kind {
		case AddMember:
			if memberInView(a.Email, i) != nil {
				fail(a.Email + " is already a member")
				continue
			}
			if e.currentMember(a.Email) != nil {
				fail(a.Email + " was added concurrently by another operation")
				continue
			}
			inUse := false
			for _, mts := range e.mterms {
				for _, mt := range mts {
					inUse = inUse || (len(mt.enders) == 0 && mt.key == a.PubKey)
				}
			}
			if inUse {
				fail("the key is already bound to a member")
				continue
			}
			if a.Invite != "" {
				if _, used := e.invites[a.Invite]; used {
					fail("invite " + a.Invite + " was already used")
					continue
				}
				e.invites[a.Invite] = i
			}
			mt := &mterm{email: a.Email, key: a.PubKey, grant: i}
			e.mterms[a.Email] = append(e.mterms[a.Email], mt)

		case RemoveMember, RemoveOwner:
			if a.Email == self {
				fail("an owner cannot remove themselves")
				continue
			}
			var ended []*oterm
			if o.Kind == RemoveMember {
				mt := memberInView(a.Email, i)
				if mt == nil {
					fail(a.Email + " is not a member")
					continue
				}
				if why := decided[i]; why != "" {
					fail(why)
					continue
				}
				mt.enders = append(mt.enders, i)
				for _, t := range e.oterms[mt] {
					t.enders = append(t.enders, i)
					ended = append(ended, t)
				}
			} else {
				var targets []*oterm
				if mt := memberInView(a.Email, i); mt != nil {
					targets = r.ownerTermsInView(e, mt, i)
				}
				if len(targets) == 0 {
					fail(a.Email + " is not an owner")
					continue
				}
				if why := decided[i]; why != "" {
					fail(why)
					continue
				}
				for _, t := range targets {
					t.enders = append(t.enders, i)
				}
				ended = targets
			}
			e.ends[i] = ended

		case AddOwner:
			// Judged in the signer's view: a grant joins the owner term
			// it saw, so a concurrent remove-owner ends it whatever the
			// visiting order.
			mt := memberInView(a.Email, i)
			if mt == nil {
				fail(a.Email + " is not a member")
				continue
			}
			if seen := r.ownerTermsInView(e, mt, i); len(seen) > 0 {
				seen[0].grants = append(seen[0].grants, i)
			} else {
				e.oterms[mt] = append(e.oterms[mt], &oterm{mt: mt, grants: []int{i}})
			}

		case ReassignDocument:
			// Rule 9: no removal needed, from is not judged.
			if memberInView(a.Owner, i) == nil {
				fail(a.Owner + " is not a member")
				continue
			}
			// Rule 8: a cut-off below one the signer already saw is stale.
			stale := false
			for j := 0; j < i && !stale; j++ {
				q := r.nodes[j].op
				stale = e.eff[j] && q.Kind == ReassignDocument && q.Args.Doc == a.Doc && q.Args.After > a.After && r.before(j, i)
			}
			if stale {
				fail("stale cut-off: a later reassignment of this document was already seen")
				continue
			}

		case AcceptTransfer:
			// Rule 10; rule 8: one accept per transfer, and per document.
			if memberInView(a.Owner, i) == nil {
				fail(a.Owner + " is not a member")
				continue
			}
			decided := false
			for j := 0; j < i && !decided; j++ {
				q := r.nodes[j].op
				decided = e.eff[j] && q.Kind == AcceptTransfer && (q.Args.Transfer == a.Transfer || q.Args.Doc == a.Doc)
			}
			if decided {
				fail("already decided: another accept of this transfer counts")
				continue
			}
		}
		e.eff[i], e.used[i] = true, ot
	}
	// Rule 8: an accept beats a decline of the same transfer, whichever
	// is visited first.
	accepted := map[string]bool{}
	for i, nd := range r.nodes {
		if e.eff[i] && nd.op.Kind == AcceptTransfer {
			accepted[nd.op.Args.Transfer] = true
		}
	}
	for i, nd := range r.nodes {
		if e.eff[i] && nd.op.Kind == DeclineTransfer && accepted[nd.op.Args.Transfer] {
			e.eff[i], e.used[i] = false, nil
			e.reason[i] = "already decided: an accept of this transfer counts"
		}
	}
	return e
}

// ownerTermsInView are mt's owner terms granted in i's view and not ended
// in i's view.
func (r *replayer) ownerTermsInView(e *eval, mt *mterm, i int) []*oterm {
	var out []*oterm
	for _, t := range e.oterms[mt] {
		seen, endedInView := false, false
		for _, g := range t.grants {
			seen = seen || r.before(g, i)
		}
		for _, x := range t.allEnders() {
			endedInView = endedInView || r.before(x, i)
		}
		if seen && !endedInView {
			out = append(out, t)
		}
	}
	return out
}

// ownerTermsUpTo is ownerTermsInView for the signer of i itself: terms
// granted before i and not ended before i.
func (r *replayer) ownerTermsUpTo(e *eval, mt *mterm, i int) []*oterm {
	var out []*oterm
	for _, t := range e.oterms[mt] {
		seen, ended := false, false
		for _, g := range t.grants {
			seen = seen || r.upTo(g, i)
		}
		for _, x := range t.allEnders() {
			ended = ended || r.before(x, i)
		}
		if seen && !ended {
			out = append(out, t)
		}
	}
	return out
}

func (r *replayer) state(e *eval) *State {
	s := &State{Team: r.team, Members: map[string]Member{}, Keys: map[string][]string{}, Terms: map[string]Term{},
		Reassigned: map[string][]Assignment{}, Transfers: map[string]Decision{}, Invites: map[string]string{},
		Effective: map[string]bool{}, Pending: r.pending, Heads: r.heads}
	if s.Pending == nil {
		s.Pending = []Discard{}
	}
	for i, nd := range r.nodes {
		if nd.op.Kind == CreateTeam {
			s.Name = nd.op.Args.Name
		}
		if e.eff[i] {
			s.Effective[nd.hash] = true
		} else {
			s.Discarded = append(s.Discarded, Discard{nd.hash, nd.op.Kind, e.reason[i]})
		}
	}
	type ranked struct {
		email string
		key   *seniority
	}
	var owners []ranked
	// Evaluated seniority: like skey, over effective grants only.
	var grantKey func(g int) *seniority
	grantKey = func(g int) *seniority {
		if r.nodes[g].op.Kind == CreateTeam {
			return &seniority{}
		}
		// The appointer's rank: the best of their owner terms in g's
		// view. Two concurrent grants make two terms; neither may depend
		// on which was visited first.
		best := &seniority{none: true}
		for _, ot := range r.ownerTermsUpTo(e, e.used[g].mt, g) {
			for _, pg := range ot.grants {
				if e.eff[pg] && r.before(pg, g) {
					if k := grantKey(pg); k.less(best) {
						best = k
					}
				}
			}
		}
		if best.none {
			return best
		}
		return &seniority{chain: append(append([]step(nil), best.chain...), step{r.nodes[g].depth, r.nodes[g].hash})}
	}
	for email, mts := range e.mterms {
		for _, mt := range mts {
			s.Keys[email] = append(s.Keys[email], mt.key)
			if len(mt.enders) > 0 {
				continue
			}
			m := Member{Email: email, PubKey: mt.key, Since: r.nodes[mt.grant].hash}
			if e.currentOwner(mt) != nil {
				m.Owner = true
				best := &seniority{none: true}
				for _, ot := range e.oterms[mt] {
					if len(ot.allEnders()) > 0 {
						continue
					}
					for _, g := range ot.grants {
						if k := grantKey(g); k.less(best) {
							best = k
						}
					}
				}
				owners = append(owners, ranked{email, best})
			}
			s.Members[email] = m
		}
	}
	// Keys in replay order of their grants.
	for email := range s.Keys {
		mts := e.mterms[email]
		sort.Slice(mts, func(a, b int) bool { return mts[a].grant < mts[b].grant })
		s.Keys[email] = s.Keys[email][:0]
		for _, mt := range mts {
			s.Keys[email] = append(s.Keys[email], mt.key)
			s.Terms[r.nodes[mt.grant].hash] = Term{Email: email, PubKey: mt.key}
		}
	}
	sort.Slice(owners, func(a, b int) bool {
		if owners[a].key.less(owners[b].key) {
			return true
		}
		if owners[b].key.less(owners[a].key) {
			return false
		}
		return owners[a].email < owners[b].email
	})
	for _, o := range owners {
		s.Owners = append(s.Owners, o.email)
	}
	// Decisions: the accept, or else the first decline.
	for i, nd := range r.nodes {
		k := nd.op.Kind
		if !e.eff[i] || (k != AcceptTransfer && k != DeclineTransfer) {
			continue
		}
		a := nd.op.Args
		if _, ok := s.Transfers[a.Transfer]; ok && k == DeclineTransfer {
			continue
		}
		s.Transfers[a.Transfer] = Decision{Op: nd.hash, Doc: a.Doc, Accepted: k == AcceptTransfer,
			Owner: a.Owner, By: e.used[i].mt.email, At: nd.op.At}
	}
	// Anchors: every effective reassignment and accept; of those with one
	// cut-off, the last in visiting order.
	for i, nd := range r.nodes {
		if !e.eff[i] || (nd.op.Kind != ReassignDocument && nd.op.Kind != AcceptTransfer) {
			continue
		}
		a := nd.op.Args
		as := s.Reassigned[a.Doc]
		x := Assignment{Op: nd.hash, Owner: a.Owner, From: a.From, After: a.After, At: nd.op.At, By: e.used[i].mt.email}
		if nd.op.Kind == AcceptTransfer {
			x = Assignment{Op: nd.hash, Owner: a.Owner, At: nd.op.At, Transfer: a.Transfer, By: e.used[i].mt.email}
		}
		k := sort.Search(len(as), func(j int) bool { return as[j].After >= a.After })
		if k < len(as) && as[k].After == a.After {
			as[k] = x
		} else {
			as = slices.Insert(as, k, x)
		}
		s.Reassigned[a.Doc] = as
	}
	for id, i := range e.invites {
		if e.eff[i] {
			s.Invites[id] = r.nodes[i].hash
		}
	}
	return s
}
