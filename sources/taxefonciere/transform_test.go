package taxefonciere

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"testing"
)

// gunzip decodes the gzipped-JSON artifact the transforms now emit.
func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip read: %v", err)
	}
	return out
}

// closeEnough reports whether two rate/ratio values agree within float
// noise. Per-commune medians are stored unrounded, so the even-count mean
// (e.g. (2.44+2.67)/2 = 2.5549999999999997) must be compared with a
// tolerance even though it serializes to "2.555".
func closeEnough(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// fixtureRawSet serves a single named file from testdata, implementing
// dataset.RawSet for the transform under test.
type fixtureRawSet struct{ path string }

func (f fixtureRawSet) Open(string) (io.ReadCloser, error) { return os.Open(f.path) }

func decodeV1(t *testing.T, b []byte) V1Index {
	t.Helper()
	var idx V1Index
	if err := json.Unmarshal(gunzip(t, b), &idx); err != nil {
		t.Fatalf("decode v1: %v", err)
	}
	return idx
}

func decodeV2(t *testing.T, b []byte) V2Index {
	t.Helper()
	var idx V2Index
	if err := json.Unmarshal(gunzip(t, b), &idx); err != nil {
		t.Fatalf("decode v2: %v", err)
	}
	return idx
}

func TestTransformV1_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transformV1(context.Background(), fixtureRawSet{"testdata/taxe_fonciere_tarifs_sample.json"}, &buf); err != nil {
		t.Fatalf("transformV1: %v", err)
	}
	if err := validateV1(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validateV1: %v", err)
	}
	idx := decodeV1(t, buf.Bytes())

	// Per-commune value = median vl_au_m2 across the commune's rows. INSEE is
	// departement (zero-padded; "2A"/DOM kept) + zero-padded code_commune.
	// The row with a null vl_au_m2 (62999) must be skipped.
	wantCommunes := map[string]float64{
		"62426":  2.555, // median(2.67,2.44,1.37,2.74) = (2.44+2.67)/2
		"62428":  3.0,   // median(2.0,3.0,4.0)
		"01004":  2.13,  // dept "1" -> "01"; single row
		"2A008":  2.515, // Corsica: median(2.29,2.74)
		"971101": 3.5,   // DOM: dept "971" + "101"
	}
	if len(idx.Communes) != len(wantCommunes) {
		t.Fatalf("communes = %d, want %d (null-vl row must be skipped)", len(idx.Communes), len(wantCommunes))
	}
	for insee, want := range wantCommunes {
		if got := idx.Communes[insee]; !closeEnough(got, want) {
			t.Errorf("commune %s = %v, want %v", insee, got, want)
		}
	}

	// dept_fallback = median of commune medians, rounded to 3 decimals
	// (round-half-to-even). Corsica groups under the 3-char prefix, DOM folds
	// into "97".
	wantDept := map[string]float64{
		"62":  2.777, // median(2.555,3.0) = 2.7775 -> 2.777 (half-to-even)
		"01":  2.13,
		"2A0": 2.515,
		"97":  3.5,
	}
	if len(idx.DeptFallback) != len(wantDept) {
		t.Fatalf("dept_fallback = %d, want %d", len(idx.DeptFallback), len(wantDept))
	}
	for dept, want := range wantDept {
		if got := idx.DeptFallback[dept]; got != want {
			t.Errorf("dept_fallback %s = %v, want %v", dept, got, want)
		}
	}

	if idx.Meta.Source != v1MetaSource {
		t.Errorf("meta.source = %q, want %q", idx.Meta.Source, v1MetaSource)
	}
	if idx.Meta.Unit != v1MetaUnit {
		t.Errorf("meta.unit = %q, want %q", idx.Meta.Unit, v1MetaUnit)
	}
}

