package ademe

import (
	"context"
	"net/http"
	"testing"
)

// TestSource_SurfaceMismatchIsNotHighConfidence is the end-to-end
// regression for the unbounded surface tie-break.
//
// PickBestByNumber returns the row whose surface is CLOSEST to the
// caller's, however far the closest one is. That is the right pick - it
// is the only certificate at that street number - but for a 30 m²
// studio at an address where ADEME holds nothing under 38 m², the
// answer is a neighbour's DPE, and it used to come back at
// ConfidenceHigh because the address legs all agreed.
//
// The fixture is the packaged Paris 11e list (38.2, 23.5, 25.1 m² at
// 82 rue de la Roquette).
func TestSource_SurfaceMismatchIsNotHighConfidence(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "list_paris11.json")

	newQuery := func(surface float64) *Result {
		t.Helper()
		srv := newStubServer(t, http.StatusOK, body)
		s := NewSource(Options{BaseURL: srv.URL, Geocoder: stubGeocoder{postCode: "75011"}})
		l := newListingParis11()
		if surface > 0 {
			l.SurfaceM2 = &surface
		}
		data, err := s.Query(context.Background(), l)
		if err != nil {
			t.Fatalf("Query(surface=%v): %v", surface, err)
		}
		return data.(*Result)
	}

	// A 24 m² studio: the 23.5 m² row agrees, everything else agrees,
	// so this is a genuine high-confidence match.
	res := newQuery(24)
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q for a 24 m² anchor against a 23.5 m² row, want high", res.Confidence)
	}
	if !res.Evidence.SurfaceComparable || !res.Evidence.SurfaceMatched {
		t.Errorf("Evidence surfaces = {comparable %v, matched %v}, want both true",
			res.Evidence.SurfaceComparable, res.Evidence.SurfaceMatched)
	}

	// A 140 m² flat: the closest row is 38.2 m², which is not it. Same
	// street, same number, real DPE label - and not the same dwelling.
	res = newQuery(140)
	if res.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q for a 140 m² anchor against a 38.2 m² row, want medium", res.Confidence)
	}
	if !res.Evidence.SurfaceComparable || res.Evidence.SurfaceMatched {
		t.Errorf("Evidence surfaces = {comparable %v, matched %v}, want {true, false}",
			res.Evidence.SurfaceComparable, res.Evidence.SurfaceMatched)
	}
	if res.Evidence.SurfaceAnchorM2 != 140 {
		t.Errorf("Evidence.SurfaceAnchorM2 = %v, want 140", res.Evidence.SurfaceAnchorM2)
	}

	// No anchor at all: nothing to compare, so nothing is held against
	// the match. The caller who supplies no surface gets what they always got.
	res = newQuery(0)
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q with no surface anchor, want high (unknown is not disagreement)", res.Confidence)
	}
	if res.Evidence.SurfaceComparable {
		t.Error("Evidence.SurfaceComparable = true with no anchor, want false")
	}
}
