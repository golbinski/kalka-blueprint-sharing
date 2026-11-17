package teamlog

import (
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The convergence harness (docs/teamlog.md): random histories of honest and hostile
// operations, delivered in random order with partitions, checked against
// the invariants of docs/teamlog.md. KALKA_SIM_N sets the number of
// histories (a full run uses 100000); KALKA_SIM_SEED the first seed.

type simActor struct {
	actor
	ops   map[string]Op // this actor's replica
	st    *State        // cached replay of ops; nil after a change
	snaps [][]string    // earlier heads, for back-dating
}

type sim struct {
	rng    *rand.Rand
	team   string
	actors []*simActor
	all    map[string]Op
	log    []string // what happened, for a failure report
}

var simEmails = []string{"alice@example.org", "bob@example.org", "carol@example.org", "dave@example.org", "erin@example.org", "frank@example.org"}

func newSim(seed int64) *sim {
	s := &sim{rng: rand.New(rand.NewSource(seed)), all: map[string]Op{}}
	for _, e := range simEmails {
		s.actors = append(s.actors, &simActor{actor: newActor(e), ops: map[string]Op{}})
	}
	c := Create(s.actors[0].key, "parcels", s.actors[0].email, t0)
	s.team = c.Hash()
	s.actors[0].ops[s.team] = c
	s.all[s.team] = c
	// A starting team everyone holds: alice, then a random number of
	// members, some of them owners.
	alice := s.actors[0]
	sg := Signer{Key: alice.key, Team: s.team}
	members := 2 + s.rng.Intn(4)
	for i := 1; i <= members; i++ {
		b := s.actors[i]
		s.put(alice, sg.AddMember(Heads(opsOf(alice.ops)), b.email, b.pub, "", t0))
		if s.rng.Intn(3) > 0 {
			s.put(alice, sg.AddOwner(Heads(opsOf(alice.ops)), b.email, t0))
		}
	}
	for _, b := range s.actors[1:] {
		for h, o := range alice.ops {
			b.ops[h] = o
		}
		b.st = nil
	}
	return s
}

func opsOf(m map[string]Op) []Op {
	keys := make([]string, 0, len(m))
	for h := range m {
		keys = append(keys, h)
	}
	sort.Strings(keys)
	out := make([]Op, len(keys))
	for i, h := range keys {
		out[i] = m[h]
	}
	return out
}

func (s *sim) put(a *simActor, ops ...Op) {
	for _, o := range ops {
		h := o.Hash()
		a.ops[h] = o
		a.st = nil
		s.all[h] = o
		s.log = append(s.log, fmt.Sprintf("%s signs %s %s %s prev=%v [%s]", short(a.email), o.Kind, o.Args.Email+o.Args.Owner, o.Args.Doc, shorts(o.Prev), short(h)))
	}
}

func shorts(hs []string) []string {
	var out []string
	for _, h := range hs {
		out = append(out, short(h)[:6])
	}
	return out
}

func (a *simActor) state(team string) *State {
	if a.st == nil {
		a.st = replay(team, opsOf(a.ops), "")
	}
	return a.st
}

// step does one random thing.
func (s *sim) step(n int) {
	r := s.rng
	a := s.actors[r.Intn(len(s.actors))]
	if r.Intn(5) > 0 { // mostly someone who believes they are an owner
		var owners []*simActor
		for _, c := range s.actors {
			if m, ok := c.state(s.team).Members[c.email]; ok && m.Owner {
				owners = append(owners, c)
			}
		}
		if len(owners) > 0 {
			a = owners[r.Intn(len(owners))]
		}
	}
	switch k := r.Intn(10); {
	case k < 4: // gossip: b receives a random part of a's replica
		b := s.actors[r.Intn(len(s.actors))]
		frac := r.Float64()
		got := 0
		for h, o := range a.ops {
			if r.Float64() < frac {
				b.ops[h] = o
				b.st = nil
				got++
			}
		}
		s.log = append(s.log, fmt.Sprintf("%s -> %s %d ops", short(a.email), short(b.email), got))
	default: // a signs something from its view, or back-dated
		heads := Heads(opsOf(a.ops))
		a.snaps = append(a.snaps, heads)
		prev := heads
		if k == 9 && len(a.snaps) > 1 { // back-dated
			prev = a.snaps[r.Intn(len(a.snaps)-1)]
		}
		st := a.state(s.team)
		o := s.randomOp(a, st, prev, n, k == 8)
		if o != nil {
			s.put(a, o...)
		}
	}
}

// randomOp picks an operation that makes sense in st, mostly; sometimes a
// pointless or hostile one. grind tries variants and keeps the lowest hash.
func (s *sim) randomOp(a *simActor, st *State, prev []string, n int, grind bool) []Op {
	r := s.rng
	sg := Signer{Key: a.key, Team: s.team}
	pick := func(want func(m Member, ok bool) bool) string {
		var c []string
		for _, e := range simEmails {
			m, ok := st.Members[e]
			if want(m, ok) {
				c = append(c, e)
			}
		}
		if len(c) == 0 {
			return simEmails[r.Intn(len(simEmails))]
		}
		return c[r.Intn(len(c))]
	}
	at := t0.Add(time.Duration(n) * time.Minute)
	best := func(f func(time.Time) []Op) []Op {
		ops := f(at)
		if !grind {
			return ops
		}
		for i := 1; i < 16; i++ {
			v := f(at.Add(time.Duration(i) * time.Second))
			if v[0].Hash() < ops[0].Hash() {
				ops = v
			}
		}
		return ops
	}
	switch r.Intn(9) {
	case 0, 1:
		e := pick(func(m Member, ok bool) bool { return !ok })
		key := newActor(e).pub
		if r.Intn(8) == 0 { // a second key for one email
			key = newActor(e + "#2").pub
		}
		inv := ""
		if r.Intn(6) == 0 {
			inv = strings.Repeat(strconv.Itoa(r.Intn(3)), 32)
		}
		return best(func(t time.Time) []Op { return []Op{sg.AddMember(prev, e, key, inv, t)} })
	case 2:
		e := pick(func(m Member, ok bool) bool { return ok && !m.Owner })
		return best(func(t time.Time) []Op { return []Op{sg.AddOwner(prev, e, t)} })
	case 3:
		e := pick(func(m Member, ok bool) bool { return ok && m.Owner && m.Email != a.email })
		return best(func(t time.Time) []Op { return []Op{sg.RemoveOwner(prev, e, t)} })
	case 4:
		e := pick(func(m Member, ok bool) bool { return ok && m.Email != a.email })
		after := int64(r.Intn(4))
		docs := []Owned{{"doc" + short(e)[:3], after}}
		return best(func(t time.Time) []Op { return sg.Remove(prev, e, docs, a.email, t) })
	case 5:
		// A present member's document, to anyone, at a random cut-off:
		// concurrent with removals of the signer and with each other.
		from := pick(func(m Member, ok bool) bool { return ok })
		e := pick(func(m Member, ok bool) bool { return ok })
		after := int64(r.Intn(5))
		return best(func(t time.Time) []Op {
			return []Op{sg.Reassign(prev, "doc"+short(from)[:3], from, e, after, t)}
		})
	case 7, 8:
		// A decision on one of a few offers: concurrent accepts, accepts
		// against declines, to members or not.
		x := r.Intn(3)
		doc, tr := fmt.Sprintf("xfer%d", x), strings.Repeat(strconv.Itoa(x+1), 64)
		if r.Intn(8) == 0 { // a second offer for one document
			tr = strings.Repeat(strconv.Itoa(x+4), 64)
		}
		if r.Intn(2) == 0 {
			e := pick(func(m Member, ok bool) bool { return ok })
			if r.Intn(6) == 0 {
				e = simEmails[r.Intn(len(simEmails))]
			}
			return best(func(t time.Time) []Op { return []Op{sg.Accept(prev, doc, e, tr, t)} })
		}
		return best(func(t time.Time) []Op { return []Op{sg.Decline(prev, doc, tr, t)} })
	default:
		from := simEmails[r.Intn(len(simEmails))]
		e := pick(func(m Member, ok bool) bool { return ok })
		after := int64(r.Intn(5))
		return best(func(t time.Time) []Op {
			return []Op{sg.Reassign(prev, "doc"+short(from)[:3], from, e, after, t)}
		})
	}
}

// check verifies the invariants on one set of operations.
func (s *sim) check(ops []Op) error {
	r := newReplayer(s.team, ops, "")
	st := r.run()
	e := r.final
	if _, ok := st.Effective[s.team]; ok && len(st.Owners) == 0 {
		return fmt.Errorf("no owner left")
	}
	for i, nd := range r.nodes {
		if !e.eff[i] || e.used[i] == nil {
			continue
		}
		for _, R := range e.used[i].allEnders() {
			if r.upTo(i, R) {
				continue
			}
			// i was signed by an owner whom R removed without seeing i.
			if !isRemoval(nd.op.Kind) {
				return fmt.Errorf("op %s by a removed signer counts (removed by %s)", short(nd.hash), short(r.nodes[R].hash))
			}
			if !r.prioLess(i, R) {
				return fmt.Errorf("removal %s counts though its signer was removed by the senior %s", short(nd.hash), short(r.nodes[R].hash))
			}
		}
		if nd.op.Kind == ReassignDocument || nd.op.Kind == AcceptTransfer {
			a := nd.op.Args
			// The new owner is a member in its view.
			member := false
			for _, mt := range e.mterms[a.Owner] {
				ended := false
				for _, x := range mt.enders {
					ended = ended || r.before(x, i)
				}
				member = member || (r.before(mt.grant, i) && !ended)
			}
			if !member {
				return fmt.Errorf("%s %s to %s, not a member in its view", nd.op.Kind, short(nd.hash), a.Owner)
			}
			if nd.op.Kind == AcceptTransfer {
				continue
			}
			// No counting cut-off below one it saw.
			for j := range r.nodes {
				q := r.nodes[j].op
				if e.eff[j] && q.Kind == ReassignDocument && q.Args.Doc == a.Doc && q.Args.After > a.After && r.before(j, i) {
					return fmt.Errorf("reassignment %s counts with a stale cut-off below %s", short(nd.hash), short(r.nodes[j].hash))
				}
			}
		}
	}
	// One accept per transfer, and no decline beside it.
	accepts, declines, docs := map[string]int{}, map[string]int{}, map[string]int{}
	for i, nd := range r.nodes {
		if e.eff[i] && nd.op.Kind == AcceptTransfer {
			accepts[nd.op.Args.Transfer]++
			if docs[nd.op.Args.Doc]++; docs[nd.op.Args.Doc] > 1 {
				return fmt.Errorf("document %s: two accepts count", nd.op.Args.Doc)
			}
		}
		if e.eff[i] && nd.op.Kind == DeclineTransfer {
			declines[nd.op.Args.Transfer]++
		}
	}
	for tr, n := range accepts {
		if n > 1 || declines[tr] > 0 {
			return fmt.Errorf("transfer %s: %d accepts and %d declines count", short(tr), n, declines[tr])
		}
		if d, ok := st.Transfers[tr]; !ok || !d.Accepted {
			return fmt.Errorf("transfer %s: the state does not show the accept", short(tr))
		}
	}
	// An owner never ranks above the owner who appointed them.
	for _, ots := range e.oterms {
		for _, ot := range ots {
			for _, g := range ot.grants {
				if !e.eff[g] || r.nodes[g].op.Kind == CreateTeam {
					continue
				}
				if r.skey(g).less(&seniority{chain: nil}) {
					return fmt.Errorf("grant ranks above the creator")
				}
			}
		}
	}
	// Visiting order: outside rule 5 conflicts, the roster does not depend
	// on it.
	if !s.rule5Conflict(r) {
		for _, salt := range []string{"x", "y"} {
			other := replay(s.team, ops, salt)
			if !reflect.DeepEqual(roster(st), roster(other)) {
				return fmt.Errorf("roster depends on visiting order:\n  %v\n  %v", roster(st), roster(other))
			}
		}
	}
	return nil
}

func roster(st *State) string {
	var b strings.Builder
	for _, e := range sortedKeys(st.Members) {
		fmt.Fprintf(&b, "%s=%s ", e, st.Members[e].PubKey[:6])
	}
	return b.String() + "owners=" + strings.Join(st.Owners, ",")
}

// rule5Conflict reports concurrent add-member operations for one email,
// one key or one invite, where the first in replay order wins by design.
func (s *sim) rule5Conflict(r *replayer) bool {
	for i, a := range r.nodes {
		if a.op.Kind != AddMember {
			continue
		}
		for j := i + 1; j < len(r.nodes); j++ {
			b := r.nodes[j]
			if b.op.Kind != AddMember || r.before(i, j) {
				continue
			}
			if a.op.Args.Email == b.op.Args.Email || a.op.Args.PubKey == b.op.Args.PubKey ||
				(a.op.Args.Invite != "" && a.op.Args.Invite == b.op.Args.Invite) {
				return true
			}
		}
	}
	return false
}

func reasonClass(r string) string {
	for _, k := range []string{"more senior owner", "removed by", "without an owner", "not an owner", "not a member",
		"already a member", "concurrently by another", "already bound", "already used", "stale cut-off", "themselves", "already decided"} {
		if strings.Contains(r, k) {
			return k
		}
	}
	return r
}

func simCount(def int) int {
	if v, err := strconv.Atoi(os.Getenv("KALKA_SIM_N")); err == nil && v > 0 {
		return v
	}
	return def
}

func TestConvergenceHarness(t *testing.T) {
	n := simCount(400)
	if testing.Short() {
		n = 50
	}
	first := int64(1)
	if v, err := strconv.ParseInt(os.Getenv("KALKA_SIM_SEED"), 10, 64); err == nil {
		first = v
	}
	seeds := make(chan int64)
	type result struct {
		stats map[string]int
		err   error
	}
	results := make(chan result)
	workers := runtime.GOMAXPROCS(0)
	for w := 0; w < workers; w++ {
		go func() {
			for seed := range seeds {
				st, err := runHistory(seed)
				results <- result{st, err}
			}
		}()
	}
	go func() {
		for seed := first; seed < first+int64(n); seed++ {
			seeds <- seed
		}
		close(seeds)
	}()
	stats := map[string]int{}
	var errs []error
	for i := 0; i < n; i++ {
		r := <-results
		for k, v := range r.stats {
			stats[k] += v
		}
		if r.err != nil {
			errs = append(errs, r.err)
		}
	}
	if len(errs) > 0 {
		t.Fatalf("%d of %d histories failed; first:\n%v", len(errs), n, errs[0])
	}
	t.Logf("%d histories: %v", n, stats)
}

// runHistory plays one random history and checks it.
func runHistory(seed int64) (map[string]int, error) {
	s := newSim(seed)
	steps := 10 + s.rng.Intn(30)
	for i := 0; i < steps; i++ {
		s.step(i)
	}
	fail := func(where string, err error) error {
		return fmt.Errorf("seed %d, %s: %v\nhistory:\n  %s", seed, where, err, strings.Join(s.log, "\n  "))
	}
	// Every partial replica satisfies the invariants...
	for _, a := range s.actors {
		if err := s.check(opsOf(a.ops)); err != nil {
			return nil, fail("replica of "+a.email, err)
		}
	}
	// ...and so does the union, which every replica reaches after a full
	// exchange, with one state whatever the delivery order.
	all := opsOf(s.all)
	if err := s.check(all); err != nil {
		return nil, fail("union", err)
	}
	want := replay(s.team, all, "")
	shuffled := append([]Op(nil), all...)
	s.rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	if got := replay(s.team, shuffled, ""); !reflect.DeepEqual(got, want) {
		return nil, fail("union", fmt.Errorf("state depends on input order"))
	}
	stats := map[string]int{"ops": len(all), "discarded": len(want.Discarded)}
	for _, d := range want.Discarded {
		stats["reason: "+reasonClass(d.Reason)]++
	}
	return stats, nil
}
