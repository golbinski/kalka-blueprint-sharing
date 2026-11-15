package core

// Waiting items (spec 11.3): what only the local user can move forward.
// All are derived from the files and the team logs, except the seen marks
// of handed-to-you items, which the daemon keeps in state.yaml and hands
// in through SetSeen.

import (
	"net/url"
	"sort"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/store"
	"github.com/golbinski/kalka/internal/teamlog"
)

// Waiting item kinds.
const (
	WaitTransfer  = "transfer"   // a pending transfer into a team you own
	WaitHanded    = "handed"     // handed off, reassigned or accepted to you
	WaitDisputed  = "disputed"   // ownership disputed, for team owners
	WaitOwnerGone = "owner-gone" // the owner is no longer a member, for team owners
)

// WaitingItem is one waiting item. At is the source's timestamp (another
// person's clock): it orders items and picks the oldest, nothing else.
type WaitingItem struct {
	Kind     string `json:"kind"`
	Team     string `json:"team"`      // hash
	TeamName string `json:"team_name"` // name
	At       string `json:"at"`
	// Target is the page's route for the item: the document, or the
	// team's waiting list for a transfer.
	Target   string `json:"target"`
	Title    string `json:"title"`
	Path     string `json:"path,omitempty"`
	KalkaID  string `json:"kalka_id,omitempty"`
	Transfer string `json:"transfer,omitempty"` // the transfer id
	// By is who caused it: the actor of a transfer, who handed off or
	// reassigned, the removed owner.
	By      string `json:"by,omitempty"`
	Message string `json:"message"`

	mark string // handed: what a seen mark names (a release id or an op hash)
}

// WaitingSummary is the waiting part of the summary (spec 13).
type WaitingSummary struct {
	Total  int           `json:"total"`
	Oldest *WaitingAt    `json:"oldest,omitempty"`
	Teams  []WaitingTeam `json:"teams"`
}

// WaitingAt is the oldest item: its team, time and the team's waiting
// list, where the tray opens the window (spec 22.1).
type WaitingAt struct {
	Team   string `json:"team"`
	At     string `json:"at"`
	Target string `json:"target"`
}

