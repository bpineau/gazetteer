package rpls

import (
	"context"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/communes"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "rpls"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial release. Embeds the data.gouv.fr "Taux de logements
//     sociaux dans les Communes" 2024 vintage at commune granularity.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Tier-classification thresholds (in %). See doc.go for the calibration
// rationale.
const (
	thresholdMixte   = 3.0
	thresholdFort    = 15.0
	thresholdSatured = 30.0
)

// Options configures a rpls Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index

	// DataDir is the gazetteer data directory. When set, a refreshed copy
	// of the processed artifact found there takes precedence over the
	// embedded one. Empty means "embedded only". Wired by the factory.
	DataDir string
}

// Source implements gazetteer.Source for the per-commune SRU social
// housing rate using an embedded gzipped JSON. Use NewSource to
// construct.
type Source struct {
	opts Options
}

// NewSource builds a rpls Source. Zero-valued Options is fine.
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
//  2. Fold Paris / Lyon / Marseille arrondissement INSEE onto their
//     parent commune (upstream publishes only the parent row).
//  3. Look up the commune in the embedded SRU index.
//  4. Return (*Result, nil). Communes absent from the dataset surface
//     as IsEmpty() == true (rare DOM-COM edge cases).
//
// Property type is irrelevant — the SRU rate is a commune-wide
// attribute.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	_ = ctx
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("rpls: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("rpls: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	// Paris / Lyon / Marseille arrondissements share the parent
	// commune's SRU rate — the upstream publishes against the parent
	// commune INSEE only.
	insee = communes.FoldArrondissement(insee)

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
	ev.CommuneLabel = e.Label
	return &Result{
		LLSRate:    e.RatePct,
		Tier:       classify(e.RatePct),
		Confidence: ConfidenceHigh,
		Evidence:   ev,
	}, nil
}

// classify maps a rate to a Tier. See doc.go for thresholds.
func classify(rate float64) Tier {
	switch {
	case rate < thresholdMixte:
		return TierRural
	case rate < thresholdFort:
		return TierMixte
	case rate < thresholdSatured:
		return TierFort
	default:
		return TierSatured
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
