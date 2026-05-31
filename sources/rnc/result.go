package rnc

// Confidence levels for the copro↔listing match.
const (
	// ConfidenceHigh: geo distance ≤ geoHighMeters AND the normalized
	// street matches.
	ConfidenceHigh = "high"
	// ConfidenceMedium: geo distance ≤ geoMaxMeters (street agreed or
	// absent).
	ConfidenceMedium = "medium"
	// ConfidenceLow: no coordinates; a single street candidate matched
	// within the commune.
	ConfidenceLow = "low"
	// ConfidenceNone: no match.
	ConfidenceNone = ""
)

// MatchMethod records how the copro was located (debugging + trust).
type MatchMethod string

const (
	MatchGeoVoie MatchMethod = "geo_voie"
	MatchVoie    MatchMethod = "voie"
	MatchNone    MatchMethod = ""
)

// Result is the copropriété context for one address.
//
// It carries NO hard distress verdict: the public RNC file omits the
// procedure/arrêté columns. Attention is a low-confidence triage hint
// derived from the weak governance fields, never a verdict.
type Result struct {
	// Immatriculation is the RNC registration number (e.g. "AA2099810").
	Immatriculation string `json:"immatriculation,omitempty"`
	// NomUsage is the copropriété's display name, when declared.
	NomUsage string `json:"nom_usage,omitempty"`

	// Attention is true when at least one governance Signal is present.
	// It is a TRIAGE HINT ("worth checking the CCV"), NOT a verdict.
	Attention bool `json:"attention"`
	// Signals lists the reasons behind Attention (stable snake_case keys):
	// "no_active_mandate", "syndic_unknown", "syndic_benevole",
	// "copro_aidee".
	Signals []string `json:"signals,omitempty"`

	// Raw governance fields (verbatim, for display — do not over-interpret).
	TypeSyndic    string `json:"type_syndic,omitempty"`     // professionnel | bénévole | "" (non connu)
	MandatEnCours string `json:"mandat_en_cours,omitempty"` // verbatim status
	CoproAidee    bool   `json:"copro_aidee,omitempty"`     // ANAH-subsidized since 2014

	// Context.
	LotsTotal          int    `json:"lots_total,omitempty"`
	LotsHabitation     int    `json:"lots_habitation,omitempty"`
	ConstructionPeriod string `json:"construction_period,omitempty"` // e.g. "AVANT_1949"
	SyndicatCooperatif bool   `json:"syndicat_cooperatif,omitempty"`
	ResidenceService   bool   `json:"residence_service,omitempty"`
	QPVCode            string `json:"qpv_code,omitempty"`
	QPVName            string `json:"qpv_name,omitempty"`

	// WebURL is the public RNC reference for this copro. A stable per-copro
	// public deep-link could not be confirmed, so this currently points at
	// the data.gouv dataset page.
	WebURL string `json:"web_url,omitempty"`

	// MatchMethod / Confidence describe how reliably the copro was tied to
	// the queried Listing.
	MatchMethod MatchMethod `json:"match_method,omitempty"`
	Confidence  string      `json:"confidence"`

	// Evidence is the reproducibility sidecar (json:"-").
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata for one query.
type Evidence struct {
	INSEE         string  `json:"insee,omitempty"`
	QueryLat      float64 `json:"query_lat,omitempty"`
	QueryLon      float64 `json:"query_lon,omitempty"`
	MatchDistance float64 `json:"match_distance_m,omitempty"`
	VoieQuery     string  `json:"voie_query,omitempty"`
	VoieMatched   string  `json:"voie_matched,omitempty"`
	RowCount      int     `json:"row_count,omitempty"`
	DataVintage   string  `json:"data_vintage,omitempty"`
}

// IsEmpty reports the "ran fine, no copro matched" sentinel.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.Confidence == ConfidenceNone
}
