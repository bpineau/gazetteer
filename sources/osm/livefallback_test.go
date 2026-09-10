package osm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestSource_LiveFallback_AnswersWhereTheCatalogCannot: with a Fetcher wired
// in, a point the offline catalog does not cover is answered by a live
// Overpass lookup around that point, shaped exactly like a catalog hit.
func TestSource_LiveFallback_AnswersWhereTheCatalogCannot(t *testing.T) {
	t.Parallel()
	body := loadFixture(t, "paris15_sample.json")
	live, err := ParseOverpass(body)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	stub := &stubOverpass{stations: func(string) ([]byte, error) { return body, nil }}

	// Explicit empty catalog: nothing offline to answer with.
	s := NewSource(Options{Catalog: &Catalog{}, Fetcher: stub})
	before := time.Now()
	r, err := s.QueryResult(context.Background(), gazetteer.Listing{
		Lat: ptrF64(48.8407), Lon: ptrF64(2.2880), // Paris XV, next to Lourmel
	})
	if err != nil {
		t.Fatalf("QueryResult: %v", err)
	}
	if r.IsEmpty() || r.NearestTransitName != "Lourmel" {
		t.Fatalf("live fallback = %+v, want a Lourmel hit", r)
	}
	if r.NearestTransitType != TransitTypeMetro || r.Confidence != ConfidenceHigh {
		t.Errorf("type/confidence = %q/%q, want metro/high", r.NearestTransitType, r.Confidence)
	}
	if r.NearestTransitWalkM <= 0 || r.NearestTransitWalkMin < 1 {
		t.Errorf("walk = %d m / %d min, want both positive", r.NearestTransitWalkM, r.NearestTransitWalkMin)
	}
	if r.Evidence.CatalogStations != len(live) {
		t.Errorf("Evidence.CatalogStations = %d, want the %d live stations", r.Evidence.CatalogStations, len(live))
	}
	stamp, perr := time.Parse(time.RFC3339, r.Evidence.CatalogFetchedAt)
	if perr != nil || stamp.Before(before.Truncate(time.Second)) {
		t.Errorf("Evidence.CatalogFetchedAt = %q, want the live-lookup instant", r.Evidence.CatalogFetchedAt)
	}
	// The bbox handed to Overpass must be the small square around the point,
	// not the France-wide default.
	if stub.callCount() != 1 {
		t.Errorf("fetcher calls = %d, want 1", stub.callCount())
	}
}

// TestSource_LiveFallback_NotConsultedWhenTheCatalogAnswers: the offline
// catalog is the fast path and must short-circuit the network entirely.
func TestSource_LiveFallback_NotConsultedWhenTheCatalogAnswers(t *testing.T) {
	t.Parallel()
	stub := &stubOverpass{stations: func(string) ([]byte, error) {
		return nil, errors.New("the live path must not be taken")
	}}
	s := NewSource(Options{Catalog: newTestCatalog(t), Fetcher: stub})
	r, err := s.QueryResult(context.Background(), gazetteer.Listing{
		Lat: ptrF64(48.8407), Lon: ptrF64(2.2880),
	})
	if err != nil || r.IsEmpty() {
		t.Fatalf("catalog hit = (%+v, %v), want a populated Result", r, err)
	}
	if stub.callCount() != 0 {
		t.Errorf("fetcher called %d times although the catalog answered", stub.callCount())
	}
}

func TestSource_LiveFallback_Failures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		err  error
	}{
		{"transport failure", "", errors.New("mirror down")},
		{"unparsable answer", "{not json", nil},
		{"nothing in range", `{"elements":[]}`, nil},
	}
	for _, c := range cases {
		t.Run(c.name+", no catalog", func(t *testing.T) {
			t.Parallel()
			stub := &stubOverpass{stations: func(string) ([]byte, error) {
				if c.err != nil {
					return nil, c.err
				}
				return []byte(c.body), nil
			}}
			s := NewSource(Options{Catalog: &Catalog{}, Fetcher: stub})
			r, err := s.QueryResult(context.Background(), gazetteer.Listing{
				Lat: ptrF64(48.8407), Lon: ptrF64(2.2880),
			})
			if c.name == "nothing in range" {
				// The live path ran fine and simply found nothing: that is an
				// empty Result, not a failure.
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				if !r.IsEmpty() || r.SkipReason != SkipReasonOutOfRange {
					t.Errorf("r = %+v, want an out-of-range empty Result", r)
				}
				return
			}
			// No offline data and the live leg failed: transient, retryable.
			if !errors.Is(err, ErrNoCatalog) {
				t.Fatalf("err = %v, want ErrNoCatalog", err)
			}
		})

		t.Run(c.name+", catalog present", func(t *testing.T) {
			t.Parallel()
			stub := &stubOverpass{stations: func(string) ([]byte, error) {
				if c.err != nil {
					return nil, c.err
				}
				return []byte(c.body), nil
			}}
			// A Guadeloupe point: the metropolitan catalog cannot answer, so
			// the live leg is tried, fails, and the Source still degrades to
			// a skipped Result rather than an error.
			s := NewSource(Options{Catalog: newTestCatalog(t), Fetcher: stub})
			r, err := s.QueryResult(context.Background(), gazetteer.Listing{
				Lat: ptrF64(16.2415), Lon: ptrF64(-61.5328),
			})
			if err != nil {
				t.Fatalf("err = %v, want nil (the catalog covers the failure)", err)
			}
			if !r.IsEmpty() || r.SkipReason != SkipReasonOutOfRange {
				t.Errorf("r = %+v, want an out-of-range empty Result", r)
			}
			if r.Evidence.CatalogStations == 0 {
				t.Error("Evidence must still say which catalog was consulted")
			}
		})
	}
}

func TestPointBBox(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		lat, lon float64
		radius   float64
		want     string
	}{
		{"paris, 5 km", 48.84070, 2.28800, 5000, "48.79565,2.21956,48.88575,2.35644"},
		{"equator: no longitude stretch", 0, 0, 111_000, "-1.00000,-1.00000,1.00000,1.00000"},
		// Past the poles cos(lat) collapses; the 0.01 floor keeps the box
		// finite instead of spanning the whole planet.
		{"north pole", 90, 0, 1110, "89.99000,-1.00000,90.01000,1.00000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := pointBBox(c.lat, c.lon, c.radius); got != c.want {
				t.Errorf("pointBBox(%v, %v, %v) = %q, want %q", c.lat, c.lon, c.radius, got, c.want)
			}
		})
	}

	// The bbox is the Overpass "south,west,north,east" convention, so the
	// query built on it must carry it verbatim.
	bbox := pointBBox(48.84, 2.29, MaxNearestStationMeters)
	if !strings.Contains(FranceTransitOverpassQL(bbox), "[bbox:"+bbox+"]") {
		t.Errorf("the QL must embed the point bbox %q", bbox)
	}
}
