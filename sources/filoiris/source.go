package filoiris

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "filoiris"

// sourceVersion bumps when the Source's internal logic changes.
//
// v1 exposes the per-IRIS Filosofi 2021 disposable-income indicators
// (median + taux de pauvreté + Gini). Risk thresholds are calibrated
// against the 2021 IRIS distribution.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can mirror
// it without reaching into the package internals.
const Version = sourceVersion

// Options configures a filoiris Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index

	// DataDir is the gazetteer data directory. When set, a refreshed copy
	// of the processed artifact found there takes precedence over the
	// embedded one. Empty means "embedded only". Wired by the factory.
	DataDir string
}

// Source implements gazetteer.Source for the INSEE Filosofi IRIS-level
// disposable-income dataset. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a filoiris Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source { return &Source{opts: opts} }

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Datasets implements gazetteer.DatasetProvider, exposing the embedded
// extract to the dataset refresh tooling.
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }

// Query implements gazetteer.Source. Pipeline:
//
//  1. Require Listing.IRIS (9-char). Without it the Source emits
//     gazetteer.ErrInsufficientInputs — the BAN normalizer's IRIS
//     resolver (or the `iris` Source) is responsible for populating it.
//  2. Look up the IRIS in the embedded Filosofi index.
//  3. Compute the income-risk flag.
//  4. Return (*Result, nil). Missing / suppressed IRIS surface as
//     IsEmpty().
//
// Property type is irrelevant — the income profile applies to the whole
// IRIS.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	iris := strings.TrimSpace(l.IRIS)
	if iris == "" {
		return nil, fmt.Errorf("filoiris: %w: listing.IRIS required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("filoiris: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	ev := Evidence{
		IRIS:              iris,
		DataYear:          idx.Meta.DataYear,
		NationalMedianEUR: idx.Meta.NationalMedianEUR,
	}

	e, ok := idx.Lookup(iris)
	if !ok || e.MedianEUR <= 0 {
		return &Result{Flag: RiskUnknown, Confidence: ConfidenceNone, Evidence: ev}, nil
	}

	conf := ConfidenceMedium
	if e.PovertyRatePct > 0 {
		conf = ConfidenceHigh
	}
	return &Result{
		MedianEUR:      e.MedianEUR,
		PovertyRatePct: e.PovertyRatePct,
		Gini:           e.Gini,
		Flag:           classifyRisk(e),
		Confidence:     conf,
		Evidence:       ev,
	}, nil
}

// classifyRisk applies income-risk thresholds calibrated against the 2021
// IRIS distribution (national median of IRIS medians ≈ 22 700 €, national
// poverty rate ≈ 15 %):
//
//	low    : median ≥ 26 000 €  AND poverty ≤ 10 %
//	high   : median ≤ 18 000 €  OR  poverty ≥ 22 %
//	medium : everything else with a populated median
//
// Unknown poverty (PovertyRatePct == 0, the suppressed/absent case) is treated
// as benign for the `low` gate but never triggers `high` — the high median
// floor carries the `low` decision, and real INSEE poverty rates are never
// exactly 0, so the conflation is safe.
func classifyRisk(e Entry) RiskFlag {
	switch {
	case e.MedianEUR >= 26000 && (e.PovertyRatePct == 0 || e.PovertyRatePct <= 10.0):
		return RiskLow
	case e.MedianEUR <= 18000 || (e.PovertyRatePct > 0 && e.PovertyRatePct >= 22.0):
		return RiskHigh
	default:
		return RiskMedium
	}
}

// Query is the atomic helper for callers who don't want the builder.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	data, err := NewSource(opts).Query(ctx, l)
	if err != nil {
		return nil, err
	}
	res, ok := data.(*Result)
	if !ok {
		return nil, errors.New("filoiris: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
