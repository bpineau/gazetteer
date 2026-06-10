package zonescore

import (
	"fmt"
	"sort"
)

// Persona profile names. Stable identifiers, selectable via the CLI
// --profile flag and resolvable with WeightsForProfile.
const (
	// ProfileYield is the default yield-first thesis (== DefaultWeights):
	// gross rental yield dominates, the rest temper it.
	ProfileYield = "yield"

	// ProfileBalanced spreads weight more evenly — for an investor who
	// weighs tenant quality and livability nearly as much as raw yield.
	ProfileBalanced = "balanced"

	// ProfilePatrimoine is capital-appreciation / low-hassle: tension,
	// safety and access lead, yield matters less. For a buy-and-hold play
	// betting on the zone rather than on day-one cash-flow.
	ProfilePatrimoine = "patrimoine"

	// ProfileTransport keeps the yield-first slant but heavily up-weights
	// access (walk to RER / métro / tram) — for the investor whose hard
	// constraint is "near a station, not Paris intra-muros".
	ProfileTransport = "transport"
)

// Personas are named, complete 6-axis weight presets for Compute. Each is a
// full weight set (an absent axis is weight 0); Compute renormalises over the
// axes actually present, so the absolute sums need not be 1. Pass one through
// Options.Weights (the CLI does this via --profile).
var Personas = map[string]map[string]float64{
	ProfileYield: DefaultWeights,
	ProfileBalanced: {
		AxisRendement:   0.25,
		AxisTension:     0.20,
		AxisSolvabilite: 0.15,
		AxisSecurite:    0.15,
		AxisFiscalite:   0.10,
		AxisAcces:       0.15,
	},
	ProfilePatrimoine: {
		AxisRendement:   0.18,
		AxisTension:     0.24,
		AxisSolvabilite: 0.15,
		AxisSecurite:    0.16,
		AxisFiscalite:   0.07,
		AxisAcces:       0.20,
	},
	ProfileTransport: {
		AxisRendement:   0.34,
		AxisTension:     0.18,
		AxisSolvabilite: 0.10,
		AxisSecurite:    0.08,
		AxisFiscalite:   0.06,
		AxisAcces:       0.24,
	},
}

// WeightsForProfile returns the weight preset for a named persona. ok is
// false for an unknown name. The returned map is the shared preset — treat it
// as read-only (Compute never mutates it).
func WeightsForProfile(name string) (map[string]float64, bool) {
	w, ok := Personas[name]
	return w, ok
}

// WeightsWith returns a fresh weight set: the named persona's preset
// (or DefaultWeights when profile is empty) with overrides applied on
// top. This is the safe way to "tweak one axis": Options.Weights
// REPLACES the default set wholesale, so passing a partial map directly
// means "score only these axes" — WeightsWith merges instead.
//
//	w, err := zonescore.WeightsWith(zonescore.ProfileBalanced,
//		map[string]float64{zonescore.AxisSecurite: 0.30})
//	score := zonescore.Compute(dossier, zonescore.Options{Weights: w})
//
// Unknown profile or axis names are errors (a typo'd axis would
// otherwise silently score an axis that doesn't exist).
func WeightsWith(profile string, overrides map[string]float64) (map[string]float64, error) {
	base := DefaultWeights
	if profile != "" {
		w, ok := Personas[profile]
		if !ok {
			return nil, fmt.Errorf("zonescore: unknown profile %q (have: %v)", profile, ProfileNames())
		}
		base = w
	}
	out := make(map[string]float64, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		if _, ok := DefaultWeights[k]; !ok {
			return nil, fmt.Errorf("zonescore: unknown axis %q in overrides", k)
		}
		out[k] = v
	}
	return out, nil
}

// ProfileNames returns the persona names in sorted order (for CLI help and
// validation messages).
func ProfileNames() []string {
	names := make([]string, 0, len(Personas))
	for n := range Personas {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
