package communes

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestExportedSymbolsCarryTheirOwnGodoc guards the repo rule that every
// exported symbol carries a godoc of its own. Both halves of the check have
// already broken here: FoldArrondissement's doc block had slid above
// ArrondissementParents, so godoc showed the folding rules on the wrong
// function and the folder itself was undocumented, while docs/helpers.md kept
// citing it by name.
func TestExportedSymbolsCarryTheirOwnGodoc(t *testing.T) {
	for _, gap := range undocumentedExports(t) {
		t.Error(gap)
	}
}

// undocumentedExports reports every exported top-level symbol in the
// package's own (non-test) sources whose godoc is missing, or whose godoc
// does not start with the symbol's own name. The second half is what catches
// a comment block that has slid onto the wrong declaration.
//
// Methods on unexported receivers are skipped: godoc never renders them.
func undocumentedExports(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	var gaps []string
	report := func(pos token.Position, name string, doc *ast.CommentGroup) {
		if !ast.IsExported(name) {
			return
		}
		if doc == nil {
			gaps = append(gaps, fmt.Sprintf("%s: %s has no godoc", pos, name))
			return
		}
		text := strings.TrimPrefix(strings.TrimSpace(doc.Text()), "Deprecated: ")
		if fields := strings.Fields(text); len(fields) == 0 || fields[0] != name {
			gaps = append(gaps, fmt.Sprintf("%s: %s's godoc does not start with its name (%q)", pos, name, text))
		}
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if unexportedReceiver(d) {
					continue
				}
				report(fset.Position(d.Pos()), d.Name.Name, d.Doc)
			case *ast.GenDecl:
				if d.Tok == token.IMPORT {
					continue
				}
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						report(fset.Position(s.Pos()), s.Name.Name, firstDoc(s.Doc, d.Doc))
					case *ast.ValueSpec:
						for _, n := range s.Names {
							report(fset.Position(s.Pos()), n.Name, firstDoc(s.Doc, d.Doc))
						}
					}
				}
			}
		}
	}
	return gaps
}

// unexportedReceiver reports whether d is a method on an unexported type.
func unexportedReceiver(d *ast.FuncDecl) bool {
	if d.Recv == nil || len(d.Recv.List) != 1 {
		return false
	}
	typ := d.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	id, ok := typ.(*ast.Ident)
	return ok && !ast.IsExported(id.Name)
}

// firstDoc returns the spec's own comment when it has one, else the
// enclosing declaration's (how godoc resolves a grouped spec's comment).
func firstDoc(spec, group *ast.CommentGroup) *ast.CommentGroup {
	if spec != nil {
		return spec
	}
	return group
}
