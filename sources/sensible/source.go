package sensible

import (
	"context"
	"fmt"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier.
const Name = "sensible"

// sourceVersion bumps when the Source's internal logic changes.
//
// v1 flags listings inside (or within NearbyMeters of) a QRR police-priority
// perimeter or an ORCOD-IN copropriété-dégradée perimeter.
const sourceVersion = 1

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

// Query implements gazetteer.Source. Pipeline:
//
//  1. Require listing coordinates (Lat/Lon). Without them the Source emits
//     gazetteer.ErrInsufficientInputs — a commune-level fallback would defeat
//     the point (the QPV source already answers at that grain).
//  2. Test the point against every QRR polygon and curated circle: inside →
//     Result.In, boundary within NearbyMeters → Result.Nearby.
//  3. Return (*Result, nil). Neither inside nor near anything → IsEmpty().
//
// Property type is irrelevant.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	if l.Lat == nil || l.Lon == nil {
		return nil, fmt.Errorf("sensible: %w: listing coordinates required", gazetteer.ErrInsufficientInputs)
	}
	lat, lon := *l.Lat, *l.Lon

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
