package catnat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/communes"
)

// Name is the canonical Source identifier. Stable; the Dossier results key.
const Name = "catnat"

// sourceVersion bumps when the Source's internal logic changes.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can mirror it.
const Version = sourceVersion

// Options configures a catnat Source. The zero value is usable.
type Options struct {
	// Index overrides the lazily-loaded singleton (tests inject a stub).
	Index *Index

	// DataDir is the gazetteer data directory; a refreshed copy there overrides
	// the embedded aggregate. Empty means embedded-only.
	DataDir string
}

// Source implements gazetteer.Source for the CatNat history enricher.
type Source struct {
	opts Options
}

// NewSource builds a catnat Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source { return &Source{opts: opts} }

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Datasets implements gazetteer.DatasetProvider.
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }

// Query implements gazetteer.Source. It looks up the listing's commune (folding
// a Paris/Lyon/Marseille arrondissement to its mother commune, since CatNat
// decrees are issued at commune level) and returns its decree history.
//
// Missing INSEE → gazetteer.ErrInsufficientInputs. A commune with no recorded
// decree → a non-nil *Result with IsEmpty() == true (StatusOKEmpty).
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("catnat: %w: missing insee", gazetteer.ErrInsufficientInputs)
	}
	insee = communes.FoldArrondissement(insee)

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("catnat: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	ev := Evidence{INSEE: insee, RefYear: idx.refYear, WindowYears: idx.windowYears}
	row, ok := idx.Lookup(insee)
	if !ok || row.Total == 0 {
		return &Result{Evidence: ev}, nil // no recorded decree
	}

	byCat := map[string]int{}
	addCat(byCat, CatInondation, row.Inond)
	addCat(byCat, CatSecheresse, row.Sech)
	addCat(byCat, CatMouvementTerrain, row.Mvt)
	addCat(byCat, CatTempete, row.Temp)

	return &Result{
		TotalArretes:  row.Total,
		RecentCount:   row.Recent,
		ByCategory:    byCat,
		LastEventYear: row.LastYear,
		Tier:          tierFor(row.Recent),
		Confidence:    ConfidenceHigh,
		Evidence:      ev,
	}, nil
}

func addCat(m map[string]int, key string, n int) {
	if n > 0 {
		m[key] = n
	}
}

// Query is the atomic helper for callers who don't want the builder.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	data, err := NewSource(opts).Query(ctx, l)
	if err != nil {
		return nil, err
	}
	res, ok := data.(*Result)
	if !ok {
		return nil, errors.New("catnat: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
