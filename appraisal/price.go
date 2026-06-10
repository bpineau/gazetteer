package appraisal

import (
	"sort"

	"github.com/bpineau/gazetteer/gazetteer"
)

// PriceEstimator is the optional interface a Source's typed Result MAY
// implement to contribute a price-per-m² estimate to PricePerM2.
type PriceEstimator interface {
	PriceEstimate() PriceEstimate
}

// PriceEstimate is one source's contribution to the consolidated
// price-per-m² synthesis.
type PriceEstimate struct {
	// EurPerM2Cents is the price-per-m² in centimes (÷100 for €/m²) — the
	// integer-cents convention shared with appraisal.RentEstimate.
	EurPerM2Cents int64
	Confidence    Confidence
	SampleSize    int    // 0 if unknown
	Method        string // short human-readable label, e.g. "dvf_median_5y_750m"
}

// EURPerM2 returns the estimate in euros per m² (the cents field ÷ 100)
// — for callers that render or compare without caring about the
// integer-cents convention.
func (e PriceEstimate) EURPerM2() float64 { return float64(e.EurPerM2Cents) / 100 }

// PriceOptions configures PricePerM2's synthesis logic. Zero-valued
// fields fall back to library defaults — pass an empty value to opt into
// every default.
type PriceOptions struct {
	// Weights overrides per-source weights. When a name is in this map
	// it takes precedence over DefaultPriceWeights.
	Weights map[string]float64

	// DefaultWeight applies to sources not in Weights and not in
	// DefaultPriceWeights. Defaults to 0.4 when unset.
	DefaultWeight float64

	// MinSources is the minimum number of contributing sources required
	// to keep the synthesised Confidence above Low. Defaults to 2.
	MinSources int

	// OutlierZScore is the MAD-based z-score threshold above which a
	// contribution is flagged as an outlier and excluded from the
	// weighted mean. Defaults to 2.5. Only applied when there are at
	// least 3 contributing sources.
	OutlierZScore float64
}

// PriceConsolidated is the synthesised output.
type PriceConsolidated struct {
	// EurPerM2Cents is the weighted-mean price-per-m² in centimes (÷100 for €/m²).
	EurPerM2Cents int64
	Confidence    Confidence
	Inputs        []PriceInput
}

// EURPerM2 returns the consolidated value in euros per m² (cents ÷ 100).
func (c PriceConsolidated) EURPerM2() float64 { return float64(c.EurPerM2Cents) / 100 }

// PriceInput is one source's contribution after weighting and outlier
// detection. Excluded entries are kept in the slice so callers can
// surface "why" in UIs and diagnostics.
type PriceInput struct {
	Source      string
	Estimate    PriceEstimate
	Weight      float64
	Excluded    bool
	ExcludedWhy string
}

// DefaultPriceWeights is the lib-shipped default weighting keyed by
// Source.Name. Names appear as plain strings so no Go-level dependency
// on out-of-tree plugin packages is introduced; callers consume their
// own weighting by overriding PriceOptions.Weights.
var DefaultPriceWeights = map[string]float64{
	"meilleursagents": 1.0,
	"dvf":             0.9,
	"pappersimmo":     0.8,
	"bienici":         0.6,
	"castorus":        0.5,
}

// PricePerM2 synthesises a consolidated price-per-m² estimate from a
// Dossier. Iterates Results, picks up everything that implements
// PriceEstimator, applies weights from opts.Weights or
// DefaultPriceWeights or opts.DefaultWeight, rejects outliers, returns
// the weighted mean and per-input breakdown.
func PricePerM2(d gazetteer.Dossier, opts ...PriceOptions) PriceConsolidated {
	o := defaultPriceOpts()
	if len(opts) > 0 {
		o = mergePriceOpts(o, opts[0])
	}

	// 1. Collect contributions in name order for deterministic output.
	names := make([]string, 0, len(d.Results))
	for n := range d.Results {
		names = append(names, n)
	}
	sort.Strings(names)

	var inputs []PriceInput
	for _, name := range names {
		r := d.Results[name]
		switch r.Status {
		case "", gazetteer.StatusOK, gazetteer.StatusOKEmpty:
		default:
			continue
		}
		est, ok := r.Data.(PriceEstimator)
		if !ok {
			continue
		}
		e := est.PriceEstimate()
		// A zero reading is "nothing to contribute": a scraper that found no
		// listing is StatusOKEmpty and returns 0. Letting it into the
		// weighted mean would drag the consolidated price toward zero (one
		// empty scraper would roughly halve a single-real-source price).
		// Skip empty estimates; only real readings reach the mean.
		if e.EurPerM2Cents <= 0 {
			continue
		}
		inputs = append(inputs, PriceInput{
			Source:   name,
			Estimate: e,
			Weight:   lookupWeight(name, o.Weights, DefaultPriceWeights, o.DefaultWeight),
		})
	}

	if len(inputs) == 0 {
		return PriceConsolidated{Confidence: ConfidenceLow}
	}

	// 2-4. Shared kernel: MAD outlier rejection, weighted mean, confidence.
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
		return PriceConsolidated{Confidence: ConfidenceLow, Inputs: inputs}
	}

	return PriceConsolidated{
		EurPerM2Cents: mean,
		Confidence:    conf,
		Inputs:        inputs,
	}
}

func defaultPriceOpts() PriceOptions {
	return PriceOptions{
		DefaultWeight: 0.4,
		MinSources:    2,
		OutlierZScore: 2.5,
	}
}

func mergePriceOpts(base, override PriceOptions) PriceOptions {
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
