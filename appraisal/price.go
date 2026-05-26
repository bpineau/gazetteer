package appraisal

import (
	"math"
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
	EurPerM2Cents int64
	Confidence    Confidence
	SampleSize    int    // 0 if unknown
	Method        string // short human-readable label, e.g. "dvf_median_5y_750m"
}

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
	EurPerM2Cents int64
	Confidence    Confidence
	Inputs        []PriceInput
}

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

// DefaultPriceWeights is the lib-shipped default weighting for known
// sources. Plugin source names (meilleursagents, bienici, …) appear as
// strings — no Go-level dependency on plugin packages is introduced.
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
		inputs = append(inputs, PriceInput{
			Source:   name,
			Estimate: e,
			Weight:   weightFor(name, o),
		})
	}

	if len(inputs) == 0 {
		return PriceConsolidated{Confidence: ConfidenceLow}
	}

	// 2. Outlier rejection (MAD z-score), only meaningful with ≥ 3 sources.
	if len(inputs) >= 3 {
		flagPriceOutliers(inputs, o.OutlierZScore)
	}

	// 3. Weighted mean across non-excluded inputs.
	var sumW, sumWV float64
	var contributing int
	for _, in := range inputs {
		if in.Excluded {
			continue
		}
		sumW += in.Weight
		sumWV += in.Weight * float64(in.Estimate.EurPerM2Cents)
		contributing++
	}
	if sumW == 0 || contributing == 0 {
		return PriceConsolidated{Confidence: ConfidenceLow, Inputs: inputs}
	}
	mean := int64(sumWV / sumW)

	// 4. Confidence derived from contributing count and per-input confidences.
	conf := computePriceConfidence(inputs, contributing, o.MinSources)

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

func weightFor(name string, o PriceOptions) float64 {
	if o.Weights != nil {
		if w, ok := o.Weights[name]; ok {
			return w
		}
	}
	if w, ok := DefaultPriceWeights[name]; ok {
		return w
	}
	return o.DefaultWeight
}

// flagPriceOutliers marks any contribution whose MAD-based z-score
// exceeds zThreshold as Excluded.
func flagPriceOutliers(inputs []PriceInput, zThreshold float64) {
	vals := make([]float64, 0, len(inputs))
	for _, in := range inputs {
		vals = append(vals, float64(in.Estimate.EurPerM2Cents))
	}
	sort.Float64s(vals)
	median := vals[len(vals)/2]

	devs := make([]float64, 0, len(vals))
	for _, v := range vals {
		devs = append(devs, math.Abs(v-median))
	}
	sort.Float64s(devs)
	mad := devs[len(devs)/2]
	if mad == 0 {
		return // all identical, nothing to flag
	}

	// 1.4826 makes MAD a consistent estimator of σ for Gaussian data.
	scale := 1.4826 * mad
	for i := range inputs {
		v := float64(inputs[i].Estimate.EurPerM2Cents)
		z := math.Abs(v-median) / scale
		if z > zThreshold {
			inputs[i].Excluded = true
			inputs[i].ExcludedWhy = "outlier_z_score"
		}
	}
}

func computePriceConfidence(inputs []PriceInput, contributing, minSources int) Confidence {
	if contributing < minSources {
		return ConfidenceLow
	}
	var sumConf int
	for _, in := range inputs {
		if in.Excluded {
			continue
		}
		sumConf += int(in.Estimate.Confidence)
	}
	avg := float64(sumConf) / float64(contributing)
	switch {
	case avg >= 1.5:
		return ConfidenceHigh
	case avg >= 0.5:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}
