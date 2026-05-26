package dvf

import (
	"context"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/fallback"
)

// newLadderHarness builds a Source + tierContext suitable for driving
// buildLadder / the per-tier Try closures in isolation. auctionLat/Lon
// are pointers so callers can leave them nil to exercise the
// SkipOn-friendly degenerate path.
func newLadderHarness(t *testing.T, auctionLat, auctionLon *float64) (*Source, *tierContext) {
	t.Helper()
	hc := newHTTPClient(t)
	s := NewSource(Options{
		HTTP:     hc,
		Geocoder: stubGeocoder{res: banx.GeocodeResult{}},
	})
	var (
		filtered        []Mutation
		totalRaw        int
		sectionsQueried int
		primaryCommunes []string
		radiusM         float64
	)
	tc := &tierContext{
		target:          "Appartement",
		cutoff:          time.Now().AddDate(-CutoffYears, 0, 0),
		listingID:       "test",
		auctionLat:      auctionLat,
		auctionLon:      auctionLon,
		totalRaw:        &totalRaw,
		sectionsQueried: &sectionsQueried,
		communesQueried: &primaryCommunes,
		filtered:        &filtered,
		radiusM:         &radiusM,
	}
	return s, tc
}

// TestLadder_Order pins the 4-tier ordering of the DVF fallback ladder:
// address_radius → commune → neighborhood → department.
func TestLadder_Order(t *testing.T) {
	s, tc := newLadderHarness(t, nil, nil)
	ladder := s.buildLadder("75110", tc)

	wantNames := []string{"address_radius", "commune", "neighborhood", "department"}
	if len(ladder) != len(wantNames) {
		t.Fatalf("ladder len = %d, want %d", len(ladder), len(wantNames))
	}
	for i, want := range wantNames {
		if ladder[i].Name != want {
			t.Errorf("ladder[%d].Name = %q want %q", i, ladder[i].Name, want)
		}
		if ladder[i].Try == nil {
			t.Errorf("ladder[%d].Try is nil", i)
		}
	}
	// address_radius uses the tighter MinSampleSizeAddressRadius
	// threshold.
	if !ladder[0].SkipOn(fallback.Output{SampleSize: MinSampleSizeAddressRadius - 1}) {
		t.Errorf("address_radius SkipOn(sample=%d) should be true", MinSampleSizeAddressRadius-1)
	}
	if ladder[0].SkipOn(fallback.Output{SampleSize: MinSampleSizeAddressRadius}) {
		t.Errorf("address_radius SkipOn(sample=%d) should be false", MinSampleSizeAddressRadius)
	}
	// commune / neighborhood use the legacy MinSampleSize.
	for i := 1; i <= 2; i++ {
		if ladder[i].SkipOn == nil {
			t.Errorf("ladder[%d] (%s): SkipOn nil — would never zoom out", i, ladder[i].Name)
			continue
		}
		if !ladder[i].SkipOn(fallback.Output{SampleSize: MinSampleSize - 1}) {
			t.Errorf("ladder[%d] (%s): SkipOn(sample=%d) should be true", i, ladder[i].Name, MinSampleSize-1)
		}
	}
	// Last tier must accept whatever it gets.
	if ladder[3].SkipOn != nil {
		t.Error("ladder[3] (department): SkipOn should be nil (last-resort tier)")
	}
}

// TestAddressRadius_SkipsWhenLatLonMissing pins the degenerate path: the
// tier's Try must return SampleSize=0 with no error when the listing
// carries no geocoded coords.
func TestAddressRadius_SkipsWhenLatLonMissing(t *testing.T) {
	s, tc := newLadderHarness(t, nil, nil)
	try := s.makeTryAddressRadius([]string{"75110"}, tc)
	out, err := try(context.Background(), fallback.Input{})
	if err != nil {
		t.Fatalf("Try returned err: %v", err)
	}
	if out.SampleSize != 0 {
		t.Errorf("expected SampleSize=0 on nil lat/lon, got %d", out.SampleSize)
	}
	if out.LevelUsed != "address_radius" {
		t.Errorf("expected LevelUsed=address_radius, got %q", out.LevelUsed)
	}
}
