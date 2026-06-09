// Package stats provides the small numeric utilities shared across source
// transforms, the appraisal synthesis kernel and the overview join: median,
// percentile, decimal rounding and MAD-based outlier flagging.
//
// Before this package existed each consumer hand-rolled its own copy with
// subtly different conventions (sort-in-place vs copy, even-length handling).
// The conventions here are the ones every call site already agreed on:
// medians average the two middle values on even lengths, percentiles use
// linear interpolation (numpy's default "linear" method), and inputs are
// never mutated unless the function says so.
package stats

import (
	"math"
	"sort"
)

// Median returns the median of vals, averaging the two middle values for
// even lengths. vals is copied before sorting (the input is not mutated).
// Returns 0 for an empty slice.
func Median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// MedianInt returns the median of xs with integer arithmetic: the mean of
// the two middle values (truncated) for even lengths. xs is copied before
// sorting. Returns 0 for an empty slice.
func MedianInt(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int(nil), xs...)
	sort.Ints(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// Percentile returns the p-quantile (p in [0, 1], clamped) of a sorted
// (ascending) slice with linear interpolation between adjacent values —
// numpy's default method="linear". Returns 0 for an empty slice.
//
// The slice must already be sorted: callers typically compute several
// quantiles from one sort.
func Percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if p <= 0 || n == 1 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[n-1]
	}
	pos := p * float64(n-1)
	lo := int(pos)
	if lo >= n-1 {
		return sorted[n-1]
	}
	return sorted[lo] + (sorted[lo+1]-sorted[lo])*(pos-float64(lo))
}

// Round rounds v to the given number of decimal places, half away from
// zero (math.Round semantics).
func Round(v float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(v*p) / p
}

// MADOutliers flags values whose MAD-based robust z-score exceeds
// zThreshold. The returned mask is aligned with vals (mask[i] reports
// whether vals[i] is an outlier). When the MAD is zero (at least half the
// values identical) nothing is flagged. vals is not mutated.
//
// The median convention here is the upper middle of the sorted values
// (not the even-length mean) — the historical convention of the appraisal
// synthesis kernel this helper was extracted from.
func MADOutliers(vals []float64, zThreshold float64) []bool {
	mask := make([]bool, len(vals))
	if len(vals) == 0 {
		return mask
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	median := s[len(s)/2]

	devs := make([]float64, 0, len(s))
	for _, v := range s {
		devs = append(devs, math.Abs(v-median))
	}
	sort.Float64s(devs)
	mad := devs[len(devs)/2]
	if mad == 0 {
		return mask // all (mostly) identical, nothing to flag
	}

	// 1.4826 makes MAD a consistent estimator of σ for Gaussian data.
	scale := 1.4826 * mad
	for i, v := range vals {
		if math.Abs(v-median)/scale > zThreshold {
			mask[i] = true
		}
	}
	return mask
}
