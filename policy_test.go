package gdco

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// sources calls f for every non-test Go file of the module, the wasm
// command included.
func sources(t *testing.T, f func(path string, file *ast.File)) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != "." && (path == "testdata" || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		f(path, file)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestDocComments checks that every exported identifier has a doc comment:
// functions, types, methods of exported types, and constants and variables,
// where a comment on their group counts.
func TestDocComments(t *testing.T) {
	sources(t, func(path string, file *ast.File) {
		if file.Doc == nil && strings.HasSuffix(path, "doc.go") {
			t.Errorf("%s: no package comment", path)
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() || d.Doc != nil {
					continue
				}
				if d.Recv != nil {
					recv := d.Recv.List[0].Type
					if star, ok := recv.(*ast.StarExpr); ok {
						recv = star.X
					}
					if id, ok := recv.(*ast.Ident); !ok || !id.IsExported() {
						continue
					}
				}
				t.Errorf("%s: %s has no doc comment", path, d.Name.Name)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() && s.Doc == nil && d.Doc == nil {
							t.Errorf("%s: type %s has no doc comment", path, s.Name.Name)
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() && s.Doc == nil && s.Comment == nil && d.Doc == nil {
								t.Errorf("%s: %s has no doc comment", path, n.Name)
							}
						}
					}
				}
			}
		}
	})
}

// TestCoreImports keeps the promise in the package documentation: the
// packages that do the work never touch files, the network or the clock,
// and the module depends on nothing outside the standard library. Only the
// wasm command, which talks to JavaScript, is exempt.
func TestCoreImports(t *testing.T) {
	forbidden := func(path string) bool {
		switch path {
		case "os", "syscall", "syscall/js", "time", "net", "io/fs", "io/ioutil",
			"path/filepath", "log", "plugin", "runtime/cgo", "unsafe", "embed":
			return true
		}
		return strings.HasPrefix(path, "os/") || strings.HasPrefix(path, "net/") ||
			strings.HasPrefix(path, "log/") || strings.Contains(strings.Split(path, "/")[0], ".")
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (path == "wasm" || path == "testdata" || strings.HasPrefix(d.Name(), ".")) && path != "." {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if forbidden(p) && !strings.HasPrefix(p, "github.com/wisebrian/gdco") {
				t.Errorf("%s imports %s", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(mod), "\n") {
		if f := strings.Fields(line); len(f) > 0 && (f[0] == "require" || f[0] == "replace") {
			t.Errorf("go.mod has a %s directive: %q", f[0], line)
		}
	}
}
