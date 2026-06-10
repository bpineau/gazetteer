package appraisal

import "github.com/bpineau/gazetteer/gazetteer"

// PriceSourceNames returns the names of every registered source whose
// typed Result implements PriceEstimator — the contributors PricePerM2
// will consider. Derived from the gazetteer payload registry, so it
// includes out-of-tree plugins whose packages have been imported (a
// source only registers when its package init runs).
//
// Use it to validate weight overrides, scope a CollectSome to the
// price-relevant subset, or render "which sources feed this number".
func PriceSourceNames() []string {
	return sourceNamesImplementing[PriceEstimator]()
}

// RentSourceNames returns the names of every registered source whose
// typed Result implements RentEstimator — the contributors RentValue
// will consider. Same registry-derived semantics as PriceSourceNames.
func RentSourceNames() []string {
	return sourceNamesImplementing[RentEstimator]()
}

// HazardSourceNames returns the names of every registered source whose
// typed Result implements HazardReporter — the contributors
// HazardProfile will consider. Same registry-derived semantics as
// PriceSourceNames.
func HazardSourceNames() []string {
	return sourceNamesImplementing[HazardReporter]()
}

// sourceNamesImplementing scans the payload registry for Result types
// satisfying the capability interface T. RegisteredNames is already
// sorted, so the output is deterministic.
func sourceNamesImplementing[T any]() []string {
	var out []string
	for _, name := range gazetteer.RegisteredNames() {
		factory := gazetteer.Lookup(name)
		if factory == nil {
			continue
		}
		if _, ok := factory().(T); ok {
			out = append(out, name)
		}
	}
	return out
}
