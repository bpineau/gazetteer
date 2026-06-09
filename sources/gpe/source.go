package gpe

import (
	"context"
	"fmt"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier.
const Name = "gpe"

// sourceVersion bumps when the Source's internal logic changes.
//
// v1 returns the nearest future Grand Paris Express station + line +
// distance, plus station counts within 1.5 km / 3 km.
const sourceVersion = 1

// Version exposes sourceVersion so callers can mirror it.
const Version = sourceVersion

// Options configures a gpe Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub.
	Index *Index

	// DataDir is the gazetteer data directory. When set, a refreshed copy of
	// the processed artifact found there takes precedence over the embedded
	// one. Empty means "embedded only". Wired by the factory.
	DataDir string
}

// Source implements gazetteer.Source for the Grand Paris Express future
// station catalog. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a gpe Source. Zero-valued Options is fine.
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
//     gazetteer.ErrInsufficientInputs.
//  2. Find the nearest future GPE station + the counts within 1.5 km / 3 km.
//  3. Return (*Result, nil). No station within MaxRelevantMeters → IsEmpty().
//
// Property type is irrelevant.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	if l.Lat == nil || l.Lon == nil {
		return nil, fmt.Errorf("gpe: %w: listing coordinates required", gazetteer.ErrInsufficientInputs)
	}
	lat, lon := *l.Lat, *l.Lon

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("gpe: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	ev := Evidence{Lat: lat, Lon: lon, StationCount: idx.Count()}
	st, within1500, within3000, ok := idx.nearest(lat, lon)
	if !ok {
		return &Result{Within1500m: within1500, Within3000m: within3000, Confidence: ConfidenceNone, Evidence: ev}, nil
	}
	station := st
	return &Result{
		Nearest:     &station,
		Within1500m: within1500,
		Within3000m: within3000,
		Confidence:  ConfidenceHigh,
		Evidence:    ev,
	}, nil
}

// Query is the atomic helper for callers who don't want the builder.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, NewSource(opts), l)
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
