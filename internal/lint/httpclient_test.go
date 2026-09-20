package lint

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

// clientKeys are the Options fields through which a Source takes the HTTP
// stack a test wants it to use. Any one of them keeps the request off the
// process-wide default transport.
var clientKeys = map[string]bool{
	"HTTPClient":      true, // the uniform seam on every live-HTTP Source
	"HTTP":            true, // dvf, which takes an *httpx.Client
	"Fetcher":         true, // the whole fetch contract, replacing the client
	"OverpassFetcher": true, // osm's own fetch seam
}

// TestOptionsFedByTestServerCarryAClient forbids the flaky class that CI
// caught on dpedist: a test builds a Source from an Options literal pointing
// at its own httptest.Server and leaves the client unset, so the Source falls
// back on gazetteer.DefaultHTTPClient, whose transport is the process-wide
// http.DefaultTransport shared by every other test in the package.
//
// httptest.Server.Close closes that shared transport's idle connections as a
// courtesy to its users. When tests run with t.Parallel they overlap, so one
// server closing can pull a live idle connection out from under a neighbour,
// which surfaces as "http: CloseIdleConnections called" where the test
// expected a status. The cure is one field: give the Source the server's own
// client (srv.Client()), whose transport nobody else touches.
//
// The check is deliberately narrow, on the shape that carries the hazard: an
// Options composite literal that mentions some server's .URL. Wiring a server
// through a package-level base-URL variable instead (dvf, osm) is not matched
// here, and those tests are serial for the unrelated reason that they mutate
// a global; see the trap recorded in AGENTS.md.
func TestOptionsFedByTestServerCarryAClient(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	fset := token.NewFileSet()
	var offenders []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bin", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return perr
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isOptionsType(lit.Type) {
				return true
			}
			if !mentionsServerURL(lit) || hasClientKey(lit) {
				return true
			}
			rel, _ := filepath.Rel(root, path)
			offenders = append(offenders, rel+":"+strconv.Itoa(fset.Position(lit.Pos()).Line))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("these Options literals point at a test server but set no HTTP client, "+
			"so the Source shares http.DefaultTransport with every parallel test in its "+
			"package; add HTTPClient: srv.Client():\n\t%s", strings.Join(offenders, "\n\t"))
	}
}

// isOptionsType reports whether a composite literal builds a Source Options,
// written bare in an in-package test or qualified in an external one.
func isOptionsType(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name == "Options"
	case *ast.SelectorExpr:
		return t.Sel.Name == "Options"
	}
	return false
}

// mentionsServerURL reports whether the literal reads some value's .URL
// field, which in a test is how an httptest.Server's address gets in.
func mentionsServerURL(lit *ast.CompositeLit) bool {
	found := false
	ast.Inspect(lit, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "URL" {
			found = true
		}
		return !found
	})
	return found
}

func hasClientKey(lit *ast.CompositeLit) bool {
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if id, ok := kv.Key.(*ast.Ident); ok && clientKeys[id.Name] {
			return true
		}
	}
	return false
}

// moduleRoot walks up from the test's working directory to the directory
// holding go.mod, so the check covers the whole tree wherever it is run from.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}
