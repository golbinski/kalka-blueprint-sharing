// Package store is the blueprint folder (spec 14): an index by path and
// kalka_id, a recursive watcher, atomic writes that check the file did not
// change underneath, conflict copies and safe names for new files.
package store

import (
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
	Path       string    `json:"path"`
	Rel        string    `json:"rel"`
	KalkaID    string    `json:"kalka_id,omitempty"`
	Title      string    `json:"title,omitempty"`
	Owner      string    `json:"owner,omitempty"`
	Rev        string    `json:"rev,omitempty"`
	Entries    int       `json:"entries"`
	Errors     int       `json:"errors"`
	ParseError string    `json:"parse_error,omitempty"`
	Duplicate  bool      `json:"duplicate,omitempty"`
	Modified   time.Time `json:"modified"`
	// OwnerEdited means owner: changed in an edit outside kalka with no new
	// handoff entry, so it is a manual change without the protection of a
	// handoff (spec 7.4). It clears on kalka's next write.
	OwnerEdited bool `json:"owner_edited,omitempty"`
	hash        [32]byte
	handoffs    int
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
	return !strings.EqualFold(filepath.Ext(name), ".md") || conflictName.MatchString(name)
}

var conflictName = regexp.MustCompile(`(?i)\.conflict-[^.]*\.md$`)

// scan indexes every blueprint under root; dirs, if not nil, collects the
// folders seen so the watcher can add them.
func (s *Store) scan(root string, dirs *[]string) {
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
		s.refresh(p)
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
	if st, err := os.Stat(p); err == nil {
		info.Modified = st.ModTime().UTC()
	}
	if d, err := bp.Parse(raw); err != nil {
		info.ParseError = err.Error()
	} else {
		info.KalkaID, info.Title, info.Owner, info.Rev = d.KalkaID(), d.Title(), d.Owner(), d.Rev()
		for _, it := range d.Items() {
			if it.Entry != nil {
				info.Entries++
				if it.Entry.Kind == bp.KindHandoff {
					info.handoffs++
				}
			}
		}
		for _, pr := range d.Problems() {
			if pr.Severity == bp.Error {
				info.Errors++
			}
		}
	}
	external = !(wasOwn && own == h)
	if external && old != nil && old.KalkaID != "" && old.KalkaID == info.KalkaID {
		edited := !bp.SameEmail(old.Owner, info.Owner) && info.handoffs <= old.handoffs
		info.OwnerEdited = edited || (old.OwnerEdited && bp.SameEmail(old.Owner, info.Owner))
	}
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
// flagged.
func (s *Store) List() []Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := map[string]int{}
	for _, i := range s.index {
		if i.KalkaID != "" {
			count[i.KalkaID]++
		}
	}
	out := make([]Info, 0, len(s.index))
	for _, i := range s.index {
		c := *i
		c.Duplicate = c.KalkaID != "" && count[c.KalkaID] > 1
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

// Find returns the file with kalkaID, or nil if there is none. Two files
// with the same id give *ErrDuplicate.
func (s *Store) Find(kalkaID string) (*File, error) {
	kalkaID = strings.ToLower(kalkaID)
	paths := s.pathsFor(kalkaID)
	if len(paths) == 0 {
		// The watcher may lag behind: rescan before deciding it is new.
		s.scan(s.dir, nil)
		paths = s.pathsFor(kalkaID)
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

func (s *Store) pathsFor(id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for p, i := range s.index {
		if i.KalkaID == id {
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

// SaveConflict copies f's content to <name>.conflict-<timestamp>.md beside
// it (spec 7.5).
func (s *Store) SaveConflict(f *File, at time.Time) (string, error) {
	base := strings.TrimSuffix(f.Path, filepath.Ext(f.Path))
	stamp := at.UTC().Format("20060102T150405Z")
	path := base + ".conflict-" + stamp + ".md"
	for i := 1; ; i++ {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			break
		}
		path = fmt.Sprintf("%s.conflict-%s-%d.md", base, stamp, i)
	}
	return path, fsutil.WriteAtomic(path, f.Raw, nil)
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
	n = conflictName.ReplaceAllString(n+".md", "")
	n = strings.TrimSuffix(n, ".md")
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
					var sub []string
					s.scan(ev.Name, &sub)
					for _, d := range sub {
						_ = w.Add(d)
					}
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
