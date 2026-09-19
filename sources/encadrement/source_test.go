package encadrement

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestLoad smokes the embedded dataset.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
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

// TestClampPiece pins the rooms → piece-bucket clamp. 0 means "unknown",
// which cellFilter reads as "span every bucket" — never as a studio.
func TestClampPiece(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   int
		want int
	}{
		{-3, 0},
		{0, 0},
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

// query is the test shorthand for one embedded-index lookup.
func query(t *testing.T, l gazetteer.Listing) *Result {
	t.Helper()
	l.PropertyType = gazetteer.PropertyApartment
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query(%+v): %v", l, err)
	}
	if res == nil {
		t.Fatalf("Query(%+v): nil result", l)
	}
	return res
}

func intp(n int) *int { return &n }

// TestQuery_LyonFourRoomsIsCapped is the regression for the dropped
// open-ended bucket: up to source v4 the Lyon snapshot published nothing
// above three rooms, so every T4+ in Lyon and Villeurbanne came back
// IsEmpty() — indistinguishable from an address outside the perimeter, which
// downstream reads as "no legal cap" and lets an uncapped market rent stand.
func TestQuery_LyonFourRoomsIsCapped(t *testing.T) {
	t.Parallel()
	for _, insee := range []string{"69383", "69266"} {
		three := query(t, gazetteer.Listing{INSEE: insee, Rooms: intp(3)})
		for _, rooms := range []int{4, 5, 7} {
			res := query(t, gazetteer.Listing{INSEE: insee, Rooms: intp(rooms)})
			if res.IsEmpty() {
				t.Fatalf("%s, %d rooms: IsEmpty(), want the open-ended cap", insee, rooms)
			}
			if res.ZoneSource != ZoneSourceLyonVilleurbanne {
				t.Errorf("%s, %d rooms: ZoneSource = %q", insee, rooms, res.ZoneSource)
			}
			// The open-ended cell is cheaper per m² than the 3-room one:
			// a wrong bucket would show up as an equal or higher cap.
			if res.LoyerRefMajEURPerM2HC >= three.LoyerRefMajEURPerM2HC {
				t.Errorf("%s, %d rooms: maj = %.2f, want below the 3-room %.2f",
					insee, rooms, res.LoyerRefMajEURPerM2HC, three.LoyerRefMajEURPerM2HC)
			}
		}
	}
}

// TestQuery_BuildYearSelectsEpoque pins that the construction period picks the
// cell instead of being averaged away. The four Paris buckets are ordered
// 1946-1970 < 1971-1990 < après 1990 < avant 1946 on the same quartier, so a
// médiane across them is wrong for every one of them.
func TestQuery_BuildYearSelectsEpoque(t *testing.T) {
	t.Parallel()
	spanned := query(t, gazetteer.Listing{Zip: "75001", Rooms: intp(3)})
	if spanned.Evidence.Epoque != "" || spanned.Evidence.BuildYear != 0 {
		t.Errorf("no BuildYear: Evidence époque = %q / year %d, want the spanning zero values",
			spanned.Evidence.Epoque, spanned.Evidence.BuildYear)
	}

	seen := map[string]float64{}
	for _, year := range []int{1930, 1960, 1980, 2010} {
		res := query(t, gazetteer.Listing{Zip: "75001", Rooms: intp(3), BuildYear: intp(year)})
		if res.IsEmpty() {
			t.Fatalf("build year %d: IsEmpty()", year)
		}
		if res.Evidence.BuildYear != year {
			t.Errorf("build year %d: Evidence.BuildYear = %d", year, res.Evidence.BuildYear)
		}
		if res.Evidence.Epoque == "" {
			t.Fatalf("build year %d: no époque pinned (cells = %d)", year, res.Evidence.NbCellsMatched)
		}
		if res.Evidence.NbCellsMatched >= spanned.Evidence.NbCellsMatched {
			t.Errorf("build year %d: matched %d cells, want fewer than the %d spanned",
				year, res.Evidence.NbCellsMatched, spanned.Evidence.NbCellsMatched)
		}
		if prev, dup := seen[res.Evidence.Epoque]; dup {
			t.Errorf("build year %d: reused époque %q (%.2f)", year, res.Evidence.Epoque, prev)
		}
		seen[res.Evidence.Epoque] = res.LoyerRefMajEURPerM2HC
	}
	if len(seen) != 4 {
		t.Fatalf("époques hit = %v, want the four published buckets", seen)
	}
	// Pre-1946 is the dearest bucket in Paris 1er and 1946-1970 the cheapest;
	// the all-époques médiane sits between them, so it overstates the cap of
	// a post-war building and understates that of a Haussmannien.
	if seen["Avant 1946"] <= spanned.LoyerRefMajEURPerM2HC {
		t.Errorf("avant 1946 cap %.2f, want above the spanning %.2f", seen["Avant 1946"], spanned.LoyerRefMajEURPerM2HC)
	}
	if seen["1946-1970"] >= spanned.LoyerRefMajEURPerM2HC {
		t.Errorf("1946-1970 cap %.2f, want below the spanning %.2f", seen["1946-1970"], spanned.LoyerRefMajEURPerM2HC)
	}
}

