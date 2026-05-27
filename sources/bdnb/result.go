package bdnb

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers (a downstream adapter, dashboards) can match on
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

// Result is the typed payload returned by Source.Query. Mirrors the
// shape currently persisted by a downstream enricher (resultBlob
// with Identity / Building / DPE / Risks / Fiabilite sub-blobs) so the
// a downstream consumer adapter can re-serialise it 1:1 into its EnrichPayload.Result.
//
// Envelope-only fields (schema_version, enricher_version, computed_at,
// input_hash) are NOT part of the gazetteer payload — those are the
// framework's responsibility (Result envelope in gazetteer.Result, or
// in a downstream payload struct).
type Result struct {
	// Identity carries the batiment_groupe id + address normalisation.
	// Nil when the picked row had no batiment_groupe_id AND no
	// libelle_adr_principale_ban.
	Identity *Identity `json:"identity,omitempty"`

	// Building carries the building-level attributes (year of
	// construction, number of dwellings, floors, surface emprise sol,
	// height). Nil when no building field was populated on the row.
	Building *Building `json:"building,omitempty"`

	// DPE carries the energy-performance attributes (class, conso,
	// emissions, isolation, distribution counts). Nil when no DPE field
	// was populated on the row.
	DPE *DPE `json:"dpe,omitempty"`

	// Risks carries the urban/heritage flags (monument historique
	// proximity, ABF perimeter, PLU patrimonial, quartier prioritaire).
	// Nil when no risk field was populated.
	Risks *Risks `json:"risks,omitempty"`

	// Fiabilite carries the BDNB self-reported address-reliability
	// markers (cr_adr_niv_1 / cr_adr_niv_2). Nil when both are empty.
	Fiabilite *Fiabilite `json:"fiabilite,omitempty"`

	// Confidence is one of "high" / "medium" / "low" per the calibration
	// in PickConfidence.
	Confidence string `json:"confidence"`

	// SampleSize is 1 when a row was picked, 0 on an empty / skipped
	// result.
	SampleSize int `json:"sample_size"`

	// Skipped is true on the no-match sentinel result so consumers can
	// route the row through their "skipped" path instead of trying to
	// render absent fields.
	Skipped bool `json:"skipped,omitempty"`

	// SkipReason is a stable identifier populated on skipped results
	// (see SkipReason* constants). Empty in the happy path.
	SkipReason string `json:"skip_reason,omitempty"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result. Not part of the wire data (json:"-") —
	// populated by Source.Query, consumed in-process by callers that
	// need to log or audit how the answer was derived (e.g.
	// a downstream payload's method params).
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query that
// produced a Result. Consumers that need to log or audit how the answer
// was derived (e.g. a downstream payload's method params) read
// these fields. Other callers can ignore them.
//
// Sidecar — not part of the wire data. Travels in-process from
// Source.Query to the adapter.
type Evidence struct {
	// MatchStrategy is the lookup mode used (today:
	// MatchByAddressILike — the BAN-id exact-match path is reserved for
	// future use).
	MatchStrategy MatchStrategy `json:"match_strategy"`

	// BANID is the BAN id used when MatchStrategy == MatchByBANID.
	// Empty otherwise.
	BANID string `json:"ban_id,omitempty"`

	// INSEE is the 5-digit code_commune_insee filter the Source applied.
	// PostgREST requires an indexed filter to avoid 57014 timeouts; INSEE
	// is the canonical one.
	INSEE string `json:"code_commune_insee"`

	// INSEEResolutionSource records which step of the INSEE cascade
	// produced INSEE: "ban_forward" or "ban_reverse" (cf.
	// helpers/banx/insee_resolver.go). Useful to audit which
	// listings had a fragile address resolution — earlier audits
	// (#119/#124) caught BDNB silently trusting low-score Paris matches
	// for Breton zips because the cascade and its score gate were
	// bypassed.
	INSEEResolutionSource string `json:"insee_resolution_source,omitempty"`

	// AddressPattern is the ilike pattern sent to PostgREST on
	// `libelle_adr_principale_ban=ilike.*<pattern>*`. Empty when
	// MatchStrategy == MatchByBANID.
	AddressPattern string `json:"address_pattern,omitempty"`

	// RawCount is the number of rows BDNB returned. 0 on empty / skipped
	// results.
	RawCount int `json:"raw_count"`

	// PickedIndex is the position (in the raw row slice) of the row the
	// Source picked. -1 on empty / skipped results.
	PickedIndex int `json:"picked_index"`

	// URL is the full PostgREST URL the Source queried. Empty when the
	// Source bailed before building a URL (insufficient inputs).
	URL string `json:"url,omitempty"`
}

// Identity carries the picked row's batiment_groupe id + BAN-normalised
// address fields.
type Identity struct {
	BatimentGroupeID string   `json:"batiment_groupe_id"`
	BANID            string   `json:"ban_id,omitempty"`
	LibelleAdresse   string   `json:"libelle_adresse,omitempty"`
	CodeIRIS         string   `json:"code_iris,omitempty"`
	CodeEPCI         string   `json:"code_epci,omitempty"`
	CodeDepartement  string   `json:"code_departement,omitempty"`
	Parcelles        []string `json:"parcelles,omitempty"`
}

// Building carries the picked row's building-level attributes.
type Building struct {
	AnneeConstruction    *int   `json:"annee_construction,omitempty"`
	AnneeConstructionDPE *int   `json:"annee_construction_dpe,omitempty"`
	TypeBatimentDPE      string `json:"type_batiment_dpe,omitempty"`
	UsagePrincipal       string `json:"usage_principal,omitempty"`
	NbLog                *int   `json:"nb_log,omitempty"`
	NbNiveau             *int   `json:"nb_niveau,omitempty"`
	SurfaceEmpriseSolM2  *int   `json:"surface_emprise_sol_m2,omitempty"`
	HauteurMeanM         *int   `json:"hauteur_mean_m,omitempty"`
}

// DPE carries the picked row's energy-performance attributes.
type DPE struct {
	ClasseBilan               string         `json:"classe_bilan,omitempty"`
	ClasseArrete2012          string         `json:"classe_arrete_2012,omitempty"`
	ConsoKwhEpM2An            *float64       `json:"conso_kwh_ep_m2_an,omitempty"`
	GESKgCO2M2An              *float64       `json:"ges_kgco2_m2_an,omitempty"`
	TypeEnergieChauffage      string         `json:"type_energie_chauffage,omitempty"`
	TypeGenerateurChauffage   string         `json:"type_generateur_chauffage,omitempty"`
	TypeIsolationMurExterieur string         `json:"type_isolation_mur_exterieur,omitempty"`
	TypeVitrage               string         `json:"type_vitrage,omitempty"`
	DistributionClasses       map[string]int `json:"distribution_classes,omitempty"`
}

// Risks carries the picked row's urban / heritage risk flags.
type Risks struct {
	MonumentHistoriqueM      *int     `json:"monument_historique_m,omitempty"`
	NomMonumentPlusProche    string   `json:"nom_monument_plus_proche,omitempty"`
	PerimetreMH              *bool    `json:"perimetre_mh,omitempty"`
	ContrainteABFAC1         *bool    `json:"contrainte_abf_ac1,omitempty"`
	ZonePLUBatiPatrimonial   *bool    `json:"zone_plu_bati_patrimonial,omitempty"`
	QuartierPrioritaire      string   `json:"quartier_prioritaire,omitempty"`
	NomQP                    string   `json:"nom_qp,omitempty"`
	ValeurFonciereRelativeM2 *float64 `json:"valeur_fonciere_relative_m2,omitempty"`
}

// Fiabilite carries the picked row's BDNB-reported address-reliability
// markers (used by PickConfidence and surfaced to the UI).
type Fiabilite struct {
	CRAdrNiv1 string `json:"cr_adr_niv_1,omitempty"`
	CRAdrNiv2 string `json:"cr_adr_niv_2,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when BDNB
// found no usable building row for the listing — the framework records
// Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.SampleSize == 0
}
