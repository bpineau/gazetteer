package carteloyers

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer"
)

// TestLoad smokes the embedded dataset: the four CSVs parse and each
// covers a meaningful chunk of communes.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx == nil {
		t.Fatalf("nil index")
	}
	for _, typ := range []Typology{TypologyApartment, TypologyHouse, TypologyApt12, TypologyApt3} {
		n := idx.Count(typ)
		if n < 5000 {
			t.Errorf("Count(%q) = %d, want ≥ 5000", typ, n)
		}
	}
}

// TestLookup_KnownCommune pins the apartment median for Paris 11e to a
// sanity range.
func TestLookup_KnownCommune(t *testing.T) {
	t.Parallel()
	idx, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	row, ok := idx.Lookup("75111", TypologyApartment)
	if !ok {
		t.Fatalf("Paris 11e not in dataset")
	}
	if row.LoyerMedCC < 15 || row.LoyerMedCC > 50 {
		t.Errorf("LoyerMedCC = %.2f, want in [15, 50]", row.LoyerMedCC)
	}
	if row.Department != "75" {
		t.Errorf("Department = %q, want %q", row.Department, "75")
	}
}

// TestQuery_HappyPath exercises the full Source.Query path with a known
// commune + apartment listing.
func TestQuery_HappyPath(t *testing.T) {
	t.Parallel()
	rooms := 3
	l := gazetteer.Listing{
		INSEE:        "75111",
		PropertyType: gazetteer.PropertyApartment,
		Rooms:        &rooms,
	}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Paris 11e apartment")
	}
	if res.Typology != TypologyApt3 {
		t.Errorf("Typology = %q, want %q", res.Typology, TypologyApt3)
	}
	if res.Confidence == "" {
		t.Errorf("Confidence empty")
	}
	if res.Evidence.INSEE != "75111" {
		t.Errorf("Evidence.INSEE = %q, want 75111", res.Evidence.INSEE)
	}
}

// TestQuery_FallbackToGeneric flips when the rooms-bucket dataset
// misses but the generic apartment dataset has a row. We exercise the
// flag with a commune that exists in the rare-fallback path; if no
// commune in metropolitan FR triggers it, the test is still useful
// as a no-op happy-path check.
func TestQuery_FallbackToGeneric(t *testing.T) {
	t.Parallel()
	idx, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Find a commune that's in TypologyApartment but missing from
	// TypologyApt12.
	var foundINSEE string
	apt12 := idx.byTypology[TypologyApt12]
	for insee := range idx.byTypology[TypologyApartment] {
		if _, hit := apt12[insee]; !hit {
			foundINSEE = insee
			break
		}
	}
	if foundINSEE == "" {
		t.Skip("no commune triggers the fallback in the embedded dataset")
	}
	rooms := 2
	l := gazetteer.Listing{
		INSEE:        foundINSEE,
		PropertyType: gazetteer.PropertyApartment,
		Rooms:        &rooms,
	}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for fallback commune %s", foundINSEE)
	}
	if !res.Evidence.FallbackToGeneric {
		t.Errorf("FallbackToGeneric = false, want true for INSEE %s", foundINSEE)
	}
	if res.Typology != TypologyApartment {
		t.Errorf("Typology = %q, want %q after fallback", res.Typology, TypologyApartment)
	}
}

// TestQuery_UnsupportedPropertyType rejects land / commercial / unknown
// with ErrUnsupportedPropertyType.
func TestQuery_UnsupportedPropertyType(t *testing.T) {
	t.Parallel()
	cases := []gazetteer.PropertyType{
		gazetteer.PropertyLand,
		gazetteer.PropertyCommercial,
		gazetteer.PropertyUnknown,
	}
	for _, pt := range cases {
		t.Run(string(pt), func(t *testing.T) {
			l := gazetteer.Listing{INSEE: "75111", PropertyType: pt}
			_, err := Query(context.Background(), Options{}, l)
			if !errors.Is(err, gazetteer.ErrUnsupportedPropertyType) {
				t.Fatalf("err = %v, want ErrUnsupportedPropertyType", err)
			}
		})
	}
}

// TestQuery_InsufficientInputs rejects empty INSEE with ErrInsufficientInputs.
func TestQuery_InsufficientInputs(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{PropertyType: gazetteer.PropertyApartment}
	_, err := Query(context.Background(), Options{}, l)
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Fatalf("err = %v, want ErrInsufficientInputs", err)
	}
}

// TestQuery_EmptyResult returns IsEmpty() == true for a synthetic INSEE
// that's not in the dataset.
func TestQuery_EmptyResult(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{
		INSEE:        "99999",
		PropertyType: gazetteer.PropertyApartment,
	}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil {
		t.Fatalf("nil result, want non-nil with IsEmpty()=true")
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty() = false, want true for synthetic INSEE")
	}
	if res.Evidence.INSEE != "99999" {
		t.Errorf("Evidence.INSEE = %q, want 99999", res.Evidence.INSEE)
	}
}

// TestPickTypology pins the typology choice for every property_type +
// rooms combination.
func TestPickTypology(t *testing.T) {
	t.Parallel()
	cases := []struct {
		propertyType string
		rooms        int
		want         Typology
		wantOK       bool
	}{
		{"house", 4, TypologyHouse, true},
		{"maison", 4, TypologyHouse, true},
		{"apartment", 0, TypologyApartment, true},
		{"apartment", 1, TypologyApt12, true},
		{"apartment", 2, TypologyApt12, true},
		{"apartment", 3, TypologyApt3, true},
		{"apartment", 4, TypologyApt3, true},
		{"flat", 3, TypologyApt3, true},
		{"land", 0, "", false},
		{"", 0, "", false},
	}
	for _, c := range cases {
		got, ok := pickTypology(c.propertyType, c.rooms)
		if got != c.want || ok != c.wantOK {
			t.Errorf("pickTypology(%q, %d) = (%q, %v), want (%q, %v)",
				c.propertyType, c.rooms, got, ok, c.want, c.wantOK)
		}
	}
}

// TestClassifyConfidence checks the four bands.
func TestClassifyConfidence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		row  Row
		want string
	}{
		{Row{PredType: "commune", NbObsCommune: 100}, ConfidenceHigh},
		{Row{PredType: "commune", NbObsCommune: 30}, ConfidenceHigh},
		{Row{PredType: "commune", NbObsCommune: 29}, ConfidenceMedium},
		{Row{PredType: "commune", NbObsCommune: 10}, ConfidenceMedium},
		{Row{PredType: "commune", NbObsCommune: 9}, ConfidenceLow},
		{Row{PredType: "maille", NbObsCommune: 1000}, ConfidenceLow},
		{Row{PredType: "voisinage", NbObsCommune: 100}, ConfidenceLow},
	}
	for _, c := range cases {
		if got := classifyConfidence(c.row); got != c.want {
			t.Errorf("classifyConfidence(%+v) = %q, want %q", c.row, got, c.want)
		}
	}
}
