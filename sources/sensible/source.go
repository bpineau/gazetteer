package sensible

import (
	"context"
	"fmt"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
)

// Name is the canonical Source identifier.
const Name = "sensible"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: flags listings inside (or within NearbyMeters of) a QRR
//     police-priority perimeter or an ORCOD-IN copropriété-dégradée
//     perimeter, measuring "nearby" to the nearest VERTEX of the contour.
//   - v2: "nearby" is measured to the nearest EDGE, which is the question
//     the doc always promised. Nearby, NearestDistanceM, the zone list and
//     IsEmpty() move wherever a contour carries a long straight stretch
//     (7 of the 62 shipped zones have an edge longer than twice the 400 m
//     window), so a v1 reading must be re-derived, not reused from a
//     cache. A point INSIDE a perimeter is unaffected. v2 also refuses a
//     (0, 0) coordinate (the "unset" sentinel, which v1 measured from,
//     answering "no sensitive zone nearby") and one coarser than
//     MinCoordPrecision.
const sourceVersion = 2

// Version exposes sourceVersion so callers can mirror it.
const Version = sourceVersion

// Options configures a sensible Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub.
	Index *Index

	// DataDir is the gazetteer data directory. When set, a refreshed copy of
	// the processed artifact found there takes precedence over the embedded
	// one. Empty means "embedded only". Wired by the factory.
	DataDir string
}

// Source implements gazetteer.Source for the sensitive-neighbourhood
// perimeters (QRR + ORCOD-IN + curated overlay). Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a sensible Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source { return &Source{opts: opts} }

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Datasets implements gazetteer.DatasetProvider.
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }

// MinCoordPrecision is the coarsest geocoder granularity this Source
// will answer from: a street centroid or finer.
//
// A perimeter question is a question about a place inside a commune, so
// a commune-centre coordinate cannot answer it - and would answer it
// confidently, since the mairie is a perfectly ordinary point that is
// either inside a QRR or not. A street centroid is on the right street,
// which is the scale these contours are drawn at. Coarser than that, the
// Source refuses: it has no commune-level fallback on purpose (the qpv
// Source already answers at that grain).
const MinCoordPrecision = banx.PrecisionStreet

// Query implements gazetteer.Source. Pipeline:
//
//  1. Require listing coordinates (Listing.Coords, so the (0, 0) null-island
//     sentinel counts as absent) at MinCoordPrecision or finer. Otherwise the
//     Source emits gazetteer.ErrInsufficientInputs - a commune-level fallback
//     would defeat the point (the QPV source already answers at that grain).
//  2. Test the point against every QRR polygon and curated circle: inside →
//     Result.In, boundary within NearbyMeters → Result.Nearby.
//  3. Return (*Result, nil). Neither inside nor near anything → IsEmpty().
//
// Property type is irrelevant.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	lat, lon, ok := l.Coords()
	if !ok {
		return nil, fmt.Errorf("sensible: %w: listing coordinates required", gazetteer.ErrInsufficientInputs)
	}
	if l.CoordPrecision.CoarserThan(MinCoordPrecision) {
		return nil, fmt.Errorf("sensible: %w: %w: listing coordinates are %q, where %q or finer is required to test a perimeter",
			gazetteer.ErrInsufficientInputs, banx.ErrCoarseMatch, l.CoordPrecision, MinCoordPrecision)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("sensible: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	in, nearby := idx.resolve(lat, lon)
	return &Result{
		Sensitive: len(in) > 0,
		In:        in,
		Nearby:    nearby,
		Evidence: Evidence{
			Lat: lat, Lon: lon,
			ZoneCount:    idx.ZoneCount(),
			CuratedCount: len(curatedZones),
		},
	}, nil
}

// Query is the atomic helper for callers who don't want the builder.
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
