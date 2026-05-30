package appraisal

import (
	"math"
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

	// Inputs lists each contributing source in deterministic name order.
	// Empty when no source implements RentEstimator.
	Inputs []RentInput
}

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
	for _, name := range names {
		r := d.Results[name]
		switch r.Status {
		case "", gazetteer.StatusOK, gazetteer.StatusOKEmpty:
		default:
			continue
		}
		est, ok := r.Data.(RentEstimator)
		if !ok {
			continue
		}
		e := est.RentEstimate()
		inputs = append(inputs, RentInput{
			Source:   name,
			Estimate: e,
			Weight:   rentWeightFor(name, o),
		})
	}

	if len(inputs) == 0 {
		return RentConsolidated{Confidence: ConfidenceLow}
	}

	// 2. Outlier rejection (MAD z-score), only meaningful with ≥ 3 sources.
	if len(inputs) >= 3 {
		flagRentOutliers(inputs, o.OutlierZScore)
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
		return RentConsolidated{Confidence: ConfidenceLow, Inputs: inputs}
	}
	mean := int64(sumWV / sumW)

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

	// 5. Confidence derived from contributing count and per-input confidences.
	conf := computeRentConfidence(inputs, contributing, o.MinSources)

	return RentConsolidated{
		EurPerM2Cents: mean,
		Confidence:    conf,
		Bracket:       bracket,
		Inputs:        inputs,
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

func rentWeightFor(name string, o RentOptions) float64 {
	if o.Weights != nil {
		if w, ok := o.Weights[name]; ok {
			return w
		}
	}
	if w, ok := DefaultRentWeights[name]; ok {
		return w
	}
	return o.DefaultWeight
}

// flagRentOutliers marks any contribution whose MAD-based z-score
// exceeds zThreshold as Excluded. Mirrors flagPriceOutliers.
func flagRentOutliers(inputs []RentInput, zThreshold float64) {
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

func computeRentConfidence(inputs []RentInput, contributing, minSources int) Confidence {
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
