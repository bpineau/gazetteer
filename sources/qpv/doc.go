// Package qpv is a gazetteer.Source that flags whether a commune
// hosts at least one Quartier Prioritaire de la Politique de la Ville
// (QPV) and lists those QPVs.
//
// QPV is the official zoning policy administered by the ANCT (decree
// 2023-1314, effective 1 January 2024) that designates the most
// disadvantaged urban neighbourhoods. The label gates several fiscal
// and social devices: rental investors targeting QPV-located stock
// face specific guardrails (Pinel restrictions, exonérations TFPB,
// ZFU exemptions) and a different tenant demographic.
//
// IMPORTANT: this Source operates at the commune granularity — it
// answers "does this commune contain QPVs?" but NOT "is this address
// in a QPV?". For address-level QPV membership, callers must hit the
// ANCT SIG Ville API (sig.ville.gouv.fr) which requires
// authentication.
//
// The Source is fully offline: the QPV → commune mapping ships
// embedded under `data/`.
//
// Required Listing inputs:
//
//   - INSEE (5-digit commune code). The Source emits
//     gazetteer.ErrInsufficientInputs when missing.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := qpv.NewSource(qpv.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "93066"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	r := data.(*qpv.Result)
//	if r.IsEmpty() {
//	    fmt.Println("commune hosts no QPV")
//	    return
//	}
//	fmt.Printf("%d QPV in this commune:\n", r.QPVCount)
//	for _, q := range r.QPVs {
//	    fmt.Printf("  %s — %s\n", q.Code, q.Label)
//	}
package qpv
