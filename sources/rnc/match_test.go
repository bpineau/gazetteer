package rnc

import (
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

func f64(v float64) *float64 { return &v }

func idxFixture() *Index {
	return NewIndexForTest([]Entry{
		{Immatriculation: "AA1", INSEE: "75102", Lat: 48.8705, Lon: 2.3370, VoieNorm: normVoie("rue de gramont"), TypeSyndic: "professionnel", MandatEnCours: "Mandat en cours", LotsTotal: 39},
		{Immatriculation: "BB2", INSEE: "75102", Lat: 48.8690, Lon: 2.3400, VoieNorm: normVoie("avenue de l'opera"), MandatEnCours: "Pas de mandat en cours"},
	})
}

func TestMatch_GeoVoieHigh(t *testing.T) {
	t.Parallel()
	idx := idxFixture()
	// ~15 m from AA1, same (abbreviated) street.
	e, m, c, d := idx.match(gazetteer.Listing{INSEE: "75102", Lat: f64(48.87052), Lon: f64(2.33702), Address: "20 r de gramont"})
	if e == nil || e.Immatriculation != "AA1" {
		t.Fatalf("want AA1, got %+v", e)
	}
	if m != MatchGeoVoie || c != ConfidenceHigh {
		t.Errorf("m=%q c=%q d=%.1f, want geo_voie/high", m, c, d)
	}
}

func TestMatch_GeoMediumNoStreet(t *testing.T) {
	t.Parallel()
	idx := idxFixture()
	// ~15 m from AA1 but no street given → medium, not high.
	e, m, c, _ := idx.match(gazetteer.Listing{INSEE: "75102", Lat: f64(48.87052), Lon: f64(2.33702)})
	if e == nil || e.Immatriculation != "AA1" {
		t.Fatalf("want AA1, got %+v", e)
	}
	if m != MatchGeoVoie || c != ConfidenceMedium {
		t.Errorf("m=%q c=%q, want geo_voie/medium", m, c)
	}
}

func TestMatch_None_OtherCommune(t *testing.T) {
	t.Parallel()
	e, _, c, _ := idxFixture().match(gazetteer.Listing{INSEE: "93066", Address: "1 rue X"})
	if e != nil || c != ConfidenceNone {
		t.Errorf("want none, got %+v / %q", e, c)
	}
}

func TestMatch_None_TooFar(t *testing.T) {
	t.Parallel()
	// > 60 m from both copros → no geo match, no single street → none.
	e, _, c, _ := idxFixture().match(gazetteer.Listing{INSEE: "75102", Lat: f64(48.9000), Lon: f64(2.4000)})
	if e != nil || c != ConfidenceNone {
		t.Errorf("want none, got %+v / %q", e, c)
	}
}