// WaitingTeam is one team's count.
type WaitingTeam struct {
	Team  string `json:"team"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// WaitingRoute is a team's waiting list in the page.
func WaitingRoute(teamName string) string { return "#/t/" + teamName + "/waiting" }

// DocRoute is a document in the page, by its path in the folder.
func DocRoute(rel string) string { return "#/doc/" + url.PathEscape(rel) }

// SetSeen gives the node the seen marks of handed-to-you items: document
// id -> the release id or reassignment it was seen at.
func (d *Node) SetSeen(f func() map[string]string) {
	d.mu.Lock()
	d.seen = f
	d.mu.Unlock()
}

func (d *Node) seenMarks() map[string]string {
	d.mu.Lock()
	f := d.seen
	d.mu.Unlock()
	if f == nil {
		return nil
	}
	return f()
}

// Waiting lists the waiting items of one scope ("" for every team, a
// team's name or hash, or "personal", which has none), oldest first.
func (d *Node) Waiting(scope string) ([]WaitingItem, error) {
	var dirs []teamDir
	switch scope {
	case "":
		dirs = d.teamDirs()
	case store.PersonalDir:
	default:
		t, err := d.findTeam(scope)
		if err != nil {
			return nil, err
		}
		dirs = []teamDir{t}
	}
	seen := d.seenMarks()
	out := []WaitingItem{}
	for _, it := range d.waitingIn(dirs) {
		if it.Kind == WaitHanded && seen[it.KalkaID] == it.mark {
			continue
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].At != out[j].At {
			return out[i].At < out[j].At
		}
		return out[i].Target < out[j].Target
	})
	return out, nil
}

// waitingIn derives every item of the teams, seen or not.
func (d *Node) waitingIn(dirs []teamDir) []WaitingItem {
	var out []WaitingItem
	teams := map[string]bool{}
	for _, t := range dirs {
		teams[t.Team] = true
		h, err := d.heldTeam(t.Team)
		if err != nil || !h.isMember(d.self.Email, d.pub) {
			continue
		}
		me, _ := memberOf(h.st, d.self.Email)
		for id, td := range d.replicaSet(h) {
			if td.tomb || td.pending || td.doc == nil {
				continue
			}
			out = append(out, d.docWaiting(h, me.Owner, id, td)...)
		}
	}
	for _, v := range d.Transfers() {
		if !teams[v.teamHash] || !v.CanDecide || v.Status != TransferPending {
			continue
		}
		out = append(out, WaitingItem{Kind: WaitTransfer, Team: v.teamHash, TeamName: v.Team, At: v.At,
			Target: WaitingRoute(v.Team), Title: v.Title, Transfer: v.ID, By: v.Actor,
			Message: v.Actor + " offers “" + v.Title + "” to " + v.Team + " (" + v.Mode + ")"})
	}
	return out
}

// docWaiting is what one team document asks of the local user.
func (d *Node) docWaiting(h *held, teamOwner bool, id string, td teamDoc) []WaitingItem {
	ch := merge.Releases(entriesOf(td.doc), id, h.team())
	base := WaitingItem{Team: h.dir.Team, TeamName: h.dir.Name, Title: td.doc.Title(), KalkaID: id,
		Path: td.path, Target: DocRoute(d.rel(td.path))}
	var out []WaitingItem
	if it, ok := d.handed(h, id, ch, base); ok {
		out = append(out, it)
	}
	if !teamOwner {
		return out
	}
	if ch.Disputed {
		it := base
		it.Kind = WaitDisputed
		for _, e := range entriesOf(td.doc) {
			if e.Kind == bp.KindRelease && strings.HasPrefix(ch.Void[e.ID], "ownership disputed") && e.Timestamp > it.At {
				it.At = e.Timestamp
			}
		}
		it.Message = "ownership of “" + it.Title + "” is disputed: two releases of one version; reassign it to settle it"
		out = append(out, it)
	}
	if next := ch.NextSigner(); next != "" {
		if _, ok := memberOf(h.st, next); !ok {
			it := base
			it.Kind, it.By = WaitOwnerGone, next
			it.At = removedAt(h, next)
			it.Message = "the owner of “" + it.Title + "”, " + next + ", is no longer a member; reassign it"
			out = append(out, it)
		}
	}
	return out
}

// handed: the document became the local user's by someone else's act,
// and the local user has made no release of their own since (spec 11.3).
// The release a new owner's kalka makes on its own after a reassignment
// or an accept does not count as theirs.
func (d *Node) handed(h *held, id string, ch merge.ReleaseChain, base WaitingItem) (WaitingItem, bool) {
	if !bp.SameEmail(ch.NextSigner(), d.self.Email) {
		return WaitingItem{}, false
	}
	head, hasHead := ch.Head()
	var version int64
	if hasHead {
		version = head.Release.Version
	}
	// The last anchor naming the local user, at or just below the head.
	as := h.st.Reassigned[strings.ToLower(id)]
	if n := len(as); n > 0 {
		a := as[n-1]
		if bp.SameEmail(a.Owner, d.self.Email) && version <= a.After+1 {
			if bp.SameEmail(a.By, d.self.Email) {
				return WaitingItem{}, false // a take-over or an accept in one's own name
			}
			it := base
			it.Kind, it.At, it.By, it.mark = WaitHanded, a.At, a.By, a.Op
			if a.Transfer != "" {
				it.Message = a.By + " accepted “" + it.Title + "” into " + it.TeamName + " in your name"
			} else {
				it.Message = a.By + " reassigned “" + it.Title + "” to you"
			}
			return it, true
		}
	}
	if hasHead && !bp.SameEmail(head.Entry.Author, d.self.Email) && bp.SameEmail(head.Release.Owner, d.self.Email) {
		it := base
		it.Kind, it.At, it.By, it.mark = WaitHanded, head.Entry.Timestamp, head.Entry.Author, head.Entry.ID
		it.Message = head.Entry.Author + " handed “" + it.Title + "” off to you"
		return it, true
	}
	return WaitingItem{}, false
}

// removedAt is when email was last removed from the team, by the log.
func removedAt(h *held, email string) string {
	at := ""
	for _, o := range h.ops {
		if o.Kind == teamlog.RemoveMember && bp.SameEmail(o.Args.Email, email) && h.st.Effective[o.Hash()] && o.At > at {
			at = o.At
		}
	}
	return at
}

// WaitingMarks are the current seen marks of handed-to-you items by
// document id, seen or not: the daemon keeps a mark only while it is
// current (spec 11.3, "a mark is dropped when that release is no longer
// the head").
func (d *Node) WaitingMarks() map[string]string {
	out := map[string]string{}
	for _, it := range d.waitingIn(d.teamDirs()) {
		if it.Kind == WaitHanded {
			out[it.KalkaID] = it.mark
		}
	}
	return out
}

// SeenMark is the mark that clears the handed-to-you item of the document
// at path, if it has one.
func (d *Node) SeenMark(path string) (id, mark string, ok bool) {
	in, found := d.Store.Info(d.resolve(path))
	if !found || in.KalkaID == "" {
		return "", "", false
	}
	id = strings.ToLower(in.KalkaID)
	m, ok := d.WaitingMarks()[id]
	return id, m, ok
}

// WaitingSummary counts the items of every team.
func (d *Node) WaitingSummary() WaitingSummary {
	items, _ := d.Waiting("")
	return summarize(items, d.teamDirs())
}

// summarize is the summary of items, with the teams in the order held.
func summarize(items []WaitingItem, dirs []teamDir) WaitingSummary {
	s := WaitingSummary{Teams: []WaitingTeam{}, Total: len(items)}
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Team]++
		if s.Oldest == nil || it.At < s.Oldest.At {
			s.Oldest = &WaitingAt{Team: it.TeamName, At: it.At, Target: WaitingRoute(it.TeamName)}
		}
	}
	for _, t := range dirs {
		if n := counts[t.Team]; n > 0 {
			s.Teams = append(s.Teams, WaitingTeam{Team: t.Team, Name: t.Name, Count: n})
		}
	}
	return s
}
