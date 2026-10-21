// Package encadrement is a gazetteer.Source for the French zoned
// rent caps ("encadrement des loyers") in force in Paris, the two
// Seine-Saint-Denis EPTs (Plaine Commune, Est Ensemble) and
// Lyon / Villeurbanne.
//
// The other territories a decree has placed under the same regime (Lille,
// Montpellier, Bordeaux, the Pays basque, Grenoble-Alpes Métropole) ship no
// grille, and an address there is NOT reported as unregulated: its Result
// carries no cap figure, ConfidenceNone, Regulation = RegulationNotCovered
// (RegulationPartialCommune for a commune the decree covers only in part),
// Territory and an Evidence.Note, and IsEmpty is false. Read Result.Regulation,
// never the zero caps alone, to decide whether a rent is free.
//
// The Source matches the listing to a zone:
//
//   - Paris by INSEE (75101..75120), or by zip (75001..75020, 75116) when
//     the listing carries no INSEE. The cap itself is set per quartier: a
//     Listing.IRIS of the arrondissement pins it (a Paris IRIS code embeds
//     the quartier number), and without one an arrondissement whose
//     quartiers carry different caps collapses across them at low
//     confidence
//   - Lyon / Villeurbanne by INSEE (69381..69389, 69266), or by zip
//     (69001..69009, 69100) likewise, zoned per IRIS the same way. A set
//     INSEE is authoritative: a contradicting zip never promotes a listing
//     into either perimeter
//   - Plaine Commune & Est Ensemble (18 communes du 93) by
//     point-in-polygon over an embedded zonage: the listing's
//     coordinates pick the exact sub-communal zone, with an
//     INSEE-commune fallback when coordinates are absent (a single-zone
//     commune resolves at medium confidence; a multi-zone one — like
//     Saint-Denis or Montreuil — collapses across its zones at low
//     confidence)
//
// then collapses the non-meublé cells of that zone (its maison cells for a
// house where the zone publishes them, the apartment ones otherwise) by median of
// LoyerRefMaxEURPerM2HC. Listing.Rooms and Listing.BuildYear each narrow the
// collapse to one bucket of the grille — the pièces bucket and the époque de
// construction — and an absent one spans every bucket on that axis rather
// than assuming a value. An absent Rooms additionally caps the Confidence at
// ConfidenceLow: the cap per m² falls by a fifth to a third from a studio to
// a four-room flat (38.00 to 29.50 EUR/m²/month in Paris 1er, 20.50 to 14.00
// in Lyon 3e), so a default would be a guess dressed as a lookup.
//
// The same selection over the zone's meublé cells gives the FURNISHED pair,
// LoyerRefMajMeubleEURPerM2HC and LoyerRefMeubleEURPerM2HC: every shipped
// grille publishes one meublé cell per unfurnished one, above it (Paris 2026
// adds 2.1 to 5.3 EUR/m²/month to the référence, by secteur and cell). A
// furnished letting is held to the meublé majoré, never to the unfurnished
// one, which would understate its legal ceiling.
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
//	fmt.Printf("majoré meublé         : %.2f €/m²/mois HC (furnished max)\n",
//	    r.LoyerRefMajMeubleEURPerM2HC)
package encadrement
