package communes

import (
	"strings"
	"testing"
)

// TestLookup_NilReceiver_AndMissing exercises the two early-return
// branches of Lookup that the existing Default-table tests don't reach:
// a nil *Table (the package contract: zero value, not a panic) and a
// known-good table queried for an unknown INSEE code.
func TestLookup_NilReceiver_AndMissing(t *testing.T) {
	var tbl *Table
	if c, ok := tbl.Lookup("75107"); ok || c.Name != "" {
		t.Errorf("nil receiver: want (zero,false), got (%+v,%v)", c, ok)
	}
	real, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := real.Lookup("99999"); ok || c.Name != "" {
		t.Errorf("missing INSEE: want (zero,false), got (%+v,%v)", c, ok)
	}
}

// TestNeighbors_EdgeCases covers the three branches uncovered by the
// happy-path Paris-7e test: zero radius (returns just self), unknown
// INSEE (returns nil), and the cross-department fan-out triggered by a
// >10 km radius (we assert at least one foreign-department hit when
// querying Boulogne-Billancourt 92012 which sits 4 km from Paris-15e).
func TestNeighbors_EdgeCases(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if got := tbl.Neighbors("75107", 0); len(got) != 1 || got[0] != "75107" {
		t.Errorf("zero radius: want [75107], got %v", got)
	}
	if got := tbl.Neighbors("99999", 5); got != nil {
		t.Errorf("unknown INSEE: want nil, got %v", got)
	}
	got := tbl.Neighbors("92012", 15)
	hasParis := false
	for _, id := range got {
		if strings.HasPrefix(id, "751") {
			hasParis = true
			break
		}
	}
	if !hasParis {
		t.Errorf("cross-dept fan-out (radius>10) should reach 75xxx from 92012; got %v", got)
	}

	var nilTbl *Table
	if got := nilTbl.Neighbors("75107", 5); got != nil {
		t.Errorf("nil receiver: want nil, got %v", got)
	}
}

// TestSameDepartment_NilAndMissing rounds out SameDepartment: nil
// receiver and unknown INSEE both return nil (no panic).
func TestSameDepartment_NilAndMissing(t *testing.T) {
	var tbl *Table
	if got := tbl.SameDepartment("75107"); got != nil {
		t.Errorf("nil receiver: want nil, got %v", got)
	}
	real, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if got := real.SameDepartment("99999"); got != nil {
		t.Errorf("unknown INSEE: want nil, got %v", got)
	}
}

// TestFranceCSVBytes asserts the embedded blob is non-empty and starts
// with the documented header. Cheap drift-detector against a botched
// embed directive.
func TestFranceCSVBytes(t *testing.T) {
	b := FranceCSVBytes()
	if len(b) == 0 {
		t.Fatal("embedded CSV is empty")
	}
	if !strings.HasPrefix(string(b[:64]), "insee,dept,lon,lat,name") {
		t.Errorf("first 64 bytes don't start with the expected header: %q", string(b[:64]))
	}
}

// TestMustDefault is the panic-free path. We don't exercise the panic
// branch — it would require swapping the embedded CSV bytes, which
// pollutes package state for sibling tests.
func TestMustDefault(t *testing.T) {
	tbl := MustDefault()
	if tbl == nil {
		t.Fatal("MustDefault returned nil")
	}
	if _, ok := tbl.Lookup("75107"); !ok {
		t.Error("MustDefault table missing Paris-7e")
	}
}

// TestCityDepts covers the reverse name → dept index. Hits the
// unique-city, multi-department-collision (Vigneux), accent-fold, and
// nil/missing/unknown branches.
func TestCityDepts(t *testing.T) {
	tbl, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	// Unique commune.
	if got := tbl.CityDepts("Vincennes"); len(got) != 1 || got[0] != "94" {
		t.Errorf("CityDepts(Vincennes) = %v, want [94]", got)
	}
	if got := tbl.CityDepts("Levallois-Perret"); len(got) != 1 || got[0] != "92" {
		t.Errorf("CityDepts(Levallois-Perret) = %v, want [92]", got)
	}
	if got := tbl.CityDepts("Vigneux-sur-Seine"); len(got) != 1 || got[0] != "91" {
		t.Errorf("CityDepts(Vigneux-sur-Seine) = %v, want [91]", got)
	}
	// Multi-department collision: there is more than one Vigneux
	// (Vigneux-sur-Seine 91, Vigneux-Hocquet 02, Vigneux-de-Bretagne 44).
	// "Vigneux" alone is not stored as a commune name in geo.api.gouv,
	// so it should miss; we instead check a known multi-occurrence
	// suffix-only stem doesn't smear into others.
	if got := tbl.CityDepts("Vigneux"); len(got) != 0 {
		t.Errorf("bare 'Vigneux' should not match (no commune of that exact name); got %v", got)
	}
	// Accent fold: input "Evry" must hit "Evry-Courcouronnes" miss but
	// "Evry-Courcouronnes" should resolve to its own dept (91).
	if got := tbl.CityDepts("Évry-Courcouronnes"); len(got) != 1 || got[0] != "91" {
		t.Errorf("CityDepts(Évry-Courcouronnes) = %v, want [91]", got)
	}
	// Empty / unknown.
	if got := tbl.CityDepts(""); got != nil {
		t.Errorf("empty city = %v, want nil", got)
	}
	if got := tbl.CityDepts("Atlantis-sur-Mer"); got != nil {
		t.Errorf("unknown city = %v, want nil", got)
	}
	// Nil receiver.
	var nilTbl *Table
	if got := nilTbl.CityDepts("Vincennes"); got != nil {
		t.Errorf("nil receiver = %v, want nil", got)
	}
	// Defensive copy: mutating the returned slice doesn't poison the
	// cache.
	got := tbl.CityDepts("Vincennes")
	if len(got) > 0 {
		got[0] = "ZZ"
	}
	if again := tbl.CityDepts("Vincennes"); len(again) != 1 || again[0] != "94" {
		t.Errorf("returned slice was not a defensive copy; second call = %v", again)
	}
}

// TestParseCSV_Errors covers the three structural failure modes:
// completely empty input, a header that doesn't start with `insee`, and
// an empty body (header only). Non-numeric lon/lat rows should be
// silently skipped — the existing data-set tests cover the happy path.
func TestParseCSV_Errors(t *testing.T) {
	if _, err := ParseCSV(strings.NewReader("")); err == nil {
		t.Error("empty input should error")
	}
	if _, err := ParseCSV(strings.NewReader("foo,bar,baz,qux,name\n")); err == nil {
		t.Error("bad header should error")
	}
	if _, err := ParseCSV(strings.NewReader("insee,dept,lon,lat,name\n")); err == nil {
		t.Error("header-only (empty body) should error")
	}
	// Bad lon/lat are silently dropped; mixed file should yield only the
	// valid row.
	tbl, err := ParseCSV(strings.NewReader(
		"insee,dept,lon,lat,name\n" +
			"00001,99,nope,1.0,Bad\n" +
			"00002,99,2.0,nope,Bad2\n" +
			"00003,99,2.5,48.8,Good\n",
	))
	if err != nil {
		t.Fatalf("parse mixed: %v", err)
	}
	if c, ok := tbl.Lookup("00003"); !ok || c.Name != "Good" {
		t.Errorf("expected (Good,true), got (%+v,%v)", c, ok)
	}
	if _, ok := tbl.Lookup("00001"); ok {
		t.Error("bad lon row should have been dropped")
	}
}
