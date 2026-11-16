package cli

// Product rule: no financial or trading examples in fixtures, mockups or
// tests (plan T7). This reads the text tests feed the code: the string
// literals of every Go test file, the web tests, testdata and samples.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var excludedDomain = regexp.MustCompile(`(?i)\b(payments?|payroll|invoices?|trad(e|es|ing|er|ers)|stocks?|shares|shareholders?|bank accounts?|banking|prices?|money|credit cards?|portfolios?|ledgers?|billing|fraud|purchase orders?|checkout|currenc(y|ies)|invest(s|ing|ment|ments|or|ors)?|brokers?|brokerage|dividends?|securities|equit(y|ies)|bonds?|cryptocurrenc(y|ies)|bitcoin|forex|hedge funds?|tickers?|wallets?)\b`)

func TestFixturesStayInNeutralDomains(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	check := func(path, text string) {
		if m := excludedDomain.FindString(text); m != "" {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s: %q, a term of the excluded domains: use library lending or parcel logistics", rel, m)
		}
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		n := d.Name()
		if d.IsDir() {
			if n == "node_modules" || n == "dist" || n == "fuzz" || (strings.HasPrefix(n, ".") && p != root) {
				return filepath.SkipDir
			}
			return nil
		}
		slash := filepath.ToSlash(p)
		switch {
		case strings.HasSuffix(n, "_test.go"):
			f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			imports := map[*ast.BasicLit]bool{}
			for _, s := range f.Imports {
				imports[s.Path] = true
			}
			ast.Inspect(f, func(x ast.Node) bool {
				if lit, ok := x.(*ast.BasicLit); ok && lit.Kind == token.STRING && !imports[lit] && !strings.Contains(lit.Value, "(?i)") {
					check(p, lit.Value)
				}
				return true
			})
		case strings.HasSuffix(n, ".test.ts"), strings.Contains(slash, "/testdata/"), strings.Contains(slash, "/mockups/"), strings.Contains(slash, "/samples/"):
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			check(p, string(raw))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
