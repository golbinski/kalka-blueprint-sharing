// Package store is the blueprint folder (spec 14): an index by path and
// kalka_id, a recursive watcher, atomic writes that check the file did not
// change underneath, conflict copies and safe names for new files.
package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/fsnotify/fsnotify"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
)

// Debounce is how long the watcher waits for a file to settle.
const Debounce = 300 * time.Millisecond

// PersonalDir is the folder of Personal documents (spec 14). Files
// directly in the folder count as Personal too.
const PersonalDir = "personal"

// ScopeOf is the scope a path relative to the folder lies in (spec 14):
// the top-level folder holding it, or "" for Personal (the folder itself,
// personal/, and so on). Whether a top-level folder is a team's is for the
// caller, which knows the team logs; the store uses scopes to count
// duplicates and to look documents up.
func ScopeOf(rel string) string {
	rel = filepath.ToSlash(rel)
	top, _, nested := strings.Cut(rel, "/")
	if !nested || strings.EqualFold(top, PersonalDir) {
		return ""
	}
	return top
}

// ErrDuplicate means two files carry the same kalka_id (spec 14).
type ErrDuplicate struct {
	KalkaID string
	Paths   []string
}

func (e *ErrDuplicate) Error() string {
	return fmt.Sprintf("two files have kalka_id %s (%s): syncing it is refused until one is removed", e.KalkaID, strings.Join(e.Paths, ", "))
}

// File is a blueprint as read from disk for one operation.
type File struct {
	Path string
	Raw  []byte
	Doc  *bp.Document
}

