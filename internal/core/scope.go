package core

// Location decides scope (spec 14, 20.1): a document belongs to the team
// whose folder it lies in, at any depth, or to Personal.

import (
	"fmt"
	"path/filepath"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/store"
)

// teamDirAt returns the team whose folder holds path, if any.
func (d *Node) teamDirAt(path string) (teamDir, bool) {
	rel, err := filepath.Rel(d.Store.Dir(), path)
	if err != nil {
		return teamDir{}, false
	}
	top := store.ScopeOf(rel)
	if top == "" {
		return teamDir{}, false
	}
	for _, t := range d.teamDirs() {
		if filepath.Base(t.Dir) == top {
			return t, true
		}
	}
	return teamDir{}, false
}

// inDir reports whether path lies below dir.
func inDir(dir, path string) bool {
	return strings.HasPrefix(path, filepath.Clean(dir)+string(filepath.Separator))
}

// heldAt returns the team whose folder holds path, when the local user is
// a member of it.
func (d *Node) heldAt(path string) (*held, error) {
	t, ok := d.teamDirAt(path)
	if !ok {
		return nil, fmt.Errorf("%s lies in Personal, not in a team's folder", d.rel(path))
	}
	h, err := d.heldTeam(t.Team)
	if err != nil {
		return nil, err
	}
	if !h.isMember(d.self.Email, d.pub) {
		return nil, fmt.Errorf("you are not a member of %s", t.Name)
	}
	return h, nil
}

// Scope states of a document (spec 14).
const (
	ScopePersonal = "personal"
	ScopeTeam     = "team"
)

// Mismatch messages: shown, never replicated (spec 14).
const (
	MismatchNotShared   = "not shared: publish it or move it to Personal"
	MismatchInPersonal  = "has team: but lies in Personal"
	MismatchOtherTeam   = "its team: names another team than the folder it lies in"
	MismatchUnreleased  = "not shared: it has no valid release; publish it or move it to Personal"
	MismatchDuplicateID = "another file in this team's folder has the same kalka_id"
)

// isTeamDoc is the one test of what replicates (spec 14, 20.1): the file
// lies in h's folder, `kalka_team:` names h, it has no format errors and no
// duplicate in the scope, and it has a valid release under h's log.
// dup is the store's duplicate flag for path. It returns "" for a team
// document, else why not.
func isTeamDoc(h *held, path string, doc *bp.Document, dup bool) string {
	switch {
	case !inDir(h.dir.Dir, path):
		return MismatchInPersonal
	case doc.Team() == "":
		return MismatchNotShared
	case doc.Team() != h.dir.Name:
		return MismatchOtherTeam
	case doc.HasErrors():
		return MismatchNotShared
	}
	if dup {
		return MismatchDuplicateID
	}
	if _, _, ok := describe(h, doc); !ok {
		return MismatchUnreleased
	}
	return ""
}

// mismatch is what the views say about a file whose location and `kalka_team:`
// disagree, or "" when they agree.
func (d *Node) mismatch(path string, doc *bp.Document) string {
	t, inTeam := d.teamDirAt(path)
	if !inTeam {
		if doc.Team() != "" {
			return MismatchInPersonal
		}
		return ""
	}
	h, err := d.heldTeam(t.Team)
	if err != nil {
		return ""
	}
	return isTeamDoc(h, path, doc, d.duplicate(path))
}

// duplicate reports whether another file in path's scope has its id.
func (d *Node) duplicate(path string) bool {
	for _, in := range d.Store.List() {
		if in.Path == path {
			return in.Duplicate
		}
	}
	return false
}

// Scope is a list's scope (spec 13, `?team=`): "" is everything, a team's
// name or hash its folder, ScopePersonal the Personal folder.
type Scope struct {
	All      bool
	Personal bool
	Team     teamDir
}

// ResolveScope reads a scope parameter.
func (d *Node) ResolveScope(s string) (Scope, error) {
	switch s {
	case "":
		return Scope{All: true}, nil
	case ScopePersonal:
		return Scope{Personal: true}, nil
	}
	t, err := d.findTeam(s)
	if err != nil {
		return Scope{}, err
	}
	return Scope{Team: t}, nil
}

// Holds reports whether the file at path (absolute, or relative to the
// folder) is in the scope.
func (d *Node) Holds(sc Scope, path string) bool {
	if sc.All {
		return true
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(d.Store.Dir(), filepath.FromSlash(path))
	}
	if sc.Personal {
		return inDir(filepath.Join(d.Store.Dir(), store.PersonalDir), path)
	}
	return inDir(sc.Team.Dir, path)
}

// HoldsTeam reports whether a team, by hash, is the scope's.
func (sc Scope) HoldsTeam(hash string) bool { return sc.All || (!sc.Personal && sc.Team.Team == hash) }

// TransfersIn lists the transfers of a scope.
func (d *Node) TransfersIn(sc Scope) []TransferView {
	out := []TransferView{}
	for _, v := range d.Transfers() {
		if sc.HoldsTeam(v.teamHash) {
			out = append(out, v)
		}
	}
	return out
}

// SeedsIn lists the seeds of a scope.
func (d *Node) SeedsIn(sc Scope) []SeedView {
	out := []SeedView{}
	for _, v := range d.Seeds() {
		if d.Holds(sc, v.Rel) {
			out = append(out, v)
		}
	}
	return out
}

// DomainsIn lists the domain documents of a scope.
func (d *Node) DomainsIn(sc Scope) []DomainRow {
	out := []DomainRow{}
	for _, r := range d.Domains() {
		if d.Holds(sc, r.Path) {
			out = append(out, r)
		}
	}
	return out
}

// BlueprintsIn lists the indexed files of a scope.
func (d *Node) BlueprintsIn(sc Scope) []store.Info {
	out := []store.Info{}
	unread := d.UnreadCounts()
	for _, in := range d.Store.List() {
		if d.Holds(sc, in.Path) {
			in.New = unread[in.Path]
			d.releaseState(&in)
			out = append(out, in)
		}
	}
	return out
}

// releaseState fills a published blueprint's version and whether its
// plan has edits the team has not been sent.
func (d *Node) releaseState(in *store.Info) {
	if in.Team == "" || in.Duplicate {
		return
	}
	h, err := d.heldAt(in.Path)
	if err != nil || h.dir.Name != in.Team {
		return
	}
	f, err := d.Store.Load(in.Path)
	if err != nil {
		return
	}
	head, ok := merge.Releases(entriesOf(f.Doc), f.Doc.KalkaID(), h.team()).Head()
	if !ok {
		return
	}
	in.Version = head.Release.Version
	in.Unreleased = head.Release.Plan != f.Doc.PlanHash()
}
