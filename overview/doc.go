// Package overview is the batch / commune-level counterpart to the per-address
// gazetteer flow. Where gazetteer.Client.Collect answers "tell me everything
// about this address", overview answers "give me one row per commune" — for
// screening or ranking communes at scale.
//
// Build joins the embedded, commune-keyed sources offline (no network I/O) into
// one CommuneOverview per commune that has DVF price data, merging price
// (dvfagg), market rent (carteloyers), the encadrement cap, income (filosofi),
// vacancy, taxe foncière, QPV, zonage ABC, zone tendue, distance-to-Paris and
// nearby transit lines:
//
//	rows, err := overview.Build(overview.Options{Depts: []string{"75", "93"}})
//	if err != nil { /* handle */ }
//	for _, c := range rows {
//	    fmt.Printf("%s %s: %.0f €/m², loyer %.1f €/m² HC\n",
//	        c.INSEE, c.Name, c.PriceMedianEURM2, c.RentMarketEURM2HC)
//	}
//
// An empty Options.Depts covers every commune nationally. Missing data for a
// given source degrades to a zero / nil field rather than failing the row.
//
// Build sits on the per-source batch-read helpers (dvfagg.Load().Codes()/
// Lookup(), qpv.Load().HasQPV(), delinquance.Load().Level(), communes.All(),
// carteloyers Row.HCEURPerM2()); reach for those directly when you need a single
// dimension across many communes.
package overview
