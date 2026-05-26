package geodist

import (
	"math"
	"testing"
)

func TestKmBetween(t *testing.T) {
	cases := []struct {
		name                   string
		lat1, lon1, lat2, lon2 float64
		wantKm                 float64
		tolKm                  float64 // tolerance for the assert
	}{
		// Same point → 0.
		{"same point Paris", 48.8566, 2.3522, 48.8566, 2.3522, 0, 0.001},

		// Paris ↔ Marseille canonical reference : ~661 km.
		{"Paris-Marseille", 48.8566, 2.3522, 43.2965, 5.3698, 661, 5},

		// Paris ↔ Lyon : ~393 km.
		{"Paris-Lyon", 48.8566, 2.3522, 45.7640, 4.8357, 393, 3},

		// Paris ↔ London : ~344 km.
		{"Paris-London", 48.8566, 2.3522, 51.5074, -0.1278, 344, 3},

		// Two near-identical points (1m apart) — small-angle precision.
		{"1m apart", 48.8566, 2.3522, 48.85660899, 2.3522, 0.001, 0.0005},

		// Inter-arrondissement Paris : 1er ↔ 16e ~5 km.
		{"Paris 1 to 16", 48.8625, 2.3360, 48.8636, 2.2616, 5.5, 0.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := KmBetween(c.lat1, c.lon1, c.lat2, c.lon2)
			if math.Abs(got-c.wantKm) > c.tolKm {
				t.Errorf("KmBetween(%v,%v,%v,%v) = %.3f km, want %.3f ±%.3f km",
					c.lat1, c.lon1, c.lat2, c.lon2, got, c.wantKm, c.tolKm)
			}
		})
	}
}

func TestKmBetween_Symmetric(t *testing.T) {
	// Distance must be symmetric : KmBetween(A, B) == KmBetween(B, A).
	a := KmBetween(48.8566, 2.3522, 43.2965, 5.3698)
	b := KmBetween(43.2965, 5.3698, 48.8566, 2.3522)
	if math.Abs(a-b) > 1e-9 {
		t.Errorf("not symmetric: A→B = %v, B→A = %v", a, b)
	}
}

func TestMetersBetween(t *testing.T) {
	// MetersBetween must be KmBetween × 1000 to numerical precision.
	km := KmBetween(48.8566, 2.3522, 48.8636, 2.2616)
	m := MetersBetween(48.8566, 2.3522, 48.8636, 2.2616)
	if math.Abs(m-km*1000) > 1e-6 {
		t.Errorf("meters %.6f ≠ km×1000 %.6f", m, km*1000)
	}
}

func TestKmBetween_NaNPropagation(t *testing.T) {
	got := KmBetween(math.NaN(), 2.3522, 48.8636, 2.2616)
	if !math.IsNaN(got) {
		t.Errorf("NaN input did not propagate: got %v", got)
	}
}
