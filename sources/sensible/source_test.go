package sensible

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/geopoly"
)

func ptr(v float64) *float64 { return &v }

// square returns a ~1.1 km square MultiPolygon centred on (lat, lon).
func square(lat, lon float64) geopoly.MultiPolygon {
	const d = 0.005
	return geopoly.MultiPolygon{{geopoly.Ring{
		{Lon: lon - d, Lat: lat - d},
		{Lon: lon + d, Lat: lat - d},
		{Lon: lon + d, Lat: lat + d},
		{Lon: lon - d, Lat: lat + d},
	}}}
}

func TestQueryRequiresCoordinates(t *testing.T) {
	_, err := NewSource(Options{Index: NewIndexForTest(nil)}).Query(context.Background(), gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Fatalf("err = %v, want ErrInsufficientInputs", err)
	}
}

func TestInsideZone(t *testing.T) {
	idx := NewIndexForTest(map[string]geopoly.MultiPolygon{"Zone Test": square(48.94, 2.52)})
	r, err := NewSource(Options{Index: idx}).QueryResult(context.Background(),
		gazetteer.Listing{Lat: ptr(48.94), Lon: ptr(2.52)})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Sensitive || len(r.In) != 1 || r.In[0].Name != "Zone Test" || r.In[0].DistanceM != 0 {
		t.Fatalf("inside: got %+v", r)
	}
	if r.IsEmpty() {
		t.Fatal("a sensitive hit must not be IsEmpty")
	}
}

func TestNearbyZone(t *testing.T) {
	// Point ~200 m east of the square's NE corner: not inside, but within
	// NearbyMeters of a boundary vertex (the distance hint is vertex-based;
	// real QRR rings are dense, the synthetic square only has corners).
	idx := NewIndexForTest(map[string]geopoly.MultiPolygon{"Zone Test": square(48.94, 2.52)})
	lon := 2.52 + 0.005 + 0.0027 // NE corner + ~200 m at this latitude
	r, err := NewSource(Options{Index: idx}).QueryResult(context.Background(),
		gazetteer.Listing{Lat: ptr(48.945), Lon: ptr(lon)})
	if err != nil {
		t.Fatal(err)
	}
	if r.Sensitive || len(r.In) != 0 {
		t.Fatalf("should not be inside: %+v", r)
	}
	if len(r.Nearby) != 1 || r.Nearby[0].DistanceM <= 0 || r.Nearby[0].DistanceM > NearbyMeters {
		t.Fatalf("nearby: got %+v", r.Nearby)
	}
	if r.IsEmpty() {
		t.Fatal("a nearby hit must not be IsEmpty")
	}
}

func TestFarFromEverything(t *testing.T) {
	idx := NewIndexForTest(map[string]geopoly.MultiPolygon{"Zone Test": square(48.94, 2.52)})
	r, err := NewSource(Options{Index: idx}).QueryResult(context.Background(),
		gazetteer.Listing{Lat: ptr(45.76), Lon: ptr(4.83)}) // Lyon — but curated circles are all IDF too
	if err != nil {
		t.Fatal(err)
	}
	if !r.IsEmpty() || r.Sensitive {
		t.Fatalf("far point should be empty, got %+v", r)
	}
}

// The curated ORCOD-IN circles apply even with an Index carrying no QRR
// polygon — the overlay lives in code, not in the artifact.
func TestCuratedCircleHit(t *testing.T) {
	idx := NewIndexForTest(nil)
	// Allée du Chêne Pointu, Clichy-sous-Bois — inside the Bas Clichy circle.
	r, err := NewSource(Options{Index: idx}).QueryResult(context.Background(),
		gazetteer.Listing{Lat: ptr(48.9023), Lon: ptr(2.5457)})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Sensitive || len(r.In) == 0 {
		t.Fatalf("Chêne Pointu should hit the ORCOD-IN circle, got %+v", r)
	}
	if r.In[0].Kind != KindORCOD || r.In[0].Note == "" {
		t.Fatalf("curated hit must carry kind+note, got %+v", r.In[0])
	}
}

// Embedded-artifact smoke test: the canonical examples must resolve — Les
// Beaudottes (Sevran) inside its QRR, Notre-Dame de Paris inside nothing.
func TestEmbeddedArtifactKnownPoints(t *testing.T) {
	idx, err := Load("-")
	if err != nil {
		t.Fatal(err)
	}
	if idx.ZoneCount() < 50 {
		t.Fatalf("embedded artifact has %d zones, want >= 50", idx.ZoneCount())
	}

	in, _ := idx.resolve(48.9442, 2.5267) // gare RER Sevran-Beaudottes
	found := false
	for _, z := range in {
		if z.Kind == KindQRR && z.Dep == "93" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Sevran-Beaudottes should sit in a 93 QRR, got %+v", in)
	}

	in, nearby := idx.resolve(48.8530, 2.3499) // Notre-Dame de Paris
	if len(in) != 0 || len(nearby) != 0 {
		t.Fatalf("Notre-Dame should match nothing, got in=%+v nearby=%+v", in, nearby)
	}

	// Les 4000 (mail Maurice de Fontenay) sit OUTSIDE the La Courneuve QRR
	// (which covers Quatre-Routes) — the curated circle must catch them.
	in, _ = idx.resolve(48.9284, 2.3785)
	found = false
	for _, z := range in {
		if z.Kind == KindCurated && z.Dep == "93" {
			found = true
		}
	}
	if !found {
		t.Fatalf("mail de Fontenay should sit in the curated 4000 circle, got %+v", in)
	}
}
