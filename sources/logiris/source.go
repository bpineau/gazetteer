package logiris

import (
	"context"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "logiris"

// sourceVersion bumps when the Source's internal logic changes.
//
// v1 exposes the per-IRIS census housing structure (renter share, social
// housing share, vacancy rate) for Île-de-France.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can mirror it.
const Version = sourceVersion

// Options configures a logiris Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub here.
	Index *Index

	// DataDir is the gazetteer data directory. When set, a refreshed copy of
	// the processed artifact found there takes precedence over the embedded
	// one. Empty means "embedded only". Wired by the factory.
	DataDir string
}

// Source implements gazetteer.Source for the INSEE census IRIS-level housing
// structure. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a logiris Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source { return &Source{opts: opts} }

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Datasets implements gazetteer.DatasetProvider, exposing the embedded
// extract to the dataset refresh tooling.
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }

// Query implements gazetteer.Source. Pipeline:
//
//  1. Require Listing.IRIS (9-char). Without it the Source emits
//     gazetteer.ErrInsufficientInputs.
//  2. Look up the IRIS in the embedded housing index.
//  3. Return (*Result, nil). Missing IRIS (outside IDF, or no résidences
//     principales) surface as IsEmpty().
//
// Property type is irrelevant — the housing profile applies to the whole
// IRIS.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	iris := strings.TrimSpace(l.IRIS)
	if iris == "" {
		return nil, fmt.Errorf("logiris: %w: listing.IRIS required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("logiris: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	ev := Evidence{IRIS: iris, DataYear: idx.Meta.DataYear}

	e, ok := idx.Lookup(iris)
	if !ok || e.TotalLogements <= 0 {
		return &Result{Confidence: ConfidenceNone, Evidence: ev}, nil
	}
	return &Result{
		RenterSharePct:        e.RenterSharePct,
		SocialHousingSharePct: e.SocialHousingSharePct,
		VacancyRatePct:        e.VacancyRatePct,
		TotalLogements:        e.TotalLogements,
		Confidence:            ConfidenceHigh,
		Evidence:              ev,
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
