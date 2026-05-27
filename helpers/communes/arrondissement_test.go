package communes

import "testing"

func TestFoldArrondissement(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		// Paris
		{"75056", "75056"}, // parent passthrough
		{"75101", "75056"},
		{"75116", "75056"},
		{"75120", "75056"},
		// Lyon
		{"69123", "69123"}, // parent passthrough
		{"69381", "69123"},
		{"69389", "69123"},
		// Marseille
		{"13055", "13055"}, // parent passthrough
		{"13201", "13055"},
		{"13216", "13055"},
		// Other (no fold)
		{"33063", "33063"}, // Bordeaux
		{"59350", "59350"}, // Lille
		// Pathological
		{"", ""},
		{"7510", "7510"},     // 4-digit, untouched
		{"751011", "751011"}, // 6-digit, untouched
		// 752xx (Paris postcode prefix but NOT arrondissement INSEE) — untouched
		{"75200", "75200"},
	}
	for _, c := range cases {
		if got := FoldArrondissement(c.in); got != c.want {
			t.Errorf("FoldArrondissement(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
