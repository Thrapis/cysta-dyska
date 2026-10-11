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

// volumeAllowed are the only golang.org/x/sys/windows identifiers the raw
// volume reader may use. Anything else (WriteFile, GENERIC_WRITE, other
// device controls, ...) could change the disk outside the file system.
var volumeAllowed = map[string]bool{
	"UTF16PtrFromString": true, "CreateFile": true, "GENERIC_READ": true,
	"FILE_SHARE_READ": true, "FILE_SHARE_WRITE": true, "FILE_SHARE_DELETE": true,
	"OPEN_EXISTING": true, "DeviceIoControl": true, "Handle": true,
	"FILE_FLAG_NO_BUFFERING": true,
}

// Proves: the MFT reader, which opens a whole volume as administrator, can
// only read: it uses an allow-list of Windows APIs, opens the volume with
// GENERIC_READ, never uses the os package's write calls, and the only device
// control code in it is FSCTL_GET_NTFS_VOLUME_DATA.
func TestVolumeReaderIsReadOnly(t *testing.T) {
	dir := filepath.Join("..", "scan", "mft")
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	checked := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		checked++
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		locals := map[string]string{}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			name := p[strings.LastIndex(p, "/")+1:]
			if imp.Name != nil {
				name = imp.Name.Name
			}
			locals[name] = p
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				id, ok := n.X.(*ast.Ident)
				if !ok {
					return true
				}
				switch locals[id.Name] {
				case "golang.org/x/sys/windows":
					if !volumeAllowed[n.Sel.Name] {
						t.Errorf("%s: windows.%s is not on the read-only allow-list", fset.Position(n.Pos()), n.Sel.Name)
					}
				case "os":
					switch n.Sel.Name {
					case "NewFile", "File":
					default:
						t.Errorf("%s: os.%s is not allowed in the volume reader", fset.Position(n.Pos()), n.Sel.Name)
					}
				case "syscall", "unsafe":
					t.Errorf("%s: %s.%s bypasses the allow-list", fset.Position(n.Pos()), id.Name, n.Sel.Name)
				}
				// Methods on the opened volume: only reads and Close.
				if strings.HasPrefix(n.Sel.Name, "Write") || n.Sel.Name == "Truncate" || n.Sel.Name == "Seek" {
					t.Errorf("%s: .%s is not allowed in the volume reader", fset.Position(n.Pos()), n.Sel.Name)
				}
			case *ast.BasicLit:
				if n.Kind == token.INT && strings.HasPrefix(strings.ToLower(n.Value), "0x0009") && n.Value != "0x00090064" {
					t.Errorf("%s: device control code %s is not FSCTL_GET_NTFS_VOLUME_DATA", fset.Position(n.Pos()), n.Value)
				}
			}
			return true
		})
	}
	if checked < 3 {
		t.Fatalf("only %d files checked in %s", checked, dir)
	}
}
