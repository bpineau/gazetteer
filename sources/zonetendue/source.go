package zonetendue

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "zonetendue"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial. Per-commune zone-tendue lookup against the
//     décret 2025-1267 of 22/12/2025 revision (data.gouv.fr).
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures a zonetendue Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here.
	Index *Index
}

// Source implements gazetteer.Source for the décret 2013-392 / 2025-1267
// zone-tendue classification.
type Source struct {
	opts Options
}

// NewSource builds a zonetendue Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Query implements gazetteer.Source. Pipeline:
//
//  1. Require Listing.INSEE. Without it, return
//     gazetteer.ErrInsufficientInputs.
//  2. Look up the commune in the embedded index.
//  3. Absence from the index is the legal answer "non_tendue" — the
//     Source returns a populated Result with Tier=TierNonTendue and
//     IsTendue=false. The framework will fold the IsEmpty() check
//     onto StatusOKEmpty in that case (since the index missed), but
//     the Result is meaningful regardless.
//
// Property type is irrelevant.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("zonetendue: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load()
		if err != nil {
			return nil, fmt.Errorf("zonetendue: load dataset: %w", err)
		}
		idx = loaded
	}

	ev := Evidence{
		INSEE:         insee,
		EffectiveDate: idx.Meta.EffectiveDate,
	}

	e, ok := idx.Lookup(insee)
	if !ok {
		// Absence == non_tendue. Return a populated, non-empty Result
		// so consumers reading r.Tier always get the legally correct
		// answer. The framework will still flag StatusOK (the Result
		// is not empty — Tier is populated).
		return &Result{
			Tier:       TierNonTendue,
			IsTendue:   false,
			Confidence: ConfidenceHigh,
			Evidence:   ev,
		}, nil
	}

	return &Result{
		Tier:           e.Tier,
		IsTendue:       e.Tier == TierTendue || e.Tier == TierTendueTouristique,
		FlaggedTLV2013: e.TLV2013,
		Confidence:     ConfidenceHigh,
		Evidence:       ev,
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
		return nil, errors.New("zonetendue: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
