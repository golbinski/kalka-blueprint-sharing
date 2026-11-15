package core

// Location decides scope (spec 14, 20.1): a document belongs to the team
// whose folder it lies in, at any depth, or to Personal.

import (
	"fmt"
	"path/filepath"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
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
// lies in h's folder, `team:` names h, it has no format errors and no
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

// mismatch is what the views say about a file whose location and `team:`
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
