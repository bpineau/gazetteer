// Package dvfagg is an offline, per-commune aggregate of DVF apartment
// sale prices (median €/m² + dispersion), embedded as a CSV and refreshed
// from the geo-dvf bulk files. It complements the live `dvf` source
// (per-address, 500 m radius) with a national commune-level lookup that
// needs no network at runtime.
//
// The two read the SAME dataset through different cohorts, so their
// plausibility bounds differ on purpose: dvfagg keeps apartments only
// (9-250 m², 300-25 000 EUR/m²), dvf every built local (9-1 000 m²,
// 100-50 000 EUR/m²). See transform.go for what each bound does to the
// shipped aggregate. Do not compare a dvfagg median with a dvf median
// as if they measured the same population.
//
//	idx, _ := dvfagg.Load(dataDir)
//	if r, ok := idx.Lookup("95268"); ok {
//	    fmt.Println(r.PriceMedianSmallEURM2) // €/m² for ~T2
//	}
package dvfagg