// Info describes an indexed file.
type Info struct {
	Path       string `json:"path"`
	Rel        string `json:"rel"`
	KalkaID    string `json:"kalka_id,omitempty"`
	Title      string `json:"title,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Rev        string `json:"rev,omitempty"`
	Entries    int    `json:"entries"`
	Errors     int    `json:"errors"`
	ParseError string `json:"parse_error,omitempty"`
	// Duplicate: another file in the same scope has this kalka_id.
	Duplicate bool      `json:"duplicate,omitempty"`
	Modified  time.Time `json:"modified"`
	// Scope is the top-level folder the file lies in, "" for Personal
	// (ScopeOf).
	Scope string `json:"scope,omitempty"`
	// Team, and for a domain document (spec 6.12) its domain and version.
	Team          string `json:"team,omitempty"`
	Domain        string `json:"domain,omitempty"`
	DomainVersion int64  `json:"domain_version,omitempty"`
	// From is the source kalka_id of a copy or a move (spec 20.7), so a
	// link to the source still resolves.
	From string `json:"from,omitempty"`
	hash [32]byte
}

// Event reports a change in the folder.
type Event struct {
	Path    string `json:"path"`
	Removed bool   `json:"removed,omitempty"`
	// External is false for changes caused by kalka's own writes.
	External bool `json:"external"`
}

// Store is one blueprint folder.
type Store struct {
	dir string

	mu    sync.Mutex
	index map[string]*Info    // by absolute path
	own   map[string][32]byte // content kalka wrote last, by path
	locks map[string]chan struct{}
	subs  map[chan Event]struct{}
}

// Open indexes dir, which must exist.
func Open(dir string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", abs)
	}
	s := &Store{dir: abs, index: map[string]*Info{}, own: map[string][32]byte{}, locks: map[string]chan struct{}{}, subs: map[chan Event]struct{}{}}
	s.scan(abs, nil)
	return s, nil
}

// Dir is the folder's absolute path.
func (s *Store) Dir() string { return s.dir }

// ignored reports whether a file or folder name is outside kalka's view.
func ignored(name string, isDir bool) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	if isDir {
		return false
	}
	return !strings.EqualFold(filepath.Ext(name), ".md") || bp.IsSnapshotName(name)
}

// dirsUnder lists root and the folders below it that are not ignored.
func dirsUnder(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if p != root && ignored(d.Name(), true) {
			return filepath.SkipDir
		}
		out = append(out, p)
		return nil
	})
	return out
}

// scan indexes every blueprint under root; dirs, if not nil, collects the
// folders seen so the watcher can add them. changed, if not nil, is told
// of every file whose index entry changed.
func (s *Store) scan(root string, dirs *[]string, changed ...func(p string, external bool)) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p != root && ignored(d.Name(), d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if dirs != nil {
				*dirs = append(*dirs, p)
			}
			return nil
		}
		if c, ext := s.refresh(p); c {
			for _, f := range changed {
				f(p, ext)
			}
		}
		return nil
	})
}

// refresh re-reads one path into the index and reports whether it changed
// and whether the change came from outside kalka.
func (s *Store) refresh(p string) (changed, external bool) {
	raw, err := os.ReadFile(p)
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.index[p]
	if err != nil {
		if old == nil {
			return false, false
		}
		delete(s.index, p)
		return true, true
	}
	h := sha256.Sum256(raw)
	own, wasOwn := s.own[p]
	if wasOwn && own == h {
		delete(s.own, p)
	}
	if old != nil && old.hash == h {
		return false, false
	}
	info := &Info{Path: p, hash: h}
	info.Rel, _ = filepath.Rel(s.dir, p)
	info.Rel = filepath.ToSlash(info.Rel) // the page's and the API's form, on every system
	info.Scope = ScopeOf(info.Rel)
	if st, err := os.Stat(p); err == nil {
		info.Modified = st.ModTime().UTC()
	}
	if d, err := bp.Parse(raw); err != nil {
		info.ParseError = err.Error()
	} else {
		info.KalkaID, info.Title, info.Owner, info.Rev = d.KalkaID(), d.Title(), d.Owner(), d.Rev()
		info.Team, info.Domain, info.DomainVersion = d.Team(), d.DomainName(), d.DomainVersion()
		_, _, info.From = d.TransferredFrom()
		for _, it := range d.Items() {
			if it.Entry != nil {
				info.Entries++
			}
		}
		for _, pr := range d.Problems() {
			if pr.Severity == bp.Error {
				info.Errors++
			}
		}
	}
	external = !(wasOwn && own == h)
	s.index[p] = info
	return true, external
}

// Info returns the index entry for an absolute path.
func (s *Store) Info(path string) (Info, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.index[path]
	if !ok {
		return Info{}, false
	}
	return *i, true
}

// List returns the indexed files sorted by relative path, with duplicates
// within a scope flagged.
func (s *Store) List() []Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := map[[2]string]int{}
	for _, i := range s.index {
		if i.KalkaID != "" {
			count[[2]string{i.Scope, i.KalkaID}]++
		}
	}
	out := make([]Info, 0, len(s.index))
	for _, i := range s.index {
		c := *i
		c.Duplicate = c.KalkaID != "" && count[[2]string{c.Scope, c.KalkaID}] > 1
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Info) int { return strings.Compare(a.Rel, b.Rel) })
	return out
}

// Contains resolves path and reports whether it lies inside the folder.
func (s *Store) Contains(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	if real, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		abs = filepath.Join(real, filepath.Base(abs))
	}
	rel, err := filepath.Rel(s.dir, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return abs, false
	}
	return abs, true
}

// Load reads a file inside the folder.
func (s *Store) Load(path string) (*File, error) {
	abs, ok := s.Contains(path)
	if !ok {
		return nil, fmt.Errorf("%s is not inside the blueprint folder %s", path, s.dir)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	d, err := bp.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", abs, err)
	}
	s.refresh(abs)
	return &File{Path: abs, Raw: raw, Doc: d}, nil
}

// Find returns the file with kalkaID anywhere in the folder, or nil if
// there is none. Two files with the same id give *ErrDuplicate.
func (s *Store) Find(kalkaID string) (*File, error) {
	return s.find(kalkaID, "")
}

// FindIn is Find within dir, such as a team's folder: documents are found
// by kalka_id within their scope (spec 14).
func (s *Store) FindIn(dir, kalkaID string) (*File, error) {
	return s.find(kalkaID, dir)
}

// Resolve is Find, falling back to a document copied or moved from
// kalkaID (spec 14, 20.7): a seed's parent, a budded link. Of several
// copies, the first by path.
func (s *Store) Resolve(kalkaID string) (*File, error) {
	if f, err := s.Find(kalkaID); f != nil || err != nil {
		return f, err
	}
	kalkaID = strings.ToLower(kalkaID)
	s.mu.Lock()
	var paths []string
	for p, i := range s.index {
		if i.From == kalkaID {
			paths = append(paths, p)
		}
	}
	s.mu.Unlock()
	slices.Sort(paths)
	for _, p := range paths {
		if f, err := s.Load(p); err == nil {
			return f, nil
		}
	}
	return nil, nil
}

func (s *Store) find(kalkaID, dir string) (*File, error) {
	kalkaID = strings.ToLower(kalkaID)
	paths := s.pathsFor(kalkaID, dir)
	if len(paths) == 0 {
		// The watcher may lag behind: rescan before deciding it is new.
		s.scan(s.dir, nil)
		paths = s.pathsFor(kalkaID, dir)
	}
	var found []*File
	for _, p := range paths {
		f, err := s.Load(p)
		if err != nil {
			continue
		}
		if f.Doc.KalkaID() == kalkaID {
			found = append(found, f)
		}
	}
	switch len(found) {
	case 0:
		return nil, nil
	case 1:
		return found[0], nil
	}
	dup := &ErrDuplicate{KalkaID: kalkaID}
	for _, f := range found {
		dup.Paths = append(dup.Paths, f.Path)
	}
	return nil, dup
}

func (s *Store) pathsFor(id, dir string) []string {
	prefix := filepath.Clean(dir) + string(filepath.Separator)
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for p, i := range s.index {
		if i.KalkaID == id && (dir == "" || strings.HasPrefix(p, prefix)) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// write stores data at path and records it as kalka's own write.
func (s *Store) write(path string, data, expect []byte) error {
	s.mu.Lock()
	s.own[path] = sha256.Sum256(data)
	s.mu.Unlock()
	if err := fsutil.WriteAtomic(path, data, expect); err != nil {
		s.mu.Lock()
		delete(s.own, path)
		s.mu.Unlock()
		return err
	}
	if changed, _ := s.refresh(path); changed {
		s.publish(Event{Path: path})
	}
	return nil
}

// Update replaces f's file with doc if the file still holds f.Raw;
// otherwise it returns fsutil.ErrChanged and the caller merges again.
func (s *Store) Update(f *File, doc *bp.Document) error {
	return s.write(f.Path, doc.Bytes(), f.Raw)
}

// Create writes a new blueprint under a sanitized version of the sender's
// suggested name, adding a numeric suffix if the name is taken.
func (s *Store) Create(doc *bp.Document, suggested string) (string, error) {
	return s.CreateIn(s.dir, doc, suggested)
}

// CreateIn is Create in a directory inside the folder, such as the
// directory of a seed's parent.
func (s *Store) CreateIn(dir string, doc *bp.Document, suggested string) (string, error) {
	if _, ok := s.Contains(dir); !ok && filepath.Clean(dir) != filepath.Clean(s.dir) {
		return "", fmt.Errorf("%s is outside the folder", dir)
	}
	base := SafeName(suggested)
	s.mu.Lock()
	var path string
	for i := 0; ; i++ {
		name := base + ".md"
		if i > 0 {
			name = fmt.Sprintf("%s-%d.md", base, i)
		}
		path = filepath.Join(dir, name)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			if _, taken := s.own[path]; !taken {
				break
			}
		}
	}
	s.own[path] = [32]byte{} // reserve until written
	s.mu.Unlock()
	return path, s.write(path, doc.Bytes(), nil)
}

// Move writes doc as a new file in dir, under f's name (with a suffix if
// taken), and removes f's file, which must still hold f.Raw (spec 20.1:
// publish moves the file into the team's folder). It returns the new path.
func (s *Store) Move(f *File, dir string, doc *bp.Document) (string, error) {
	raw, err := os.ReadFile(f.Path)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(raw, f.Raw) {
		return "", fsutil.ErrChanged
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path, err := s.CreateIn(dir, doc, strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path)))
	if err != nil {
		return "", err
	}
	if err := os.Remove(f.Path); err != nil {
		return path, fmt.Errorf("%s was written, but %s could not be removed: %w", path, f.Path, err)
	}
	s.Forget(f.Path)
	return path, nil
}

var reserved = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9])$`)

