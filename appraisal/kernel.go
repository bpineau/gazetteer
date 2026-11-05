package appraisal

import (
	"math"

	"github.com/bpineau/gazetteer/helpers/stats"
)

// kernel.go holds the synthesis math shared by PricePerM2 and RentValue.
// Both consolidations follow the same pipeline — MAD outlier rejection,
// weighted mean over the survivors, confidence from contributor count and
// per-input confidence average — over the same integer-cents convention;
// only the input/output envelope types differ.

// Stable ExcludedWhy reasons, shared by PriceInput and RentInput.
const (
	// excludedOutlier: the MAD outlier filter dropped the reading.
	excludedOutlier = "outlier_z_score"
	// excludedWeight: the input's weight is zero, negative, NaN or
	// infinite, so it cannot take part in a weighted mean. A zero weight
	// is how a caller switches a source off; it must not count as a
	// corroborating dataset either.
	excludedWeight = "non_positive_weight"
)

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
// entry per contributing input, in caller order):
//
//  1. inputs whose weight is not a finite positive number are set aside
//     (excludedWeight): they neither move the mean nor count towards the
//     outlier statistics or the confidence;
//  2. MAD-based outlier flagging over the remaining inputs (only meaningful
//     with ≥ 3 of them, excludedOutlier);
//  3. weighted mean of the survivors, and the consolidated Confidence
//     (ConfidenceLow when fewer than minSources DISTINCT datasets survive,
//     else from the average of the survivors' own confidences).
//
// datasets names each input's underlying dataset: two inputs cut from the
// same one (dvf and dvfagg) are one corroboration, so the floor counts
// distinct surviving datasets, never inputs.
//
// The returned why is aligned with vals: the ExcludedWhy reason of each
// input, "" for a survivor. ok is false when nothing survives; the caller
// then returns its low-confidence zero value.
func synthesizeCents(vals, weights []float64, confs []Confidence, datasets []string, minSources int, outlierZ float64) (mean int64, why []string, conf Confidence, ok bool) {
	why = make([]string, len(vals))
	var usable []int
	for i, w := range weights {
		if !(w > 0) || math.IsInf(w, 1) { // !(w > 0) also catches NaN
			why[i] = excludedWeight
			continue
		}
		usable = append(usable, i)
	}
	if len(usable) >= 3 {
		uv := make([]float64, len(usable))
		for k, i := range usable {
			uv[k] = vals[i]
		}
		for k, out := range stats.MADOutliers(uv, outlierZ) {
			if out {
				why[usable[k]] = excludedOutlier
			}
		}
	}

	var sumW, sumWV float64
	var sumConf, contributing int
	independent := make(map[string]struct{}, len(vals))
	for i, v := range vals {
		if why[i] != "" {
			continue
		}
		sumW += weights[i]
		sumWV += weights[i] * v
		sumConf += int(confs[i])
		contributing++
		independent[datasets[i]] = struct{}{}
	}
	if contributing == 0 {
		return 0, why, ConfidenceLow, false
	}

	conf = ConfidenceLow
	if len(independent) >= minSources {
		avg := float64(sumConf) / float64(contributing)
		switch {
		case avg >= 1.5:
			conf = ConfidenceHigh
		case avg >= 0.5:
			conf = ConfidenceMedium
		}
	}
	return int64(sumWV / sumW), why, conf, true
}
