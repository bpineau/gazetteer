package osm

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogIsFresh(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	station := Station{OSMID: 1, Lat: 48.85, Lon: 2.35}

	cases := []struct {
		name string
		c    *Catalog
		want bool
	}{
		{"nil", nil, false},
		{"empty", &Catalog{FetchedAt: now}, false},
		{"fresh", &Catalog{FetchedAt: now.Add(-time.Hour), Stations: []Station{station}}, true},
		{"stale", &Catalog{FetchedAt: now.Add(-RefreshAfter - time.Hour), Stations: []Station{station}}, false},
	}
	for _, c := range cases {
		if got := c.c.IsFresh(now); got != c.want {
			t.Errorf("%s: IsFresh = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNearestStation(t *testing.T) {
	c := &Catalog{
		SchemaVersion: CatalogSchemaVersion,
		Stations: []Station{
			{OSMID: 1, Name: "Châtelet", Lat: 48.8583, Lon: 2.3470},
			{OSMID: 2, Name: "Part-Dieu", Lat: 45.7605, Lon: 4.8596},
		},
	}

	st, meters, walk := c.NearestStation(48.8600, 2.3500)
	if st == nil || st.Name != "Châtelet" {
		t.Fatalf("NearestStation = %+v, want Châtelet", st)
	}
	if meters <= 0 || meters > 1000 {
		t.Errorf("haversine = %.0f m, want a few hundred metres", meters)
	}
	if walk < int(meters) {
		t.Errorf("walk = %d m must be >= haversine %.0f m (sinuosity)", walk, meters)
	}

	// The proximity cap refuses matches beyond it (DOM-TOM guard).
	if st, _, _ := c.NearestStationWithinMeters(16.24, -61.53, 5000); st != nil {
		t.Errorf("a Guadeloupe point must not match a metropolitan station, got %+v", st)
	}

	// (0, 0) sentinel and empty catalog return nothing.
	if st, _, _ := c.NearestStation(0, 0); st != nil {
		t.Errorf("(0,0) sentinel must not match, got %+v", st)
	}
	var nilCat *Catalog
	if st, _, _ := nilCat.NearestStation(48.86, 2.35); st != nil {
		t.Errorf("nil catalog must not match, got %+v", st)
	}
}

func TestCatalogSaveLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "osm", "transit_stations.json")
	in := &Catalog{
		SchemaVersion: CatalogSchemaVersion,
		FetchedAt:     time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		BBox:          "france",
		Stations:      []Station{{OSMID: 7, Name: "Mairie de Montreuil", Lat: 48.8625, Lon: 2.4415, Type: TransitTypeMetro, Lines: []string{"9"}}},
	}
	if err := SaveCatalog(path, in); err != nil {
		t.Fatalf("SaveCatalog: %v", err)
	}

	out, err := LoadCatalog(path)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if out == nil || len(out.Stations) != 1 || out.Stations[0].Name != "Mairie de Montreuil" {
		t.Fatalf("roundtrip mismatch: %+v", out)
	}
	if out.Stations[0].Lines[0] != "9" {
		t.Errorf("lines lost in roundtrip: %+v", out.Stations[0])
	}
}

func TestLoadCatalog_MissIsNotAnError(t *testing.T) {
	// Missing file: nil catalog, nil error ("needs refresh").
	c, err := LoadCatalog(filepath.Join(t.TempDir(), "absent.json"))
	if c != nil || err != nil {
		t.Errorf("missing file: got (%v, %v), want (nil, nil)", c, err)
	}

	// Empty path: in-memory-only mode.
	c, err = LoadCatalog("")
	if c != nil || err != nil {
		t.Errorf("empty path: got (%v, %v), want (nil, nil)", c, err)
	}

	// Schema mismatch: cold miss, not an error.
	path := filepath.Join(t.TempDir(), "old.json")
	old := &Catalog{SchemaVersion: CatalogSchemaVersion - 1, Stations: []Station{{OSMID: 1}}}
	if err := SaveCatalog(path, old); err != nil {
		t.Fatalf("SaveCatalog: %v", err)
	}
	c, err = LoadCatalog(path)
	if c != nil || err != nil {
		t.Errorf("schema mismatch: got (%v, %v), want (nil, nil)", c, err)
	}
}

func TestSaveCatalog_Errors(t *testing.T) {
	if err := SaveCatalog("", &Catalog{}); err == nil {
		t.Error("empty path should error")
	}
	if err := SaveCatalog(filepath.Join(t.TempDir(), "x.json"), nil); err == nil {
		t.Error("nil catalog should error")
	}
}

func TestDefaultCatalogPath(t *testing.T) {
	if got := DefaultCatalogPath(""); got != "" {
		t.Errorf("empty dataDir should yield empty path, got %q", got)
	}
	want := filepath.Join("d", "osm", "transit_stations.json")
	if got := DefaultCatalogPath("d"); got != want {
		t.Errorf("DefaultCatalogPath = %q, want %q", got, want)
	}
}
