package dvfagg

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

// sanity bounds, identical to the validated screening methodology.
const (
	minSurface = 9.0
	maxSurface = 250.0
	minPPM     = 300.0
	maxPPM     = 25000.0
	smallLo    = 18.0 // T1–T2 band
	smallHi    = 55.0
)

// acc accumulates the €/m² of every kept sale for one commune.
type acc struct {
	dept  string
	all   []float64
	small []float64
}

// accumulate reads one (already-decompressed) geo-dvf CSV and appends the
// €/m² of every single-lot apartment Vente into m, keyed by INSEE. A
// mutation is kept only when it holds exactly one Appartement and no other
// built local (Maison / commercial) — this drops multi-lot sales whose
// valeur_fonciere covers several lots.
func accumulate(src io.Reader, m map[string]*acc) error {
	r := csv.NewReader(src)
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return fmt.Errorf("dvfagg: header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	need := []string{"id_mutation", "nature_mutation", "type_local", "valeur_fonciere", "surface_reelle_bati", "code_commune", "code_departement"}
	for _, n := range need {
		if _, ok := col[n]; !ok {
			return fmt.Errorf("dvfagg: missing column %q", n)
		}
	}
	// Group rows by mutation so we can apply the single-lot filter.
	type row struct{ tl, insee, dept, vf, surf string }
	muts := map[string][]row{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("dvfagg: row: %w", err)
		}
		if rec[col["nature_mutation"]] != "Vente" {
			continue
		}
		tl := rec[col["type_local"]]
		if tl != "Appartement" && tl != "Maison" && !strings.HasPrefix(tl, "Local") {
			continue // ignore Dépendance / Terrain etc.
		}
		id := rec[col["id_mutation"]]
		muts[id] = append(muts[id], row{
			tl: tl, insee: rec[col["code_commune"]], dept: rec[col["code_departement"]],
			vf: rec[col["valeur_fonciere"]], surf: rec[col["surface_reelle_bati"]],
		})
	}
	for _, rows := range muts {
		apts := 0
		var a row
		bad := false
		for _, x := range rows {
			switch {
			case x.tl == "Appartement":
				apts++
				a = x
			case x.tl == "Maison", strings.HasPrefix(x.tl, "Local"):
				bad = true
			}
		}
		if apts != 1 || bad {
			continue
		}
		vf, e1 := strconv.ParseFloat(a.vf, 64)
		surf, e2 := strconv.ParseFloat(a.surf, 64)
		if e1 != nil || e2 != nil || surf < minSurface || surf > maxSurface || vf <= 0 {
			continue
		}
		ppm := vf / surf
		if ppm < minPPM || ppm > maxPPM {
			continue
		}
		ins := strings.TrimSpace(a.insee)
		if ins == "" {
			continue
		}
		e := m[ins]
		if e == nil {
			e = &acc{dept: strings.TrimSpace(a.dept)}
			m[ins] = e
		}
		e.all = append(e.all, ppm)
		if surf >= smallLo && surf <= smallHi {
			e.small = append(e.small, ppm)
		}
	}
	return nil
}

// finalize turns the accumulators into per-commune Results (percentiles).
func finalize(m map[string]*acc) map[string]Result {
	out := make(map[string]Result, len(m))
	for ins, e := range m {
		sort.Float64s(e.all)
		sort.Float64s(e.small)
		r := Result{
			Dept:             e.dept,
			N:                len(e.all),
			NSmall:           len(e.small),
			PriceP25EURM2:    round2(percentile(e.all, 0.25)),
			PriceMedianEURM2: round2(percentile(e.all, 0.50)),
			PriceP75EURM2:    round2(percentile(e.all, 0.75)),
		}
		if len(e.small) > 0 {
			r.PriceMedianSmallEURM2 = round2(percentile(e.small, 0.50))
		}
		out[ins] = r
	}
	return out
}

