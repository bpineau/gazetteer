package bpe

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "bpe"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial release. Curated 16-bucket subset of INSEE BPE 2024
//     per-commune counts, gzipped JSON embed.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures a bpe Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index
}

// Source implements gazetteer.Source for the INSEE BPE per-commune
// curated subset. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a bpe Source. Zero-valued Options is fine.
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
//  2. Look up the commune in the embedded curated subset.
//  3. Return (*Result, nil). Communes with zero curated facilities
//     surface as IsEmpty() (small communes typically only carry
//     A129 Mairie, which is not in the curated subset).
//
// Property type is irrelevant — equipment counts apply to the whole
// commune.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	_ = ctx
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("bpe: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load()
		if err != nil {
			return nil, fmt.Errorf("bpe: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	ev := Evidence{
		INSEE:            insee,
		ReferenceDate:    idx.Meta.ReferenceDate,
		RowCountCommunes: idx.Count(),
	}

	counts, ok := idx.Lookup(insee)
	if !ok || len(counts) == 0 {
		return &Result{
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}, nil
	}

	// Defensive copy to avoid aliasing the embedded singleton on the
	// wire Result.
	out := make(map[Bucket]int, len(counts))
	total := 0
	for k, v := range counts {
		if v <= 0 {
			continue
		}
		out[k] = v
		total += v
	}
	if total == 0 {
		return &Result{
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}, nil
	}
	return &Result{
		Counts:          out,
		TotalFacilities: total,
		Confidence:      ConfidenceHigh,
		Evidence:        ev,
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
		return nil, errors.New("bpe: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
