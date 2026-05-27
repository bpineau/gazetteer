package osm

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers (appraisers, dashboards) can match on them
// without importing this package's constants.
const (
	ConfidenceHigh = "high"
	ConfidenceLow  = "low"
)

// SkipReason sentinels populated on empty (skipped) results. Stable
// wire contract — downstream consumers group on these values to decide
// whether to surface a transient or permanent skip error to the
// runner.
const (
	// SkipReasonOutOfRange — the listing's coordinates resolve a
	// nearest station BEYOND MaxNearestStationMeters. Permanent for
	// that (lat, lon): the metropolitan catalog will never grow fast
	// enough to cover this point.
	SkipReasonOutOfRange = "out_of_range"
)

// Result is the typed payload returned by Source.Query.
//
// Envelope-only fields (schema_version, source_version, computed_at,
// input_hash) are NOT part of this payload — those are the framework's
// responsibility (see gazetteer.Result).
type Result struct {
	// NearestTransitName is the user-visible station name (e.g.
	// "Lourmel"). Empty on an empty/skipped result.
	NearestTransitName string `json:"nearest_transit_name"`

	// NearestTransitType is the catalog station's TransitType
	// (metro / rer / transilien / train / tram). Empty on an
	// empty/skipped result.
	NearestTransitType TransitType `json:"nearest_transit_type"`

	// NearestTransitLines lists the lines serving the picked station
	// ("8", "RER A", "T3a"). May be empty (Transilien halts often
	// have no `ref` tag on OSM).
	NearestTransitLines []string `json:"nearest_transit_lines,omitempty"`

	// NearestTransitWalkM is the walking distance in metres
	// (haversine × sinuosity). 0 on an empty/skipped result.
	NearestTransitWalkM int `json:"nearest_transit_walk_m"`

	// NearestTransitWalkMin is NearestTransitWalkM / WalkSpeedMetersPerMinute,
	// floored at 1. 0 on an empty/skipped result.
	NearestTransitWalkMin int `json:"nearest_transit_walk_min"`

	// Confidence is "high" on a populated result, "low" on an
	// empty/skipped one — same convention as other gazetteer Sources.
	Confidence string `json:"confidence"`

	// SampleSize is 1 when a station was picked, 0 on an empty result.
	SampleSize int `json:"sample_size"`

	// Skipped is true on a sentinel result (no station within the
	// proximity cap, etc.) so consumers can route the row through
	// their "skipped" path. Not persisted in the pre-port wire format
	// — adapter-side discriminator only (json:"-").
	Skipped bool `json:"-"`

	// SkipReason is a stable identifier populated on skipped results
	// (see SkipReason* constants). Empty in the happy path. Not part
	// of the pre-port wire data — adapter-side discriminator only
	// (json:"-").
	SkipReason string `json:"-"`

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
	// AuctionLat / AuctionLon are the input coordinates Source.Query
	// resolved (typically Listing.Lat / Listing.Lon).
	AuctionLat float64 `json:"auction_lat"`
	AuctionLon float64 `json:"auction_lon"`

	// HaversineMeters is the raw great-circle distance to the picked
	// station, in metres. 0 on an empty/skipped result.
	HaversineMeters int `json:"haversine_meters"`

	// WalkMultiplier is the sinuosity multiplier applied to the
	// haversine distance to derive walk metres
	// (WalkSinuosityMultiplier).
	WalkMultiplier float64 `json:"walk_multiplier"`

	// ProximityCapM is the upper bound on haversine distance Source.Query
	// tolerated when picking the nearest station (MaxNearestStationMeters).
	ProximityCapM float64 `json:"proximity_cap_m"`

	// CatalogFetchedAt is the RFC3339-formatted timestamp of the catalog
	// snapshot Source.Query consulted. Empty when the Source ran without
	// a catalog (the snapshot is missing or empty).
	CatalogFetchedAt string `json:"catalog_fetched_at"`

	// CatalogStations is the number of stations in the catalog snapshot
	// Source.Query consulted. 0 when the catalog was empty/missing.
	CatalogStations int `json:"catalog_stations"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when OSM
// found no walkable station for the listing — the framework records
// Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.SampleSize == 0
}
