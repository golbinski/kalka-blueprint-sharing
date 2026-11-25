package core

// Read marks: which entries of each team document the user has seen in
// the page, so "Needs you" lists what is new since they last looked
// (spec 11.3). Local state only, in read.json beside the config; it never
// leaves this machine and is never a waiting item.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/store"
)

// ReadFile holds the read marks.
const ReadFile = "read.json"

// readMarks is read.json: kalka_id -> the entry ids seen.
type readMarks struct {
	Docs map[string][]string `json:"docs"`
}

// Unread is a team document with entries the user has not seen.
type Unread struct {
	Path     string `json:"path"`
	Rel      string `json:"rel"`
	KalkaID  string `json:"kalka_id"`
	Title    string `json:"title"`
	Team     string `json:"team"`
	Owner    string `json:"owner,omitempty"`
	Mine     bool   `json:"mine,omitempty"`
	Comments int    `json:"comments"`
	// Deviations are counted apart: what was done differently; and
	// approvals, by the owner (spec 6.18).
	Deviations int      `json:"deviations"`
	Approvals  int      `json:"approvals"`
	Authors    []string `json:"authors"`
	// Never: someone else's document arrived and was never opened here.
	Never bool      `json:"never,omitempty"`
	Since time.Time `json:"since,omitzero"`
}

// newKinds are the entries that count as new to read.
var newKinds = map[string]bool{bp.KindComment: true, bp.KindDeviation: true, bp.KindApproved: true}

func (d *Node) readPath() string { return filepath.Join(d.dir, ReadFile) }

// loadRead reads read.json. A missing file is made from everything held
// (Run does so at start), so a first start lists nothing as new.
func (d *Node) loadRead() readMarks {
	m := readMarks{Docs: map[string][]string{}}
	raw, err := os.ReadFile(d.readPath())
	if err == nil && json.Unmarshal(raw, &m) == nil && m.Docs != nil {
		return m
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return m
	}
	m.Docs = map[string][]string{}
	for _, in := range d.Store.List() {
		if in.KalkaID != "" {
			m.Docs[in.KalkaID] = noteIDs(in.Notes)
		}
	}
	_ = d.saveRead(m)
	return m
}

func (d *Node) saveRead(m readMarks) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return fsutil.WritePrivate(d.readPath(), raw)
}

func noteIDs(ns []store.Note) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.ID)
	}
	return out
}

// MarkRead records every entry the document holds now as seen.
func (d *Node) MarkRead(path string) error {
	in, ok := d.Store.Info(d.resolve(path))
	if !ok || in.KalkaID == "" {
		return nil
	}
	d.readMu.Lock()
	m := d.loadRead()
	ids := noteIDs(in.Notes)
	old := m.Docs[in.KalkaID]
	changed := len(old) != len(ids)
	if !changed {
		seen := map[string]bool{}
		for _, id := range old {
			seen[id] = true
		}
		for _, id := range ids {
			if !seen[id] {
				changed = true
			}
		}
	}
	if changed {
		// Keep what was seen before: a merge can drop and bring back.
		seen := map[string]bool{}
		for _, id := range ids {
			seen[id] = true
		}
		for _, id := range old {
			if !seen[id] {
				ids = append(ids, id)
			}
		}
		m.Docs[in.KalkaID] = ids
		err := d.saveRead(m)
		d.readMu.Unlock()
		d.bus.Publish(BusEvent{Type: "waiting"})
		return err
	}
	d.readMu.Unlock()
	return nil
}

// Unread lists the team documents with comments or deviations by others
// that the user has not seen here, oldest news first.
func (d *Node) Unread() []Unread {
	d.readMu.Lock()
	m := d.loadRead()
	d.readMu.Unlock()
	teams := map[string]string{}
	for _, t := range d.teamDirs() {
		teams[filepath.Base(t.Dir)] = t.Name
	}
	out := []Unread{}
	for _, in := range d.Store.List() {
		team, ok := teams[in.Scope]
		if !ok || in.KalkaID == "" || in.ParseError != "" {
			continue
		}
		seen, opened := m.Docs[in.KalkaID]
		known := map[string]bool{}
		for _, id := range seen {
			known[id] = true
		}
		mine := bp.SameEmail(in.Owner, d.self.Email)
		u := Unread{Path: in.Path, Rel: in.Rel, KalkaID: in.KalkaID, Title: in.Title, Team: team, Owner: in.Owner,
			Mine: mine, Never: !opened && !mine}
		who := map[string]bool{}
		for _, n := range in.Notes {
			if known[n.ID] || !newKinds[n.Kind] || bp.SameEmail(n.Author, d.self.Email) {
				continue
			}
			switch n.Kind {
			case bp.KindDeviation:
				u.Deviations++
			case bp.KindApproved:
				u.Approvals++
			default:
				u.Comments++
			}
			if !who[n.Author] {
				who[n.Author] = true
				u.Authors = append(u.Authors, n.Author)
			}
			if u.Since.IsZero() || n.At.Before(u.Since) {
				u.Since = n.At
			}
		}
		if u.Comments+u.Deviations+u.Approvals == 0 && !u.Never {
			continue
		}
		if u.Authors == nil {
			u.Authors = []string{}
		}
		if u.Since.IsZero() {
			u.Since = in.Modified
		}
		out = append(out, u)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}

// UnreadCounts is the number of unseen comments, deviations and
// approvals per document path, for the lists' "N new".
func (d *Node) UnreadCounts() map[string]int {
	out := map[string]int{}
	for _, u := range d.Unread() {
		out[u.Path] = u.Comments + u.Deviations + u.Approvals
	}
	return out
}
