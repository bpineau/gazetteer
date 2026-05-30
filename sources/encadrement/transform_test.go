package encadrement

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
)

// fixtureRawSet serves a single named file from testdata, implementing
// dataset.RawSet for the transform under test (the transforms each read a
// single raw input, so the requested name is ignored).
type fixtureRawSet struct{ path string }

func (f fixtureRawSet) Open(string) (io.ReadCloser, error) { return os.Open(f.path) }

func TestTransformParis_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transformParis(context.Background(), fixtureRawSet{"testdata/paris_sample.json"}, &buf); err != nil {
		t.Fatalf("transformParis: %v", err)
	}
	if err := validateParis(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validateParis: %v", err)
	}

	var rows []parisRow
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The fixture has 3 rows for parisYear (2025) and 2 for 2024; only the
	// 2025 rows survive the year filter, in upstream order.
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (year filter must drop non-%d)", len(rows), parisYear)
	}
	for _, r := range rows {
		if r.Annee != parisYear {
			t.Errorf("row annee = %d, want %d", r.Annee, parisYear)
		}
	}
	// First row mapping: meuble_txt → bool, ref/min/max → *_eur_m2.
	got := rows[0]
	want := parisRow{
		Annee: 2025, IDZone: 5, IDQuartier: 38, NomQuartier: "Porte-Saint-Denis",
		CodeGrandQuartier: 7511038, Piece: 2, Epoque: "1946-1970", Meuble: false,
		RefEURPerM2: 27.4, MinEURPerM2: 19.2, MaxEURPerM2: 32.9,
	}
	if got != want {
		t.Errorf("row[0] = %+v, want %+v", got, want)
	}
}

func TestTransformPlaineCommune_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transformPlaineCommune(context.Background(), fixtureRawSet{"testdata/plaine_commune_sample.json"}, &buf); err != nil {
		t.Fatalf("transformPlaineCommune: %v", err)
	}
	if err := validatePlaineCommune(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validatePlaineCommune: %v", err)
	}

	var rows []plaineCommuneRow
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 6 {
		t.Fatalf("rows = %d, want 6", len(rows))
	}
	// Row 0: a non-open-ended meublé apartment cell; French "23,4" → 23.4.
	r0 := rows[0]
	want0 := plaineCommuneRow{
		Zone: 310, Piece: 1, PieceOpenEnded: false, Epoque: "avant 1946",
		Meuble: true, Maison: false, RefEURPerM2: 23.4, MinEURPerM2: 16.4, MaxEURPerM2: 28.1,
	}
	if r0 != want0 {
		t.Errorf("row[0] = %+v, want %+v", r0, want0)
	}
	// Row 1: the "4 et plus" label must map to Piece=4 / open-ended.
	r1 := rows[1]
	if r1.Piece != 4 || !r1.PieceOpenEnded {
		t.Errorf("row[1] piece/open-ended = %d/%v, want 4/true", r1.Piece, r1.PieceOpenEnded)
	}
	if r1.RefEURPerM2 != 14.6 {
		t.Errorf("row[1] ref = %v, want 14.6", r1.RefEURPerM2)
	}
}

func TestTransformLyon_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transformLyon(context.Background(), fixtureRawSet{"testdata/lyon_sample.geojson"}, &buf); err != nil {
		t.Fatalf("transformLyon: %v", err)
	}
	if err := validateLyon(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validateLyon: %v", err)
	}

	var rows []lyonRow
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Two features × pieces {1,2,3} (open-ended dropped) × 5 époques × 2
	// meublé = 2 × 30 = 60 rows.
	if len(rows) != 60 {
		t.Fatalf("rows = %d, want 60 (open-ended bucket must be dropped)", len(rows))
	}
	// The open-ended "4 et plus" bucket (Piece would be 4) must be absent.
	for _, r := range rows {
		if r.Piece == 4 {
			t.Fatalf("found a piece=4 row; open-ended bucket should be dropped: %+v", r)
		}
	}
	// First feature is Villeurbanne IRIS 692660101 zone 4; first emitted cell
	// is piece 1 / 1946-1970 / meublé per upstream key order.
	r0 := rows[0]
	if r0.Insee != "69266" || r0.IRIS != "692660101" || r0.Zone != "4" || r0.Commune != "Villeurbanne" {
		t.Errorf("row[0] identity = %+v", r0)
	}
	if r0.Piece != 1 || r0.Epoque != "1946-1970" || !r0.Meuble {
		t.Errorf("row[0] cell = piece %d / %q / meuble %v, want 1 / 1946-1970 / true", r0.Piece, r0.Epoque, r0.Meuble)
	}
	if r0.RefEURPerM2 != 17.6 || r0.MinEURPerM2 == nil || *r0.MinEURPerM2 != 12.3 || r0.MaxEURPerM2 == nil || *r0.MaxEURPerM2 != 21.1 {
		t.Errorf("row[0] rates = ref %v min %v max %v, want 17.6/12.3/21.1", r0.RefEURPerM2, r0.MinEURPerM2, r0.MaxEURPerM2)
	}
}
