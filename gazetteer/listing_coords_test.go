package gazetteer

import (
	"math"
	"testing"
)

func TestCoords_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		lat, lon float64
		ok       bool
	}{
		{48.85, 2.35, true},
		{0, 0, false},
		{math.NaN(), 2.35, false},
		{48.85, math.Inf(1), false},
		{91, 2.35, false},
		{48.85, -181, false},
	}
	for _, c := range cases {
		l := Listing{Lat: &c.lat, Lon: &c.lon}
		if _, _, ok := l.Coords(); ok != c.ok {
			t.Errorf("Coords(%v, %v) ok = %v, want %v", c.lat, c.lon, ok, c.ok)
		}
	}
}
