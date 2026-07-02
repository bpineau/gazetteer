# stats — shared numeric conventions

The small numeric utilities shared across source transforms, the
appraisal synthesis kernel and the overview join: median, percentile,
decimal rounding and MAD-based outlier flagging.

Before this package existed each consumer hand-rolled its own copy with
subtly different conventions (sort-in-place vs copy, even-length
handling). The conventions here are the ones every call site agreed on:

- Medians average the two middle values on even lengths.
- Percentiles use linear interpolation (numpy's default "linear"
  method) and take an already-sorted slice.
- Inputs are never mutated unless the function says so.

## Quick start

```go
import "github.com/bpineau/gazetteer/helpers/stats"

m := stats.Median([]float64{3, 1, 2})        // 2 (copies, then sorts)
p := stats.Percentile(sorted, 75)            // linear interpolation
r := stats.Round(3.14159, 2)                 // 3.14

// Flag price outliers more than 3.5 robust z-scores from the median:
mask := stats.MADOutliers(prices, 3.5)
```

## Public API

See `go doc github.com/bpineau/gazetteer/helpers/stats`:

- `func Median(vals []float64) float64`, `func MedianInt(xs []int) int`
- `func Percentile(sorted []float64, p float64) float64`
- `func Round(v float64, decimals int) float64`
- `func MADOutliers(vals []float64, zThreshold float64) []bool`

## Status

Stable. Symbols may be added but not renamed or removed without a
deprecation cycle.
