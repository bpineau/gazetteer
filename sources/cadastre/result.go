package cadastre

// Confidence values are not exposed by this Source — the result is
// either present (the API Carto returned a parcel under the lat/lon) or
// empty (IsEmpty() == true). The bâti enrichment is opt-in and either
// fills in or fails soft; both outcomes are encoded on the Result
// struct itself, not on a confidence label.

// Result is the typed payload returned by Source.Query. The wire
// contract leaves the door open for multi-parcel responses (v2 candidate
// — marchand-de-bien view) by exposing Parcels as a slice, but v1
// always populates exactly 0 or 1 entry.
//
// Envelope-only fields (schema_version, source_version, computed_at,
// input_hash) are NOT part of this payload — those are the framework's
// responsibility (see gazetteer.Result).
type Result struct {
	// Parcels carries the cadastral parcel(s) under the listing's
	// point. Length 0 on an empty / no-match result; length 1 on the
	// happy path. The slice shape leaves room for a future multi-parcel
	// extension without breaking the wire contract.
	Parcels []Parcel `json:"parcels"`

	// BatiM2 is the total planar area (m²) of buildings whose centroid
	// sits inside the parcel. Nil when IncludeBati is false or when the
	// bâti enrichment soft-failed.
	BatiM2 *float64 `json:"bati_m2_on_parcel,omitempty"`

	// BatiCount is the number of building polygons sitting on the
	// parcel (centroid PIP filter). Nil when IncludeBati is false or
	// when the bâti enrichment soft-failed.
	BatiCount *int `json:"bati_count_on_parcel,omitempty"`

	// EmpriseRatio is BatiM2 / ContenanceM2 — the share of the parcel
	// covered by buildings. Nil when either factor is missing. Values
	// can legitimately exceed 1.0 on buildings that overhang the parcel
	// boundary (centroid PIP + raw building area is a small but
	// acceptable over-estimate per the v1 design tradeoff).
	EmpriseRatio *float64 `json:"emprise_ratio,omitempty"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result. Not part of the wire data (json:"-") —
	// populated by Source.Query, consumed in-process by callers that
	// need to log or audit how the answer was derived.
	Evidence Evidence `json:"-"`
}

// Parcel is the typed shape of a single cadastral parcel — the Etalab
// 14-char id (and its 4 components) + the surface, exposed in three
// equivalent units for investor ergonomics, + a deeplink to the
// cadastre viewer.
type Parcel struct {
	// ID is the 14-char Etalab parcel id: INSEE(5) + Prefixe(3) +
	// Section(2) + Numero(4), all left-zero-padded. Example:
	// "75104000AE0003".
	ID string `json:"id"`

	// INSEE is the 5-digit commune INSEE the parcel anchors on. For
	// Paris / Lyon / Marseille, this is the arrondissement code (the
	// one the Etalab id embeds), NOT the parent code.
	INSEE string `json:"insee"`

	// Prefixe is the 3-char "com_abs" prefix. Typically "000"; non-zero
	// only for legacy / fusioned communes.
	Prefixe string `json:"prefixe"`

	// Section is the 2-char cadastral section code (e.g. "AE", "0B").
	// Left-zero-padded when the upstream returned a single letter.
	Section string `json:"section"`

	// Numero is the 4-char parcel number within the section (e.g.
	// "0003", "0698"). Left-zero-padded.
	Numero string `json:"numero"`

	// ContenanceM2 is the parcel's official surface in square meters
	// (as recorded by the cadastre — not always physically exact, but
	// the legal reference).
	ContenanceM2 int `json:"contenance_m2"`

	// ContenanceAres is ContenanceM2 / 100, exposed for ergonomics:
	// French agricultural / large-lot listings quote surface in ares.
	ContenanceAres float64 `json:"contenance_ares"`

	// ContenanceHa is ContenanceM2 / 10000, exposed for ergonomics:
	// French rural / forest listings quote surface in hectares.
	ContenanceHa float64 `json:"contenance_ha"`

	// MapURL is the public Etalab cadastre-viewer deeplink for this
	// parcel (`https://cadastre.data.gouv.fr/map?style=ortho&parcelleId=...`).
	// Empty when ID is empty.
	MapURL string `json:"map_url"`
}

// Evidence captures reproducibility metadata about the query that
// produced a Result. Consumers that need to log or audit how the answer
// was derived read these fields. Other callers can ignore them.
//
// Sidecar — not part of the wire data. Travels in-process from
// Source.Query to the adapter.
type Evidence struct {
	// Lat / Lon are the coordinates the Source used to build the
	// query URL. Echoed verbatim from the listing inputs (no rounding
	// here — URL builder caps at LatLonDecimals decimals).
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`

	// ParcelleAPIURL is the full API Carto URL the Source queried.
	// Empty when the Source bailed before building a URL (insufficient
	// inputs).
	ParcelleAPIURL string `json:"parcelle_api_url,omitempty"`

	// BatiBaseURL is the building dump URL the Source queried (when
	// IncludeBati was true). Empty when bâti was not requested.
	BatiBaseURL string `json:"bati_base_url,omitempty"`

	// BatiRawCount is the number of building features the upstream
	// returned before the centroid PIP filter. 0 when bâti was not
	// requested or the dump was empty.
	BatiRawCount int `json:"bati_raw_count,omitempty"`

	// BatiCached is true when the in-process cache served the building
	// polygons (no upstream fetch in this Query). Useful to audit
	// "this enrichment was free of network cost".
	BatiCached bool `json:"bati_cached,omitempty"`

	// BatiError is the short, redacted error string from a soft-failed
	// bâti enrichment. Empty on the happy path. Populated when the
	// bâti fetch / parse failed and the Source returned parcel data
	// without footprint.
	BatiError string `json:"bati_error,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// FeatureCollection from API Carto carried zero parcels — the
// framework records Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return len(r.Parcels) == 0
}

// MakeParcel composes a Parcel from its raw upstream components and
// pre-computes the derived ares / hectares values and the MapURL. The
// derivations are pure functions of contenanceM2 and id — exposed as a
// helper so parser tests and the Source code path share a single
// implementation.
//
// When idu is non-empty it wins over the recomposed ParcelID — the
// upstream id is authoritative (notably for Paris / Lyon / Marseille
// where it embeds the arrondissement code instead of the parent
// commune INSEE).
func MakeParcel(idu, insee, prefixe, section, numero string, contenanceM2 int) Parcel {
	id := idu
	if id == "" {
		id = ParcelID(insee, prefixe, section, numero)
	}
	return Parcel{
		ID:             id,
		INSEE:          insee,
		Prefixe:        prefixe,
		Section:        section,
		Numero:         numero,
		ContenanceM2:   contenanceM2,
		ContenanceAres: float64(contenanceM2) / 100.0,
		ContenanceHa:   float64(contenanceM2) / 10000.0,
		MapURL:         MapURL(id),
	}
}
