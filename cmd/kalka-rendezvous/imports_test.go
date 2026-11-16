package main

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/golbinski/kalka/"

// The server holds none of the client (spec 9.6, 15): nothing it reaches in
// this module imports the core, the daemon, the UI or WebRTC.
func TestServerImports(t *testing.T) {
	banned := []string{
		module + "internal/core", module + "internal/daemon", module + "internal/ui", module + "internal/cli",
		module + "internal/desktop", module + "internal/transport", "github.com/pion/webrtc",
	}
	seen := map[string]bool{}
	queue := []string{"."}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		for _, p := range imports(t, dir) {
			for _, b := range banned {
				if p == b || strings.HasPrefix(p, b+"/") {
					t.Errorf("%s imports %s", dir, p)
				}
			}
			if strings.HasPrefix(p, module) && !seen[p] {
				seen[p] = true
				queue = append(queue, filepath.Join("..", "..", strings.TrimPrefix(p, module)))
			}
		}
	}
	if !seen[module+"internal/rendezvous"] {
		t.Fatalf("the walk missed internal/rendezvous: %v", seen)
	}
}

// imports lists the imports of the non-test Go files in dir.
func imports(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no Go files in %s: %v", dir, err)
	}
	var out []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range af.Imports {
			p, _ := strconv.Unquote(s.Path.Value)
			out = append(out, p)
		}
	}
	return out
}
