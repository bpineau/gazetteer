package dvfagg

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
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
