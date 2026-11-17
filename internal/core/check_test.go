package core

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/check"
)

// copyCorpus copies testdata/check/corpus into dir and returns the
// blueprints in it (snapshots left out).
func copyCorpus(t *testing.T, dir string) []string {
	t.Helper()
	src := filepath.Join("..", "..", "testdata", "check", "corpus")
	var docs []string
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := os.WriteFile(dst, raw, 0o644); err != nil {
			return err
		}
		if !bp.IsSnapshotName(p) {
			docs = append(docs, dst)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

// The app and kalka check report the same diagnostics for the same files
// (spec 6.17): the document view runs the checker with the domains held
// here, kalka check with the domains under a folder.
func TestCheckParity(t *testing.T) {
	w := newWorld(t)
	n := w.node("ida@library.example")
	dir := filepath.Join(n.folder, "personal", "corpus")
	docs := copyCorpus(t, dir)
	waitIndexed(t, n, len(docs))
	folder := check.NewFolder(dir)
	compared := 0
	for _, p := range docs {
		v, err := n.d.Document(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(p)
		doc, err := bp.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		want := check.Document(doc, check.Options{Domains: folder}).Problems
		if !reflect.DeepEqual(v.Problems, want) {
			t.Errorf("%s:\n app   %+v\n check %+v", filepath.Base(p), v.Problems, want)
		}
		for _, pr := range want {
			if strings.HasPrefix(pr.Rule, "R") {
				compared++
			}
		}
	}
	if compared < 3 {
		t.Fatalf("the corpus exercised %d domain rules", compared)
	}
}
