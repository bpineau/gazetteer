package pinel

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "pinel"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial release. Embeds the data.gouv.fr "Zonage ABC des
//     communes" dataset and exposes Pinel eligibility + a coarse
//     tension bucket per commune.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures a pinel Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index
}

// Source implements gazetteer.Source for the per-commune Pinel /
// zonage ABC lookup using an embedded CSV. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a pinel Source. Zero-valued Options is fine.
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
//     gazetteer.ErrInsufficientInputs — callers that don't carry the
//     INSEE should layer a Geocoder above the Source.
//  2. Look up the commune in the embedded zonage ABC index.
//  3. Return (*Result, nil). Missing communes (rare — the dataset
//     covers metropolitan + DOM) surface as IsEmpty().
//
// Property type is irrelevant for this Source — the ABC zoning is a
// commune-wide attribute.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	_ = ctx
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("pinel: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load()
		if err != nil {
			return nil, fmt.Errorf("pinel: load dataset: %w", err)
		}
		idx = loaded
	}

	ev := Evidence{
		INSEE:    insee,
		RowCount: idx.Count(),
	}
	e, ok := idx.Lookup(insee)
	if !ok {
		return &Result{
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}, nil
	}
	ev.CommuneLabel = e.CommuneLabel
	return &Result{
		Zone:          e.Zone,
		PinelEligible: pinelEligible(e.Zone),
		TensionLabel:  tensionFor(e.Zone),
		Confidence:    ConfidenceHigh,
		Evidence:      ev,
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
		return nil, errors.New("pinel: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
