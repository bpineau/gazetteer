package chomage

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"math"
	"testing"

	"github.com/xuri/excelize/v2"
)

// memRawSet serves named in-memory raw inputs, implementing dataset.RawSet
// for the transform under test.
type memRawSet map[string][]byte

func (m memRawSet) Open(name string) (io.ReadCloser, error) {
	b, ok := m[name]
	if !ok {
		return nil, errFixtureMissing(name)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

type errFixtureMissing string

func (e errFixtureMissing) Error() string { return "fixture missing: " + string(e) }

// buildRatesXLSX builds a tiny rates workbook: a couple of banner rows, the
// machine-header row (ZE2020, LIBZE2020, REG, LIBREG, then quarter
// columns), and one data row per ZE. quarters is oldest-first; rates is
// per-ZE aligned with quarters.
func buildRatesXLSX(t *testing.T, quarters []string, rates map[string][]float64) []byte {
	t.Helper()
	f := excelize.NewFile()
	idx, _ := f.NewSheet(ratesSheet)
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	setRow(t, f, ratesSheet, 1, []string{"Taux de chômage localisés — banner"})
	setRow(t, f, ratesSheet, 2, []string{"Source : Insee"})
	header := []string{"ZE2020", "LIBZE2020", "REG", "LIBREG"}
	header = append(header, quarters...)
	setRow(t, f, ratesSheet, 4, header)

	row := 5
	for ze, series := range rates {
		rec := []any{ze, "rates-libelle-" + ze, "00", "REGION"}
		for _, v := range series {
			rec = append(rec, v)
		}
		setRowAny(t, f, ratesSheet, row, rec)
		row++
	}
	return toBytes(t, f)
}

// buildAppartZIP builds a tiny appartenance workbook (COM +
// Zones_supra_communales sheets) and wraps it in a ZIP, mirroring the
// upstream layout.
func buildAppartZIP(t *testing.T, communes map[string]string, labels map[string]string) []byte {
	t.Helper()
	f := excelize.NewFile()
	comIdx, _ := f.NewSheet(comSheet)
	_, _ = f.NewSheet(supraSheet)
	f.SetActiveSheet(comIdx)
	_ = f.DeleteSheet("Sheet1")

	// COM: banner rows then header CODGEO LIBGEO DEP ... ZE2020 ...
	setRow(t, f, comSheet, 1, []string{"Table d'appartenance — banner"})
	setRow(t, f, comSheet, 4, []string{"CODGEO", "LIBGEO", "DEP", "ZE2020"})
	row := 5
	for insee, ze := range communes {
		setRow(t, f, comSheet, row, []string{insee, "com-" + insee, "00", ze})
		row++
	}

	// Zones_supra_communales: NIVGEO CODGEO LIBGEO NB_COM. Include a couple
	// of non-ZE2020 rows that must be ignored.
	setRow(t, f, supraSheet, 1, []string{"Zones supra — banner"})
	setRow(t, f, supraSheet, 4, []string{"NIVGEO", "CODGEO", "LIBGEO", "NB_COM"})
	srow := 5
	setRow(t, f, supraSheet, srow, []string{"DEP", "00", "Some departement", "1"})
	srow++
	for code, label := range labels {
		setRow(t, f, supraSheet, srow, []string{"ZE2020", code, label, "1"})
		srow++
	}
	setRow(t, f, supraSheet, srow, []string{"REG", "84", "Some region", "1"})

	xlsxBytes := toBytes(t, f)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("table-appartenance-geo-communes-2025.xlsx")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write(xlsxBytes); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func setRow(t *testing.T, f *excelize.File, sheet string, row int, vals []string) {
	t.Helper()
	anyVals := make([]any, len(vals))
	for i, v := range vals {
		anyVals[i] = v
	}
	setRowAny(t, f, sheet, row, anyVals)
}

func setRowAny(t *testing.T, f *excelize.File, sheet string, row int, vals []any) {
	t.Helper()
	cell, err := excelize.CoordinatesToCellName(1, row)
	if err != nil {
		t.Fatalf("coords: %v", err)
	}
	if err := f.SetSheetRow(sheet, cell, &vals); err != nil {
		t.Fatalf("set row: %v", err)
	}
}

func toBytes(t *testing.T, f *excelize.File) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	return buf.Bytes()
}

func TestTransform_Golden(t *testing.T) {
	t.Parallel()

	// keptQuarters trailing columns are retained; pad with extra leading
	// quarters that must be dropped, and end on latestQuarter.
	allQuarters := []string{"2020-T3", "2020-T4"}
	allQuarters = append(allQuarters, goldenQuarters()...)

	// Three ZEs. Rates for the kept window; the leading two quarters carry
	// throwaway values that must not survive the keep-last-N trim.
	zeA := pad(2, []float64{7.0, 7.2, 7.1, 6.9, 6.8, 6.7, 6.6, 6.5, 6.4, 6.3, 6.2, 6.1, 6.0, 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7})
	zeB := pad(2, []float64{9.0, 9.1, 9.2, 9.3, 9.4, 9.5, 9.6, 9.7, 9.8, 9.9, 10.0, 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8, 10.9})
	zeC := pad(2, []float64{8.0, 8.0, 8.0, 8.0, 8.0, 8.0, 8.0, 8.0, 8.0, 8.0, 8.0, 8.0, 8.0, 8.0, 8.0, 8.0, 8.0, 8.0, 8.0, 8.0})
	rates := map[string][]float64{"0001": zeA, "0002": zeB, "0003": zeC}

	// Labels come from the supra sheet — note the accented forms, distinct
	// from the rates file's "rates-libelle-*".
	labels := map[string]string{
		"0001": "Évry-Courcouronnes",
		"0002": "Alençon",
		"0003": "Zone Trois",
	}
	communes := map[string]string{
		"01001": "0001",
		"01002": "0001",
		"02001": "0002",
		"03001": "0003",
	}

	raw := memRawSet{
		ratesRawName:  buildRatesXLSX(t, allQuarters, rates),
		appartRawName: buildAppartZIP(t, communes, labels),
	}

	var buf bytes.Buffer
	if err := transform(context.Background(), raw, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}

	// Quarters: trimmed to the last keptQuarters, dropping the two leading
	// padding quarters.
	if len(idx.Quarters) != keptQuarters {
		t.Fatalf("quarters = %d, want %d", len(idx.Quarters), keptQuarters)
	}
	if idx.Quarters[0] != "2021-T1" || idx.Quarters[len(idx.Quarters)-1] != latestQuarter {
		t.Errorf("quarter window = %s..%s, want 2021-T1..%s", idx.Quarters[0], idx.Quarters[len(idx.Quarters)-1], latestQuarter)
	}

	// Per-ZE rate series: the kept window only (no padding leakage).
	wantSeries := map[string][]float64{
		"0001": zeA[2:], "0002": zeB[2:], "0003": zeC[2:],
	}
	for ze, want := range wantSeries {
		got, ok := idx.LookupZone(ze)
		if !ok {
			t.Fatalf("ze %s missing", ze)
		}
		if len(got.RatePct) != len(want) {
			t.Fatalf("ze %s series len = %d, want %d", ze, len(got.RatePct), len(want))
		}
		for i := range want {
			if math.Abs(got.RatePct[i]-want[i]) > 1e-9 {
				t.Errorf("ze %s [%d] = %v, want %v", ze, i, got.RatePct[i], want[i])
			}
		}
		if got.Label != labels[ze] {
			t.Errorf("ze %s label = %q, want %q (must come from supra sheet)", ze, got.Label, labels[ze])
		}
	}

	// Crosswalk.
	if idx.CommuneCount() != len(communes) {
		t.Errorf("commune count = %d, want %d", idx.CommuneCount(), len(communes))
	}
	for insee, ze := range communes {
		if got, ok := idx.LookupZE(insee); !ok || got != ze {
			t.Errorf("commune %s -> (%q,%v), want %q", insee, got, ok, ze)
		}
	}

	// Derived national series: per-quarter unweighted mean across the 3
	// ZEs, rounded to 2 dp.
	for q := 0; q < keptQuarters; q++ {
		mean := round2((zeA[2+q] + zeB[2+q] + zeC[2+q]) / 3.0)
		if math.Abs(idx.NationalRatePctSeries[q]-mean) > 1e-9 {
			t.Errorf("national[%d] = %v, want %v", q, idx.NationalRatePctSeries[q], mean)
		}
	}
	lastMean := idx.NationalRatePctSeries[keptQuarters-1]
	if math.Abs(idx.Meta.NationalRatePct-lastMean) > 1e-9 {
		t.Errorf("meta.NationalRatePct = %v, want last national series %v", idx.Meta.NationalRatePct, lastMean)
	}

	// Meta shape.
	if idx.Meta.Source != metaSource {
		t.Errorf("meta.Source = %q, want %q", idx.Meta.Source, metaSource)
	}
	if idx.Meta.SeriesStart != "2021-T1" || idx.Meta.SeriesEnd != latestQuarter {
		t.Errorf("meta series window = %s..%s", idx.Meta.SeriesStart, idx.Meta.SeriesEnd)
	}
	if idx.Meta.QuarterCount != keptQuarters {
		t.Errorf("meta.QuarterCount = %d, want %d", idx.Meta.QuarterCount, keptQuarters)
	}
	if idx.Meta.ZECount != len(rates) {
		t.Errorf("meta.ZECount = %d, want %d", idx.Meta.ZECount, len(rates))
	}
	if idx.Meta.CommuneCount != len(communes) {
		t.Errorf("meta.CommuneCount = %d, want %d", idx.Meta.CommuneCount, len(communes))
	}
}

func TestIsQuarter(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"2021-T1": true, "2003-T4": true, "2025-T2": true,
		"2021-T5": false, "2021-T0": false, "2021T1": false,
		"abcd-T1": false, "2021-Q1": false, "": false, "2021-T1 ": false,
	}
	for in, want := range cases {
		if got := isQuarter(in); got != want {
			t.Errorf("isQuarter(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseRate(t *testing.T) {
	t.Parallel()
	cases := map[string]float64{
		"7.4": 7.4, "7,4": 7.4, " 6.9 ": 6.9, "": 0, "n/a": 0,
	}
	for in, want := range cases {
		if got := parseRate(in); math.Abs(got-want) > 1e-9 {
			t.Errorf("parseRate(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNationalSeries_SkipsEmptyCells(t *testing.T) {
	t.Parallel()
	// A ZE quarter that is blank/garbled in the source parses to 0 (parseRate).
	// It must NOT enter the national mean as a real 0 % rate — no employment
	// zone has a 0 % unemployment rate, so 0 means "missing" and averaging it
	// in drags the national average down. Same class as the empty-source guard
	// in appraisal.RentValue: skip empty contributors before the mean.
	zes := map[string][]float64{
		"ZE-A": {8.0},
		"ZE-B": {6.0},
		"ZE-C": {0.0}, // blank/missing cell — must be skipped, not averaged as 0
	}
	got := nationalSeries(zes, 1)
	want := 7.0 // mean of 8 and 6, NOT (8+6+0)/3 = 4.67
	if len(got) != 1 || math.Abs(got[0]-want) > 1e-9 {
		t.Errorf("nationalSeries = %v, want [%v] (blank cell skipped, not averaged as 0)", got, want)
	}
}

// goldenQuarters returns 2021-T1 .. latestQuarter (keptQuarters labels).
func goldenQuarters() []string {
	out := make([]string, 0, keptQuarters)
	year := 2021
	q := 1
	for len(out) < keptQuarters {
		out = append(out, formatQuarter(year, q))
		q++
		if q > 4 {
			q = 1
			year++
		}
	}
	return out
}

func formatQuarter(year, q int) string {
	return itoa(year) + "-T" + itoa(q)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// pad prepends n zero quarters to series (throwaway leading values the
// keep-last-N trim must discard).
func pad(n int, series []float64) []float64 {
	out := make([]float64, 0, n+len(series))
	for i := 0; i < n; i++ {
		out = append(out, 99.0)
	}
	return append(out, series...)
}
