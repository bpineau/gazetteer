package delinquance

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "delinquance"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial release. Embeds the SSMSI 2024 commune-level extract
//     covering 14 État 4001 indicators (burglary, vehicle thefts,
//     violence, sexual violence, vandalism, drugs, fraud).
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures a delinquance Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index
}

// Source implements gazetteer.Source for the per-commune SSMSI crime
// indicators using an embedded gzipped JSON. Use NewSource to
// construct.
type Source struct {
	opts Options
}

// NewSource builds a delinquance Source. Zero-valued Options is fine.
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
//  2. Look up the commune in the embedded SSMSI index.
//  3. Classify the commune into a peer-relative risk bucket.
//  4. Return (*Result, nil). Missing communes surface as IsEmpty().
//
// Property type is irrelevant — the SSMSI rates are a commune-wide
// attribute.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	_ = ctx
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("delinquance: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load()
		if err != nil {
			return nil, fmt.Errorf("delinquance: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	ev := Evidence{
		INSEE:    insee,
		DataYear: idx.Meta.DataYear,
		Unit:     idx.Meta.Unit,
	}
	e, ok := idx.Lookup(insee)
	if !ok || len(e.Rates) == 0 {
		return &Result{
			Flag:       RiskUnknown,
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}, nil
	}
	return &Result{
		Rates:      e.Rates,
		Population: e.Population,
		Flag:       classifyRisk(e.Rates),
		Confidence: ConfidenceHigh,
		Evidence:   ev,
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
		return nil, errors.New("delinquance: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
