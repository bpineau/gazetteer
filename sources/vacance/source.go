package vacance

import (
	"context"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
//
// IMPORTANT — disambiguation: `lovac` is the FISCAL source (the LOVAC
// dataset used to assess the Taxe sur les Logements Vacants). `vacance`
// is the DEMOGRAPHIC vacancy-rate source (INSEE census base logement).
// The two are correlated but not interchangeable.
const Name = "vacance"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial release. Embeds the INSEE RP 2021 base communale
//     logement at commune+arrondissement granularity.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Tier-classification thresholds (in %). See doc.go for the calibration
// rationale.
const (
	thresholdNormal  = 4.0
	thresholdEleve   = 8.0
	thresholdDeprise = 15.0
)

// Options configures a vacance Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index

	// DataDir is the gazetteer data directory. When set, a refreshed copy
	// of the processed artifact found there takes precedence over the
	// embedded one. Empty means "embedded only". Wired by the factory.
	DataDir string
}

// Source implements gazetteer.Source for the per-commune demographic
// vacancy rate using an embedded gzipped JSON. Use NewSource to
// construct.
type Source struct {
	opts Options
}

// NewSource builds a vacance Source. Zero-valued Options is
// fine.
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Datasets implements gazetteer.DatasetProvider, exposing the embedded
// extract to the dataset refresh tooling.
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }

// Query implements gazetteer.Source. Pipeline:
//
//  1. Require Listing.INSEE (5-digit). Without it the Source emits
//     gazetteer.ErrInsufficientInputs.
//  2. Look up the commune in the embedded index. Paris / Lyon /
//     Marseille arrondissements are NOT folded — the upstream
//     publishes one row per arrondissement.
//  3. Return (*Result, nil). Communes absent from the dataset surface
//     as IsEmpty() == true.
//
// Property type is irrelevant — the vacancy rate is a commune-wide
// attribute.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	_ = ctx
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("vacance: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("vacance: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	ev := Evidence{
		INSEE:            insee,
		DataYear:         idx.Meta.DataYear,
		RowCountCommunes: idx.Count(),
	}
	e, ok := idx.Lookup(insee)
	if !ok {
		return &Result{
			Tier:       TierUnknown,
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}, nil
	}
	return &Result{
		VacancyRate:           e.VacancyRatePct,
		VacantCount:           e.Vac,
		TotalLogements:        e.Log,
		ResidencesPrincipales: e.RP,
		ResidencesSecondaires: e.RSec,
		Tier:                  classify(e.VacancyRatePct),
		Confidence:            ConfidenceHigh,
		Evidence:              ev,
	}, nil
}

// classify maps a rate to a Tier. See doc.go for thresholds.
func classify(rate float64) Tier {
	switch {
	case rate < thresholdNormal:
		return TierTendu
	case rate < thresholdEleve:
		return TierNormal
	case rate < thresholdDeprise:
		return TierEleve
	default:
		return TierDeprise
	}
}

// Query is the atomic helper for callers who don't want the builder.
// The error is non-nil only when the Source failed; a successful but
// empty response still returns a non-nil *Result with IsEmpty() == true.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, NewSource(opts), l)
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
