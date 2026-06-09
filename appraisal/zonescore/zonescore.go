package zonescore

import (
	"github.com/bpineau/gazetteer/appraisal"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/stats"
)

// Axis names. Stable identifiers used as the DefaultWeights / Options.Weights
// keys and surfaced in each Axis.Name.
const (
	AxisRendement   = "rendement"   // gross yield (dominant)
	AxisTension     = "tension"     // lettability: rental demand vs vacancy
	AxisSolvabilite = "solvabilite" // tenant reliability: income + employment
	AxisSecurite    = "securite"    // safety
	AxisFiscalite   = "fiscalite"   // net-yield drag (property-tax rate)
	AxisAcces       = "acces"       // access + livability
)

// DefaultWeights encodes the yield-first thesis: the gross-yield axis carries
// the most weight, the rest temper it. They sum to 1, but need not — Compute
// renormalises over the axes actually present.
var DefaultWeights = map[string]float64{
	AxisRendement:   0.42,
	AxisTension:     0.20,
	AxisSolvabilite: 0.13,
	AxisSecurite:    0.10,
	AxisFiscalite:   0.08,
	AxisAcces:       0.07,
}

// Options tunes Compute. The zero value is valid (uses DefaultWeights).
type Options struct {
	// Weights, when non-nil, REPLACES the default weight set wholesale: an axis
	// not in the map gets weight 0 (excluded). This makes a partial map an
	// intentional "score only these axes" profile rather than a surprise
	// 6-axis blend. Leave nil to use DefaultWeights.
	Weights map[string]float64
}

// Axis is one scored dimension of the composite.
type Axis struct {
	// Name is the axis identifier (see Axis* constants).
	Name string `json:"name"`

	// Value is the axis score, 0–100 (higher is better for the investor).
	// Meaningful only when Present.
	Value float64 `json:"value"`

	// Weight is the weight applied to this axis in the composite.
	Weight float64 `json:"weight"`

	// Present reports whether the axis could be scored (its sources contributed).
	// Absent axes do not enter the composite.
	Present bool `json:"present"`

	// Reason is a short human-readable explanation of the value.
	Reason string `json:"reason,omitempty"`

	// Sources lists the Source names that fed this axis.
	Sources []string `json:"sources,omitempty"`
}

// Score is the consolidated zone score.
type Score struct {
	// Composite is the weighted-mean score over present axes, 0–100. Higher is a
	// better yield-first opportunity. 0 when no axis could be scored.
	Composite float64 `json:"composite"`

	// Axes lists every axis in a stable order, present or not, so callers can
	// show the full breakdown (and why an axis was skipped).
	Axes []Axis `json:"axes"`

	// Confidence reflects how much of the intended weight was present, and
	// whether the dominant yield axis was among it.
	Confidence appraisal.Confidence `json:"confidence"`
}

// axisSpec binds an axis name to its scorer, in display/compute order.
type axisSpec struct {
	name   string
	scorer func(gazetteer.Dossier) axisResult
}

// axisResult is one scorer's output.
type axisResult struct {
	value   float64
	reason  string
	sources []string
	present bool
}

// axisSpecs is the ordered axis registry.
var axisSpecs = []axisSpec{
	{AxisRendement, scoreRendement},
	{AxisTension, scoreTension},
	{AxisSolvabilite, scoreSolvabilite},
	{AxisSecurite, scoreSecurite},
	{AxisFiscalite, scoreFiscalite},
	{AxisAcces, scoreAcces},
}

// Compute scores a Dossier. It evaluates every axis, drops the ones whose
// sources are absent, and returns the weight-renormalised composite plus the
// full per-axis breakdown.
func Compute(d gazetteer.Dossier, opts ...Options) Score {
	o := Options{}
	if len(opts) > 0 {
		o = opts[0]
	}

	axes := make([]Axis, 0, len(axisSpecs))
	var sumW, sumWV, presentW float64
	rendementPresent := false
	for _, spec := range axisSpecs {
		w := weightFor(spec.name, o)
		r := spec.scorer(d)
		axes = append(axes, Axis{
			Name: spec.name, Value: stats.Round(r.value, 1), Weight: w,
			Present: r.present, Reason: r.reason, Sources: r.sources,
		})
		if r.present && w > 0 {
			sumW += w
			sumWV += w * clamp01to100(r.value)
			presentW += w
			if spec.name == AxisRendement {
				rendementPresent = true
			}
		}
	}

	composite := 0.0
	if sumW > 0 {
		composite = sumWV / sumW
	}
	return Score{
		Composite:  stats.Round(composite, 1),
		Axes:       axes,
		Confidence: confidenceFor(presentW, totalWeight(o), rendementPresent),
	}
}

// weightFor resolves an axis weight. A non-nil Options.Weights replaces the
// defaults wholesale (absent axis → 0); otherwise DefaultWeights applies.
func weightFor(name string, o Options) float64 {
	if o.Weights != nil {
		return o.Weights[name] // 0 when absent — overrides are a complete set
	}
	return DefaultWeights[name]
}

// totalWeight is the sum of all axis weights under the effective options — the
// denominator for the present-weight coverage.
func totalWeight(o Options) float64 {
	var t float64
	for _, spec := range axisSpecs {
		t += weightFor(spec.name, o)
	}
	return t
}

// confidenceFor derives the score confidence from how much of the intended
// weight was present and whether the dominant yield axis contributed.
func confidenceFor(presentW, totalW float64, rendementPresent bool) appraisal.Confidence {
	if totalW <= 0 || presentW <= 0 {
		return appraisal.ConfidenceLow
	}
	coverage := presentW / totalW
	switch {
	case coverage >= 0.8 && rendementPresent:
		return appraisal.ConfidenceHigh
	case coverage >= 0.5:
		return appraisal.ConfidenceMedium
	default:
		return appraisal.ConfidenceLow
	}
}

// --- normalisation helpers ---------------------------------------------------

// lerp maps x linearly from [lo, hi] onto [0, 100], clamped. When hi < lo the
// mapping is inverted (lower x scores higher) — used for "lower is better"
// signals like the tax rate.
func lerp(x, lo, hi float64) float64 {
	if lo == hi {
		return 50
	}
	v := (x - lo) / (hi - lo) * 100
	return clamp01to100(v)
}

func clamp01to100(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// mean averages the present sub-scores; ok is false when none were present.
func mean(vals ...*float64) (float64, bool) {
	var sum float64
	var n int
	for _, v := range vals {
		if v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}
