// Package sensible is a gazetteer.Source that flags addresses located inside
// (or right next to) the neighbourhoods officially identified by the French
// State as the most distressed — the areas an investor wants to know about
// BEFORE buying, because the usual commune-level signals (QPV flag,
// delinquency tier) are far too coarse to isolate them.
//
// Two official layers are combined:
//
//   - QRR — the 62 "quartiers de reconquête républicaine" perimeters
//     (police-priority zones designated by the ministère de l'Intérieur,
//     2018–2021, against entrenched trafficking). Far more selective than the
//     ~1500 QPV: this is the State's own shortlist of the hardest
//     neighbourhoods, with official polygons (DCSP, WGS84). National coverage.
//   - ORCOD-IN — the "opérations de requalification des copropriétés
//     dégradées d'intérêt national": the catastrophically degraded
//     copropriétés the State expropriates at scale (décrets en Conseil
//     d'État). Exactly the kind of lots that surface at judicial auctions at
//     tempting prices. Modelled as curated center+radius circles from the
//     decree perimeters (4 sites, all in Île-de-France today).
//
// The lists are administrative snapshots (QRR last updated 2021, no
// additions since) — a stable photo of structurally distressed areas, not a
// live feed. A curated overlay (same mechanism as ORCOD-IN) lets the package
// add well-documented zones the official layers miss; every curated entry
// must cite its source in Note.
//
// Fully offline: the QRR polygons ship embedded under `data/`. Spatial —
// needs the listing's coordinates.
package sensible

// Zone kinds reported in Zone.Kind. Stable strings so downstream consumers
// can match on them without importing this package's constants.
const (
	// KindQRR is a "quartier de reconquête républicaine" police-priority
	// perimeter (ministère de l'Intérieur, official polygon).
	KindQRR = "qrr"

	// KindORCOD is an "opération de requalification des copropriétés
	// dégradées d'intérêt national" perimeter (décret en Conseil d'État),
	// approximated as a circle around the quartier.
	KindORCOD = "orcod"

	// KindCurated is a hand-maintained, documented entry that the official
	// layers miss. Zone.Note cites the source.
	KindCurated = "curated"
)

// NearbyMeters is the "right next to" radius: zones whose boundary lies
// within this distance of the listing are reported in Result.Nearby. Wide
// enough to absorb geocoding imprecision (an auction listing geocoded to the
// street can land a block away), narrow enough to stay meaningful.
const NearbyMeters = 400

// Zone is one sensitive-area hit (the listing is inside it, or near it).
type Zone struct {
	// Name is the zone's official label (e.g. "Aulnay-sous-Bois/Sevran -
	// Gros-Saule/Beudottes", "Grigny — Grigny 2").
	Name string `json:"name"`

	// Kind classifies the layer: KindQRR, KindORCOD or KindCurated.
	Kind string `json:"kind"`

	// Dep is the department code (e.g. "93"), when known.
	Dep string `json:"dep,omitempty"`

	// Vague is the QRR designation wave (1–3), 0 for non-QRR zones.
	Vague int `json:"vague,omitempty"`

	// DistanceM is the distance from the listing to the zone boundary in
	// metres: 0 when the listing is INSIDE the zone, the boundary distance
	// (vertex distance for polygons, edge-of-circle distance for ORCOD/
	// curated circles) when it is nearby.
	DistanceM int `json:"distance_m"`

	// Note documents the zone beyond its name — for ORCOD/curated entries it
	// cites the source (e.g. the décret). Empty for QRR zones (the kind says
	// it all).
	Note string `json:"note,omitempty"`
}

// Result is the typed payload returned by Source.Query.
type Result struct {
	// Sensitive is true when the listing is INSIDE at least one zone — the
	// headline "warn the user" flag.
	Sensitive bool `json:"sensitive"`

	// In lists the zones containing the listing (a point can sit in both a
	// QRR polygon and an ORCOD circle). DistanceM is 0 for each.
	In []Zone `json:"in,omitempty"`

	// Nearby lists the zones whose boundary lies within NearbyMeters of the
	// listing without containing it — the "à 200 m des Beaudottes" caution,
	// which also absorbs geocoding imprecision.
	Nearby []Zone `json:"nearby,omitempty"`

	// Evidence captures reproducibility metadata. Sidecar — not wire data.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query.
type Evidence struct {
	// Lat / Lon are the listing coordinates the containment test anchored on.
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`

	// ZoneCount is the size of the embedded QRR catalog; CuratedCount the
	// size of the in-code ORCOD/curated overlay.
	ZoneCount    int `json:"zone_count,omitempty"`
	CuratedCount int `json:"curated_count,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter: true when the listing is neither
// inside nor near any sensitive zone — the common, reassuring case.
func (r *Result) IsEmpty() bool {
	return r == nil || (!r.Sensitive && len(r.Nearby) == 0)
}
