package check

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	bp "github.com/golbinski/kalka/internal/blueprint"
)

// Folder is a Domains read from files and folders: a version is its
// snapshot `*@v<N>.md` first, then a domain document whose kalka_version
// is that version (spec 12.4, 18.2). A document without kalka_team serves
// any team, so a repository's copy can be named with --domain.
//
// It also serves the plan graph (Docs) from every document it read, and a
// domain's children (Siblings).
type Folder struct {
	byKey    map[string]map[int64]found
	byID     map[string]*bp.Document
	children map[string][]*bp.Document
	seen     map[string]bool
	// searched: a folder or document was read beyond the files checked,
	// so a plan node missing here is missing, not merely not given.
	searched bool
}

type found struct {
	path     string
	snapshot bool
}

var snapVersionRe = regexp.MustCompile(`(?i)@v([1-9][0-9]*)\.md$`)

// NewFolder reads the domain documents under the paths, files or folders.
// Unreadable files are skipped.
func NewFolder(paths ...string) *Folder {
	f := &Folder{byKey: map[string]map[int64]found{}, byID: map[string]*bp.Document{}, children: map[string][]*bp.Document{}, seen: map[string]bool{}}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		f.searched = true
		if !st.IsDir() {
			f.add(p)
			continue
		}
		_ = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != p && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".md") {
				f.add(path)
			}
			return nil
		})
	}
	return f
}

// Add reads one more file into the folder.
func (f *Folder) Add(path string) { f.add(path) }

func (f *Folder) add(path string) {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if f.seen[path] {
		return
	}
	f.seen[path] = true
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > bp.MaxFile {
		return
	}
	doc, err := bp.Parse(raw)
	if err != nil {
		return
	}
	if id := doc.KalkaID(); id != "" && !bp.IsSnapshotName(filepath.Base(path)) && !doc.IsDomain() {
		if _, dup := f.byID[id]; !dup {
			f.byID[id] = doc
			if p := doc.PlanParent(); p != "" {
				f.children[p] = append(f.children[p], doc)
			}
		}
	}
	if !doc.IsDomain() || doc.DomainName() == "" {
		return
	}
	v, snap := doc.DomainVersion(), false
	if m := snapVersionRe.FindStringSubmatch(filepath.Base(path)); m != nil {
		v, _ = strconv.ParseInt(m[1], 10, 64)
		snap = true
	}
	if v == 0 {
		return
	}
	k := doc.Team() + "/" + doc.DomainName()
	if f.byKey[k] == nil {
		f.byKey[k] = map[int64]found{}
	}
	if old, ok := f.byKey[k][v]; ok && (old.snapshot || !snap) && old.path <= path {
		return // a snapshot wins; between equals, the first path
	}
	f.byKey[k][v] = found{path, snap}
}

// versions are those of team/domain, and of a document naming no team.
func (f *Folder) versions(team, domain string) map[int64]found {
	out := map[int64]found{}
	for _, k := range []string{"/" + domain, team + "/" + domain} {
		for v, x := range f.byKey[k] {
			out[v] = x
		}
	}
	return out
}

// Version implements Domains.
func (f *Folder) Version(team, domain string, v int64) *bp.Document {
	x, ok := f.versions(team, domain)[v]
	if !ok {
		return nil
	}
	raw, err := os.ReadFile(x.path)
	if err != nil {
		return nil
	}
	doc, err := bp.Parse(raw)
	if err != nil {
		return nil
	}
	return doc
}

// Latest implements Domains.
func (f *Folder) Latest(team, domain string) int64 {
	var vs []int64
	for v := range f.versions(team, domain) {
		vs = append(vs, v)
	}
	if len(vs) == 0 {
		return 0
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i] < vs[j] })
	return vs[len(vs)-1]
}

// ByID implements Docs.
func (f *Folder) ByID(id string) *bp.Document { return f.byID[strings.ToLower(id)] }

// Searched implements Searcher: whether documents beyond the files
// checked were read.
func (f *Folder) Searched() bool { return f.searched }

// Children implements Docs.
func (f *Folder) Children(id string) []*bp.Document { return f.children[strings.ToLower(id)] }

// DomainChildren implements Siblings through domainChildren: the live
// documents, at their newest version, of the domains whose parent is
// domain.
func (f *Folder) domainChildren(team, domain string) []*bp.Document {
	var out []*bp.Document
	var keys []string
	for k := range f.byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	seen := map[string]bool{}
	for _, k := range keys {
		t, name, _ := strings.Cut(k, "/")
		if (t != "" && t != team) || seen[name] {
			continue
		}
		v := f.Latest(team, name)
		doc := f.Version(team, name, v)
		if doc == nil {
			continue
		}
		if up, ok := doc.DomainParent(); ok && up.Domain == domain {
			seen[name] = true
			out = append(out, doc)
		}
	}
	return out
}

// FolderSiblings adapts a Folder to Siblings.
type folderSiblings struct{ *Folder }

func (s folderSiblings) Children(team, domain string) []*bp.Document {
	return s.domainChildren(team, domain)
}

// Tree is the folder as Domains with Siblings, for the checker.
func (f *Folder) Tree() Domains { return folderSiblings{f} }
