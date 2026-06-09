package appraisal

import (
	"github.com/bpineau/gazetteer/helpers/stats"
)

// kernel.go holds the synthesis math shared by PricePerM2 and RentValue.
// Both consolidations follow the same pipeline — MAD outlier rejection,
// weighted mean over the survivors, confidence from contributor count and
// per-input confidence average — over the same integer-cents convention;
// only the input/output envelope types differ.

// lookupWeight resolves a source's weight: caller override first, then
// the lib-shipped defaults, then the option fallback.
func lookupWeight(name string, override, defaults map[string]float64, fallback float64) float64 {
	if override != nil {
		if w, ok := override[name]; ok {
			return w
		}
	}
	if w, ok := defaults[name]; ok {
		return w
	}
	return fallback
}

// synthesizeCents runs the shared math kernel over parallel slices (one
// entry per contributing input, in caller order): MAD-based outlier
// flagging (only meaningful with ≥ 3 contributors), weighted mean of the
// non-flagged values, and consolidated Confidence (ConfidenceLow when
// fewer than minSources contribute, else from the average of the
// contributors' own confidences).
//
// The returned mask is aligned with vals so the caller can surface
// Excluded/ExcludedWhy on its typed inputs. ok is false when nothing
// contributes to the mean (all weights zero) — the caller returns its
// low-confidence zero value.
func synthesizeCents(vals, weights []float64, confs []Confidence, minSources int, outlierZ float64) (mean int64, mask []bool, conf Confidence, ok bool) {
	if len(vals) >= 3 {
		mask = stats.MADOutliers(vals, outlierZ)
	} else {
		mask = make([]bool, len(vals))
	}

	var sumW, sumWV float64
	var sumConf, contributing int
	for i, v := range vals {
		if mask[i] {
			continue
		}
		sumW += weights[i]
		sumWV += weights[i] * v
		sumConf += int(confs[i])
		contributing++
	}
	if sumW == 0 || contributing == 0 {
		return 0, mask, ConfidenceLow, false
	}

	conf = ConfidenceLow
	if contributing >= minSources {
		avg := float64(sumConf) / float64(contributing)
		switch {
		case avg >= 1.5:
			conf = ConfidenceHigh
		case avg >= 0.5:
			conf = ConfidenceMedium
		}
	}
	return int64(sumWV / sumW), mask, conf, true
}