// percentile uses linear interpolation on a pre-sorted slice (matches numpy
// 'linear' / the validated python). Empty ⇒ 0.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	k := p * float64(n-1)
	f := int(k)
	if f >= n-1 {
		return sorted[n-1]
	}
	return sorted[f] + (sorted[f+1]-sorted[f])*(k-float64(f))
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// Vintage: the 3 most recent full DVF years. Bump on refresh.
var years = []string{"2022", "2023", "2024"}

// depts lists every geo-dvf department file code (métropole + Corse + DOM).
var depts = buildDepts()

func buildDepts() []string {
	var d []string
	for i := 1; i <= 95; i++ {
		if i == 20 { // Corsica is 2A/2B in geo-dvf
			continue
		}
		d = append(d, fmt.Sprintf("%02d", i))
	}
	d = append(d, "2A", "2B", "971", "972", "973", "974", "976")
	return d
}

// rawName is the flat dataset-local name for a year × dept file.
// Must be a clean single path element (no slashes) to satisfy dataset.validName.
func rawName(year, dep string) string { return year + "_" + dep + ".csv.gz" }

func rawURL(year, dep string) string {
	return "https://files.data.gouv.fr/geo-dvf/latest/csv/" + year + "/departements/" + dep + ".csv.gz"
}

// rawFiles is the full dept×year input list declared on the dataset.Set.
func rawFiles() []dataset.File {
	out := make([]dataset.File, 0, len(years)*len(depts))
	for _, y := range years {
		for _, d := range depts {
			out = append(out, dataset.File{Name: rawName(y, d), URL: rawURL(y, d)})
		}
	}
	return out
}

// transform is the dataset.Transform: gunzip every raw file, aggregate, write CSV.
func transform(ctx context.Context, raw dataset.RawSet, dst io.Writer) error {
	names := make([]string, 0, len(years)*len(depts))
	for _, y := range years {
		for _, d := range depts {
			names = append(names, rawName(y, d))
		}
	}
	return transformFiles(ctx, raw, names, dst)
}

// transformFiles is the testable inner loop (decoupled from rawFiles()).
func transformFiles(ctx context.Context, raw dataset.RawSet, names []string, dst io.Writer) error {
	m := map[string]*acc{}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		rc, err := raw.Open(name)
		if err != nil {
			// A missing dept/year file is non-fatal (some DOM gaps).
			continue
		}
		gzr, err := gzip.NewReader(rc)
		if err != nil {
			_ = rc.Close()
			return fmt.Errorf("dvfagg: gunzip %s: %w", name, err)
		}
		err = accumulate(gzr, m)
		_ = gzr.Close()
		_ = rc.Close()
		if err != nil {
			return fmt.Errorf("dvfagg: %s: %w", name, err)
		}
	}
	rows := finalize(m)
	if len(rows) == 0 {
		return errors.New("dvfagg: transform produced no rows")
	}
	// deterministic order
	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	w := csv.NewWriter(dst)
	w.Comma = ';'
	if err := w.Write([]string{"INSEE_C", "DEP", "n", "p25", "p50", "p75", "n_small", "p50_small"}); err != nil {
		return err
	}
	for _, k := range keys {
		r := rows[k]
		if err := w.Write([]string{
			k, r.Dept, strconv.Itoa(r.N),
			f2(r.PriceP25EURM2), f2(r.PriceMedianEURM2), f2(r.PriceP75EURM2),
			strconv.Itoa(r.NSmall), f2(r.PriceMedianSmallEURM2),
		}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// f2 prints a float with no trailing zeros (point decimals), e.g. 2500 not 2500.00.
func f2(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// validate gates publication: the rebuilt CSV must parse and be non-empty.
func validate(r io.Reader) error {
	idx, err := parseCSV(r)
	if err != nil {
		return err
	}
	if len(idx.byINSEE) == 0 {
		return errors.New("dvfagg: validated artifact has no rows")
	}
	return nil
}
