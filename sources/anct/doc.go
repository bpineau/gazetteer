// Package anct is a gazetteer.Source that flags whether a commune
// participates in one of France's territorial revitalization
// programmes run by the ANCT (Agence Nationale de la Cohésion des
// Territoires):
//
//   - Action Cœur de Ville (ACV) — 245 medium-sized cities with State
//     support for downtown revitalisation.
//   - Petites Villes de Demain (PVD) — ~1 600 small communes (< 20 000
//     hab) with central functions, supported on the same model.
//   - Opération de Revitalisation de Territoire (ORT) — the legal
//     wrapper enabling several derogations, notably the Denormandie
//     tax device on renovated rentals.
//
// For a rental investor the signal matters because ORT-signing
// communes unlock the Denormandie tax device; ACV / PVD communes
// typically attract State-funded urbanism projects (better
// medium-term value trajectory).
//
// The Source is fully offline: the merged extract ships embedded
// under `data/`.
//
// Required Listing inputs:
//
//   - INSEE (5-digit commune code). The Source emits
//     gazetteer.ErrInsufficientInputs when missing.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := anct.NewSource(anct.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "26362"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	r := data.(*anct.Result)
//	if r.IsEmpty() {
//	    fmt.Println("commune participates in no ANCT programme")
//	    return
//	}
//	fmt.Printf("ACV=%v PVD=%v ORT=%v Denormandie=%v\n",
//	    r.ACV, r.PVD, r.ORT, r.DenormandieEligible)
//	for _, p := range r.Programmes {
//	    fmt.Println("-", p)
//	}
package anct