// SafeName turns a suggested file name into a base name (without .md)
// that is valid on every platform and cannot leave the folder.
func SafeName(suggested string) string {
	n := suggested
	if i := strings.LastIndexAny(n, `/\`); i >= 0 {
		n = n[i+1:]
	}
	if strings.EqualFold(filepath.Ext(n), ".md") {
		n = n[:len(n)-3]
	}
	var b strings.Builder
	for _, r := range n {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune(' ')
		default:
			b.WriteRune('-')
		}
	}
	n = strings.Trim(b.String(), " .-")
	if len(n) > 100 {
		n = strings.TrimRight(n[:100], " .")
	}
	if n == "" || reserved.MatchString(strings.SplitN(n, ".", 2)[0]) {
		n = "blueprint-" + n
		n = strings.TrimRight(n, "-")
	}
	return n
}

// Lock serializes operations on one kalka_id. It waits up to wait and
// reports false if the id stayed busy.
func (s *Store) Lock(kalkaID string, wait time.Duration) (unlock func(), ok bool) {
	s.mu.Lock()
	ch := s.locks[kalkaID]
	if ch == nil {
		ch = make(chan struct{}, 1)
		s.locks[kalkaID] = ch
	}
	s.mu.Unlock()
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, true
	case <-time.After(wait):
		return nil, false
	}
}

// Subscribe delivers folder events until cancel is called.
func (s *Store) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 32)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

func (s *Store) publish(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Watch follows changes in the folder until ctx ends. fsnotify is not
// recursive on Linux and macOS, so folders are added as they appear.
func (s *Store) Watch(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	var dirs []string
	s.scan(s.dir, &dirs)
	for _, d := range dirs {
		_ = w.Add(d)
	}

	timers := map[string]*time.Timer{}
	fire := make(chan string, 64)
	defer func() {
		for _, t := range timers {
			t.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			_ = err // overflow: a rescan on the next event catches up
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			name := filepath.Base(ev.Name)
			if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
				if !ignored(name, true) && ev.Has(fsnotify.Create) {
					// Watch first, then scan: a file written in between is
					// caught by one or the other. A folder copied or
					// unpacked in arrives with its files already inside,
					// so each is announced.
					for _, d := range dirsUnder(ev.Name) {
						_ = w.Add(d)
					}
					s.scan(ev.Name, nil, func(p string, external bool) {
						s.publish(Event{Path: p, External: external})
					})
					s.publish(Event{Path: ev.Name, External: true})
				}
				continue
			}
			if ignored(name, false) {
				// A removed folder: drop what was indexed below it.
				if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
					s.dropBelow(ev.Name)
				}
				continue
			}
			p := ev.Name
			if t := timers[p]; t != nil {
				t.Reset(Debounce)
			} else {
				timers[p] = time.AfterFunc(Debounce, func() { fire <- p })
			}
		case p := <-fire:
			delete(timers, p)
			if changed, external := s.refresh(p); changed {
				_, err := os.Stat(p)
				s.publish(Event{Path: p, Removed: err != nil, External: external})
			}
		}
	}
}

func (s *Store) dropBelow(dir string) {
	prefix := dir + string(filepath.Separator)
	var gone []string
	s.mu.Lock()
	for p := range s.index {
		if strings.HasPrefix(p, prefix) {
			delete(s.index, p)
			gone = append(gone, p)
		}
	}
	s.mu.Unlock()
	for _, p := range gone {
		s.publish(Event{Path: p, Removed: true, External: true})
	}
}

// Forget re-reads path after kalka removed or moved it, so the index does
// not wait for the watcher.
func (s *Store) Forget(path string) {
	if changed, _ := s.refresh(path); changed {
		s.publish(Event{Path: path, Removed: true})
	}
}

// SaveSnapshot keeps version v of the live domain document at path beside
// it, as `<name>@v<N>.md` (spec 6.12). The index never lists snapshots.
// An existing snapshot of that version is left alone: a version's plan
// never changes.
func (s *Store) SaveSnapshot(live string, v int64, doc *bp.Document) (string, error) {
	p := bp.SnapshotPath(live, v)
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	return p, fsutil.WriteAtomic(p, doc.Bytes(), nil)
}

// Snapshots lists the versions kept beside the live domain document at
// path, ascending.
func (s *Store) Snapshots(live string) []int64 {
	base := strings.TrimSuffix(live, filepath.Ext(live))
	matches, _ := filepath.Glob(globEscape(base) + "@v*.md")
	var out []int64
	for _, m := range matches {
		var v int64
		if _, err := fmt.Sscanf(strings.TrimPrefix(m, base+"@v"), "%d.md", &v); err == nil && v > 0 && bp.SnapshotPath(live, v) == m {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}

// Snapshot reads version v of the live domain document at path.
func (s *Store) Snapshot(live string, v int64) (*bp.Document, error) {
	raw, err := os.ReadFile(bp.SnapshotPath(live, v))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("version %d is not kept here", v)
		}
		return nil, err
	}
	return bp.Parse(raw)
}

// FindDomain lists the live documents of one domain of a team.
func (s *Store) FindDomain(team, domain string) []Info {
	var out []Info
	for _, in := range s.List() {
		if in.Team == team && in.Domain == domain {
			out = append(out, in)
		}
	}
	return out
}

func globEscape(p string) string {
	r := strings.NewReplacer("*", "[*]", "?", "[?]", "[", "[[]")
	return r.Replace(p)
}
