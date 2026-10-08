package platform

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Runtime consumers must restore PG snapshots, not compile deployment sources.
func TestRuntimeConsumersCannotReadDeploymentSource(t *testing.T) {
	for _, dir := range []string{"../api", "../service"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			name := filepath.Join(dir, entry.Name())
			file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				selector, ok := n.(*ast.SelectorExpr)
				if ok && (selector.Sel.Name == "CompileTransferRelease" || selector.Sel.Name == "TransferContext") {
					t.Errorf("runtime source access in %s", name)
				}
				return true
			})
		}
	}
}
