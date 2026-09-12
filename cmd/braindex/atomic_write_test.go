package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestProductionNoOSWriteFile(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		aliases := map[string]bool{}
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if path == "os" {
				alias := "os"
				if imp.Name != nil {
					alias = imp.Name.Name
				}
				aliases[alias] = true
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			bad := false
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok {
					bad = aliases[id.Name] && sel.Sel.Name == "WriteFile"
				}
			}
			if id, ok := call.Fun.(*ast.Ident); ok {
				bad = aliases["."] && id.Name == "WriteFile"
			}
			if bad {
				t.Errorf("%s: os.WriteFile の代わりに fsutil.WriteAtomic を使う", fset.Position(call.Pos()))
			}
			return true
		})
	}
}
