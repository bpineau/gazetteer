package appraisal

import (
	"sort"

	"github.com/bpineau/gazetteer/gazetteer"
)

// RentEstimator is the optional interface a Source's typed Result MAY
// implement to contribute a rent-per-m² (€/m²/month) estimate to
// RentValue.
//
// Symmetric with PriceEstimator / HazardReporter — a Source whose typed
// Result implements RentEstimator participates in the consolidated rent
// synthesis ; everything else is silently skipped.
type RentEstimator interface {
	RentEstimate() RentEstimate
}

// RentCapper is the optional interface a Source's typed Result MAY implement to
// contribute the LEGAL rent ceiling — the loyer de référence majoré, the rent
// above which a lease is illegal in an encadrement zone. It is distinct from
// RentEstimator: the estimator feeds the market blend (encadrement contributes
// its reference median there), the capper feeds the ceiling that clamps it.
// Only encadrement zones have one; everywhere else ok is false.
type RentCapper interface {
	RentCap() (eurPerM2Cents int64, ok bool)
}

// EffectiveRentCents is the single definition of the legally chargeable rent
// per m²/month (centimes): the market blend capped by the legal ceiling when
// both are present, whichever alone is present otherwise, 0 when neither is.
// Consumers (RentConsolidated.EffectiveEURPerM2, overview) call this instead of
// re-deriving min(market, cap).
func EffectiveRentCents(blendCents, capCents int64) int64 {
	switch {
	case blendCents > 0 && capCents > 0:
		return min(blendCents, capCents)
	case blendCents > 0:
		return blendCents
	default:
		return capCents // the cap alone, or 0 when there is no cap either
	}
}

// RentEstimate is one source's contribution to the consolidated rent
// synthesis.
//
// EurPerM2Cents is the per-m²/month rent in centimes — same cents
// convention as PriceEstimate so a single math kernel handles both.
type RentEstimate struct {
	// EurPerM2Cents is the source's rent reading, in centimes per m²
	// per month. Zero when the source had nothing to contribute (the
	// caller is expected to filter such inputs upstream via IsEmpty).
	EurPerM2Cents int64

	// Confidence is the source's self-reported certainty for this
	// reading. Influences the synthesised RentConsolidated.Confidence
	// (averaged across non-excluded contributors).
	Confidence Confidence

	// Bracket optionally identifies a regulated rent zone the source
	// resolved (e.g. "encadrement_paris_zone_3"). When at least one
	// contributor populates Bracket, RentValue surfaces it on the
	// consolidated output — UI / callers can then label the rent as a
	// "loyer de référence" rather than a market estimate.
	Bracket string

	// Method is a short human-readable label
	// (e.g. "carteloyers_T2_appartement") for audit / logs.
	Method string
}

// EURPerM2 returns the estimate in euros per m² per month (cents ÷ 100).
func (e RentEstimate) EURPerM2() float64 { return float64(e.EurPerM2Cents) / 100 }

// RentOptions configures RentValue's synthesis logic. Zero-valued
// fields fall back to library defaults — pass an empty value to opt into
// every default.
type RentOptions struct {
	// Weights overrides per-source weights. When a name is in this map
	// it takes precedence over DefaultRentWeights.
	Weights map[string]float64

	// DefaultWeight applies to sources not in Weights and not in
	// DefaultRentWeights. Defaults to 0.4 when unset.
	DefaultWeight float64

	// MinSources is the minimum number of contributing sources required
	// to keep the synthesised Confidence above Low. Defaults to 1 —
	// rent is usually decisive on encadrement zones alone (the
	// regulated cap is a legal value, not a sample mean).
	MinSources int

	// OutlierZScore is the MAD-based z-score threshold above which a
	// contribution is flagged as an outlier and excluded from the
	// weighted mean. Defaults to 2.5. Only applied when there are at
	// least 3 contributing sources.
	OutlierZScore float64
}

// RentConsolidated is the synthesised output.
type RentConsolidated struct {
	// EurPerM2Cents is the consolidated rent reading in centimes per
	// m² per month — the weighted mean across non-excluded
	// contributors.
	EurPerM2Cents int64

	// Confidence reflects both contributor count (vs MinSources) and
	// per-input confidence averages.
	Confidence Confidence

	// Bracket is populated when any contributor supplied a regulated
	// zone identifier. The first non-empty Bracket (in sorted-name
	// order) wins ; other inputs' Bracket fields are not merged.
	Bracket string

	// CapEurPerM2Cents is the legal rent ceiling (loyer de référence majoré) in
	// centimes per m²/month, from the most binding RentCapper contributor
	// (encadrement). Zero when the address is not in an encadrement zone.
	CapEurPerM2Cents int64

	// Inputs lists each contributing source in deterministic name order.
	// Empty when no source implements RentEstimator.
	Inputs []RentInput
}

// EffectiveEURPerM2 returns the legally chargeable rent in €/m²/month: the
// consolidated market blend capped by the legal majoré when both exist, the cap
// alone when there is no market reading, the blend alone when there is no cap.
// This is the rent a rental-yield decision should use — not the raw blend,
// which can exceed the legal ceiling in an encadrement zone.
func (c RentConsolidated) EffectiveEURPerM2() float64 {
	return float64(EffectiveRentCents(c.EurPerM2Cents, c.CapEurPerM2Cents)) / 100
}

// EURPerM2 returns the consolidated value in euros per m² per month
// (cents ÷ 100).
func (c RentConsolidated) EURPerM2() float64 { return float64(c.EurPerM2Cents) / 100 }

