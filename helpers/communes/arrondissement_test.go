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

func TestArrondissementParents(t *testing.T) {
	m := ArrondissementParents()
	if len(m) != 45 {
		t.Fatalf("len = %d, want 45 (20 Paris + 9 Lyon + 16 Marseille)", len(m))
	}
	for alias, parent := range m {
		// The enumerable map and the query-time fold must agree.
		if got := FoldArrondissement(alias); got != parent {
			t.Errorf("FoldArrondissement(%s) = %s, want %s", alias, got, parent)
		}
	}
	for _, probe := range []struct{ alias, parent string }{
		{"75101", "75056"}, {"75120", "75056"},
		{"69381", "69123"}, {"69389", "69123"},
		{"13201", "13055"}, {"13216", "13055"},
	} {
		if m[probe.alias] != probe.parent {
			t.Errorf("m[%s] = %s, want %s", probe.alias, m[probe.alias], probe.parent)
		}
	}
}
