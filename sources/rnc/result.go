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

// Parcelle is a cadastral parcel identifier in the canonical 14-character
// French form: INSEE(5) + préfixe(3) + section(2, right-justified) +
// numéro(4), e.g. "75056102AG0011". The accessors slice out the components;
// they return "" when the string is shorter than the canonical width.
//
// This is the key on which a robust building-level join is built (against DVF,
// the cadastre source, or an auction fiche's parsed parcelles) — far more
// reliable than the geo+street match this Source performs internally.
type Parcelle string

// INSEE returns the 5-digit commune code component.
func (p Parcelle) INSEE() string { return p.slice(0, 5) }

// Prefixe returns the 3-character préfixe (usually "000", non-zero for the
// PLM arrondissements and merged communes).
func (p Parcelle) Prefixe() string { return p.slice(5, 8) }

// Section returns the 2-character cadastral section (e.g. "AG").
func (p Parcelle) Section() string { return p.slice(8, 10) }

// Numero returns the 4-digit parcel number within the section.
func (p Parcelle) Numero() string { return p.slice(10, 14) }

func (p Parcelle) slice(a, b int) string {
	if len(p) < b {
		return ""
	}
	return string(p[a:b])
}

// Result is the copropriété context for one address.
//
// It carries NO hard distress verdict: the RNC open-data export redacts the
// financial declarations and the legal-procedure/arrêté columns. Attention is
// a low-confidence triage hint derived from the governance + structural fields
// that ARE published, never a verdict.
type Result struct {
	// Immatriculation is the RNC registration number (e.g. "AA2099810").
	Immatriculation string `json:"immatriculation,omitempty"`
	// NomUsage is the copropriété's display name, when declared.
	NomUsage string `json:"nom_usage,omitempty"`

	// Attention is true when at least one Signal is present. It is a TRIAGE
	// HINT ("worth checking the CCV / the annuaire before bidding"), NOT a
	// verdict — a consumer (e.g. locador) can surface it per address the way
	// it surfaces a rotten-zone flag.
	Attention bool `json:"attention"`
	// Signals lists the reasons behind Attention (stable snake_case keys):
	// "no_active_mandate", "syndic_unknown", "syndic_benevole",
	// "copro_aidee", "fragile_profile". See amberSignals for the exact rule
	// behind each; the syndic_* signals fire only on a large copropriété.
	Signals []string `json:"signals,omitempty"`

	// Raw governance fields (verbatim, for display — do not over-interpret).
	TypeSyndic    string `json:"type_syndic,omitempty"`     // professionnel | bénévole | "" (non connu)
	MandatEnCours string `json:"mandat_en_cours,omitempty"` // verbatim status
	MandatFin     string `json:"mandat_fin,omitempty"`      // last declared mandate end date (ISO), may be in the future
	// CoproAidee reports an engaged ANAH subsidy since 2014. NOTE: since 2020
	// this also counts MaPrimeRénov' Copropriété (mass energy-renovation aid
	// open to healthy copropriétés), so it is no longer distress-specific —
	// treat it as a weak hint, not a difficulty marker.
	CoproAidee bool `json:"copro_aidee,omitempty"`

	// Cadastre lists the copropriété's cadastral parcelles (see Parcelle).
	// Empty when the RNC row declared none. Use it to verify or override the
	// Source's own geo+street match against an authoritative parcel key.
	Cadastre []Parcelle `json:"cadastre,omitempty"`

	// Public-intervention programme perimeters (context, not a verdict).
	CoproACV bool `json:"copro_acv,omitempty"` // Action cœur de ville
	CoproPVD bool `json:"copro_pvd,omitempty"` // Petites villes de demain
	CoproPDP bool `json:"copro_pdp,omitempty"` // "copro_dans_pdp" — the ANAH open-data notice does not expand PDP

	// Context.
	LotsTotal          int    `json:"lots_total,omitempty"`
	LotsHabitation     int    `json:"lots_habitation,omitempty"`
	LotsStationnement  int    `json:"lots_stationnement,omitempty"`
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
