// Package chomage is a gazetteer.Source that returns the latest INSEE
// estimate of the local unemployment rate (taux de chômage localisé) for
// the zone d'emploi a commune belongs to, plus a short trend window.
//
// The signal matters for a rental investor because tension on the local
// labour market is a leading proxy for tenant solvency and turnover:
// communes embedded in a high-unemployment zone d'emploi systematically
// show longer rent recovery cycles and higher arrears risk, regardless
// of the dwelling's own characteristics.
//
// Granularity is the INSEE zone d'emploi 2020 (302 zones covering
// metropolitan France + DOM, excluding Mayotte and French Guiana per
// the source). Every commune maps to exactly one ZE via the embedded
// commune → ZE2020 crosswalk.
//
// The Source is fully offline: the merged crosswalk + quarterly rate
// table ships embedded under `data/`.
//
// Required Listing inputs:
//
//   - INSEE (5-digit commune code). The Source emits
//     gazetteer.ErrInsufficientInputs when missing.
//
// Property type is irrelevant — labour-market tension applies to the
// whole zone.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := chomage.NewSource(chomage.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "75056"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	r := data.(*chomage.Result)
//	if r.IsEmpty() {
//	    fmt.Println("commune missing from the ZE2020 crosswalk (DOM-COM oddities)")
//	    return
//	}
//	fmt.Printf("ZE %s — taux chômage %.1f%% (national %.1f%%, écart %+.1f pp)\n",
//	    r.ZELabel, r.RatePct, r.NationalRatePct, r.DeltaVsNationalPP)
package chomage

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh = "high"
	ConfidenceNone = ""
)

// TensionFlag is a coarse, peer-relative bucket derived from the
// commune's zone d'emploi rate against the national average:
//
//	tight   : rate ≤ national − 1.0 pp (low labour market tension)
//	balanced: rate within ± 1.0 pp of national
//	loose   : rate ≥ national + 1.0 pp (high labour market tension)
//	unknown : commune missing from the embedded crosswalk
//
// Informative only — never folded into a score by this Source.
type TensionFlag string

const (
	TensionUnknown  TensionFlag = "unknown"
	TensionTight    TensionFlag = "tight"
	TensionBalanced TensionFlag = "balanced"
	TensionLoose    TensionFlag = "loose"
)
