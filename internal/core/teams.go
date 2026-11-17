package core

// Teams (spec 19): the team logs held in the folder, the open invites this
// installation issued (invites.yaml) and the joins it is waiting on
// (joins.yaml), and the roster operations a team owner signs.

import (
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/golbinski/kalka/internal/addr"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/peerid"
	"github.com/golbinski/kalka/internal/store"
	"github.com/golbinski/kalka/internal/teamlog"
)

// Config-directory files of spec 19.7, written with owner-only permissions.
const (
	InvitesFile = "invites.yaml"
	JoinsFile   = "joins.yaml"
)

// yamlList is a list of records in one owner-only YAML file in the config
// directory, read before every change (so the CLI and an old process
// never lose each other's writes) and written atomically.
type yamlList[T any] struct {
	path string
	key  string // the top-level key
	mu   sync.Mutex
}

func (l *yamlList[T]) load() ([]T, error) {
	raw, err := os.ReadFile(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f map[string][]T
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", l.path, err)
	}
	return f[l.key], nil
}

// update loads the list, lets change edit it, and saves it if change
// returns true.
func (l *yamlList[T]) update(change func([]T) ([]T, bool)) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	cur, err := l.load()
	if err != nil {
		return err
	}
	next, save := change(cur)
	if !save {
		return nil
	}
	data, err := yaml.Marshal(map[string][]T{l.key: next})
	if err != nil {
		return err
	}
	return fsutil.WritePrivate(l.path, data)
}

func (l *yamlList[T]) list() []T {
	l.mu.Lock()
	defer l.mu.Unlock()
	out, _ := l.load()
	return out
}

// inviteRecord is one invite this installation issued. The token holds
// the secret, so the file is owner-only.
type inviteRecord struct {
	ID       string    `yaml:"id"`
	Team     string    `yaml:"team"`
	Name     string    `yaml:"name"`
	Email    string    `yaml:"email"`
	Token    string    `yaml:"token"`
	Created  time.Time `yaml:"created"`
	Expires  time.Time `yaml:"expires"`
	UsedAt   time.Time `yaml:"used_at,omitempty"`
	UsedKey  string    `yaml:"used_key,omitempty"`   // the joiner's key, base64url
	AddedBy  string    `yaml:"add_member,omitempty"` // the add-member hash
	Canceled bool      `yaml:"canceled,omitempty"`
}

// joinRecord is a join this installation is waiting on or has finished.
type joinRecord struct {
	ID      string    `yaml:"id"`
	Team    string    `yaml:"team"`
	Name    string    `yaml:"name"`
	Token   string    `yaml:"token"`
	Created time.Time `yaml:"created"`
	Expires time.Time `yaml:"expires"`
	State   string    `yaml:"state"` // pending, joined, failed
	Detail  string    `yaml:"detail,omitempty"`
	Kind    string    `yaml:"kind,omitempty"`
	Tries   int       `yaml:"tries,omitempty"`
	LastTry time.Time `yaml:"last_try,omitempty"`
	Owner   string    `yaml:"owner_email,omitempty"` // learned on meeting
}

// Join states.
const (
	JoinPending = "pending"
	JoinDone    = "joined"
	JoinFailed  = "failed"
)

// teams is the daemon's team state; the logs themselves live in the folder.
type teams struct {
	invites yamlList[inviteRecord]
	joins   yamlList[joinRecord]
	mu      sync.Mutex // serializes log writes and redemptions
	trying  map[string]bool
	lastTry map[string]time.Time // wall clock, for the retry interval
}

func newTeams(dir string) *teams {
	return &teams{
		invites: yamlList[inviteRecord]{path: filepath.Join(dir, InvitesFile), key: "invites"},
		joins:   yamlList[joinRecord]{path: filepath.Join(dir, JoinsFile), key: "joins"},
		trying:  map[string]bool{}, lastTry: map[string]time.Time{},
	}
}

// teamDir is one team log held in the folder.
type teamDir struct {
	Team string // hash of create-team
	Name string
	Dir  string
	Log  string
}

