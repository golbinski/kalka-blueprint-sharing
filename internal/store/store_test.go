package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
)

const doc1 = "---\nkalka_id: 6f1c2a0e-8d1b-4c57-9a43-0e2b7c9d51aa\nowner: alice@corp.com\n---\n# T\n\n## Plan\nP\n"

func parse(t *testing.T, s string) *bp.Document {
	t.Helper()
	d, err := bp.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestIndexFindDuplicate(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "a.md"), []byte(doc1), 0o644)
	os.WriteFile(filepath.Join(dir, ".hidden", "b.md"), []byte(doc1), 0o644)
	os.WriteFile(filepath.Join(dir, "a.conflict-20261112T100000Z.md"), []byte(doc1), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(doc1), 0o644)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l := s.List(); len(l) != 1 || l[0].Rel != filepath.Join("sub", "a.md") || l[0].Title != "T" {
		t.Fatalf("list %+v", l)
	}
	f, err := s.Find("6F1C2A0E-8D1B-4C57-9A43-0E2B7C9D51AA")
	if err != nil || f == nil {
		t.Fatal(f, err)
	}
	os.WriteFile(filepath.Join(dir, "copy.md"), []byte(doc1), 0o644)
	s.scan(s.dir, nil)
	var dup *ErrDuplicate
	if _, err := s.Find(f.Doc.KalkaID()); !errors.As(err, &dup) || len(dup.Paths) != 2 {
		t.Fatalf("want duplicate, got %v", err)
	}
	if l := s.List(); !l[0].Duplicate || !l[1].Duplicate {
		t.Fatalf("duplicates not flagged: %+v", l)
	}
}

func TestUpdateDetectsChange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.md")
	os.WriteFile(p, []byte(doc1), 0o644)
	s, _ := Open(dir)
	f, err := s.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p, []byte(doc1+"more\n"), 0o644)
	if err := s.Update(f, parse(t, doc1+"x\n")); !errors.Is(err, fsutil.ErrChanged) {
		t.Fatalf("want ErrChanged, got %v", err)
	}
	if _, ok := s.Contains(filepath.Join(dir, "..", "x.md")); ok {
		t.Fatal("path outside the folder accepted")
	}
}

func TestCreateNames(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	d := parse(t, doc1)
	p1, err := s.Create(d, "../../etc/Plan: v2.md")
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := s.Create(d, "Plan- v2.md")
	if filepath.Dir(p1) != s.Dir() || filepath.Base(p1) != "Plan- v2.md" || filepath.Base(p2) != "Plan- v2-1.md" {
		t.Fatal(p1, p2)
	}
	for in, want := range map[string]string{
		"CON.md": "blueprint-CON", "": "blueprint", "a.conflict-1.md": "a", `C:\x\y z.md`: "y z", "...": "blueprint",
	} {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
	f, _ := s.Load(p1)
	c, err := s.SaveConflict(f, time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC))
	if err != nil || !strings.HasSuffix(c, "Plan- v2.conflict-20261112T100000Z.md") {
		t.Fatal(c, err)
	}
}

func TestWatchSuppressesOwnWrites(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.md")
	os.WriteFile(p, []byte(doc1), 0o644)
	s, _ := Open(dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Watch(ctx)
	time.Sleep(100 * time.Millisecond)
	ev, stop := s.Subscribe()
	defer stop()

	f, _ := s.Load(p)
	if err := s.Update(f, parse(t, doc1+"own\n")); err != nil {
		t.Fatal(err)
	}
	if e := <-ev; e.External {
		t.Fatalf("own write reported external: %+v", e)
	}
	os.MkdirAll(filepath.Join(dir, "new"), 0o755)
	time.Sleep(100 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "new", "b.md"), []byte(strings.Replace(doc1, "6f1c", "7f1c", 1)), 0o644)
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-ev:
			if filepath.Base(e.Path) == "b.md" {
				if !e.External {
					t.Fatal("editor write reported as own")
				}
				if len(s.List()) != 2 {
					t.Fatalf("index %+v", s.List())
				}
				return
			}
			if e.Path == p {
				t.Fatalf("own write produced a second event: %+v", e)
			}
		case <-deadline:
			t.Fatal("no event for a file in a new subfolder")
		}
	}
}
