package encadrement

import (
	"testing"
	"time"
)

// TestEpoqueRange pins the three published vocabularies and the strict reading
// of "avant" / "après" that makes the buckets tile the timeline.
func TestEpoqueRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		label  string
		lo, hi int
		ok     bool
	}{
		{"Avant 1946", minEpoqueYear, 1945, true}, // Paris
		{"avant 1946", minEpoqueYear, 1945, true}, // 93 and Lyon
		{"1946-1970", 1946, 1970, true},
		{"1971-1990", 1971, 1990, true},
		{"Apres 1990", 1991, maxEpoqueYear, true}, // Paris
		{"apres 1990", 1991, maxEpoqueYear, true}, // 93
		{"1991-2005", 1991, 2005, true},           // Lyon only
		{"après 2005", 2006, maxEpoqueYear, true}, // Lyon only, accented
		{"", 0, 0, false},
		{"récent", 0, 0, false},
		{"1990-1946", 0, 0, false}, // reversed
		{"avant mille", 0, 0, false},
	}
	for _, c := range cases {
		lo, hi, ok := epoqueRange(c.label)
		if ok != c.ok || (ok && (lo != c.lo || hi != c.hi)) {
			t.Errorf("epoqueRange(%q) = %d, %d, %v; want %d, %d, %v",
				c.label, lo, hi, ok, c.lo, c.hi, c.ok)
		}
	}
}

// TestEpoqueCovers_Tiles checks the property the matcher relies on: on each
// published grille, every plausible construction year falls in exactly one
// bucket. A gap would silently drop a dwelling out of its grille; an overlap
// would median two caps together.
func TestEpoqueCovers_Tiles(t *testing.T) {
	t.Parallel()
	grilles := map[string][]string{
		"paris": {"Avant 1946", "1946-1970", "1971-1990", "Apres 1990"},
		"ept":   {"avant 1946", "1946-1970", "1971-1990", "apres 1990"},
		"lyon":  {"avant 1946", "1946-1970", "1971-1990", "1991-2005", "après 2005"},
	}
	for name, labels := range grilles {
		for year := 1800; year <= 2030; year++ {
			n := 0
			for _, l := range labels {
				if epoqueCovers(l, year) {
					n++
				}
			}
			if n != 1 {
				t.Fatalf("%s grille: year %d matched %d buckets, want exactly 1", name, year, n)
			}
		}
	}
}

// TestUsableBuildYear rejects the sentinels and the typos, and allows the
// forward-dated completion year of an off-plan sale.
func TestUsableBuildYear(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		in   *int
		want int
	}{
		{nil, 0},
		{intp(0), 0},
		{intp(-1875), 0},
		{intp(20), 0},  // a 2-digit typo
		{intp(999), 0}, // below the floor
		{intp(1000), 1000},
		{intp(1875), 1875},
		{intp(2031), 2031}, // VEFA, five years out
		{intp(2032), 0},    // beyond the allowance
	}
	for _, c := range cases {
		if got := usableBuildYear(c.in, now); got != c.want {
			t.Errorf("usableBuildYear(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}
