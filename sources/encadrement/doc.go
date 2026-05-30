// Package encadrement is a gazetteer.Source for the French zoned
// rent caps ("encadrement des loyers") in force in Paris, the two
// Seine-Saint-Denis EPTs (Plaine Commune, Est Ensemble) and
// Lyon / Villeurbanne.
//
// The Source matches the listing to a zone:
//
//   - Paris by zip (75001..75020, 75116)
//   - Lyon / Villeurbanne by INSEE (69381..69389, 69266)
//   - Plaine Commune & Est Ensemble (18 communes du 93) by
//     point-in-polygon over an embedded zonage: the listing's
//     coordinates pick the exact sub-communal zone, with an
//     INSEE-commune fallback when coordinates are absent (a single-zone
//     commune resolves at medium confidence; a multi-zone one — like
//     Saint-Denis or Montreuil — collapses across its zones at low
//     confidence)
//
// then collapses the cells matching (pieces, non-meublé, non-maison)
// by median of LoyerRefMaxEURPerM2HC. The *Result satisfies
// appraisal.RentEstimator with Bracket populated, so consumers can
// label the rent as a "loyer de référence" rather than a market
// estimate.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := encadrement.NewSource(encadrement.Options{})
//	rooms, surface := 3, 50.0
//	data, err := src.Query(ctx, gazetteer.Listing{
//	    Zip:          "75001",
//	    PropertyType: gazetteer.PropertyApartment,
//	    Rooms:        &rooms,
//	    SurfaceM2:    &surface,
//	})
//	if err != nil { log.Fatal(err) }
//	r := data.(*encadrement.Result)
//	if r.IsEmpty() {
//	    fmt.Println("address falls outside any encadrement zone")
//	    return
//	}
//	fmt.Printf("zone %s (%s)\n", r.Zone, r.ZoneSource)
//	fmt.Printf("loyer de référence    : %.2f €/m²/mois HC\n",
//	    r.LoyerRefEURPerM2HC)
//	fmt.Printf("loyer de réf. majoré  : %.2f €/m²/mois HC (legal max)\n",
//	    r.LoyerRefMajEURPerM2HC)
package encadrement
