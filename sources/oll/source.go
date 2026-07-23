package oll

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "oll"

// sourceVersion bumps when the Source's internal logic changes. Callers gate
// cache invalidation on it.
//
// v2 adds the relet ("emménagés récents", <1 an) median alongside the
// all-tenancies median and makes RentEstimate prefer it — the artifact gains a
// relet_median field, so a v1 datadir cache must be superseded.
const sourceVersion = 2

// Version exposes sourceVersion so callers that wrap the Source can mirror it.
const Version = sourceVersion

// Options configures an oll Source. The zero value is usable.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub here;
	// production callers leave it nil.
	Index *Index

	// DataDir is the gazetteer data directory. A refreshed copy found there
	// overrides the embedded snapshot. Empty means embedded-only.
	DataDir string
}

// Source implements gazetteer.Source for the OLL observed-rent enricher. Use
// NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds an oll Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source { return &Source{opts: opts} }

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Datasets implements gazetteer.DatasetProvider.
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }

// Query implements gazetteer.Source. It resolves the listing's commune to its
// OLL zone and returns the observed median rent for the rooms bucket.
//
// Error mapping:
//   - Non-residential property type → gazetteer.ErrUnsupportedPropertyType.
//   - Missing INSEE → gazetteer.ErrInsufficientInputs (the answer is
//     commune-specific).
//   - Commune outside the covered perimeter → a non-nil *Result with
//     IsEmpty() == true (StatusOKEmpty).
//
// Rooms are optional: with a room count the Source returns the matching bucket;
// without one (or when that bucket has no observed cell) it falls back to the
// zone-level all-sizes median (Result.Pieces == 0), so OLL still contributes a
// market reading rather than dropping out of the consolidated synthesis.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	logger := gazetteer.LoggerFrom(ctx).With(slog.String("source", Name))

	if !residential(string(l.PropertyType)) {
		return nil, fmt.Errorf("oll: %w: %q", gazetteer.ErrUnsupportedPropertyType, l.PropertyType)
	}
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("oll: %w: missing insee", gazetteer.ErrInsufficientInputs)
	}
	pieces := 0 // 0 = the zone-level all-sizes aggregate (used when rooms unknown)
	if l.Rooms != nil && *l.Rooms > 0 {
		pieces = clampPieces(*l.Rooms)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("oll: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	ref, cell, ok := idx.Lookup(insee, pieces)
	if !ok && pieces != 0 {
		// No observed cell for this rooms bucket → fall back to the zone-level
		// all-sizes median.
		ref, cell, ok = idx.Lookup(insee, 0)
		pieces = 0
	}
	if !ok {
		// Commune outside the perimeter (ref empty) or no observed cell at all.
		return &Result{Evidence: Evidence{INSEE: insee, AggloCode: ref.agglo, ZoneID: ref.zone, Year: ref.year}}, nil
	}

	logger.Debug("oll.hit", slog.String("zone", ref.label), slog.Int("pieces", pieces), slog.Int("n", cell.N))
	return &Result{
		ObservedMedianEURPerM2:       cell.MedianEURPerM2,
		ObservedRecentMedianEURPerM2: cell.ReletMedianEURPerM2,
		ObservedQ1EURPerM2:           cell.Q1EURPerM2,
		ObservedQ3EURPerM2:           cell.Q3EURPerM2,
		AvgSurfaceM2:                 cell.SurfaceM2,
		SampleSize:                   cell.N,
		Zone:                         ref.label,
		Agglo:                        ref.name,
		Pieces:                       pieces,
		Confidence:                   confidenceForN(cell.N),
		Evidence:                     Evidence{INSEE: insee, AggloCode: ref.agglo, ZoneID: ref.zone, Year: ref.year},
	}, nil
}

// residential accepts apartments and houses (OLL publishes appartement cells;
// houses simply won't match downstream). Everything else is out of scope.
func residential(pt string) bool {
	switch strings.ToLower(strings.TrimSpace(pt)) {
	case "apartment", "flat", "appartement", "house", "maison":
		return true
	default:
		return false
	}
}

// clampPieces bounds a rooms count to the [1, 4] OLL buckets (4 = "4 et plus").
func clampPieces(rooms int) int {
	if rooms < 1 {
		return 1
	}
	if rooms > 4 {
		return 4
	}
	return rooms
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
