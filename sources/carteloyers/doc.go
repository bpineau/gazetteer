// Package carteloyers is a gazetteer.Source for the ANIL / DHUP
// "carte des loyers" offline dataset — a per-commune × typology
// reference rent in EUR/m²/month.
//
// The Source picks a typology bucket (House, Apt 1-2 pieces, Apt 3+,
// generic Apt) from the listing's property type + rooms, looks it up
// in the embedded CSV index, and returns a *Result that satisfies
// appraisal.RentEstimator. When the rooms-bucket dataset misses on a
// commune the Source falls back to the generic apartment dataset and
// stamps Evidence.FallbackToGeneric = true.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := carteloyers.NewSource(carteloyers.Options{})
//	rooms := 3
//	data, err := src.Query(ctx, gazetteer.Listing{
//	    INSEE:        "75101",
//	    PropertyType: gazetteer.PropertyApartment,
//	    Rooms:        &rooms,
//	})
//	if err != nil { log.Fatal(err) }
//	r := data.(*carteloyers.Result)
//	if r.IsEmpty() {
//	    fmt.Println("no rent reading for this commune × typology")
//	    return
//	}
//	fmt.Printf("rent: %.2f €/m²/mois CC (range %.2f-%.2f, %d obs, %s)\n",
//	    r.LoyerMedEURPerM2CC,
//	    r.LoyerLowEURPerM2CC, r.LoyerHighEURPerM2CC,
//	    r.NbObservations, r.Confidence)
package carteloyers
