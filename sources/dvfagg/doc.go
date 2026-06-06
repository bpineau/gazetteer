// Package dvfagg is an offline, per-commune aggregate of DVF apartment
// sale prices (median €/m² + dispersion), embedded as a CSV and refreshed
// from the geo-dvf bulk files. It complements the live `dvf` source
// (per-address, 500 m radius) with a national commune-level lookup that
// needs no network at runtime.
//
//	idx, _ := dvfagg.Load(dataDir)
//	if r, ok := idx.Lookup("95268"); ok {
//	    fmt.Println(r.PriceMedianSmallEURM2) // €/m² for ~T2
//	}
package dvfagg
