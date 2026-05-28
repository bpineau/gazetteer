package ips_ecoles

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "ips_ecoles"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial release. Embeds the DEPP per-school IPS for rentrée
//     2024-2025, aggregated at commune granularity (UNWEIGHTED median).
//     Paris / Lyon / Marseille arrondissements are kept granular.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Tier-classification thresholds (on median IPS). See doc.go for the
// calibration rationale.
const (
	thresholdMixte    = 80.0
	thresholdMoyen    = 95.0
	thresholdFavorise = 120.0
)

// minSchoolsHighConfidence is the cut-off above which the Source stamps
// ConfidenceHigh on the Result. With < 3 schools the median is jumpy
// (one outlier moves it by tens of points), so we downgrade to
// ConfidenceMedium.
const minSchoolsHighConfidence = 3

// Options configures an ips_ecoles Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index
}

// Source implements gazetteer.Source for the per-commune median IPS
// over écoles primaires using an embedded gzipped JSON. Use NewSource
// to construct.
type Source struct {
	opts Options
}

// NewSource builds an ips_ecoles Source. Zero-valued Options is fine.
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
//  2. Look up the commune in the embedded index. Paris / Lyon /
//     Marseille arrondissements are NOT folded — the upstream
//     publishes per-arrondissement data and this is the only commune-
//     level Source in the gazetteer to preserve it.
//  3. Return (*Result, nil). Communes hosting no école surface as
//     IsEmpty() == true.
//
// Property type is irrelevant — the IPS speaks to the catchment.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	_ = ctx
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("ips_ecoles: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load()
		if err != nil {
			return nil, fmt.Errorf("ips_ecoles: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	ev := Evidence{
		INSEE:            insee,
		DataYearLabel:    idx.Meta.DataYearLabel,
		RowCountCommunes: idx.Count(),
		RowCountSchools:  idx.Meta.RowCountSchools,
	}
	e, ok := idx.Lookup(insee)
	if !ok {
		return &Result{
			Tier:       TierUnknown,
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}, nil
	}
	conf := ConfidenceHigh
	if e.SchoolCount < minSchoolsHighConfidence {
		conf = ConfidenceMedium
	}
	return &Result{
		IPSMedian:   e.IPSMedian,
		IPSMin:      e.IPSMin,
		IPSMax:      e.IPSMax,
		SchoolCount: e.SchoolCount,
		Tier:        classify(e.IPSMedian),
		Confidence:  conf,
		Evidence:    ev,
	}, nil
}

// classify maps a median IPS to a Tier. See doc.go for thresholds.
func classify(median float64) Tier {
	switch {
	case median < thresholdMixte:
		return TierPrecaire
	case median < thresholdMoyen:
		return TierMixte
	case median < thresholdFavorise:
		return TierMoyen
	default:
		return TierFavorise
	}
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
		return nil, errors.New("ips_ecoles: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
