// Package encadrement is a gazetteer.Source for the French zoned
// rent caps ("encadrement des loyers") in force in Paris, the two
// Seine-Saint-Denis EPTs (Plaine Commune, Est Ensemble) and
// Lyon / Villeurbanne.
//
// The Source matches the listing to a zone:
//
//   - Paris by zip (75001..75020, 75116) or INSEE (75101..75120)
//   - Lyon / Villeurbanne by INSEE (69381..69389, 69266) or zip
//     (69001..69009, 69100)
//   - Plaine Commune & Est Ensemble (18 communes du 93) by
//     point-in-polygon over an embedded zonage: the listing's
//     coordinates pick the exact sub-communal zone, with an
//     INSEE-commune fallback when coordinates are absent (a single-zone
//     commune resolves at medium confidence; a multi-zone one — like
//     Saint-Denis or Montreuil — collapses across its zones at low
//     confidence)
//
// then collapses the non-meublé, non-maison cells of that zone by median of
// LoyerRefMaxEURPerM2HC. Listing.Rooms and Listing.BuildYear each narrow the
// collapse to one bucket of the grille — the pièces bucket and the époque de
// construction — and an absent one spans every bucket on that axis rather
// than assuming a value. An absent Rooms additionally caps the Confidence at
// ConfidenceLow: the cap per m² falls by about a third from a studio to a
// four-room flat, so a default would be a guess dressed as a lookup.
//
// The *Result satisfies appraisal.RentEstimator with Bracket populated, so
// consumers can label the rent as a "loyer de référence" rather than a market
// estimate.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := encadrement.NewSource(encadrement.Options{})
//	rooms, built := 3, 1935
//	data, err := src.Query(ctx, gazetteer.Listing{
//	    Zip:          "75001",
//	    PropertyType: gazetteer.PropertyApartment,
//	    Rooms:        &rooms,
//	    BuildYear:    &built,
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
