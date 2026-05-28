// Package vacance is a gazetteer.Source for the LOVAC 2025 per-commune
// housing vacancy dataset, served from an embedded CSV.
//
// The Source needs Listing.INSEE. It returns the commune-wide
// vacancy rate plus a long-term-vacancy split. Property type is not
// consulted (vacance is a commune-wide metric). Missing communes
// (secret statistique) surface as IsEmpty()==true.
//
// Disambiguation: this Source surfaces the FISCAL vacancy reading
// (LOVAC — the dataset Bercy uses to assess TLV / THRS). For the
// DEMOGRAPHIC vacancy rate from the INSEE census base logement, see
// sources/vacance_logements. The two signals are correlated but
// distinct.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := vacance.NewSource(vacance.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "75101"})
//	if err != nil { log.Fatal(err) }
//	r := data.(*vacance.Result)
//	if r.IsEmpty() {
//	    fmt.Println("commune absent from LOVAC (secret statistique)")
//	    return
//	}
//	fmt.Printf("vacancy rate: %.1f%% (long-term: %.1f%%)\n",
//	    r.VacancePct, r.VacanceLongPct)
package vacance
