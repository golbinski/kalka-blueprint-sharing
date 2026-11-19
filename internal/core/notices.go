package core

// Notices (spec 22.4): the core decides what is worth a notification and
// publishes it on the bus as a "notice" event; the desktop shell or the
// headless daemon shows it.

import (
	"fmt"
	"github.com/golbinski/kalka/internal/addr"
	"sort"
	"strings"
	"sync"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/merge"
)

// Notice kinds.
const (
	NoticeComments = "comments"     // new comments on a document you own
	NoticeJoined   = "joined"       // someone joined with your invite
	NoticeJoinDone = "join-done"    // your join completed
	NoticeJoinFail = "join-failed"  // your join failed for good
	NoticeDomain   = "domain-newer" // a domain you reference has a newer version
	NoticeUpdate   = "update"       // a new kalka is downloaded and waits for a restart
)

// Notice is one notification.
type Notice struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Message string `json:"message"`
	// Path is the document the notice is about, if any (a click opens
	// it); Team the team, if any.
	Path string `json:"path,omitempty"`
	Team string `json:"team,omitempty"`
}

// notices holds the switch and what was already said.
type notices struct {
	mu  sync.Mutex
	off bool
	// domainSaid dedupes "newer domain version": one per domain and
	// version.
	domainSaid map[string]bool
	// opsSeen holds the team log operations already noticed, or made
	// here (reassignments, spec 7.3).
	opsSeen map[string]bool
}

// SetNotifications turns notices on or off (spec 22.2, Notifications).
func (d *Node) SetNotifications(on bool) {
	d.notes.mu.Lock()
	d.notes.off = !on
	d.notes.mu.Unlock()
}

func (d *Node) notice(n Notice) {
	d.notes.mu.Lock()
	off := d.notes.off
	d.notes.mu.Unlock()
	if off {
		return
	}
	d.logf("notice: %s", n.Message)
	d.bus.Publish(BusEvent{Type: "notice", Data: n})
}

// commentNotice compares a document the local user owns before and after
// a merge from a peer: entries by someone else that are comments,
// deviations or custom kinds make one notice. A first copy (before nil)
// raises none.
func (d *Node) commentNotice(before, after *bp.Document, path string) {
	if before == nil || after == nil || !bp.SameEmail(after.Owner(), d.self.Email) {
		return
	}
	had := map[string]bool{}
	for _, e := range entriesOf(before) {
		had[e.ID] = true
	}
	n, by := 0, map[string]bool{}
	for _, e := range entriesOf(after) {
		if had[e.ID] || bp.SameEmail(e.Author, d.self.Email) {
			continue
		}
		switch e.Kind {
		case bp.KindRelease, bp.KindBudded:
			continue
		}
		n++
		by[addr.Canonical(e.Author)] = true
	}
	if n == 0 {
		return
	}
	var who []string
	for a := range by {
		who = append(who, a)
	}
	sort.Strings(who)
	what := "new comment"
	if n > 1 {
		what = fmt.Sprintf("%d new comments", n)
	}
	d.notice(Notice{Kind: NoticeComments, Title: after.Title(), Path: path,
		Message: fmt.Sprintf("%s on “%s” from %s", what, after.Title(), strings.Join(who, ", "))})
}

// domainNotice: a received release raised a domain to a version above
// the one a document owned by the local user references.
func (d *Node) domainNotice(h *held, doc *bp.Document) {
	if !doc.IsDomain() {
		return
	}
	ch := merge.Releases(entriesOf(doc), doc.KalkaID(), h.team())
	head, ok := ch.Head()
	if !ok {
		return
	}
	v, dom := head.Release.Version, doc.DomainName()
	key := fmt.Sprintf("%s/%s@v%d", h.dir.Name, dom, v)
	for _, in := range d.Store.List() {
		f, err := d.Store.Load(in.Path)
		// A document with no owner line yet is the local user's own draft.
		if err != nil || (f.Doc.Owner() != "" && !bp.SameEmail(f.Doc.Owner(), d.self.Email)) {
			continue
		}
		ref, ok := f.Doc.DomainRef()
		if !ok || ref.Team != h.dir.Name || ref.Domain != dom || ref.Version >= v {
			continue
		}
		d.notes.mu.Lock()
		said := d.notes.domainSaid[key]
		if d.notes.domainSaid == nil {
			d.notes.domainSaid = map[string]bool{}
		}
		d.notes.domainSaid[key] = true
		d.notes.mu.Unlock()
		if said {
			return
		}
		d.notice(Notice{Kind: NoticeDomain, Title: doc.Title(), Path: in.Path, Team: h.dir.Name,
			Message: fmt.Sprintf("%s/%s has version %d; “%s” references version %d", h.dir.Name, dom, v, f.Doc.Title(), ref.Version)})
		return
	}
}
