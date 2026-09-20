package qpv

import (
	"context"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/communes"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "qpv"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: commune granularity — embedded the QPV 2024 list from the ANCT
//     CSV and answered "does this commune host one or more QPVs?".
//   - v2: point-in-polygon. Embeds the QPV 2024 contours (WGS84) and,
//     when the Listing carries coordinates, answers the real "is THIS
//     address inside a QPV?". Falls back to the commune-level list (lower
//     confidence) only when coordinates are absent.
//   - v3: the nearest-QPV hint measures to the nearest EDGE of a perimeter
//     instead of its nearest VERTEX. NearestCode, NearestDistanceM and
//     IsEmpty() move wherever a contour carries a long straight stretch
//     (the shipped artifact's longest edge is 3 961 m), so a v2 reading of
//     those fields must be re-derived, not reused from a cache. HasQPV for
//     a point INSIDE a QPV is unaffected. v3 also sends a listing whose
//     coordinates are only commune-precise (MinCoordPrecision) down the
//     commune-level path, instead of answering about the mairie at
//     MatchLevelPoint / ConfidenceHigh.
const sourceVersion = 3 // v3

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// NearestQPVMaxMeters caps the nearest-QPV hint: on a point match that
// lands outside every QPV, the Source records the closest QPV only when a
// QPV boundary lies within this distance. A hint only — it never affects
// HasQPV. ~1 km is "the next street over could be a QPV" territory.
const NearestQPVMaxMeters = 1000.0

// MinCoordPrecision is the coarsest geocoder granularity the
// point-in-polygon path will accept: a street centroid or finer.
//
// A QPV is a neighbourhood. A street centroid is inside one or outside
// one and the answer means something; a commune CENTRE is just the
// mairie, which is inside a QPV in a handful of communes and outside it
// in the rest, for reasons that have nothing to do with the address
// asked about. Since the coarse answer would come back as
// MatchLevelPoint / ConfidenceHigh, it is the commune-level path
// (MatchLevelCommune / ConfidenceMedium) that such a listing gets: same
// grain as its coordinates, and labelled as such.
const MinCoordPrecision = banx.PrecisionStreet

// Options configures a qpv Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here (see NewIndexForTest); production callers leave it nil.
	Index *Index

	// DataDir is the gazetteer data directory. When set, a refreshed copy
	// of the processed artifact found there takes precedence over the
	// embedded one. Empty means "embedded only". Wired by the factory.
	DataDir string
}

// Source implements gazetteer.Source for the QPV lookup using embedded
// contours. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a qpv Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Datasets implements gazetteer.DatasetProvider, exposing the embedded
// contours to the dataset refresh tooling.
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }

func (s *Source) index() (*Index, error) {
	if s.opts.Index != nil {
		return s.opts.Index, nil
	}
	return Load(s.opts.DataDir)
}

// Query implements gazetteer.Source. Pipeline:
//
//  1. Require Listing.INSEE (5-digit). Without it the Source emits
//     gazetteer.ErrInsufficientInputs.
//  2. If Lat/Lon are present: point-in-polygon. Inside a QPV → HasQPV
//     true with that single QPV; outside all → HasQPV false (the correct
//     answer for most addresses). Both are MatchLevelPoint /
//     ConfidenceHigh. An outside hit optionally records a NearestQPV hint
//     when a QPV lies within NearestQPVMaxMeters.
//  3. If coordinates are absent, or are only as precise as the commune
//     itself (MinCoordPrecision): fall back to the commune-level list
//     (arrondissements folded), MatchLevelCommune / ConfidenceMedium.
//
// Property type is irrelevant — QPV designation is geographic.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	_ = ctx
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("qpv: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx, err := s.index()
	if err != nil {
		return nil, fmt.Errorf("qpv: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
	}

	// Point-in-polygon path — the authoritative answer when we have
	// coordinates ((0,0) is the "unset coords" sentinel, see Coords)
	// precise enough to stand for the address rather than for the
	// commune (see MinCoordPrecision).
	if lat, lon, ok := l.CoordsAtLeast(MinCoordPrecision); ok {
		return s.queryPoint(idx, lat, lon), nil
	}

	// Commune-level fallback — no coordinates, or coordinates that are
	// only commune-level themselves.
	return s.queryCommune(idx, insee), nil
}

// queryPoint runs point-in-polygon over the QPV contours.
func (s *Source) queryPoint(idx *Index, lat, lon float64) *Result {
	ev := Evidence{
		Lat:          lat,
		Lon:          lon,
		PolygonCount: idx.PolygonCount(),
	}
	if h := idx.resolvePoint(lat, lon); h != nil {
		return &Result{
			HasQPV:     true,
			QPVCount:   1,
			QPVs:       []QPV{{Code: h.Code, Label: h.Label}},
			MatchLevel: MatchLevelPoint,
			Confidence: ConfidenceHigh,
			Evidence:   ev,
		}
	}
	// Outside every QPV — the correct (high-confidence) answer for most
	// addresses. Attach the nearest-QPV hint if one is close enough.
	res := &Result{
		MatchLevel: MatchLevelPoint,
		Confidence: ConfidenceHigh,
		Evidence:   ev,
	}
	if near, dist := idx.nearest(lat, lon, NearestQPVMaxMeters); near != nil {
		res.NearestCode = near.Code
		res.NearestLabel = near.Label
		res.NearestMeters = dist
	}
	return res
}

// queryCommune runs the coordinate-less commune-level fallback.
func (s *Source) queryCommune(idx *Index, insee string) *Result {
	// Paris / Lyon / Marseille arrondissements share the parent commune's
	// QPV list. Folding is only needed on this path — point-in-polygon
	// needs no commune at all.
	insee = communes.FoldArrondissement(insee)
	ev := Evidence{
		INSEE:            insee,
		PolygonCount:     idx.PolygonCount(),
		RowCountCommunes: idx.CommuneCount(),
	}
	e, ok := idx.lookupCommune(insee)
	if !ok {
		return &Result{
			MatchLevel: MatchLevelCommune,
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}
	}
	ev.CommuneLabel = e.Label
	return &Result{
		HasQPV:     true,
		QPVCount:   len(e.QPVs),
		QPVs:       copyQPVs(e.QPVs),
		MatchLevel: MatchLevelCommune,
		Confidence: ConfidenceMedium,
		Evidence:   ev,
	}
}

// copyQPVs returns a shallow copy of s. The Source's embedded singleton
// index is shared across all Query calls; without the copy a caller
// mutating Result.QPVs would corrupt the next call's reading.
func copyQPVs(s []QPV) []QPV {
	if s == nil {
		return nil
	}
	out := make([]QPV, len(s))
	copy(out, s)
	return out
}

// Query is the atomic helper for callers who don't want the builder.
// The error is non-nil only when the Source failed; a successful but
// empty response still returns a non-nil *Result with IsEmpty() == true.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, NewSource(opts), l)
}

// QueryResult is Query with the package's typed Result — for callers
// holding a constructed Source instance. Equivalent to the package-level
// Query helper without rebuilding the Source per call.
func (s *Source) QueryResult(ctx context.Context, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, s, l)
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
