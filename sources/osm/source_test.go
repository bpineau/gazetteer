package osm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/gazetteer"
)

// loadFixture reads a testdata JSON file. Bailout-fast on failure
// since every test depends on it.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return body
}

// newTestCatalog builds an in-memory catalog from the Paris XV fixture
// for tests that need a real station set.
func newTestCatalog(t *testing.T) *Catalog {
	t.Helper()
	body := loadFixture(t, "paris15_sample.json")
	stations, err := ParseOverpass(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return &Catalog{
		SchemaVersion: CatalogSchemaVersion,
		FetchedAt:     time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
		BBox:          FranceMetropolitanBBox,
		Stations:      stations,
	}
}

func ptrF64(v float64) *float64 { return &v }

func TestSource_NameVersion(t *testing.T) {
	s := NewSource(Options{})
	if s.Name() != Name {
		t.Errorf("Name() = %q, want %q", s.Name(), Name)
	}
	if s.Version() != sourceVersion {
		t.Errorf("Version() = %d, want %d", s.Version(), sourceVersion)
	}
}

func TestSource_HappyPath_Paris15Lourmel(t *testing.T) {
	s := NewSource(Options{Catalog: newTestCatalog(t)})
	// Listing at Paris XV, ~50 m from Lourmel.
	l := gazetteer.Listing{
		Address: "23 rue Lourmel 75015 Paris",
		Lat:     ptrF64(48.8407),
		Lon:     ptrF64(2.2880),
	}
	data, err := s.Query(context.Background(), l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res, ok := data.(*Result)
	if !ok {
		t.Fatalf("Query returned %T, want *Result", data)
	}
	if res.IsEmpty() {
		t.Error("IsEmpty() = true, want false on happy path")
	}
	if res.NearestTransitName != "Lourmel" {
		t.Errorf("name = %q, want Lourmel", res.NearestTransitName)
	}
	if res.NearestTransitType != TransitTypeMetro {
		t.Errorf("type = %q, want metro", res.NearestTransitType)
	}
	if res.NearestTransitWalkM <= 0 || res.NearestTransitWalkM > 300 {
		t.Errorf("walk_m = %d, want 0-300", res.NearestTransitWalkM)
	}
	if res.NearestTransitWalkMin < 1 || res.NearestTransitWalkMin > 5 {
		t.Errorf("walk_min = %d, want 1-5", res.NearestTransitWalkMin)
	}
	if len(res.NearestTransitLines) == 0 || res.NearestTransitLines[0] != "8" {
		t.Errorf("lines = %v, want [8]", res.NearestTransitLines)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("confidence = %q, want high", res.Confidence)
	}
	if res.SampleSize != 1 {
		t.Errorf("sample_size = %d, want 1", res.SampleSize)
	}

	// Evidence cross-check.
	ev := res.Evidence
	if ev.AuctionLat != 48.8407 || ev.AuctionLon != 2.2880 {
		t.Errorf("Evidence coords = (%v,%v), want (48.8407, 2.2880)", ev.AuctionLat, ev.AuctionLon)
	}
	if ev.WalkMultiplier != WalkSinuosityMultiplier {
		t.Errorf("Evidence.WalkMultiplier = %v, want %v", ev.WalkMultiplier, WalkSinuosityMultiplier)
	}
	if ev.ProximityCapM != MaxNearestStationMeters {
		t.Errorf("Evidence.ProximityCapM = %v, want %v", ev.ProximityCapM, MaxNearestStationMeters)
	}
	if ev.HaversineMeters <= 0 || ev.HaversineMeters > 200 {
		t.Errorf("Evidence.HaversineMeters = %d, want 0-200", ev.HaversineMeters)
	}
	if ev.CatalogStations == 0 {
		t.Error("Evidence.CatalogStations = 0, want >0")
	}
	if ev.CatalogFetchedAt == "" {
		t.Error("Evidence.CatalogFetchedAt is empty")
	}
}

// TestSource_CommercialNoSurface verifies that a commercial listing with no
// surface_m2 is not rejected by OSM — the source has no property-type gate
// and only requires lat/lon coordinates.
func TestSource_CommercialNoSurface(t *testing.T) {
	s := NewSource(Options{Catalog: newTestCatalog(t)})
	// Listing with PropertyType=commercial and nil SurfaceM2 but valid coords.
	// Uses the same Paris-XV coordinates as the happy-path test.
	data, err := s.Query(context.Background(), gazetteer.Listing{
		Address:      "77160 Provins",
		City:         "Provins",
		Zip:          "77160",
		PropertyType: gazetteer.PropertyCommercial,
		Lat:          ptrF64(48.8407),
		Lon:          ptrF64(2.2880),
		// SurfaceM2 intentionally nil
	})
	if err != nil {
		t.Fatalf("Query(commercial, no surface) = %v, want nil", err)
	}
	res, ok := data.(*Result)
	if !ok {
		t.Fatalf("Query returned %T, want *Result", data)
	}
	// The catalog fixture is Paris XV so we expect a result, not empty.
	if res.IsEmpty() {
		t.Error("IsEmpty() = true, want false: OSM must not gate on property type or surface")
	}
}

func TestSource_MissingLatLon(t *testing.T) {
	s := NewSource(Options{Catalog: newTestCatalog(t)})
	_, err := s.Query(context.Background(), gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(no lat/lon) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_ZeroZeroLatLonRejected(t *testing.T) {
	s := NewSource(Options{Catalog: newTestCatalog(t)})
	_, err := s.Query(context.Background(), gazetteer.Listing{
		Lat: ptrF64(0),
		Lon: ptrF64(0),
	})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("Query(0,0) = %v, want ErrInsufficientInputs", err)
	}
}

func TestSource_NoCatalog_ReturnsErrNoCatalog(t *testing.T) {
	s := NewSource(Options{}) // no catalog
	_, err := s.Query(context.Background(), gazetteer.Listing{
		Lat: ptrF64(48.84),
		Lon: ptrF64(2.28),
	})
	if !errors.Is(err, ErrNoCatalog) {
		t.Errorf("Query with no catalog = %v, want ErrNoCatalog", err)
	}
}

func TestSource_EmptyCatalogOptions_TreatedAsNoCatalog(t *testing.T) {
	// Passing an empty (non-nil) catalog must be treated as "no catalog"
	// — Query returns ErrNoCatalog. This matches the encheridor behaviour
	// the boot sequence relies on (the serve process registers the
	// enricher with an empty catalog and starts a background refresh).
	s := NewSource(Options{Catalog: &Catalog{}})
	_, err := s.Query(context.Background(), gazetteer.Listing{
		Lat: ptrF64(48.84),
		Lon: ptrF64(2.28),
	})
	if !errors.Is(err, ErrNoCatalog) {
		t.Errorf("Query with empty catalog = %v, want ErrNoCatalog", err)
	}
}

func TestSource_OutOfRange_ReturnsSkippedResult(t *testing.T) {
	s := NewSource(Options{Catalog: newTestCatalog(t)})
	// Pointe-à-Pitre — Guadeloupe, ~6 700 km from the Paris XV catalog.
	data, err := s.Query(context.Background(), gazetteer.Listing{
		Lat: ptrF64(16.2415),
		Lon: ptrF64(-61.5328),
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	res, ok := data.(*Result)
	if !ok {
		t.Fatalf("Query returned %T, want *Result", data)
	}
	if !res.IsEmpty() {
		t.Error("IsEmpty() = false, want true on out-of-range")
	}
	if res.SkipReason != SkipReasonOutOfRange {
		t.Errorf("SkipReason = %q, want %q", res.SkipReason, SkipReasonOutOfRange)
	}
	if !res.Skipped {
		t.Error("Skipped = false, want true")
	}
	if res.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low", res.Confidence)
	}
	if res.NearestTransitName != "" {
		t.Errorf("name = %q, want empty on out-of-range", res.NearestTransitName)
	}
}

func TestSource_UpdateCatalog_HotSwap(t *testing.T) {
	s := NewSource(Options{}) // no catalog
	a := gazetteer.Listing{Lat: ptrF64(48.8407), Lon: ptrF64(2.2880)}

	// Before update: ErrNoCatalog.
	if _, gotErr := s.Query(context.Background(), a); !errors.Is(gotErr, ErrNoCatalog) {
		t.Errorf("before UpdateCatalog: err = %v, want ErrNoCatalog", gotErr)
	}

	// Nil and empty updates must be no-ops.
	s.UpdateCatalog(nil)
	s.UpdateCatalog(&Catalog{})
	if _, gotErr := s.Query(context.Background(), a); !errors.Is(gotErr, ErrNoCatalog) {
		t.Errorf("after nil/empty UpdateCatalog: err = %v, still want ErrNoCatalog", gotErr)
	}

	// After a real update: lookup succeeds.
	s.UpdateCatalog(newTestCatalog(t))
	data, gotErr := s.Query(context.Background(), a)
	if gotErr != nil {
		t.Fatalf("after UpdateCatalog: err = %v, want nil", gotErr)
	}
	res, _ := data.(*Result)
	if res == nil || res.NearestTransitName != "Lourmel" {
		t.Errorf("after UpdateCatalog: res = %+v, want Lourmel", res)
	}
}

func TestSource_Catalog_AccessorReturnsLive(t *testing.T) {
	s := NewSource(Options{})
	if got := s.Catalog(); got != nil {
		t.Errorf("Catalog() = %+v, want nil before UpdateCatalog", got)
	}
	c := newTestCatalog(t)
	s.UpdateCatalog(c)
	if got := s.Catalog(); got != c {
		t.Errorf("Catalog() = %p, want %p", got, c)
	}
}

func TestQueryAtomicHelper(t *testing.T) {
	res, err := Query(context.Background(), Options{Catalog: newTestCatalog(t)},
		gazetteer.Listing{Lat: ptrF64(48.8407), Lon: ptrF64(2.2880)})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("Query atomic = %+v, want non-empty Result", res)
	}
}

func TestSource_RegistryRoundtrip(t *testing.T) {
	// Confirm the init() registration is in place: Lookup(Name) must
	// return a factory producing *Result.
	factory := gazetteer.Lookup(Name)
	if factory == nil {
		t.Fatal("gazetteer.Lookup(osm_transit) = nil, want a factory")
	}
	val := factory()
	if _, ok := val.(*Result); !ok {
		t.Errorf("factory() = %T, want *Result", val)
	}
}

func TestFrom_Dossier(t *testing.T) {
	res := &Result{NearestTransitName: "Lourmel", SampleSize: 1, Confidence: ConfidenceHigh}
	d := gazetteer.Dossier{
		Results: map[string]gazetteer.Result{
			Name: {Name: Name, Status: gazetteer.StatusOK, Data: res},
		},
	}
	got, ok := gazetteer.Get[*Result](d, Name)
	if !ok || got != res {
		t.Errorf("From(d) = (%v, %v), want (%v, true)", got, ok, res)
	}
}

func TestFrom_DossierMissing(t *testing.T) {
	d := gazetteer.Dossier{Results: map[string]gazetteer.Result{}}
	got, ok := gazetteer.Get[*Result](d, Name)
	if ok || got != nil {
		t.Errorf("From(empty d) = (%v, %v), want (nil, false)", got, ok)
	}
}

func TestResult_JSONRoundtrip(t *testing.T) {
	// Marshal a populated Result and confirm the wire shape carries
	// the expected snake_case keys mirroring the pre-port persistence.
	res := &Result{
		NearestTransitName:    "Lourmel",
		NearestTransitType:    TransitTypeMetro,
		NearestTransitLines:   []string{"8"},
		NearestTransitWalkM:   90,
		NearestTransitWalkMin: 2,
		Confidence:            ConfidenceHigh,
		SampleSize:            1,
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(raw)
	for _, want := range []string{
		`"nearest_transit_name":"Lourmel"`,
		`"nearest_transit_type":"metro"`,
		`"nearest_transit_lines":["8"]`,
		`"nearest_transit_walk_m":90`,
		`"nearest_transit_walk_min":2`,
		`"confidence":"high"`,
		`"sample_size":1`,
	} {
		if !contains(s, want) {
			t.Errorf("JSON missing %q in: %s", want, s)
		}
	}
	// Evidence is NOT serialised (json:"-").
	if contains(s, `"evidence"`) || contains(s, `"auction_lat"`) {
		t.Errorf("Evidence leaked into wire data: %s", s)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestSource_PopulatesEvidence pins the contract that Source.Query
// stamps Evidence on every returned Result — both happy-path and
// out-of-range path.
func TestSource_PopulatesEvidence(t *testing.T) {
	s := NewSource(Options{Catalog: newTestCatalog(t)})

	t.Run("happy path", func(t *testing.T) {
		data, err := s.Query(context.Background(), gazetteer.Listing{
			Lat: ptrF64(48.8407),
			Lon: ptrF64(2.2880),
		})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		res := data.(*Result)
		ev := res.Evidence
		if ev.AuctionLat != 48.8407 {
			t.Errorf("Evidence.AuctionLat = %v, want 48.8407", ev.AuctionLat)
		}
		if ev.HaversineMeters <= 0 {
			t.Errorf("Evidence.HaversineMeters = %d, want >0", ev.HaversineMeters)
		}
		if ev.WalkMultiplier == 0 {
			t.Error("Evidence.WalkMultiplier = 0, want WalkSinuosityMultiplier")
		}
		if ev.ProximityCapM == 0 {
			t.Error("Evidence.ProximityCapM = 0, want MaxNearestStationMeters")
		}
		if ev.CatalogFetchedAt == "" {
			t.Error("Evidence.CatalogFetchedAt is empty")
		}
		if ev.CatalogStations <= 0 {
			t.Errorf("Evidence.CatalogStations = %d, want >0", ev.CatalogStations)
		}
	})

	t.Run("out of range", func(t *testing.T) {
		data, err := s.Query(context.Background(), gazetteer.Listing{
			Lat: ptrF64(16.2415),
			Lon: ptrF64(-61.5328),
		})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		res := data.(*Result)
		ev := res.Evidence
		if ev.AuctionLat != 16.2415 {
			t.Errorf("Evidence.AuctionLat = %v, want 16.2415", ev.AuctionLat)
		}
		if ev.HaversineMeters != 0 {
			t.Errorf("Evidence.HaversineMeters = %d, want 0 on out-of-range", ev.HaversineMeters)
		}
		if ev.WalkMultiplier == 0 {
			t.Error("Evidence.WalkMultiplier = 0, want WalkSinuosityMultiplier")
		}
		if ev.ProximityCapM == 0 {
			t.Error("Evidence.ProximityCapM = 0, want MaxNearestStationMeters")
		}
		if ev.CatalogStations <= 0 {
			t.Errorf("Evidence.CatalogStations = %d, want >0 (catalog was loaded)", ev.CatalogStations)
		}
	})
}