// TestQuery_ImplausibleBuildYearSpans keeps a sentinel or a typo from
// selecting no cell at all: it must read as "époque unknown", not as a
// narrowed lookup and not as an unregulated address.
func TestQuery_ImplausibleBuildYearSpans(t *testing.T) {
	t.Parallel()
	want := query(t, gazetteer.Listing{Zip: "75001", Rooms: intp(3)})
	for _, year := range []int{0, -1, 20, 9999} {
		res := query(t, gazetteer.Listing{Zip: "75001", Rooms: intp(3), BuildYear: intp(year)})
		if res.LoyerRefMajEURPerM2HC != want.LoyerRefMajEURPerM2HC {
			t.Errorf("build year %d: maj = %.2f, want the spanning %.2f",
				year, res.LoyerRefMajEURPerM2HC, want.LoyerRefMajEURPerM2HC)
		}
		if res.Evidence.EpoqueUnmatched {
			t.Errorf("build year %d: EpoqueUnmatched, want it treated as absent", year)
		}
	}
}

// TestQuery_UnknownRoomsSpansAtLowConfidence pins that a missing Rooms no
// longer quotes the studio cap (the dearest cell of every grille) under a
// medium-confidence badge.
func TestQuery_UnknownRoomsSpansAtLowConfidence(t *testing.T) {
	t.Parallel()
	unknown := query(t, gazetteer.Listing{Zip: "75001"})
	studio := query(t, gazetteer.Listing{Zip: "75001", Rooms: intp(1)})

	if unknown.IsEmpty() {
		t.Fatalf("unknown rooms: IsEmpty(), want a spanning reading")
	}
	if unknown.Confidence != ConfidenceLow {
		t.Errorf("unknown rooms: Confidence = %q, want %q", unknown.Confidence, ConfidenceLow)
	}
	if unknown.Evidence.Piece != 0 {
		t.Errorf("unknown rooms: Evidence.Piece = %d, want 0", unknown.Evidence.Piece)
	}
	if unknown.LoyerRefMajEURPerM2HC >= studio.LoyerRefMajEURPerM2HC {
		t.Errorf("unknown rooms: maj = %.2f, want strictly below the studio %.2f",
			unknown.LoyerRefMajEURPerM2HC, studio.LoyerRefMajEURPerM2HC)
	}
	if studio.Confidence != ConfidenceMedium {
		t.Errorf("1 room: Confidence = %q, want %q", studio.Confidence, ConfidenceMedium)
	}
}

// TestQuery_IdentifierEitherWay: a Paris listing carrying only its INSEE, and
// a Lyon one carrying only its zip, are inside the perimeter and must not be
// reported unregulated. The BAN fills both, but hand-built Listings routinely
// carry one.
func TestQuery_IdentifierEitherWay(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		both, one  gazetteer.Listing
		wantZone   string
		wantSource string
	}{
		{
			name:       "paris by insee",
			both:       gazetteer.Listing{Zip: "75011", INSEE: "75111", Rooms: intp(3)},
			one:        gazetteer.Listing{INSEE: "75111", Rooms: intp(3)},
			wantZone:   "Paris 11e",
			wantSource: ZoneSourceParis,
		},
		{
			name:       "lyon by zip",
			both:       gazetteer.Listing{Zip: "69003", INSEE: "69383", Rooms: intp(3)},
			one:        gazetteer.Listing{Zip: "69003", Rooms: intp(3)},
			wantZone:   "Lyon 3e",
			wantSource: ZoneSourceLyonVilleurbanne,
		},
		{
			name:       "villeurbanne by zip",
			both:       gazetteer.Listing{Zip: "69100", INSEE: "69266", Rooms: intp(3)},
			one:        gazetteer.Listing{Zip: "69100", Rooms: intp(3)},
			wantZone:   "Villeurbanne",
			wantSource: ZoneSourceLyonVilleurbanne,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			full, partial := query(t, c.both), query(t, c.one)
			if partial.IsEmpty() {
				t.Fatalf("IsEmpty(), want the %s cap", c.wantZone)
			}
			if partial.Zone != c.wantZone || partial.ZoneSource != c.wantSource {
				t.Errorf("zone = %q/%q, want %q/%q", partial.Zone, partial.ZoneSource, c.wantZone, c.wantSource)
			}
			if partial.LoyerRefMajEURPerM2HC != full.LoyerRefMajEURPerM2HC {
				t.Errorf("maj = %.2f, want the both-identifier %.2f",
					partial.LoyerRefMajEURPerM2HC, full.LoyerRefMajEURPerM2HC)
			}
		})
	}
}

// TestQuery_NotControlledStaysNotControlled guards the other direction: the
// looser identifier matching must not drag a neighbouring commune in. Paris's
// parent code, the Lyon parent code and a plain 69 commune are all outside.
func TestQuery_NotControlledStaysNotControlled(t *testing.T) {
	t.Parallel()
	for _, l := range []gazetteer.Listing{
		{INSEE: "75056", Zip: "75000", Rooms: intp(3)}, // Paris parent code
		{INSEE: "69123", Rooms: intp(3)},               // Lyon parent code
		{INSEE: "69029", Zip: "69380", Rooms: intp(3)}, // Chasselay, Métropole but unregulated
		{INSEE: "13201", Zip: "13001", Rooms: intp(3)}, // Marseille: no grille ships
		{INSEE: "93029", Zip: "93700", Rooms: intp(3)}, // Drancy, in neither 93 EPT
	} {
		res := query(t, l)
		if !res.IsEmpty() {
			t.Errorf("%+v: maj = %.2f, want no cap (outside every shipped grille)", l, res.LoyerRefMajEURPerM2HC)
		}
		if res.Confidence != ConfidenceNone {
			t.Errorf("%+v: Confidence = %q, want %q", l, res.Confidence, ConfidenceNone)
		}
	}
}
