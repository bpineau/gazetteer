package delinquance

import (
	"context"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
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
//
// v2 narrowed classifyRisk to the burglary indicator only and added
// the `RatesPerInhabitantInflated` caveat for Paris/Lyon/Marseille
// arrondissements. Earlier versions tripped RiskHigh on every Paris
// arrondissement because theft_no_violence (per-inhabitant) is
// inflated 5–15× by ambient population in tourist / business
// districts.
//
// v3 re-orients the Flag semantically: from generic-crime to
// social-distress. Burglary turned out to be an anti-signal — luxury
// /tourist neighbourhoods score highest precisely because they
// concentrate stealable wealth. classifyRisk now anchors on
// drug-trafficking + street-violence + unarmed-robbery and suppresses
// the flag entirely for arrondissement-split cities (where commune-
// level rates cannot distinguish the Goutte-d'Or from Auteuil).
// Cache invalidation: bump consumes the prior v2 classifications.
const sourceVersion = 3

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures a delinquance Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index

	// DataDir is the gazetteer data directory. When set, a refreshed copy
	// of the processed artifact found there takes precedence over the
	// embedded one. Empty means "embedded only". Wired by the factory.
	DataDir string
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

// Datasets implements gazetteer.DatasetProvider, exposing the embedded
// extract to the dataset refresh tooling.
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }

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
		loaded, err := Load(s.opts.DataDir)
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
	inflated := hasInflatedPerInhabitantRates(insee)
	flag := classifyRisk(e.Rates)
	if inflated {
		// classifyRisk anchors on per-inhabitant indicators
		// (drug_trafficking, violence_outside_family, robbery_unarmed)
		// which are 5–15× inflated for Paris/Lyon/Marseille
		// arrondissements by ambient (daytime / tourist) population.
		// The resulting "high" verdict on Paris 1er — a wealthy
		// neighbourhood that is not a social-distress zone — would be
		// misleading. Suppress the flag and let consumers compose QPV
		// + Filosofi + chomage for arrondissement-split cities.
		flag = RiskUnknown
	}
	return &Result{
		Rates:                      copyRateMap(e.Rates),
		Population:                 e.Population,
		Flag:                       flag,
		RatesPerInhabitantInflated: inflated,
		Confidence:                 ConfidenceHigh,
		Evidence:                   ev,
	}, nil
}

// copyRateMap returns a shallow copy of m. The Source's embedded
// singleton index is shared across all Query calls; without the copy
// a caller mutating Result.Rates would corrupt the next call's
// reading. Cheap (these maps carry at most ~14 SSMSI indicators).
func copyRateMap(m map[string]float64) map[string]float64 {
	if m == nil {
		return nil
	}
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
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
