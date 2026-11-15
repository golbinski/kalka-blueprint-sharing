package core

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
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

// TestNoOneToOneLeft (spec 16, T1): nothing in the code or the page refers
// to the deleted one-to-one sharing: its messages, endpoints, files,
// prompts, flags or handoff entries.
func TestNoOneToOneLeft(t *testing.T) {
	gone := []string{
		`"sync_offer"`, `"sync_reply"`, `"sync_ack"`, `"verify-code"`, `"accept-handoff"`,
		"/api/peers", "/api/prompts", "/api/sync", "/api/verify",
		"peers.yaml", "KindHandoff", "IntentHandoff", "PinVerified", "SaveConflict",
		"owner_edited", "no-popups", "syncproto.Prompt", "syncproto.Session",
	}
	root := filepath.Join("..", "..")
	self, _ := filepath.Abs("imports_test.go")
	walk := func(dir string, exts ...string) {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "dist" || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			if d.IsDir() || !slices.Contains(exts, filepath.Ext(p)) {
				return nil
			}
			if abs, _ := filepath.Abs(p); abs == self {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for _, g := range gone {
				if strings.Contains(string(b), g) {
					t.Errorf("%s still refers to %s", p, g)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	walk("internal", ".go")
	walk("cmd", ".go")
	walk(filepath.Join("web", "src"), ".ts", ".tsx")
}
