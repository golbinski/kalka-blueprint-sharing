package core

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/golbinski/kalka/"

// TestCoreImports keeps the core free of front ends (spec 4): the core
// itself serves no HTTP, and nothing it reaches in this module may pull in
// the UI, popups, the daemon, the CLI or the desktop shell.
func TestCoreImports(t *testing.T) {
	banned := []string{
		module + "internal/ui", module + "internal/notify", module + "internal/daemon",
		module + "internal/cli", module + "internal/desktop", module + "internal/autostart",
		"github.com/wailsapp/",
	}
	direct := imports(t, ".")
	for _, p := range direct {
		if p == "net/http" {
			t.Errorf("core imports net/http")
		}
	}
	seen := map[string]string{} // package -> who imported it
	queue := []string{module + "internal/core"}
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		dir := filepath.Join("..", "..", strings.TrimPrefix(pkg, module))
		for _, p := range imports(t, dir) {
			for _, b := range banned {
				if p == b || strings.HasPrefix(p, b+"/") || (strings.HasSuffix(b, "/") && strings.HasPrefix(p, b)) {
					t.Errorf("%s imports %s", pkg, p)
				}
			}
			if strings.HasPrefix(p, module) {
				if _, ok := seen[p]; !ok {
					seen[p] = pkg
					queue = append(queue, p)
				}
			}
		}
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
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(fset, f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range af.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			out = append(out, p)
		}
	}
	return out
}