// teamDirs finds the team logs in the folder's direct subfolders (spec
// 19.1), sorted by name.
func (d *Node) teamDirs() []teamDir {
	root := d.Store.Dir()
	ents, _ := os.ReadDir(root)
	var out []teamDir
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.EqualFold(e.Name(), store.PersonalDir) {
			continue // personal/ is never a team's folder (spec 14)
		}
		dir := filepath.Join(root, e.Name())
		logs, _ := filepath.Glob(filepath.Join(dir, "*-team.log"))
		for _, lp := range logs {
			ops, _, err := teamlog.ReadFile(lp)
			if err != nil {
				continue
			}
			for _, o := range ops {
				if o.Kind == teamlog.CreateTeam && o.Team == "" && teamlog.LogName(o.Args.Name) == filepath.Base(lp) {
					out = append(out, teamDir{Team: o.Hash(), Name: o.Args.Name, Dir: dir, Log: lp})
					break
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Dir < out[j].Dir
	})
	return out
}

// teamByHash returns the folder holding team, if any.
func (d *Node) teamByHash(team string) (teamDir, bool) {
	for _, t := range d.teamDirs() {
		if t.Team == team {
			return t, true
		}
	}
	return teamDir{}, false
}

// findTeam resolves a team given by name or hash; an empty ref is the only
// team held.
func (d *Node) findTeam(ref string) (teamDir, error) {
	ts := d.teamDirs()
	if ref == "" {
		switch len(ts) {
		case 0:
			return teamDir{}, &Error{Kind: KindNoTeam, Msg: fmt.Sprintf("you are in no team: create one with `%s team create <name>` or join with `%[1]s join <link>`", name.Binary)}
		case 1:
			return ts[0], nil
		}
		return teamDir{}, &Error{Kind: KindAmbiguousTeam, Msg: "you are in several teams: say which with --team <name>"}
	}
	var hit []teamDir
	for _, t := range ts {
		if t.Team == ref || t.Name == ref {
			hit = append(hit, t)
		}
	}
	switch len(hit) {
	case 0:
		return teamDir{}, &Error{Kind: KindUnknownTeam, Msg: fmt.Sprintf("no team %q in the folder", ref)}
	case 1:
		return hit[0], nil
	}
	return teamDir{}, &Error{Kind: KindAmbiguousTeam, Msg: fmt.Sprintf("two teams are named %q: give the team hash instead", ref)}
}

// newTeamDir picks the folder for a team (spec 19.2): <folder>/<name>, or
// with a suffix when that folder holds another team's log.
func (d *Node) newTeamDir(teamName, team string) teamDir {
	root := d.Store.Dir()
	for i := 1; ; i++ {
		dirName := teamName
		if i > 1 || teamName == store.PersonalDir {
			dirName = fmt.Sprintf("%s-%d", teamName, max(i, 2))
		}
		dir := filepath.Join(root, dirName)
		lp := filepath.Join(dir, teamlog.LogName(teamName))
		ops, _, err := teamlog.ReadFile(lp)
		if err == nil && len(ops) == 0 {
			if _, err := os.Stat(lp); errors.Is(err, os.ErrNotExist) {
				return teamDir{Team: team, Name: teamName, Dir: dir, Log: lp}
			}
		}
		for _, o := range ops {
			if o.Kind == teamlog.CreateTeam && o.Hash() == team {
				return teamDir{Team: team, Name: teamName, Dir: dir, Log: lp}
			}
		}
	}
}

// load reads and replays a team's log.
func (d *Node) load(t teamDir) (*teamlog.State, []teamlog.Op, []teamlog.BadLine, []teamlog.Rejected, error) {
	ops, bad, err := teamlog.ReadFile(t.Log)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	st, rej := teamlog.Replay(t.Team, ops)
	return st, ops, bad, rej, nil
}

func memberOf(st *teamlog.State, email string) (teamlog.Member, bool) {
	for e, m := range st.Members {
		if bp.SameEmail(e, email) {
			return m, true
		}
	}
	return teamlog.Member{}, false
}

// ownerOf loads a team and checks that the local user is one of its owners
// under the local key, which every roster change needs (spec 19.3).
func (d *Node) ownerOf(t teamDir) (*teamlog.State, error) {
	st, _, _, _, err := d.load(t)
	if err != nil {
		return nil, err
	}
	m, ok := memberOf(st, d.self.Email)
	if !ok || m.PubKey != teamlog.EncodeKey(d.pub) {
		return nil, &Error{Kind: KindNotMember, Msg: fmt.Sprintf("you are not a member of team %s", t.Name)}
	}
	if !m.Owner {
		return nil, &Error{Kind: KindNotTeamOwner, Msg: fmt.Sprintf("only a team owner can do that; the owners of %s are %s", t.Name, strings.Join(st.Owners, ", "))}
	}
	return st, nil
}

func (d *Node) teamSigner(t teamDir) teamlog.Signer {
	return teamlog.Signer{Key: d.self.Key, Team: t.Team}
}

// commit appends ops to the team's log and checks that the first of them
// counts after replay.
func (d *Node) commit(t teamDir, ops []teamlog.Op) (*teamlog.State, error) {
	all, err := teamlog.AppendFile(t.Log, ops)
	if err != nil {
		return nil, err
	}
	d.sawOps(ops) // no notice about what the local user did
	st, _ := teamlog.Replay(t.Team, all)
	d.bus.Publish(BusEvent{Type: "teams"})
	defer d.pushTeam(t.Team)
	defer d.background(func() { d.afterLogChange(&held{dir: t, st: st, ops: all}) })
	h := ops[0].Hash()
	if !st.Effective[h] {
		reason := "it is not effective"
		for _, x := range st.Discarded {
			if x.Hash == h {
				reason = x.Reason
			}
		}
		return st, fmt.Errorf("the operation was written but does not count: %s", reason)
	}
	return st, nil
}

// CreateTeam starts a team with the local user as its first owner.
func (d *Node) CreateTeam(teamName string) (TeamView, error) {
	teamName = strings.TrimSpace(teamName)
	if !teamlog.ValidTeamName(teamName) {
		return TeamView{}, fmt.Errorf("%q cannot name a team: use lower-case letters, digits and dashes, starting with a letter or digit", teamName)
	}
	if teamName == store.PersonalDir {
		return TeamView{}, fmt.Errorf("%q is reserved for your Personal folder; choose another team name", teamName)
	}
	d.teams.mu.Lock()
	defer d.teams.mu.Unlock()
	for _, t := range d.teamDirs() {
		if t.Name == teamName {
			return TeamView{}, fmt.Errorf("you already hold a team named %s (in %s)", teamName, t.Dir)
		}
	}
	op := teamlog.Create(d.self.Key, teamName, addr.Canonical(d.self.Email), d.now())
	t := d.newTeamDir(teamName, op.Hash())
	if _, err := d.commit(t, []teamlog.Op{op}); err != nil {
		return TeamView{}, err
	}
	d.logf("created team %s in %s", teamName, t.Dir)
	return d.teamView(t), nil
}

// InviteResult is a new invite (spec 19.7 step 1).
type InviteResult struct {
	Token   string    `json:"token"`
	Link    string    `json:"link"` // kalka://join/<token>
	ID      string    `json:"id"`
	Team    string    `json:"team"`
	Name    string    `json:"name"`
	Email   string    `json:"email"`
	Expires time.Time `json:"expires"`
}

// Invite makes a one-time invite for email. kalka does not send it.
func (d *Node) Invite(ref, email string, ttl time.Duration) (InviteResult, error) {
	email = addr.Canonical(strings.TrimSpace(email))
	if !bp.ValidEmail(email) {
		return InviteResult{}, fmt.Errorf("%q is not an email address", email)
	}
	if ttl == 0 {
		ttl = teamlog.DefaultInviteTTL
	}
	if ttl < time.Minute || ttl > 30*24*time.Hour {
		return InviteResult{}, errors.New("an invite lasts between one minute and 30 days")
	}
	t, err := d.findTeam(ref)
	if err != nil {
		return InviteResult{}, err
	}
	st, err := d.ownerOf(t)
	if err != nil {
		return InviteResult{}, err
	}
	if _, ok := memberOf(st, email); ok {
		return InviteResult{}, fmt.Errorf("%s is already a member of %s", email, t.Name)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return InviteResult{}, err
	}
	now := d.now()
	iv := teamlog.Invite{TeamName: t.Name, Team: t.Team, Owner: teamlog.EncodeKey(d.pub), Inviter: d.self.Email, Email: email,
		Secret: secret, Expires: now.Add(ttl).UTC().Truncate(time.Second), Rendezvous: d.cfg.Rendezvous}
	rec := inviteRecord{ID: iv.ID(), Team: t.Team, Name: t.Name, Email: email, Token: iv.Token(), Created: now.UTC(), Expires: iv.Expires}
	if err := d.teams.invites.update(func(l []inviteRecord) ([]inviteRecord, bool) { return append(l, rec), true }); err != nil {
		return InviteResult{}, err
	}
	d.bus.Publish(BusEvent{Type: "teams"})
	d.logf("invited %s to %s (expires %s)", email, t.Name, iv.Expires.Format(time.RFC3339))
	return InviteResult{Token: rec.Token, Link: iv.Link(), ID: rec.ID, Team: t.Team, Name: t.Name, Email: email, Expires: iv.Expires}, nil
}

// CancelInvite withdraws an open invite this installation issued.
func (d *Node) CancelInvite(id string) error {
	found := false
	err := d.teams.invites.update(func(l []inviteRecord) ([]inviteRecord, bool) {
		for i := range l {
			if l[i].ID == id && l[i].UsedAt.IsZero() {
				l[i].Canceled, found = true, true
			}
		}
		return l, found
	})
	if err == nil && !found {
		err = errors.New("no open invite with that id")
	}
	d.bus.Publish(BusEvent{Type: "teams"})
	return err
}

// Reassigned is a document a removal hands to the removing owner (19.5).
type Reassigned struct {
	KalkaID string `json:"kalka_id"`
	Title   string `json:"title"`
	Path    string `json:"path"`
	After   int64  `json:"after"`
	Domain  string `json:"domain,omitempty"` // a domain document moves with its owner like any other (6.12)
	Plan    string `json:"-"`
}

// RemovalPreview lists the published documents of the team, held here,
// whose owner is email: removing email reassigns them to the local user.
func (d *Node) RemovalPreview(ref, email string) ([]Reassigned, error) {
	t, err := d.findTeam(ref)
	if err != nil {
		return nil, err
	}
	return d.ownedBy(t, email), nil
}

// ownedBy lists the team documents held here that email owns: the next
// release is theirs. After and Plan are the highest valid release held.
func (d *Node) ownedBy(t teamDir, email string) []Reassigned {
	out := []Reassigned{}
	h, err := d.heldTeam(t.Team)
	if err != nil {
		return out
	}
	for id, td := range d.replicaSet(h) {
		if td.tomb {
			continue
		}
		ch := merge.Releases(entriesOf(td.doc), id, h.team())
		owner, _ := nextReleaser(ch, td.doc.Owner())
		if !bp.SameEmail(owner, email) {
			continue
		}
		r := Reassigned{KalkaID: id, Title: td.doc.Title(), Path: td.path}
		if td.doc.IsDomain() {
			r.Domain = td.doc.DomainName()
		}
		if head, ok := ch.Head(); ok {
			r.After, r.Plan = head.Release.Version, head.Release.Plan
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Remove removes a member; their published documents held here go to the
// local user (spec 19.5).
func (d *Node) Remove(ref, email string) (TeamView, error) {
	email = addr.Canonical(strings.TrimSpace(email))
	d.teams.mu.Lock()
	defer d.teams.mu.Unlock()
	t, err := d.findTeam(ref)
	if err != nil {
		return TeamView{}, err
	}
	st, err := d.ownerOf(t)
	if err != nil {
		return TeamView{}, err
	}
	if bp.SameEmail(email, d.self.Email) {
		if len(st.Owners) == 1 {
			return TeamView{}, &Error{Kind: KindLastOwner, Msg: fmt.Sprintf("you are the only owner of %s: make another member an owner first", t.Name)}
		}
		return TeamView{}, errors.New("you cannot remove yourself: another owner removes you (spec 19.4 rule 7)")
	}
	if _, ok := memberOf(st, email); !ok {
		return TeamView{}, &Error{Kind: KindNotMember, Msg: fmt.Sprintf("%s is not a member of %s", email, t.Name)}
	}
	var docs []teamlog.Owned
	for _, r := range d.ownedBy(t, email) {
		docs = append(docs, teamlog.Owned{Doc: r.KalkaID, After: r.After, Plan: r.Plan})
	}
	ops := d.teamSigner(t).Remove(st.Heads, email, docs, addr.Canonical(d.self.Email), d.now())
	if _, err := d.commit(t, ops); err != nil {
		return TeamView{}, err
	}
	d.logf("removed %s from %s, %d document(s) reassigned", email, t.Name, len(docs))
	return d.teamView(t), nil
}

// SetOwner adds or removes a team owner.
func (d *Node) SetOwner(ref, email string, owner bool) (TeamView, error) {
	email = addr.Canonical(strings.TrimSpace(email))
	d.teams.mu.Lock()
	defer d.teams.mu.Unlock()
	t, err := d.findTeam(ref)
	if err != nil {
		return TeamView{}, err
	}
	st, err := d.ownerOf(t)
	if err != nil {
		return TeamView{}, err
	}
	m, ok := memberOf(st, email)
	if !ok {
		return TeamView{}, &Error{Kind: KindNotMember, Msg: fmt.Sprintf("%s is not a member of %s: invite them first", email, t.Name)}
	}
	var op teamlog.Op
	if owner {
		if m.Owner {
			return TeamView{}, fmt.Errorf("%s is already an owner", email)
		}
		op = d.teamSigner(t).AddOwner(st.Heads, email, d.now())
	} else {
		if !m.Owner {
			return TeamView{}, fmt.Errorf("%s is not an owner", email)
		}
		if bp.SameEmail(email, d.self.Email) {
			if len(st.Owners) == 1 {
				return TeamView{}, &Error{Kind: KindLastOwner, Msg: fmt.Sprintf("you are the only owner of %s: make another member an owner first", t.Name)}
			}
			return TeamView{}, errors.New("you cannot remove yourself as owner: another owner does that (spec 19.4 rule 7)")
		}
		op = d.teamSigner(t).RemoveOwner(st.Heads, email, d.now())
	}
	if _, err := d.commit(t, []teamlog.Op{op}); err != nil {
		return TeamView{}, err
	}
	return d.teamView(t), nil
}

// TeamView is a team as the Team view and `kalka team` show it.
type TeamView struct {
	Team  string `json:"team"`
	Name  string `json:"name"`
	Dir   string `json:"dir"`
	Log   string `json:"log"`
	Me    string `json:"me"` // owner, member or none
	Error string `json:"error,omitempty"`
	// Waiting counts the team's waiting items (spec 11.3); set in Teams.
	Waiting int `json:"waiting"`

	Members []MemberView  `json:"members"`
	Owners  []string      `json:"owners"`
	Invites []InviteView  `json:"invites"`
	History []HistoryLine `json:"history"`
	// BadLines are log lines that do not decode; Rejected have a bad
	// signature. Neither counts.
	BadLines []teamlog.BadLine `json:"bad_lines,omitempty"`
	Rejected int               `json:"rejected,omitempty"`
	Pending  int               `json:"pending,omitempty"`
	// Replication is the last round's status line (spec 11.1).
	Replication *TeamStatus `json:"replication,omitempty"`
	// Published counts the team's published documents held here; Deleted
	// lists the tombstones.
	Published int          `json:"published"`
	Deleted   []DeletedDoc `json:"deleted"`
	// NotShared lists the mismatches (spec 14).
	NotShared []NotSharedDoc `json:"not_shared"`
}

// DeletedDoc is a document deleted for the team.
type DeletedDoc struct {
	KalkaID string `json:"kalka_id"`
	Title   string `json:"title"`
	Version int64  `json:"version"`
	By      string `json:"by"`
}

// MemberView is one current member.
type MemberView struct {
	Email       string `json:"email"`
	Owner       bool   `json:"owner,omitempty"`
	Fingerprint string `json:"fingerprint"`
	Since       string `json:"since,omitempty"` // the signer's clock at add-member
	You         bool   `json:"you,omitempty"`
	// Presence is what this daemon last saw of the member (spec 9.3):
	// online, offline or unknown; empty for the local user.
	Presence string `json:"presence,omitempty"`
}

// InviteView is an invite this installation issued for the team.
type InviteView struct {
	ID      string    `json:"id"`
	Email   string    `json:"email"`
	Expires time.Time `json:"expires"`
	State   string    `json:"state"` // open, used, expired, canceled
	UsedAt  time.Time `json:"used_at,omitzero"`
	// Token and Link only while the invite is open, so the owner can pass
	// it on again (spec 11, Team: Copy link, Copy token).
	Token string `json:"token,omitempty"`
	Link  string `json:"link,omitempty"`
}

// HistoryLine is one operation of the log in words.
type HistoryLine struct {
	Hash      string `json:"hash"`
	At        string `json:"at"`
	Signer    string `json:"signer"`
	Op        string `json:"op"`
	Text      string `json:"text"`
	Effective bool   `json:"effective"`
	Reason    string `json:"reason,omitempty"`
}

// Teams lists the teams held in the folder.
func (d *Node) Teams() []TeamView {
	out := []TeamView{}
	counts := map[string]int{}
	for _, w := range d.WaitingSummary().Teams {
		counts[w.Team] = w.Count
	}
	for _, t := range d.teamDirs() {
		v := d.teamView(t)
		v.Waiting = counts[t.Team]
		out = append(out, v)
	}
	return out
}

func (d *Node) teamView(t teamDir) TeamView {
	v := TeamView{Team: t.Team, Name: t.Name, Dir: t.Dir, Log: t.Log, Me: "none",
		Members: []MemberView{}, Owners: []string{}, Invites: []InviteView{}, History: []HistoryLine{}}
	st, ops, bad, rej, err := d.load(t)
	if err != nil {
		v.Error = err.Error()
		return v
	}
	v.BadLines, v.Rejected, v.Pending = bad, len(rej), len(st.Pending)
	v.Owners = append(v.Owners, st.Owners...)
	self := teamlog.EncodeKey(d.pub)
	for _, m := range st.Members {
		mv := MemberView{Email: m.Email, Owner: m.Owner}
		if k, err := teamlog.DecodeKey(m.PubKey); err == nil {
			mv.Fingerprint = peerid.Display(peerid.Fingerprint(k))
			if bp.SameEmail(m.Email, d.self.Email) && m.PubKey == self {
				mv.You = true
				v.Me = "member"
				if m.Owner {
					v.Me = "owner"
				}
			}
		}
		if !mv.You {
			mv.Presence = string(d.presenceOf(m.Email))
		}
		v.Members = append(v.Members, mv)
	}
	byHash := map[string]teamlog.Op{}
	for _, o := range ops {
		byHash[o.Hash()] = o
	}
	for i := range v.Members {
		if o, ok := byHash[st.Members[v.Members[i].Email].Since]; ok {
			v.Members[i].Since = o.At
		}
	}
	sort.Slice(v.Members, func(i, j int) bool {
		a, b := v.Members[i], v.Members[j]
		ia, ib := slices.Index(st.Owners, a.Email), slices.Index(st.Owners, b.Email)
		if (ia >= 0) != (ib >= 0) {
			return ia >= 0
		}
		if ia >= 0 && ia != ib {
			return ia < ib
		}
		return a.Email < b.Email
	})
	now := d.now()
	for _, r := range d.teams.invites.list() {
		if r.Team != t.Team {
			continue
		}
		iv := InviteView{ID: r.ID, Email: r.Email, Expires: r.Expires, State: "open", UsedAt: r.UsedAt}
		switch {
		case !r.UsedAt.IsZero():
			iv.State = "used"
		case r.Canceled:
			iv.State = "canceled"
		case !now.Before(r.Expires):
			iv.State = "expired"
		default:
			iv.Token, iv.Link = r.Token, teamlog.LinkPrefix+r.Token
		}
		v.Invites = append(v.Invites, iv)
	}
	titles := map[string]string{}
	for _, in := range d.Store.List() {
		if in.KalkaID != "" && inDir(t.Dir, in.Path) {
			titles[strings.ToLower(in.KalkaID)] = in.Title
		}
	}
	v.History = history(st, ops, titles)
	v.Replication = d.replStatus(t.Team)
	v.Deleted = []DeletedDoc{}
	h := &held{dir: t, st: st, ops: ops}
	for _, td := range d.replicaSet(h) {
		if !td.tomb {
			v.Published++
			continue
		}
		dd := DeletedDoc{KalkaID: td.item.ID, Title: td.item.ID, Version: td.item.Version}
		if head, ok := td.chain.Head(); ok {
			dd.By = head.Entry.Author
		}
		if matches, _ := filepath.Glob(filepath.Join(trashDir(h), "*."+td.item.ID[:8]+".md")); len(matches) > 0 {
			if raw, err := os.ReadFile(matches[0]); err == nil {
				if doc, err := bp.Parse(raw); err == nil && doc.Title() != "" {
					dd.Title = doc.Title()
				}
			}
		}
		v.Deleted = append(v.Deleted, dd)
	}
	sort.Slice(v.Deleted, func(i, j int) bool { return v.Deleted[i].Title < v.Deleted[j].Title })
	v.NotShared = d.notShared(h)
	return v
}

// NotSharedDoc is a file whose location and `kalka_team:` disagree, or that lies
// in a team's folder without being a team document: listed, never
// replicated (spec 14).
type NotSharedDoc struct {
	Rel     string `json:"rel"`
	Title   string `json:"title"`
	KalkaID string `json:"kalka_id,omitempty"`
	Reason  string `json:"reason"`
}

// notShared lists the files in h's folder that do not replicate, and the
// Personal files whose `kalka_team:` names h.
func (d *Node) notShared(h *held) []NotSharedDoc {
	out := []NotSharedDoc{}
	for _, in := range d.Store.List() {
		f, err := d.Store.Load(in.Path)
		if err != nil {
			continue
		}
		var why string
		switch {
		case inDir(h.dir.Dir, in.Path):
			why = isTeamDoc(h, in.Path, f.Doc, in.Duplicate)
		case in.Scope == "" && f.Doc.Team() == h.dir.Name:
			why = MismatchInPersonal
		}
		if why != "" {
			out = append(out, NotSharedDoc{Rel: in.Rel, Title: in.Title, KalkaID: in.KalkaID, Reason: why})
		}
	}
	return out
}

// history renders the log in causal order, oldest first.
// history renders the log; titles maps the kalka ids of the team's
// documents held here to their titles, for operations that name one.
func history(st *teamlog.State, ops []teamlog.Op, titles map[string]string) []HistoryLine {
	doc := func(id string) string {
		if t := titles[strings.ToLower(id)]; t != "" {
			return fmt.Sprintf("%q", t)
		}
		if len(id) > 8 {
			id = id[:8]
		}
		return "document " + id
	}
	who := map[string]string{}
	for email, keys := range st.Keys {
		for _, k := range keys {
			who[k] = email
		}
	}
	for _, o := range ops {
		if o.Kind == teamlog.CreateTeam {
			who[o.Signer] = o.Args.Email
		}
	}
	reason := map[string]string{}
	for _, x := range st.Discarded {
		reason[x.Hash] = x.Reason
	}
	for _, x := range st.Pending {
		reason[x.Hash] = "pending: " + x.Reason
	}
	depth := map[string]int{}
	var depthOf func(h string, seen map[string]bool) int
	byHash := map[string]teamlog.Op{}
	for _, o := range ops {
		byHash[o.Hash()] = o
	}
	depthOf = func(h string, seen map[string]bool) int {
		if v, ok := depth[h]; ok {
			return v
		}
		o, ok := byHash[h]
		if !ok || seen[h] {
			return 0
		}
		seen[h] = true
		n := 0
		for _, p := range o.Prev {
			n = max(n, depthOf(p, seen)+1)
		}
		depth[h] = n
		return n
	}
	sorted := slices.Clone(ops)
	sort.SliceStable(sorted, func(i, j int) bool {
		hi, hj := sorted[i].Hash(), sorted[j].Hash()
		di, dj := depthOf(hi, map[string]bool{}), depthOf(hj, map[string]bool{})
		if di != dj {
			return di < dj
		}
		return hi < hj
	})
	out := []HistoryLine{}
	for _, o := range sorted {
		h := o.Hash()
		signer := who[o.Signer]
		if signer == "" {
			signer = "key " + peerid.Display(fingerprintOf(o.Signer))
		}
		a := o.Args
		var text string
		switch o.Kind {
		case teamlog.CreateTeam:
			text = fmt.Sprintf("created the team %s", a.Name)
		case teamlog.AddMember:
			text = "added " + a.Email
			if a.Invite != "" {
				text += " (by invite)"
			}
		case teamlog.RemoveMember:
			text = "removed " + a.Email
		case teamlog.AddOwner:
			text = "made " + a.Email + " an owner"
		case teamlog.RemoveOwner:
			text = "made " + a.Email + " a plain member"
		case teamlog.ReassignDocument:
			text = fmt.Sprintf("reassigned %s from %s to %s from version %d", doc(a.Doc), a.From, a.Owner, a.After+1)
		case teamlog.AcceptTransfer:
			text = fmt.Sprintf("accepted %s from another team, owned by %s", doc(a.Doc), a.Owner)
		case teamlog.DeclineTransfer:
			text = fmt.Sprintf("declined %s, offered from another team", doc(a.Doc))
		default:
			text = o.Kind
		}
		out = append(out, HistoryLine{Hash: h[:12], At: o.At, Signer: signer, Op: o.Kind, Text: text, Effective: st.Effective[h], Reason: reason[h]})
	}
	return out
}

func fingerprintOf(key string) string {
	k, err := teamlog.DecodeKey(key)
	if err != nil {
		return "?"
	}
	return peerid.Fingerprint(k)
}

// teamKeyFor reports whether a team the local user belongs to binds key
// to email as a current member: such a key counts as verified for that
// team's traffic (spec 8.2, 20.2).
func (d *Node) teamKeyFor(team, email string, key []byte) bool {
	t, ok := d.teamByHash(team)
	if !ok {
		return false
	}
	st, _, _, _, err := d.load(t)
	if err != nil {
		return false
	}
	me, ok := memberOf(st, d.self.Email)
	if !ok || me.PubKey != teamlog.EncodeKey(d.pub) {
		return false
	}
	m, ok := memberOf(st, email)
	return ok && m.PubKey == teamlog.EncodeKey(key)
}

// Team is the view of one team, by name or hash.
func (d *Node) Team(ref string) (TeamView, error) {
	t, err := d.findTeam(ref)
	if err != nil {
		return TeamView{}, err
	}
	return d.teamView(t), nil
}

// TeamHash resolves a team's name or hash to its hash.
func (d *Node) TeamHash(ref string) (string, error) {
	t, err := d.findTeam(ref)
	return t.Team, err
}
