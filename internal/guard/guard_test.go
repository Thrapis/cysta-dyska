// Package guard holds tests that enforce project-wide safety rules by
// inspecting the source code itself.
package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// registryWrites are the identifiers of golang.org/x/sys/windows/registry
// that can change the registry.
var registryWrites = map[string]bool{
	"CreateKey": true, "DeleteKey": true, "DeleteValue": true,
	"SetStringValue": true, "SetExpandStringValue": true, "SetStringsValue": true,
	"SetDWordValue": true, "SetQWordValue": true, "SetBinaryValue": true,
	"SET_VALUE": true, "CREATE_SUB_KEY": true, "WRITE": true, "ALL_ACCESS": true,
	"CREATE_LINK": true,
}

// Proves: no Go file in the project uses a registry API that can write.
// The app must only ever read the registry (a data-safety must-have).
func TestRegistryIsReadOnly(t *testing.T) {
	root := filepath.Join("..", "..")
	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "frontend", "node_modules", "build", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		checked++
		checkFile(t, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 10 {
		t.Fatalf("only %d Go files checked; is the walk root right?", checked)
	}
}

func checkFile(t *testing.T, path string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Errorf("%s: %v", path, err)
		return
	}
	// Find the local name of the registry import, if any.
	local := ""
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p == "golang.org/x/sys/windows/registry" {
			local = "registry"
			if imp.Name != nil {
				local = imp.Name.Name
			}
		}
	}
	if local == "" {
		return
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// registry.CreateKey(...) or registry.SET_VALUE
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == local && registryWrites[sel.Sel.Name] {
			t.Errorf("%s: uses registry.%s, which can write to the registry", fset.Position(sel.Pos()), sel.Sel.Name)
		}
		// key.SetStringValue(...) and other method calls on a Key
		if strings.HasPrefix(sel.Sel.Name, "Set") || sel.Sel.Name == "DeleteValue" {
			t.Errorf("%s: calls .%s in a file that imports the registry package", fset.Position(sel.Pos()), sel.Sel.Name)
		}
		return true
	})
}
