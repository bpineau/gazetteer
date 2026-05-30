package cdsr

// Confidence value returned in Result.Confidence. Stable string so downstream
// consumers can match on it without importing this package. CDSR copros carry
// exact geolocation, so a match is always high confidence.
const ConfidenceHigh = "high"

// Result is the typed payload returned by Source.Query.
//
// Envelope-only fields (status, version, computed_at) are the framework's
// responsibility (see gazetteer.Result), not part of this payload.
type Result struct {
	// NearestM is the haversine distance in metres to the closest CDSR copro
	// within MaxNearestMeters. 0 when none is in range (IsEmpty is then true).
	NearestM int `json:"nearest_m"`

	// Within500m / Within3km count the CDSR copros within those radii of the
	// listing. Within500m flags an immediate-neighbourhood concentration;
	// Within3km a broader-area one. Within3km counts within MaxNearestMeters
	// (3 km) — i.e. every copro the Source considered — so it equals the
	// uncapped match count even when Nearest is truncated.
	Within500m int `json:"within_500m"`
	Within3km  int `json:"within_3km"`

	// Nearest lists the CDSR copros within MaxNearestMeters, sorted by
	// ascending distance and capped at maxNearestItems. Empty when none is in
	// range.
	Nearest []Item `json:"nearest,omitempty"`

	// Confidence is "high" on a match (exact geolocation), empty otherwise.
	Confidence string `json:"confidence,omitempty"`

	// Evidence captures reproducibility metadata. Sidecar — not wire data.
	Evidence Evidence `json:"-"`
}

// Item is one CDSR copro near the listing.
type Item struct {
	// Name is the copro / residence name (e.g. "Résidence La Bruyère"). Falls
	// back to the address when the upstream leaves the name blank.
	Name string `json:"name"`

	// Address is the copro's street address.
	Address string `json:"address"`

	// Commune is the copro's commune label, verbatim from the upstream.
	Commune string `json:"commune"`

	// Lots is the declared number of condominium lots.
	Lots int `json:"lots"`

	// LabelYear is the year the CDSR label was voted (the difficulty was
	// formally recognised). 0 when the upstream date is missing.
	LabelYear int `json:"label_year,omitempty"`

	// DistanceM is the haversine distance from the listing, in metres.
	DistanceM int `json:"distance_m"`
}

// Evidence captures reproducibility metadata about the query that produced a
// Result. Sidecar — travels in-process from Source.Query to the consumer.
type Evidence struct {
	// ListingLat / ListingLon are the input coordinates the query used.
	ListingLat float64 `json:"listing_lat"`
	ListingLon float64 `json:"listing_lon"`

	// MaxMeters is the proximity cap applied (MaxNearestMeters).
	MaxMeters float64 `json:"max_meters"`

	// CatalogSize is the number of CDSR copros the query scanned.
	CatalogSize int `json:"catalog_size"`
}

// IsEmpty satisfies gazetteer.EmptyReporter: true when no CDSR copro lies
// within MaxNearestMeters of the listing — the common, reassuring case, which
// the framework records as StatusOKEmpty.
func (r *Result) IsEmpty() bool {
	return r == nil || len(r.Nearest) == 0
}
