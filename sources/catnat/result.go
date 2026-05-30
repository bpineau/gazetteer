package catnat

import "github.com/bpineau/gazetteer/appraisal"

// Confidence value returned in Result.Confidence. It reflects the authority of
// the RECORD (GASPAR is the exhaustive official register), not present-day
// exposure: a commune with a single 1985 decree still reads "high" because the
// historical fact is certain — recency is conveyed separately by RecentCount /
// Tier / LastEventYear.
const ConfidenceHigh = "high"

// Tier classifies recent CatNat frequency (decrees in the recent window).
const (
	TierRare     = "rare"     // 0 in the recent window
	TierModerate = "modéré"   // 1–3
	TierFrequent = "fréquent" // ≥ 4
)

// Canonical risk categories surfaced in Result.ByCategory and contributed to
// appraisal.HazardProfile. Stable snake_case identifiers — consumers translate
// labels.
const (
	CatInondation       = "inondation"
	CatSecheresse       = "secheresse"
	CatMouvementTerrain = "mouvement_terrain"
	CatTempete          = "tempete"
)

// Result is the typed payload returned by Source.Query: the commune's CatNat
// history.
type Result struct {
	// TotalArretes is the number of CatNat decrees the commune accumulated
	// since 1982 (every category).
	TotalArretes int `json:"total_arretes"`

	// RecentCount is the number of decrees whose event began in the recent
	// window (the last RecentWindowYears years up to the dataset vintage —
	// see Evidence.RefYear).
	RecentCount int `json:"recent_count"`

	// ByCategory maps the four investor-relevant categories to their decree
	// count (all-time). Categories with a zero count are omitted.
	ByCategory map[string]int `json:"by_category,omitempty"`

	// LastEventYear is the start year of the most recent recognised event.
	LastEventYear int `json:"last_event_year,omitempty"`

	// Tier is the recent-frequency tier (see Tier* constants).
	Tier string `json:"tier,omitempty"`

	// Confidence is "high" on a populated reading, empty otherwise.
	Confidence string `json:"confidence,omitempty"`

	// Evidence captures reproducibility metadata. Sidecar — not wire data.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query.
type Evidence struct {
	// INSEE is the commune code the lookup used (after PLM arrondissement fold).
	INSEE string `json:"insee,omitempty"`

	// RefYear is the dataset vintage the recent window is measured against (the
	// latest event year in the snapshot).
	RefYear int `json:"ref_year,omitempty"`

	// WindowYears is the width of the recent window (RecentWindowYears).
	WindowYears int `json:"window_years,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter: true when the commune has no
// recorded CatNat decree (unknown commune, or a genuinely unaffected one).
func (r *Result) IsEmpty() bool {
	return r == nil || r.TotalArretes == 0
}

// HazardReport satisfies appraisal.HazardReporter: the categories the commune
// has actually been recognised for become confirmed natural risks in the
// consolidated HazardProfile. Empty when the commune has no history.
//
// Semantics: these are HISTORICALLY-RECOGNISED risks (realised sinistralité),
// not the modelled present-day exposure georisques contributes. HazardProfile
// takes the set union of both, but keeps per-source provenance in HazardInput,
// so a consumer can still tell which source confirmed a given category.
func (r *Result) HazardReport() appraisal.HazardReport {
	if r == nil || r.TotalArretes == 0 {
		return appraisal.HazardReport{}
	}
	var risks []string
	for _, cat := range []string{CatInondation, CatSecheresse, CatMouvementTerrain, CatTempete} {
		if r.ByCategory[cat] > 0 {
			risks = append(risks, cat)
		}
	}
	return appraisal.HazardReport{
		NaturalRisks: risks,
		Confidence:   appraisal.ConfidenceHigh,
	}
}

// tierFor maps a recent-window decree count to its frequency tier. The cutoffs
// are deliberately coarse: in a 10-year window, ≥ 4 recognised events marks a
// commune that is repeatedly hit (a standing insurer concern), 1–3 is the
// common "occasionally affected" case, and 0 is quiet.
func tierFor(recent int) string {
	switch {
	case recent >= 4:
		return TierFrequent
	case recent >= 1:
		return TierModerate
	default:
		return TierRare
	}
}
