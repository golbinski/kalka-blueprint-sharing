package daemon

// Teams (spec 19): the team logs held in the folder, the open invites this
// installation issued (invites.yaml) and the joins it is waiting on
// (joins.yaml), and the roster operations a team owner signs.

import (
	"crypto/rand"
	"errors"
	"fmt"
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
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/peerid"
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
func (d *Daemon) teamDirs() []teamDir {
	root := d.Store.Dir()
	ents, _ := os.ReadDir(root)
	var out []teamDir
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
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
func (d *Daemon) teamByHash(team string) (teamDir, bool) {
	for _, t := range d.teamDirs() {
		if t.Team == team {
			return t, true
		}
	}
	return teamDir{}, false
}

// findTeam resolves a team given by name or hash; an empty ref is the only
// team held.
func (d *Daemon) findTeam(ref string) (teamDir, error) {
	ts := d.teamDirs()
	if ref == "" {
		switch len(ts) {
		case 0:
			return teamDir{}, fmt.Errorf("you are in no team: create one with `%s team create <name>` or join with `%[1]s join <token>`", name.Binary)
		case 1:
			return ts[0], nil
		}
		return teamDir{}, errors.New("you are in several teams: say which with --team <name>")
	}
	var hit []teamDir
	for _, t := range ts {
		if t.Team == ref || t.Name == ref {
			hit = append(hit, t)
		}
	}
	switch len(hit) {
	case 0:
		return teamDir{}, fmt.Errorf("no team %q in the folder", ref)
	case 1:
		return hit[0], nil
	}
	return teamDir{}, fmt.Errorf("two teams are named %q: give the team hash instead", ref)
}

// newTeamDir picks the folder for a team (spec 19.2): <folder>/<name>, or
// with a suffix when that folder holds another team's log.
func (d *Daemon) newTeamDir(teamName, team string) teamDir {
	root := d.Store.Dir()
	for i := 1; ; i++ {
		dirName := teamName
		if i > 1 {
			dirName = fmt.Sprintf("%s-%d", teamName, i)
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
func (d *Daemon) load(t teamDir) (*teamlog.State, []teamlog.Op, []teamlog.BadLine, []teamlog.Rejected, error) {
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
func (d *Daemon) ownerOf(t teamDir) (*teamlog.State, error) {
	st, _, _, _, err := d.load(t)
	if err != nil {
		return nil, err
	}
	m, ok := memberOf(st, d.self.Email)
	if !ok || m.PubKey != teamlog.EncodeKey(d.pub) {
		return nil, fmt.Errorf("you are not a member of team %s", t.Name)
	}
	if !m.Owner {
		return nil, fmt.Errorf("only a team owner can do that; the owners of %s are %s", t.Name, strings.Join(st.Owners, ", "))
	}
	return st, nil
}

func (d *Daemon) teamSigner(t teamDir) teamlog.Signer {
	return teamlog.Signer{Key: d.self.Key, Team: t.Team}
}

// commit appends ops to the team's log and checks that the first of them
// counts after replay.
func (d *Daemon) commit(t teamDir, ops []teamlog.Op) (*teamlog.State, error) {
	all, err := teamlog.AppendFile(t.Log, ops)
	if err != nil {
		return nil, err
	}
	st, _ := teamlog.Replay(t.Team, all)
	d.bus.Publish(BusEvent{Type: "teams"})
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
func (d *Daemon) CreateTeam(teamName string) (TeamView, error) {
	teamName = strings.TrimSpace(teamName)
	if !teamlog.ValidTeamName(teamName) {
		return TeamView{}, fmt.Errorf("%q cannot name a team: use lower-case letters, digits and dashes, starting with a letter or digit", teamName)
	}
	d.teams.mu.Lock()
	defer d.teams.mu.Unlock()
	for _, t := range d.teamDirs() {
		if t.Name == teamName {
			return TeamView{}, fmt.Errorf("you already hold a team named %s (in %s)", teamName, t.Dir)
		}
	}
	op := teamlog.Create(d.self.Key, teamName, strings.ToLower(d.self.Email), d.now())
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
	ID      string    `json:"id"`
	Team    string    `json:"team"`
	Name    string    `json:"name"`
	Email   string    `json:"email"`
	Expires time.Time `json:"expires"`
}

// Invite makes a one-time invite for email. kalka does not send it.
func (d *Daemon) Invite(ref, email string, ttl time.Duration) (InviteResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))
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
	iv := teamlog.Invite{TeamName: t.Name, Team: t.Team, Owner: teamlog.EncodeKey(d.pub), Email: email,
		Secret: secret, Expires: now.Add(ttl).UTC().Truncate(time.Second), Rendezvous: d.cfg.Rendezvous}
	rec := inviteRecord{ID: iv.ID(), Team: t.Team, Name: t.Name, Email: email, Token: iv.Token(), Created: now.UTC(), Expires: iv.Expires}
	if err := d.teams.invites.update(func(l []inviteRecord) ([]inviteRecord, bool) { return append(l, rec), true }); err != nil {
		return InviteResult{}, err
	}
	d.bus.Publish(BusEvent{Type: "teams"})
	d.logf("invited %s to %s (expires %s)", email, t.Name, iv.Expires.Format(time.RFC3339))
	return InviteResult{Token: rec.Token, ID: rec.ID, Team: t.Team, Name: t.Name, Email: email, Expires: iv.Expires}, nil
}

// CancelInvite withdraws an open invite this installation issued.
func (d *Daemon) CancelInvite(id string) error {
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
}

// RemovalPreview lists the published documents of the team, held here,
// whose owner is email: removing email reassigns them to the local user.
func (d *Daemon) RemovalPreview(ref, email string) ([]Reassigned, error) {
	t, err := d.findTeam(ref)
	if err != nil {
		return nil, err
	}
	return d.ownedBy(t, email), nil
}

func (d *Daemon) ownedBy(t teamDir, email string) []Reassigned {
	out := []Reassigned{}
	for _, in := range d.Store.List() {
		if in.KalkaID == "" || !bp.SameEmail(in.Owner, email) {
			continue
		}
		f, err := d.Store.Load(in.Path)
		if err != nil || f.Doc.Team() != t.Name {
			continue
		}
		out = append(out, Reassigned{KalkaID: in.KalkaID, Title: in.Title, Path: in.Path, After: lastRelease(f.Doc)})
	}
	return out
}

// lastRelease is the highest release version a document holds.
func lastRelease(doc *bp.Document) int64 {
	var v int64
	for _, it := range doc.Items() {
		if it.Entry == nil {
			continue
		}
		if r, ok := it.Entry.Release(); ok && r.Version > v {
			v = r.Version
		}
	}
	return v
}

// Remove removes a member; their published documents held here go to the
// local user (spec 19.5).
func (d *Daemon) Remove(ref, email string) (TeamView, error) {
	email = strings.ToLower(strings.TrimSpace(email))
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
		return TeamView{}, errors.New("you cannot remove yourself: another owner removes you (spec 19.4 rule 7)")
	}
	if _, ok := memberOf(st, email); !ok {
		return TeamView{}, fmt.Errorf("%s is not a member of %s", email, t.Name)
	}
	var docs []teamlog.Owned
	for _, r := range d.ownedBy(t, email) {
		docs = append(docs, teamlog.Owned{Doc: r.KalkaID, After: r.After})
	}
	ops := d.teamSigner(t).Remove(st.Heads, email, docs, strings.ToLower(d.self.Email), d.now())
	if _, err := d.commit(t, ops); err != nil {
		return TeamView{}, err
	}
	d.logf("removed %s from %s, %d document(s) reassigned", email, t.Name, len(docs))
	return d.teamView(t), nil
}

// SetOwner adds or removes a team owner.
func (d *Daemon) SetOwner(ref, email string, owner bool) (TeamView, error) {
	email = strings.ToLower(strings.TrimSpace(email))
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
		return TeamView{}, fmt.Errorf("%s is not a member of %s: invite them first", email, t.Name)
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
			return TeamView{}, errors.New("you cannot remove yourself as owner: another owner does that (spec 19.4 rule 7)")
		}
		op = d.teamSigner(t).RemoveOwner(st.Heads, email, d.now())
	}
	if _, err := d.commit(t, []teamlog.Op{op}); err != nil {
		return TeamView{}, err
	}
	return d.teamView(t), nil
}

// AssignDomain appoints a domain owner (spec 19.6). The cut-off is the
// last release of that domain's document held here.
func (d *Daemon) AssignDomain(ref, domain, email string) (TeamView, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !teamlog.ValidDomainName(domain) {
		return TeamView{}, fmt.Errorf("%q cannot name a domain: use lower-case letters, digits and dashes", domain)
	}
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
	if _, ok := memberOf(st, email); !ok {
		return TeamView{}, fmt.Errorf("%s is not a member of %s", email, t.Name)
	}
	var after int64
	for _, in := range d.Store.List() {
		if f, err := d.Store.Load(in.Path); err == nil && f.Doc.Team() == t.Name {
			if v, _ := f.Doc.Frontmatter().Get("domain"); v == domain {
				after = max(after, lastRelease(f.Doc))
			}
		}
	}
	op := d.teamSigner(t).AssignDomain(st.Heads, domain, email, after, d.now())
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

	Members []MemberView  `json:"members"`
	Owners  []string      `json:"owners"`
	Domains []DomainView  `json:"domains"`
	Invites []InviteView  `json:"invites"`
	History []HistoryLine `json:"history"`
	// BadLines are log lines that do not decode; Rejected have a bad
	// signature. Neither counts.
	BadLines []teamlog.BadLine `json:"bad_lines,omitempty"`
	Rejected int               `json:"rejected,omitempty"`
	Pending  int               `json:"pending,omitempty"`
}

// MemberView is one current member.
type MemberView struct {
	Email       string `json:"email"`
	Owner       bool   `json:"owner,omitempty"`
	Fingerprint string `json:"fingerprint"`
	Since       string `json:"since,omitempty"` // the signer's clock at add-member
	You         bool   `json:"you,omitempty"`
	// Pin compares peers.yaml with the log (spec 8.2): match, none, or
	// clash when another key is pinned.
	Pin string `json:"pin"`
}

// DomainView is an effective domain assignment.
type DomainView struct {
	Domain string `json:"domain"`
	Owner  string `json:"owner"`
	After  int64  `json:"after"`
}

// InviteView is an invite this installation issued for the team.
type InviteView struct {
	ID      string    `json:"id"`
	Email   string    `json:"email"`
	Expires time.Time `json:"expires"`
	State   string    `json:"state"` // open, used, expired, canceled
	UsedAt  time.Time `json:"used_at,omitzero"`
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
func (d *Daemon) Teams() []TeamView {
	out := []TeamView{}
	for _, t := range d.teamDirs() {
		out = append(out, d.teamView(t))
	}
	return out
}

func (d *Daemon) teamView(t teamDir) TeamView {
	v := TeamView{Team: t.Team, Name: t.Name, Dir: t.Dir, Log: t.Log, Me: "none",
		Members: []MemberView{}, Owners: []string{}, Domains: []DomainView{}, Invites: []InviteView{}, History: []HistoryLine{}}
	st, ops, bad, rej, err := d.load(t)
	if err != nil {
		v.Error = err.Error()
		return v
	}
	v.BadLines, v.Rejected, v.Pending = bad, len(rej), len(st.Pending)
	v.Owners = append(v.Owners, st.Owners...)
	self := teamlog.EncodeKey(d.pub)
	for _, m := range st.Members {
		mv := MemberView{Email: m.Email, Owner: m.Owner, Pin: "none"}
		if k, err := teamlog.DecodeKey(m.PubKey); err == nil {
			mv.Fingerprint = peerid.Display(peerid.Fingerprint(k))
			if bp.SameEmail(m.Email, d.self.Email) && m.PubKey == self {
				mv.You, mv.Pin = true, "match"
				v.Me = "member"
				if m.Owner {
					v.Me = "owner"
				}
			} else if p, ok := d.Peers.Get(m.Email); ok {
				mv.Pin = "match"
				if pk := p.Key(); pk == nil || !pk.Equal(k) || p.KeyChanged != nil {
					mv.Pin = "clash"
				}
			}
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
	for dm, a := range st.Domains {
		v.Domains = append(v.Domains, DomainView{Domain: dm, Owner: a.Owner, After: a.After})
	}
	sort.Slice(v.Domains, func(i, j int) bool { return v.Domains[i].Domain < v.Domains[j].Domain })
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
		}
		v.Invites = append(v.Invites, iv)
	}
	v.History = history(st, ops)
	return v
}

// history renders the log in causal order, oldest first.
func history(st *teamlog.State, ops []teamlog.Op) []HistoryLine {
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
		case teamlog.AssignDomain:
			text = fmt.Sprintf("assigned domain %s to %s from version %d", a.Domain, a.Owner, a.After+1)
		case teamlog.ReassignDocument:
			text = fmt.Sprintf("reassigned document %s from %s to %s from version %d", a.Doc, a.From, a.Owner, a.After+1)
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
func (d *Daemon) teamKeyFor(team, email string, key []byte) bool {
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
