package dvf

import (
	"sort"
	"strings"
	"time"

	"github.com/bpineau/gazetteer/helpers/proptype"
)

// Confidence values stamped into Result.Confidence by PickConfidence.
// Stable wire-format strings — downstream consumers (a downstream consumer's
// adapter, dashboards) compare against these without importing the
// package.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Filtering thresholds.
const (
	TargetSampleSize = 30 // confidence high
	MinSampleSize    = 10 // explicit minimum

	// MinSampleSizeAddressRadius is the tighter floor for the
	// `address_radius` tier — a sub-commune disk pulls fewer
	// mutations than the commune-wide fan-out, so we lower the
	// MinSample bar a notch to make the tier publishable. Empirically
	// chosen so the tier clears in ≈89 % of probed Paris/IDF/Lyon
	// auctions while leaving the rural / industrial-geocode outliers
	// to fall back to the commune tier.
	MinSampleSizeAddressRadius = 12

	// MaxMutationsPerParcelle caps the number of mutations we keep per
	// distinct `id_parcelle` after the anti-anomaly filter pass. It is a
	// defensive guard against the "one tower with 30 same-floor resales"
	// anomaly that drags the local median toward a single building's
	// pricing. Applied globally inside FilterMutations so every tier
	// (commune, neighborhood, department, address_radius) benefits.
	MaxMutationsPerParcelle = 4

	SurfaceMinM2  = 9.0
	SurfaceMaxM2  = 1000.0
	PricePerM2Min = 100.0
	PricePerM2Max = 50000.0
	CutoffYears   = 5
)

// NatureMutationVente is the only nature_mutation value we accept by
// default — i.e. an ordinary resale of an existing dwelling, which is
// what MeilleursAgents and Pappersimmo measure street-level. Excluding
// the other DVF natures keeps the comparable cohort homogeneous:
//
//   - "Vente en l'état futur d'achèvement" (VEFA) is new-build sold off-plan
//     at developer pricing, typically 30-60 % above the local ancien — its
//     inclusion was the documented root cause of MA's apparent -23 % bias in
//     VEFA-heavy departments (cf. doc/audits/ma_bias_per_dept_2026-05-19.md).
//   - "Adjudication" duplicates the population we are scoring (forced
//     auction sales) and creates a self-referencing feedback loop.
//   - "Vente terrain à bâtir" mixes raw land into a per-m²-of-bati metric.
//   - "Echange" has no monetary valeur_fonciere.
//   - "Expropriation" is administrative, not market.
const NatureMutationVente = "Vente"

// MapPropertyTypeToDVF maps our internal property_type enum to the
// DVF `type_local` value. Returns "" when the type is unsupported (the
// enricher should short-circuit with ErrUnsupportedPropertyType).
//
// `parking`/`mixed`/`other` deliberately return
// ""; `land` is also unsupported in v1 (DVF lacks a clean type_local
// for it).
func MapPropertyTypeToDVF(pt string) string {
	switch proptype.Normalize(pt) {
	case proptype.Apartment:
		return "Appartement"
	case proptype.House:
		return "Maison"
	case proptype.Commercial:
		return "Local industriel. commercial ou assimilé"
	default:
		return ""
	}
}

// FilterMutations applies the anti-anomaly criteria to the input
// mutations and returns those that survive. Keeps only entries whose
//   - NatureMutation == "Vente" (ordinary resales of existing dwellings;
//     VEFA neuf, adjudication, échange and terrain-à-bâtir are excluded
//     so the cohort stays comparable to the MA / Pappersimmo street-level
//     ancien-rue surfaces we cross-reference);
//   - TypeLocal matches target (case-insensitive);
//   - DateMutation is on or after cutoff;
//   - surface falls within [SurfaceMinM2, SurfaceMaxM2];
//   - price-per-m² is within [PricePerM2Min, PricePerM2Max].
//
// After the per-row filter pass a second pass caps the number of
// surviving mutations at MaxMutationsPerParcelle (4) per distinct
// `id_parcelle`. This protects every downstream tier (commune, …,
// address_radius) against the "one tower with 30 same-floor resales"
// anomaly that would otherwise drag the median toward one building's
// pricing. Mutations are kept in encounter order — the DVF API already
// biases newer-first so a stable first-N cap retains the freshest sales.
// Rows whose `id_parcelle` is empty (pre-2018 DVF rows are the typical
// case) are treated as their own unique parcelle, i.e. never grouped
// together by the cap.
func FilterMutations(in []Mutation, target string, cutoff time.Time) []Mutation {
	out := make([]Mutation, 0, len(in))
	for _, m := range in {
		if m.NatureMutation != NatureMutationVente {
			continue
		}
		if !strings.EqualFold(m.TypeLocal, target) {
			continue
		}
		d, err := time.Parse("2006-01-02", m.DateMutation)
		if err != nil || d.Before(cutoff) {
			continue
		}
		s := m.Surface()
		if s < SurfaceMinM2 || s > SurfaceMaxM2 {
			continue
		}
		v := m.Valeur()
		if v <= 0 {
			continue
		}
		ppm := v / s
		if ppm < PricePerM2Min || ppm > PricePerM2Max {
			continue
		}
		out = append(out, m)
	}
	return capPerParcelle(out, MaxMutationsPerParcelle)
}

