// Package pinel ships an offline Source that maps a commune INSEE to
// its housing-tension classification (zonage ABC : Abis, A, B1, B2, C).
// The zoning drives eligibility for a wide range of French rental
// investment tax devices (Pinel, Loc'Avantages, Denormandie, PTZ, PAS,
// Action Logement, etc.). For a rental investor this is a first-order
// signal: only zones A/Abis/B1 are typically Pinel-eligible, and the
// zone gates whether rent-cap regimes apply.
//
// The dataset ships embedded under `data/zonage_abc_communes.csv`:
// every metropolitan + DOM commune is listed with its A/Abis/B1/B2/C
// code. Lookup is by 5-digit INSEE.
//
// The Source is fully offline — no network, no auth.
package pinel

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh = "high"
	ConfidenceNone = ""
)

// Zone is the ABC-zone label assigned by the Ministère du Logement to
// every French commune. The labels are stable strings; downstream
// consumers may match on them without importing this package.
type Zone string

const (
	// ZoneUnknown signals the commune is missing from the dataset —
	// rare in practice (the dataset covers ~35 000 communes including
	// DOM).
	ZoneUnknown Zone = ""

	// ZoneAbis is the most tension zone (Paris and inner ring).
	ZoneAbis Zone = "Abis"

	// ZoneA covers the rest of the high-tension Île-de-France ring,
	// Côte d'Azur, Geneva border, and the largest metropolitan areas
	// (Lyon, Marseille, Lille, …).
	ZoneA Zone = "A"

	// ZoneB1 covers medium-tension agglomérations of 250 000+
	// inhabitants and select tourism / border communes.
	ZoneB1 Zone = "B1"

	// ZoneB2 covers low-tension communes formerly Pinel-eligible by
	// préfectoral derogation; new Pinel investments excluded since 2018.
	ZoneB2 Zone = "B2"

	// ZoneC covers the rest of the territory — typically rural — and
	// is excluded from Pinel.
	ZoneC Zone = "C"
)

// Result is the typed payload returned by Source.Query.
type Result struct {
	// Zone is the ABC classification ("Abis", "A", "B1", "B2", "C").
	// Empty when the commune was not found in the embedded dataset.
	Zone Zone `json:"zone,omitempty"`

	// PinelEligible is true for zones A, Abis and B1 — the zones in
	// which the Pinel device accepts new investments under the current
	// regime. (Zone B2 properties are eligible only under a transitory
	// préfectoral agreement, which this Source does not track.)
	PinelEligible bool `json:"pinel_eligible"`

	// TensionLabel is a coarse human-friendly bucket derived from
	// Zone: "very_high" (Abis), "high" (A), "medium" (B1),
	// "low" (B2) or "minimal" (C). Empty when Zone is unknown.
	TensionLabel string `json:"tension_label,omitempty"`

	// Confidence is "high" when the commune was found in the dataset,
	// ConfidenceNone otherwise.
	Confidence string `json:"confidence"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result. Not part of the wire data (json:"-") —
	// populated by Source.Query, consumed in-process by callers that
	// need to log or audit how the answer was derived.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query that
// produced a Result.
//
// Sidecar — not part of the wire data. Travels in-process from
// Source.Query to the adapter.
type Evidence struct {
	// INSEE is the 5-digit commune code the Source filtered on. Drawn
	// from Listing.INSEE (mandatory).
	INSEE string `json:"insee"`

	// CommuneLabel is the human-readable commune name from the source
	// file. Useful for logging / diagnostics; not always populated.
	CommuneLabel string `json:"commune_label,omitempty"`

	// RowCount is the total number of communes in the embedded
	// dataset. Sanity scalar for downstream renderers.
	RowCount int `json:"row_count,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// commune was not found in the dataset — the framework records
// Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.Zone == ZoneUnknown
}

// tensionFor maps a Zone to its coarse tension bucket.
func tensionFor(z Zone) string {
	switch z {
	case ZoneAbis:
		return "very_high"
	case ZoneA:
		return "high"
	case ZoneB1:
		return "medium"
	case ZoneB2:
		return "low"
	case ZoneC:
		return "minimal"
	default:
		return ""
	}
}

// pinelEligible returns true for zones the Pinel device accepts under
// the current (post-2018) regime: A, Abis and B1.
func pinelEligible(z Zone) bool {
	return z == ZoneA || z == ZoneAbis || z == ZoneB1
}
