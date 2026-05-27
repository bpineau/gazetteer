// Package cartofriches is a gazetteer.Source that aggregates the
// Cerema "Cartofriches" national inventory of friches (industrial,
// commercial and habitation brownfields) per commune.
//
// For a rental investor this Source carries a dual signal:
//
//   - High count → the commune has substantial vacant or derelict
//     stock that could pressure values OR conversely represent
//     regeneration upside (Action Logement / EPF / Denormandie
//     pipeline).
//   - Zero / low count → mature, stable urban tissue.
//
// The Source returns the per-commune count, breakdowns by site type
// (industriel, habitat, commercial, …) and by site status (avec
// projet, sans projet, reconverti), plus the cumulative unité
// foncière surface in m².
//
// The Source is fully offline: the aggregate ships embedded under
// `data/`.
//
// Required Listing inputs:
//
//   - INSEE (5-digit commune code). The Source emits
//     gazetteer.ErrInsufficientInputs when missing.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := cartofriches.NewSource(cartofriches.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "59350"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	r := data.(*cartofriches.Result)
//	if r.IsEmpty() {
//	    fmt.Println("no Cartofriches sites referenced for this commune")
//	    return
//	}
//	fmt.Printf("%d sites, %d m² total\n", r.SiteCount, r.TotalSurfaceM2)
//	for kind, n := range r.ByType {
//	    fmt.Printf("  %s: %d\n", kind, n)
//	}
package cartofriches
