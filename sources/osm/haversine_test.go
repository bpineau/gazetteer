package osm

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/helpers/geodist"
)

// TestEarthRadiusMetersBackCompat pins the deprecated constant's value. The
// `// Deprecated:` marker tells staticcheck and IDEs to steer new code to
// geodist, but the symbol itself is exported API: it must keep answering the
// exact metre radius the geodist constant carries.
func TestEarthRadiusMetersBackCompat(t *testing.T) {
	t.Parallel()
	if got, want := float64(EarthRadiusMeters), geodist.EarthRadiusKm*1000; got != want {
		t.Errorf("EarthRadiusMeters = %v, want %v (geodist.EarthRadiusKm * 1000)", got, want)
	}
	if got := float64(EarthRadiusMeters); got != 6_371_000 {
		t.Errorf("EarthRadiusMeters = %v, want 6371000", got)
	}
}

// TestEarthRadiusMetersUnusedOutsideThisPackage keeps the deprecation
// machine-readable AND quiet: staticcheck's SA1019 fires on every use of a
// deprecated symbol from another package, so a call site elsewhere in the tree
// would turn `make lint` red. There is none today; this test says so, and
// fails the day one appears (use geodist.EarthRadiusKm, or geodist's own
// distance helpers, instead of reviving this constant).
func TestEarthRadiusMetersUnusedOutsideThisPackage(t *testing.T) {
	t.Parallel()
	const root = "../.." // the module root, from sources/osm
	self, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	var offenders []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return fs.SkipDir
			}
			abs, aerr := filepath.Abs(path)
			if aerr == nil && abs == self {
				return fs.SkipDir // this package may use its own deprecated symbol
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, rerr := os.ReadFile(path) //nolint:gosec // walking the module's own sources
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(b), "EarthRadiusMeters") {
			offenders = append(offenders, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(offenders) > 0 {
		t.Errorf("deprecated osm.EarthRadiusMeters is used in %v; SA1019 will fail the lint gate", offenders)
	}
}
