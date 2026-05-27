// Package dpedist is a gazetteer.Source that returns the distribution of
// energy-performance labels (étiquettes DPE A..G) across every DPE
// observed in the commune of a Listing. It hits ADEME's public
// "DPE Logements existants (depuis juillet 2021)" dataset via the
// data-fair `values_agg` endpoint and projects the per-class counts
// onto a typed Result.
//
// The signal matters for a rental investor because the share of
// "passoires thermiques" (classes F and G) anchors several legal
// constraints already in force or imminent:
//
//   - F-class rents have been frozen since 2022.
//   - G-class dwellings can no longer be rented since 2025 (excluded
//     from the legal "logement décent" definition).
//   - F-class will be excluded from January 2028.
//   - E-class will be excluded from January 2034.
//
// The per-commune ratio of F+G against the total catalog is a useful
// proxy for "how much of the commune's housing stock is about to
// become un-rentable" — a market with 25 % passoires has a very
// different supply trajectory than one with 5 %.
//
// Backend: HTTP GET against `data.ademe.fr` (data-fair `values_agg`,
// no auth, no quota observed at the time of writing).
//
// Required Listing inputs:
//
//   - INSEE (5-digit commune code). The Source emits
//     gazetteer.ErrInsufficientInputs when missing.
//
// Property type is irrelevant — DPE distribution applies to the
// whole commune.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := dpedist.NewSource(dpedist.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "01053"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	r := data.(*dpedist.Result)
//	if r.IsEmpty() {
//	    fmt.Println("no DPE issued in this commune since July 2021")
//	    return
//	}
//	fmt.Printf("DPE issued: %d (passoires F+G = %.1f%%)\n",
//	    r.NbTotal, r.PassoireSharePct)
//	for _, label := range dpedist.AllLabels {
//	    fmt.Printf("  %s: %d (%.1f%%)\n", label,
//	        r.Counts[label], r.Shares[label])
//	}
package dpedist

// DefaultBaseURL is the data-fair endpoint root for the ADEME DPE
// dataset's `values_agg` aggregation route. The Source appends the
// per-Listing query string at runtime.
const DefaultBaseURL = "https://data.ademe.fr/data-fair/api/v1/datasets/dpe03existant/values_agg"

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	// ConfidenceHigh : the upstream returned a populated aggregation,
	// with at least one DPE in the commune.
	ConfidenceHigh = "high"

	// ConfidenceLow : at least one DPE was returned but the sample is
	// thin (NbTotal < ThinSampleThreshold). The class shares may not
	// be representative of the actual housing stock.
	ConfidenceLow = "low"

	// ConfidenceNone : the API responded correctly but the commune
	// has zero DPE.
	ConfidenceNone = ""
)

// ThinSampleThreshold below which we downgrade Confidence to "low".
// 50 DPE was picked as a coarse but reasonable floor — below that
// number, a single passoire shifts the share by ≥ 2 percentage
// points which is large enough to mislead a quick-look UI.
const ThinSampleThreshold = 50

// Label is the seven-letter DPE class plus a sentinel "N" for
// not-evaluated rows the API occasionally emits.
type Label string

const (
	LabelA Label = "A"
	LabelB Label = "B"
	LabelC Label = "C"
	LabelD Label = "D"
	LabelE Label = "E"
	LabelF Label = "F"
	LabelG Label = "G"
	LabelN Label = "N" // "non évalué" — folded onto LabelN
)

// AllLabels enumerates the DPE classes in stable display order
// (best → worst, then the not-evaluated sentinel). Useful for
// renderers that want a deterministic iteration.
var AllLabels = []Label{
	LabelA, LabelB, LabelC, LabelD, LabelE, LabelF, LabelG, LabelN,
}
