// Package filosofi is a gazetteer.Source for the INSEE Filosofi 2021
// per-commune income and minima-sociaux statistics, served from an
// embedded JSON snapshot.
//
// The Source needs the listing's INSEE; property type is irrelevant
// (the Filosofi profile applies to the whole commune). It returns a
// *Result carrying median household disposable income, minima-sociaux
// percentage and a coarse income-risk flag (low / medium / high /
// unknown).
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := filosofi.NewSource(filosofi.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "93066"})
//	if err != nil { log.Fatal(err) }
//	r := data.(*filosofi.Result)
//	if r.IsEmpty() {
//	    fmt.Println("commune absent from Filosofi (small / DOM-TOM / secret stat)")
//	    return
//	}
//	fmt.Printf("median revenue: %d €/an, minima sociaux: %.1f%%\n",
//	    r.MedianEUR, r.MinimaPct)
//	fmt.Printf("income risk bucket: %s\n", r.Flag)
package filosofi
