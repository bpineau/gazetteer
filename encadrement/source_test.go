package encadrement

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer"
)

// TestLoad smokes the embedded dataset.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx == nil {
		t.Fatalf("nil index")
	}
	if got := idx.CountParis(); got < 500 {
		t.Errorf("CountParis = %d, want ≥ 500", got)
	}
	if got := idx.CountPlaineCommune(); got < 20 {
		t.Errorf("CountPlaineCommune = %d, want ≥ 20", got)
	}
	if got := idx.CountLyon(); got < 100 {
		t.Errorf("CountLyon = %d, want ≥ 100", got)
	}
}

// TestQuery_Paris exercises the happy path for a Paris 11e listing.
func TestQuery_Paris(t *testing.T) {
	t.Parallel()
	rooms := 3
	l := gazetteer.Listing{
		Zip:          "75011",
		PropertyType: gazetteer.PropertyApartment,
		Rooms:        &rooms,
	}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Paris 11e 3 pièces")
	}
	if res.ZoneSource != ZoneSourceParis {
		t.Errorf("ZoneSource = %q, want %q", res.ZoneSource, ZoneSourceParis)
	}
	if res.Zone != "Paris 11e" {
		t.Errorf("Zone = %q, want %q", res.Zone, "Paris 11e")
	}
	if res.LoyerRefMajEURPerM2HC < 15 || res.LoyerRefMajEURPerM2HC > 60 {
		t.Errorf("LoyerRefMajEURPerM2HC = %.2f, want in [15, 60]", res.LoyerRefMajEURPerM2HC)
	}
	if res.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceMedium)
	}
}

// TestQuery_Lyon exercises the happy path for a Lyon 3e listing.
func TestQuery_Lyon(t *testing.T) {
	t.Parallel()
	rooms := 2
	l := gazetteer.Listing{
		INSEE:        "69383",
		PropertyType: gazetteer.PropertyApartment,
		Rooms:        &rooms,
	}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Lyon 3e")
	}
	if res.ZoneSource != ZoneSourceLyonVilleurbanne {
		t.Errorf("ZoneSource = %q, want %q", res.ZoneSource, ZoneSourceLyonVilleurbanne)
	}
	if res.Zone != "Lyon 3e" {
		t.Errorf("Zone = %q, want %q", res.Zone, "Lyon 3e")
	}
}

// TestQuery_Villeurbanne resolves 69266 to the Villeurbanne label.
func TestQuery_Villeurbanne(t *testing.T) {
	t.Parallel()
	rooms := 2
	l := gazetteer.Listing{
		INSEE:        "69266",
		PropertyType: gazetteer.PropertyApartment,
		Rooms:        &rooms,
	}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Villeurbanne")
	}
	if res.Zone != "Villeurbanne" {
		t.Errorf("Zone = %q, want %q", res.Zone, "Villeurbanne")
	}
}

// TestQuery_OutsidePerimeter returns a non-matching result.
func TestQuery_OutsidePerimeter(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{
		Zip:          "33000", // Bordeaux — outside the shipped Paris/Lyon/PC perimeter.
		INSEE:        "33063",
		PropertyType: gazetteer.PropertyApartment,
	}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil {
		t.Fatalf("nil result")
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty() = false, want true for Bordeaux")
	}
	if res.Confidence != ConfidenceNone {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceNone)
	}
}

// TestQuery_UnsupportedPropertyType rejects land / commercial / unknown.
func TestQuery_UnsupportedPropertyType(t *testing.T) {
	t.Parallel()
	cases := []gazetteer.PropertyType{
		gazetteer.PropertyLand,
		gazetteer.PropertyCommercial,
		gazetteer.PropertyUnknown,
	}
	for _, pt := range cases {
		t.Run(string(pt), func(t *testing.T) {
			l := gazetteer.Listing{Zip: "75011", PropertyType: pt}
			_, err := Query(context.Background(), Options{}, l)
			if !errors.Is(err, gazetteer.ErrUnsupportedPropertyType) {
				t.Fatalf("err = %v, want ErrUnsupportedPropertyType", err)
			}
		})
	}
}

// TestParisArrondissementFromZip pins the zip→arr extraction logic.
func TestParisArrondissementFromZip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		zip  string
		want string
	}{
		{"75001", "01"},
		{"75011", "11"},
		{"75020", "20"},
		{"75116", "16"},
		{"75021", ""},
		{"75100", ""},
		{"75000", ""},
		{"94100", ""},
		{"", ""},
		{"7501", ""},
	}
	for _, c := range cases {
		if got := parisArrondissementFromZip(c.zip); got != c.want {
			t.Errorf("parisArrondissementFromZip(%q) = %q, want %q", c.zip, got, c.want)
		}
	}
}

// TestLyonZoneLabel pins the INSEE→label mapping.
func TestLyonZoneLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		insee string
		want  string
	}{
		{"69381", "Lyon 1er"},
		{"69383", "Lyon 3e"},
		{"69389", "Lyon 9e"},
		{"69266", "Villeurbanne"},
		{"69123", "Lyon Métropole"},
	}
	for _, c := range cases {
		if got := lyonZoneLabel(c.insee); got != c.want {
			t.Errorf("lyonZoneLabel(%q) = %q, want %q", c.insee, got, c.want)
		}
	}
}

// TestClampPiece pins the rooms → piece-bucket clamp.
func TestClampPiece(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   int
		want int
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 3},
		{4, 4},
		{5, 4},
		{10, 4},
	}
	for _, c := range cases {
		if got := clampPiece(c.in); got != c.want {
			t.Errorf("clampPiece(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestMedianFloat pins the median helper.
func TestMedianFloat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   []float64
		want float64
	}{
		{nil, 0},
		{[]float64{}, 0},
		{[]float64{1}, 1},
		{[]float64{1, 2, 3}, 2},
		{[]float64{3, 1, 2}, 2},
		{[]float64{1, 2, 3, 4}, 2.5},
	}
	for _, c := range cases {
		got := medianFloat(append([]float64(nil), c.in...))
		if got != c.want {
			t.Errorf("medianFloat(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