// capPerParcelle keeps at most `max` mutations per distinct
// `id_parcelle`. Empty `id_parcelle` rows are not grouped (each counts
// as a unique parcelle). Stable order: earlier rows are kept, later
// duplicates are dropped.
func capPerParcelle(in []Mutation, max int) []Mutation {
	if max <= 0 || len(in) == 0 {
		return in
	}
	seen := make(map[string]int, len(in))
	out := make([]Mutation, 0, len(in))
	for _, m := range in {
		if m.IDParcelle == "" {
			out = append(out, m)
			continue
		}
		if seen[m.IDParcelle] >= max {
			continue
		}
		seen[m.IDParcelle]++
		out = append(out, m)
	}
	return out
}

// CountUniqueParcelles returns the number of distinct `id_parcelle`
// values in the input mutation slice. Empty `id_parcelle` rows each
// count as one (consistent with the cap-per-parcelle semantics).
func CountUniqueParcelles(in []Mutation) int {
	if len(in) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(in))
	uniq := 0
	for _, m := range in {
		if m.IDParcelle == "" {
			uniq++
			continue
		}
		if _, ok := seen[m.IDParcelle]; ok {
			continue
		}
		seen[m.IDParcelle] = struct{}{}
		uniq++
	}
	return uniq
}

// PerM2Quartiles returns (p25, median, p75) of price-per-m² over the
// filtered mutations, in **euros** (NOT cents). Values are 0 when the
// input is empty.
//
// Projects the Mutation slice down to a flat []float64 of price-per-m²
// values, then sorts in place and runs linear-interpolated percentiles
// (numpy "linear" semantics).
func PerM2Quartiles(in []Mutation) (p25, p50, p75 float64) {
	if len(in) == 0 {
		return 0, 0, 0
	}
	values := make([]float64, 0, len(in))
	for _, m := range in {
		s := m.Surface()
		v := m.Valeur()
		if s > 0 && v > 0 {
			values = append(values, v/s)
		}
	}
	return quartiles(values)
}

// quartiles returns (p25, median, p75) of the input float64 slice. The
// input is sorted in place. Returns (0, 0, 0) when empty.
func quartiles(values []float64) (p25, median, p75 float64) {
	if len(values) == 0 {
		return 0, 0, 0
	}
	sort.Float64s(values)
	return percentile(values, 0.25), percentile(values, 0.50), percentile(values, 0.75)
}

// percentile returns the p-percentile of a sorted (ascending) float64
// slice with linear interpolation between adjacent values. p is clamped
// to [0, 1]. Identical to numpy's default (method="linear").
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	pos := p * float64(len(sorted)-1)
	lo := int(pos)
	hi := lo + 1
	if hi >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := pos - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// isHighConfidenceLevel returns true for tiers whose granularity is
// tight enough to warrant the "high" promotion when the sample size
// crosses TargetSampleSize. Sub-commune and commune tiers qualify ;
// neighborhood / department fan-outs mix multiple INSEEs and therefore
// stay capped at "medium" regardless of sample count.
func isHighConfidenceLevel(level string) bool {
	return level == "commune" || level == "address_radius"
}

// PickConfidence implements the confidence-level rules:
//
//	high     : sample ≥ TargetSampleSize AND isHighConfidenceLevel(level)
//	medium   : sample ≥ MinSampleSize (otherwise)
//	low      : sample <  MinSampleSize
func PickConfidence(n int, level string) string {
	switch {
	case n >= TargetSampleSize && isHighConfidenceLevel(level):
		return ConfidenceHigh
	case n >= MinSampleSize:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}
