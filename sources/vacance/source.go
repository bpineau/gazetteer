package vacance

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "vacance"

// sourceVersion bumps when the Source's internal logic changes.
//
// v1 exposes the per-commune LOVAC 2025 vacance rate and long-term
// split.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures a vacance Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index
}

// Source implements gazetteer.Source for the LOVAC commune vacancy
// dataset using an embedded CSV. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a vacance Source. Zero-valued Options is fine.
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
//     gazetteer.ErrInsufficientInputs — the wrapper is responsible
//     for resolving INSEE from (zip, city).
//  2. Look up the commune in the embedded LOVAC index.
//  3. Return (*Result, nil). Missing communes (secret statistique)
//     surface as IsEmpty().
//
// Property type is irrelevant for this source — the vacance rate
// applies to the whole commune.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("vacance: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load()
		if err != nil {
			return nil, fmt.Errorf("vacance: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	ev := Evidence{INSEE: insee}
	e, ok := idx.Lookup(insee)
	if !ok {
		return &Result{
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}, nil
	}
	return &Result{
		VacancePct:     e.VacancePct,
		VacanceLongPct: e.VacanceLongPct,
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
		return nil, errors.New("vacance: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
