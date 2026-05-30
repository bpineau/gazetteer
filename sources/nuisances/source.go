package nuisances

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; the Dossier results key.
const Name = "nuisances"

// sourceVersion bumps when the Source's internal logic changes.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can mirror it.
const Version = sourceVersion

// MaxCellMeters caps the distance from a listing to the nearest grid cell
// centre. The grid step is 500 m, so a point inside a cell lies within ~354 m
// (half the diagonal) of its centre; 400 m tolerates edge rounding while still
// rejecting points outside the Île-de-France grid.
const MaxCellMeters = 400.0

// Options configures a nuisances Source. The zero value is usable.
type Options struct {
	// Index overrides the lazily-loaded singleton (tests inject a stub).
	Index *Index

	// DataDir is the gazetteer data directory; a refreshed copy there overrides
	// the embedded snapshot. Empty means embedded-only.
	DataDir string
}

// Source implements gazetteer.Source for the nuisance-grid enricher.
type Source struct {
	opts Options
}

// NewSource builds a nuisances Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source { return &Source{opts: opts} }

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Datasets implements gazetteer.DatasetProvider.
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }

// Query implements gazetteer.Source. It resolves the listing's coordinates to
// the containing 500 m grid cell and returns its nuisance exposure.
//
// Missing Lat/Lon → gazetteer.ErrInsufficientInputs. A point outside the
// Île-de-France grid → a non-nil *Result with IsEmpty() == true (StatusOKEmpty).
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	logger := gazetteer.LoggerFrom(ctx).With(slog.String("source", Name))

	if l.Lat == nil || l.Lon == nil {
		return nil, fmt.Errorf("nuisances: %w: missing lat/lon", gazetteer.ErrInsufficientInputs)
	}
	lat, lon := *l.Lat, *l.Lon
	if lat == 0 && lon == 0 {
		return nil, fmt.Errorf("nuisances: %w: lat/lon=0,0 sentinel", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("nuisances: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	ev := Evidence{ListingLat: lat, ListingLon: lon, GridCells: idx.Count()}
	c, dist, ok := idx.nearest(lat, lon, MaxCellMeters)
	if !ok {
		return &Result{Evidence: ev}, nil // outside the IDF grid
	}
	ev.CellDistanceM = int(dist)

	logger.Debug("nuisances.hit", slog.Int("nuis", c.Nuis), slog.Bool("pne", c.PNE))
	return &Result{
		NuisanceCount: c.Nuis,
		PointNoir:     c.PNE,
		Tier:          tierFor(c.Nuis),
		Confidence:    ConfidenceHigh,
		Evidence:      ev,
	}, nil
}

// Query is the atomic helper for callers who don't want the builder.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	data, err := NewSource(opts).Query(ctx, l)
	if err != nil {
		return nil, err
	}
	res, ok := data.(*Result)
	if !ok {
		return nil, errors.New("nuisances: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
