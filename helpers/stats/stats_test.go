package stats

import (
	"math"
	"testing"
)

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestMedian(t *testing.T) {
	cases := []struct {
		name string
		in   []float64
		want float64
	}{
		{"empty", nil, 0},
		{"single", []float64{42}, 42},
		{"odd", []float64{3, 1, 2}, 2},
		{"even", []float64{4, 1, 3, 2}, 2.5},
	}
	for _, c := range cases {
		if got := Median(c.in); !almostEqual(got, c.want) {
			t.Errorf("%s: Median(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

func TestMedianDoesNotMutate(t *testing.T) {
	in := []float64{3, 1, 2}
	_ = Median(in)
	if in[0] != 3 || in[1] != 1 || in[2] != 2 {
		t.Errorf("Median mutated its input: %v", in)
	}
}

func TestMedianInt(t *testing.T) {
	cases := []struct {
		name string
		in   []int
		want int
	}{
		{"empty", nil, 0},
		{"odd", []int{30, 10, 20}, 20},
		{"even_truncates", []int{1, 2}, 1}, // (1+2)/2 integer division
	}
	for _, c := range cases {
		if got := MedianInt(c.in); got != c.want {
			t.Errorf("%s: MedianInt(%v) = %d, want %d", c.name, c.in, got, c.want)
		}
	}
}

func TestPercentile(t *testing.T) {
	sorted := []float64{10, 20, 30, 40}
	cases := []struct {
		p    float64
		want float64
	}{
		{-1, 10}, // clamped
		{0, 10},
		{0.25, 17.5}, // numpy.percentile([10,20,30,40], 25) == 17.5
		{0.5, 25},
		{0.75, 32.5},
		{1, 40},
		{2, 40}, // clamped
	}
	for _, c := range cases {
		if got := Percentile(sorted, c.p); !almostEqual(got, c.want) {
			t.Errorf("Percentile(%v, %v) = %v, want %v", sorted, c.p, got, c.want)
		}
	}
	if got := Percentile(nil, 0.5); got != 0 {
		t.Errorf("Percentile(nil) = %v, want 0", got)
	}
	if got := Percentile([]float64{7}, 0.5); got != 7 {
		t.Errorf("Percentile(single) = %v, want 7", got)
	}
}

func TestRound(t *testing.T) {
	cases := []struct {
		v        float64
		decimals int
		want     float64
	}{
		{1.2345, 2, 1.23},
		{1.235, 2, 1.24},
		{-1.235, 2, -1.24}, // half away from zero
		{12.34, 0, 12},
		{1234.5, -2, 1200},
	}
	for _, c := range cases {
		if got := Round(c.v, c.decimals); !almostEqual(got, c.want) {
			t.Errorf("Round(%v, %d) = %v, want %v", c.v, c.decimals, got, c.want)
		}
	}
}

func TestMADOutliers(t *testing.T) {
	// 1000 is a blatant outlier of the tight cluster around 10.
	vals := []float64{10, 11, 9, 10.5, 1000}
	mask := MADOutliers(vals, 2.5)
	if len(mask) != len(vals) {
		t.Fatalf("mask length = %d, want %d", len(mask), len(vals))
	}
	for i := range 4 {
		if mask[i] {
			t.Errorf("vals[%d]=%v flagged, want kept", i, vals[i])
		}
	}
	if !mask[4] {
		t.Errorf("vals[4]=%v not flagged, want outlier", vals[4])
	}
}

func TestMADOutliersZeroMAD(t *testing.T) {
	// MAD == 0 (majority identical): nothing must be flagged, even the
	// off value — the historical appraisal-kernel behaviour.
	vals := []float64{5, 5, 5, 50}
	for i, flagged := range MADOutliers(vals, 2.5) {
		if flagged {
			t.Errorf("vals[%d] flagged despite zero MAD", i)
		}
	}
	if got := MADOutliers(nil, 2.5); len(got) != 0 {
		t.Errorf("MADOutliers(nil) = %v, want empty", got)
	}
}