func TestTransformV2_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transformV2(context.Background(), fixtureRawSet{"testdata/fiscalite_locale_sample.json"}, &buf); err != nil {
		t.Fatalf("transformV2: %v", err)
	}
	if err := validateV2(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validateV2: %v", err)
	}
	idx := decodeV2(t, buf.Bytes())

	// Per-commune: tfpb_pct = taux_global_tfb (dropped when > 100),
	// teom_pct = taux_plein_teom (omitted when null or zero).
	wantCommunes := map[string]V2Entry{
		"12003": {TFPBPct: 38.64},                // teom null -> omitted
		"12007": {TFPBPct: 37.2, TEOMPct: 12.0},  //
		"13055": {TFPBPct: 47.87, TEOMPct: 18.1}, // Marseille commune-mère
		"13046": {TFPBPct: 46.56, TEOMPct: 14.0}, //
		"11360": {TEOMPct: 17.1},                 // tfpb 101.13 > 100 -> dropped
		"33000": {TFPBPct: 40.0},                 // teom 0 -> omitted
		"97411": {TFPBPct: 30.0, TEOMPct: 9.0},   // DOM
	}
	for insee, want := range wantCommunes {
		if got := idx.Communes[insee]; got != want {
			t.Errorf("commune %s = %+v, want %+v", insee, got, want)
		}
	}

	// Arrondissement aliasing: 13201..13216 inherit the Marseille mère
	// (13055) entry. The empty-INSEE row must be skipped. Total = 7 real +
	// 16 Marseille arrondissements = 23 (Lyon/Paris mères absent here).
	if got := idx.Communes["13201"]; got != (V2Entry{TFPBPct: 47.87, TEOMPct: 18.1}) {
		t.Errorf("arrondissement 13201 = %+v, want commune-mère entry", got)
	}
	if got := idx.Communes["13216"]; got != (V2Entry{TFPBPct: 47.87, TEOMPct: 18.1}) {
		t.Errorf("arrondissement 13216 = %+v, want commune-mère entry", got)
	}
	if _, ok := idx.Communes[""]; ok {
		t.Error("empty-INSEE row must be skipped")
	}
	if len(idx.Communes) != 23 {
		t.Fatalf("communes = %d, want 23 (7 real + 16 Marseille arrondissements)", len(idx.Communes))
	}

	// dept_fallback = per-dept median of each rate, rounded to 2 decimals.
	// Arrondissement aliases are excluded from the median (so dept 13 is the
	// median of 13055 + 13046 only, not weighted by the 16 aliases).
	wantDept := map[string]V2Entry{
		"12":  {TFPBPct: 37.92, TEOMPct: 12.0},  // median(38.64,37.2) ; teom median(12.0)
		"13":  {TFPBPct: 47.22, TEOMPct: 16.05}, // median(47.87,46.56) ; median(18.1,14.0)
		"11":  {TEOMPct: 17.1},                  // tfpb dropped -> no tfpb median
		"33":  {TFPBPct: 40.0},                  // teom omitted -> no teom median
		"974": {TFPBPct: 30.0, TEOMPct: 9.0},    // DOM 3-char dept key
	}
	if len(idx.DeptFallback) != len(wantDept) {
		t.Fatalf("dept_fallback = %d, want %d", len(idx.DeptFallback), len(wantDept))
	}
	for dept, want := range wantDept {
		if got := idx.DeptFallback[dept]; got != want {
			t.Errorf("dept_fallback %s = %+v, want %+v", dept, got, want)
		}
	}

	// applyV2Defaults backstops the VLC meta fields.
	if idx.Meta.VLCTariffEURPerM2 != 90.0 {
		t.Errorf("meta.vlc_tariff = %v, want 90.0", idx.Meta.VLCTariffEURPerM2)
	}
	if idx.Meta.VLCAbattement != 0.5 {
		t.Errorf("meta.vlc_abattement = %v, want 0.5", idx.Meta.VLCAbattement)
	}
	if idx.Meta.DataYear != v2Exercice {
		t.Errorf("meta.data_year = %d, want %d", idx.Meta.DataYear, v2Exercice)
	}
}
