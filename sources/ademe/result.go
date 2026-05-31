package ademe

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers (appraisers, dashboards) can match on them
// without importing this package's constants.
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

// Result is the typed payload returned by Source.Query. Groups the
// DPE label, GES label, logement attributes, and a picked adresse
// candidate into the four sub-blobs used by downstream renderers.
//
// Envelope-only fields (schema_version, source_version, computed_at,
// input_hash) are NOT part of this payload — those are the framework's
// responsibility (see gazetteer.Result).
type Result struct {
	// DPE is non-nil when at least one DPE-letter field was populated
	// on the picked row. Nil-pointer = no DPE letter found.
	DPE *DPE `json:"dpe,omitempty"`

	// Logement carries the building / surface attributes from the picked
	// row. Nil when no logement field was populated.
	Logement *Logement `json:"logement,omitempty"`

	// Adresse carries the BAN-normalised address fields from the picked
	// row. Nil when no address field was populated.
	Adresse *Adresse `json:"adresse,omitempty"`

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
	// MatchStrategy is the lookup mode used (today: MatchByZipFulltext).
	MatchStrategy MatchStrategy `json:"match_strategy"`

	// Zip is the 5-digit FR postal code the Source filtered on, after
	// resolving Listing.Zip and/or falling back to the Geocoder.
	Zip string `json:"zip"`

	// Query is the full-text query string sent to data-fair on
	// `q=<query>&q_fields=adresse_ban`.
	Query string `json:"query"`

	// RawCount is the number of rows the data-fair API returned. 0 on
	// empty / skipped results.
	RawCount int `json:"raw_count"`

	// PickedIndex is the position (in the raw row slice) of the row the
	// Source picked. -1 on empty / skipped results.
	PickedIndex int `json:"picked_index"`

	// NumberMatched is true when PickBestByNumber matched the listing's
	// street number against the row's adresse_ban / adresse_brut.
	// False on full-text fallback or empty results.
	NumberMatched bool `json:"number_matched"`

	// StreetMatched is true when the picked row's street (voie type word
	// + name tokens) matched the listing's street — the discriminator
	// that tells "8 Rue des Petites Ecuries" apart from "8 Cour des
	// Petites Ecuries". False when the query carried no usable street or
	// the picked row is on a different voie (a wrong-street pick).
	StreetMatched bool `json:"street_matched"`

	// URL is the full data-fair URL the Source queried. Empty when the
	// Source bailed before building a URL (insufficient inputs).
	URL string `json:"url,omitempty"`
}

// DPE carries the energy-performance certificate identifiers and dates
// from a picked ADEME row.
type DPE struct {
	EtiquetteDPE         string `json:"etiquette_dpe,omitempty"`
	EtiquetteGES         string `json:"etiquette_ges,omitempty"`
	NumeroDPE            string `json:"numero_dpe,omitempty"`
	DateEtablissementDPE string `json:"date_etablissement_dpe,omitempty"`
	DateFinValiditeDPE   string `json:"date_fin_validite_dpe,omitempty"`
}

// Logement carries the picked row's building / surface attributes.
type Logement struct {
	SurfaceHabitableM2 *float64 `json:"surface_habitable_m2,omitempty"`
	AnneeConstruction  *int     `json:"annee_construction,omitempty"`
	TypeBatiment       string   `json:"type_batiment,omitempty"`
}

// Adresse carries the picked row's address fields (BAN-normalised plus
// the raw diagnostiqueur-typed adresse_brut).
type Adresse struct {
	AdresseBrut   string `json:"adresse_brut,omitempty"`
	AdresseBAN    string `json:"adresse_ban,omitempty"`
	CodePostalBAN string `json:"code_postal_ban,omitempty"`
	NomCommuneBAN string `json:"nom_commune_ban,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when ADEME
// found no usable DPE row for the listing — the framework records
// Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.SampleSize == 0
}
