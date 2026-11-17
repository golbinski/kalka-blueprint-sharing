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
type Folder struct {
	byKey map[string]map[int64]found
}

type found struct {
	path     string
	snapshot bool
}

var snapVersionRe = regexp.MustCompile(`(?i)@v([1-9][0-9]*)\.md$`)

// NewFolder reads the domain documents under the paths, files or folders.
// Unreadable files are skipped.
func NewFolder(paths ...string) *Folder {
	f := &Folder{byKey: map[string]map[int64]found{}}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
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

func (f *Folder) add(path string) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > bp.MaxFile {
		return
	}
	doc, err := bp.Parse(raw)
	if err != nil || !doc.IsDomain() || doc.DomainName() == "" {
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
