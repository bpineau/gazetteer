package georisques

import (
	"sort"

	"github.com/bpineau/gazetteer/appraisal"
)

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers (encheridor's adapter, dashboards) can match on
// them without importing this package's constants.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// SkipReason sentinels populated on empty (no-match) results. Stable
// wire contract — downstream consumers group on these values.
const (
	SkipReasonNoMatch = "no_match"
)

// LevelAddress / LevelCommune are the granularity tiers exposed via the
// canonical `level_used` field — symmetric with DVF / MA / Pappers /
// Castorus. LevelAddress means BRGM resolved the request down to the
// building's exact lat/lon (Adresse.Libelle populated, statutAdresse
// surfaceable). LevelCommune is the broader fallback when only the
// commune scope is available.
const (
	LevelAddress = "address"
	LevelCommune = "commune"
)

// Result is the typed payload returned by Source.Query. Mirrors the
// shape currently persisted by encheridor's Géorisques enricher
// (resultBlob with Address / Commune / Naturels / Technos / Summary
// sub-blobs) so the encheridor adapter can re-serialise it 1:1 into its
// EnrichPayload.Result.
//
// Envelope-only fields (schema_version, enricher_version, computed_at,
// input_hash) are NOT part of the gazetteer payload — those are the
// framework's responsibility (Result envelope in gazetteer.Result, or
// in encheridor's enrich.EnrichPayload).
type Result struct {
	// Address carries the BRGM-resolved address scope (libelle + lat/lon
	// echoed back by the API). Nil when BRGM downgraded to commune-scope.
	Address *Address `json:"address,omitempty"`

	// Commune carries the commune-scope identifiers (INSEE + code postal
	// + libelle). Nil when the report had no commune block (very rare —
	// happens on coords outside French territory).
	Commune *Commune `json:"commune,omitempty"`

	// ReportURL is the public georisques.gouv.fr permalink for the
	// address scope. Echoed verbatim from the API.
	ReportURL string `json:"report_url,omitempty"`

	// Naturels maps stable snake_case keys (cf. canonicalNaturels) to
	// per-risk RiskBlob. Always 12 entries on the happy path.
	Naturels map[string]RiskBlob `json:"naturels,omitempty"`

	// Technos maps stable snake_case keys (cf. canonicalTechnos) to
	// per-risk RiskBlob. Always 6 entries on the happy path.
	Technos map[string]RiskBlob `json:"technos,omitempty"`

	// Summary aggregates the per-risk maps into operator-actionable
	// counters + red flags.
	Summary Summary `json:"summary"`

	// Confidence is one of "high" / "medium" / "low" per the calibration
	// in BuildResult.
	Confidence string `json:"confidence"`

	// LevelUsed records the BRGM granularity tier ("address" or
	// "commune") that produced this Result.
	LevelUsed string `json:"level_used,omitempty"`

	// Skipped is true on the no-match sentinel result (today: only when
	// the report parsed as fully empty) so consumers can route the row
	// through their "skipped" path instead of trying to render absent
	// fields.
	Skipped bool `json:"skipped,omitempty"`

	// SkipReason is a stable identifier populated on skipped results
	// (see SkipReason* constants). Empty in the happy path.
	SkipReason string `json:"skip_reason,omitempty"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result. Not part of the wire data (json:"-") —
	// populated by Source.Query, consumed in-process by callers that
	// need to log or audit how the answer was derived (e.g.
	// encheridor's EnrichPayload.Method.Params).
	Evidence Evidence `json:"-"`
}

// Address mirrors the `adresse` block of the BRGM rapport. Fields are
// populated only when BRGM resolved the request to the building's exact
// position; on commune-tier fallback the whole struct is omitted.
type Address struct {
	Libelle string  `json:"libelle,omitempty"`
	Lat     float64 `json:"lat,omitempty"`
	Lon     float64 `json:"lon,omitempty"`
}

// Commune mirrors the `commune` block of the BRGM rapport.
type Commune struct {
	Insee      string `json:"insee,omitempty"`
	CodePostal string `json:"code_postal,omitempty"`
	Libelle    string `json:"libelle,omitempty"`
}

// RiskBlob is the flattened per-risk shape persisted under
// `naturels.*` / `technos.*`. Mirrors the BRGM Risk subset surfaced to
// the operator (`present` + the two scope-libellés + `specifique`).
type RiskBlob struct {
	Present       bool   `json:"present"`
	StatutCommune string `json:"statut_commune,omitempty"`
	StatutAdresse string `json:"statut_adresse,omitempty"`
	Specifique    string `json:"specifique,omitempty"`
}

// Summary aggregates the per-risk maps into operator-actionable
// counters + a red-flag list. RedFlags is always a non-nil slice so the
// wire bytes stay stable across happy / empty paths.
type Summary struct {
	NaturelsPresentCount int      `json:"naturels_present_count"`
	TechnosPresentCount  int      `json:"technos_present_count"`
	RedFlags             []string `json:"red_flags"`
}

// Evidence captures reproducibility metadata about the query that
// produced a Result. Consumers that need to log or audit how the answer
// was derived (e.g. encheridor's EnrichPayload.Method.Params) read
// these fields. Other callers can ignore them.
//
// Sidecar — not part of the wire data. Travels in-process from
// Source.Query to the adapter.
type Evidence struct {
	// Lat / Lon are the geocoded coordinates the Source used to build
	// the URL. Echoed verbatim from the inputs (no rounding here — the
	// URL builder caps at 6 decimals).
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`

	// URL is the full georisques.gouv.fr URL the Source queried. Empty
	// when the Source bailed before building a URL (insufficient
	// inputs).
	URL string `json:"url,omitempty"`

	// LevelUsed mirrors Result.LevelUsed for callers that audit only
	// the sidecar.
	LevelUsed string `json:"level_used"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// BRGM report carries no Address, no Commune, and no risk data — the
// framework records Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.Skipped
}

// HazardReport satisfies appraisal.HazardReporter. Emits the keys of
// Naturels / Technos whose Present flag is true, in stable sorted order,
// so the appraisal-layer set union stays deterministic.
//
// Géorisques is the canonical state-data source for French natural /
// technological risks, so its self-reported Confidence is hard-coded to
// High here (the per-source Confidence string still carries the BRGM
// calibration for any other consumer that cares).
func (r *Result) HazardReport() appraisal.HazardReport {
	if r == nil {
		return appraisal.HazardReport{}
	}
	natural := make([]string, 0, len(r.Naturels))
	for key, blob := range r.Naturels {
		if blob.Present {
			natural = append(natural, key)
		}
	}
	industrial := make([]string, 0, len(r.Technos))
	for key, blob := range r.Technos {
		if blob.Present {
			industrial = append(industrial, key)
		}
	}
	sort.Strings(natural)
	sort.Strings(industrial)
	return appraisal.HazardReport{
		NaturalRisks:    natural,
		IndustrialRisks: industrial,
		Confidence:      appraisal.ConfidenceHigh,
	}
}
