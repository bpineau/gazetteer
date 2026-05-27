package chomage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "chomage"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial release. Reads the INSEE chômage localisés ZE2020
//     quarterly series + commune crosswalk from an embedded JSON, and
//     surfaces the latest quarter rate plus a 20-quarter trend.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// tensionThresholdPP is the absolute-value gap (in percentage points)
// vs the national rate above which a zone is classified TensionLoose
// (above) or TensionTight (below). 1.0 pp is the rounded standard
// deviation across the 302 ZEs of the latest quarter.
const tensionThresholdPP = 1.0

// Options configures a chomage Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index
}

// Source implements gazetteer.Source for the INSEE chômage localisés
// per-zone-d'emploi dataset using an embedded JSON. Use NewSource to
// construct.
type Source struct {
	opts Options
}

// NewSource builds a chomage Source. Zero-valued Options is fine.
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
//  2. Map the INSEE to its zone d'emploi 2020 code via the embedded
//     crosswalk.
//  3. Look up the ZE's quarterly rate series, pick the latest non-nil
//     value as the headline reading, and compute the peer-relative
//     tension flag.
//  4. Return (*Result, nil). Communes absent from the crosswalk
//     surface as IsEmpty() (DOM-COM edge cases, Saint-Martin and
//     similar enclaves).
//
// Property type is irrelevant — labour-market tension applies to the
// whole zone.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	_ = ctx
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("chomage: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load()
		if err != nil {
			return nil, fmt.Errorf("chomage: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	ev := Evidence{
		INSEE:            insee,
		SeriesStart:      idx.Meta.SeriesStart,
		SeriesEnd:        idx.Meta.SeriesEnd,
		QuarterLabels:    idx.Quarters,
		RowCountZones:    idx.ZoneCount(),
		RowCountCommunes: idx.CommuneCount(),
	}

	ze, ok := idx.LookupZE(insee)
	if !ok {
		return &Result{
			Tension:    TensionUnknown,
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}, nil
	}

	zone, ok := idx.LookupZone(ze)
	if !ok || len(zone.RatePct) == 0 {
		return &Result{
			ZECode:     ze,
			Tension:    TensionUnknown,
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}, nil
	}

	rate, qLabel, qIdx := latest(zone.RatePct, idx.Quarters)
	if rate <= 0 {
		return &Result{
			ZECode:     ze,
			ZELabel:    zone.Label,
			Tension:    TensionUnknown,
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}, nil
	}

	national := idx.Meta.NationalRatePct
	// If the manifest's stored national rate is for the same quarter as
	// the latest available reading, prefer it; otherwise fall back to
	// the per-quarter national series for the matching index.
	if qIdx >= 0 && qIdx < len(idx.NationalRatePctSeries) && idx.NationalRatePctSeries[qIdx] > 0 {
		national = idx.NationalRatePctSeries[qIdx]
	}

	delta := round1(rate - national)
	return &Result{
		ZECode:            ze,
		ZELabel:           zone.Label,
		QuarterLabel:      qLabel,
		RatePct:           round1(rate),
		NationalRatePct:   round1(national),
		DeltaVsNationalPP: delta,
		Tension:           classify(delta),
		RecentTrendSeries: cloneFloats(zone.RatePct),
		Confidence:        ConfidenceHigh,
		Evidence:          ev,
	}, nil
}

// latest returns the most recent non-zero value of `series`, its
// quarter label and its index. Series is interpreted as oldest-first;
// `quarters[i]` aligns with `series[i]`. Returns (0, "", -1) when every
// value is zero/missing.
func latest(series []float64, quarters []string) (float64, string, int) {
	for i := len(series) - 1; i >= 0; i-- {
		if series[i] > 0 {
			q := ""
			if i < len(quarters) {
				q = quarters[i]
			}
			return series[i], q, i
		}
	}
	return 0, "", -1
}

// classify maps the delta to a TensionFlag. Symmetric thresholds around
// the national rate; see tensionThresholdPP.
func classify(deltaPP float64) TensionFlag {
	switch {
	case deltaPP <= -tensionThresholdPP:
		return TensionTight
	case deltaPP >= tensionThresholdPP:
		return TensionLoose
	default:
		return TensionBalanced
	}
}

// round1 rounds x to one decimal place. Avoids rendering 7.7999999 in
// downstream UIs where the JSON float would surface verbatim.
func round1(x float64) float64 {
	if x >= 0 {
		return float64(int64(x*10+0.5)) / 10
	}
	return -float64(int64(-x*10+0.5)) / 10
}

// cloneFloats returns a defensive copy of in. Avoids aliasing the
// embedded singleton via the wire Result.
func cloneFloats(in []float64) []float64 {
	if len(in) == 0 {
		return nil
	}
	out := make([]float64, len(in))
	copy(out, in)
	return out
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
		return nil, errors.New("chomage: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
