package cartofriches

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "cartofriches"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial release. Embeds the Cerema Cartofriches national
//     inventory aggregated per commune.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures a cartofriches Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index
}

// Source implements gazetteer.Source for the per-commune Cartofriches
// aggregate using an embedded JSON. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a cartofriches Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Query implements gazetteer.Source. Pipeline:
//
//  1. Require Listing.INSEE (5-digit). Without it the Source emits
//     gazetteer.ErrInsufficientInputs.
//  2. Look up the commune in the embedded Cartofriches aggregate.
//  3. Return (*Result, nil). Communes hosting no referenced friche
//     surface as IsEmpty() (~25 000 of ~35 000 communes are empty).
//
// Property type is irrelevant — the count of friches is a
// commune-wide attribute.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	_ = ctx
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("cartofriches: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load()
		if err != nil {
			return nil, fmt.Errorf("cartofriches: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	ev := Evidence{
		INSEE:            insee,
		RowCountCommunes: idx.Count(),
		RowCountSites:    idx.Meta.RowCountSites,
	}
	e, ok := idx.Lookup(insee)
	if !ok || e.SiteCount == 0 {
		return &Result{
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}, nil
	}
	ev.CommuneLabel = e.Label
	return &Result{
		SiteCount:      e.SiteCount,
		TotalSurfaceM2: e.TotalSurfaceM2,
		ByType:         e.ByType,
		ByStatus:       e.ByStatus,
		Confidence:     ConfidenceHigh,
		Evidence:       ev,
	}, nil
}

// Query is the atomic helper for callers who don't want the builder.
// The error is non-nil only when the Source failed; a successful but
// empty response still returns a non-nil *Result with IsEmpty() == true.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	data, err := NewSource(opts).Query(ctx, l)
	if err != nil {
		return nil, err
	}
	res, ok := data.(*Result)
	if !ok {
		return nil, errors.New("cartofriches: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
