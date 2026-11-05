// Package taxefonciere is a gazetteer.Source that estimates the
// annual taxe foncière (FR property tax) from offline DGFiP datasets.
//
// The Source needs Listing.INSEE and Listing.SurfaceM2. It tries the
// V2 path (DGFiP voted TFPB/TEOM rates × VLC tariff × surface ×
// abattement): a commune hit yields ConfidenceHigh, a département fallback
// ConfidenceMedium. When V2 has no signal at all, the V1 dataset still
// gives the commune's per-m² valeur locative (VLEURPerM2), a tax base
// without a rate, so the Result carries no estimate (IsEmpty).
//
// Property type is not consulted — the TF estimate applies to the
// habitable surface regardless of apartment vs house. Callers that
// care can filter via Listing.PropertyType themselves.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := taxefonciere.NewSource(taxefonciere.Options{})
//	surface := 50.0
//	data, err := src.Query(ctx, gazetteer.Listing{
//	    INSEE:     "75101",
//	    SurfaceM2: &surface,
//	})
//	if err != nil { log.Fatal(err) }
//	r := data.(*taxefonciere.Result)
//	if r.IsEmpty() {
//	    fmt.Println("no taxe-foncière estimate for this commune")
//	    return
//	}
//	fmt.Printf("estimated TF: %.0f €/an (TFPB %.2f%% + TEOM %.2f%%)\n",
//	    r.EstimatedEURPerYear, r.TauxTFPBApplied, r.TauxTEOMApplied)
//	if r.TEOMEURPerYear > 0 {
//	    fmt.Printf("dont %.0f €/an récupérables auprès du locataire\n",
//	        r.TEOMEURPerYear)
//	}
package taxefonciere