// RentInput is one source's contribution after weighting and outlier
// detection. Excluded entries are kept in the slice so callers can
// surface "why" in UIs and diagnostics.
type RentInput struct {
	Source      string
	Estimate    RentEstimate
	Weight      float64
	Excluded    bool
	ExcludedWhy string
}

// DefaultRentWeights ranks rent sources. Encadrement (legal cap) is
// strongest ; carteloyers (INSEE / ANIL reference) is just below. Both
// are deterministic offline sources, so confidence comes from the
// rule itself, not from corroboration.
var DefaultRentWeights = map[string]float64{
	"encadrement": 1.0,  // hard legal cap — strongest signal
	"oll":         0.95, // observed market rents — real field data > model
	"carteloyers": 0.9,  // INSEE / DHUP reference (modelled)
}

// RentValue synthesises a consolidated rent-per-m²/month estimate from
// a Dossier.
//
// Mirrors PricePerM2's structure : iterates Results in deterministic
// name order, picks up everything that implements RentEstimator and
// returned StatusOK or StatusOKEmpty, applies weights from opts.Weights
// or DefaultRentWeights or opts.DefaultWeight, rejects MAD-based
// outliers (only when ≥ 3 contributors), returns the weighted mean and
// per-input breakdown.
//
// Bracket precedence : when several contributors populate Bracket, the
// first non-empty value (in sorted contributor name order) wins.
// Encadrement is alphabetically before carteloyers, so the legal-cap
// label naturally takes precedence when both are present.
func RentValue(d gazetteer.Dossier, opts ...RentOptions) RentConsolidated {
	o := defaultRentOpts()
	if len(opts) > 0 {
		o = mergeRentOpts(o, opts[0])
	}

	// 1. Collect contributions in name order for deterministic output.
	names := make([]string, 0, len(d.Results))
	for n := range d.Results {
		names = append(names, n)
	}
	sort.Strings(names)

	var inputs []RentInput
	var capCents int64 // most binding legal ceiling across RentCapper contributors
	for _, name := range names {
		r := d.Results[name]
		switch r.Status {
		case "", gazetteer.StatusOK, gazetteer.StatusOKEmpty:
		default:
			continue
		}
		if capper, ok := r.Data.(RentCapper); ok {
			if c, ok := capper.RentCap(); ok && c > 0 && (capCents == 0 || c < capCents) {
				capCents = c
			}
		}
		est, ok := r.Data.(RentEstimator)
		if !ok {
			continue
		}
		e := est.RentEstimate()
		// A zero reading is "nothing to contribute" (see RentEstimate doc):
		// a source outside its perimeter (oll/encadrement on a rural commune)
		// is StatusOKEmpty and returns 0. Letting it into the weighted mean
		// would drag the result toward zero — e.g. a real 14.85 €/m² from
		// carteloyers collapses to ~4.7 once two empty sources are averaged
		// in. Skip empty estimates; only real readings reach the mean.
		if e.EurPerM2Cents <= 0 {
			continue
		}
		inputs = append(inputs, RentInput{
			Source:   name,
			Estimate: e,
			Weight:   lookupWeight(name, o.Weights, DefaultRentWeights, o.DefaultWeight),
		})
	}

	if len(inputs) == 0 {
		// No market reading, but a legal cap alone is still a usable rent
		// (EffectiveEURPerM2 returns the cap when the blend is absent).
		return RentConsolidated{Confidence: ConfidenceLow, CapEurPerM2Cents: capCents}
	}

	// 2-3. Shared kernel: MAD outlier rejection, weighted mean, confidence.
	vals := make([]float64, len(inputs))
	weights := make([]float64, len(inputs))
	confs := make([]Confidence, len(inputs))
	for i, in := range inputs {
		vals[i] = float64(in.Estimate.EurPerM2Cents)
		weights[i] = in.Weight
		confs[i] = in.Estimate.Confidence
	}
	mean, mask, conf, ok := synthesizeCents(vals, weights, confs, o.MinSources, o.OutlierZScore)
	for i := range inputs {
		if mask[i] {
			inputs[i].Excluded = true
			inputs[i].ExcludedWhy = "outlier_z_score"
		}
	}
	if !ok {
		return RentConsolidated{Confidence: ConfidenceLow, CapEurPerM2Cents: capCents, Inputs: inputs}
	}

	// 4. Bracket precedence — first non-empty Bracket in (sorted) name
	// order wins. Inputs already iterates names sorted, and Excluded
	// entries don't disqualify their Bracket (the regulated label is
	// useful even when the value got outlier-rejected from the mean).
	var bracket string
	for _, in := range inputs {
		if in.Estimate.Bracket != "" {
			bracket = in.Estimate.Bracket
			break
		}
	}

	return RentConsolidated{
		EurPerM2Cents:    mean,
		Confidence:       conf,
		Bracket:          bracket,
		CapEurPerM2Cents: capCents,
		Inputs:           inputs,
	}
}

func defaultRentOpts() RentOptions {
	return RentOptions{
		DefaultWeight: 0.4,
		MinSources:    1,
		OutlierZScore: 2.5,
	}
}

func mergeRentOpts(base, override RentOptions) RentOptions {
	if override.Weights != nil {
		base.Weights = override.Weights
	}
	if override.DefaultWeight > 0 {
		base.DefaultWeight = override.DefaultWeight
	}
	if override.MinSources > 0 {
		base.MinSources = override.MinSources
	}
	if override.OutlierZScore > 0 {
		base.OutlierZScore = override.OutlierZScore
	}
	return base
}
