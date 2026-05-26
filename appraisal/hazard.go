package appraisal

import (
	"sort"

	"github.com/bpineau/gazetteer/gazetteer"
)

// HazardReporter is the optional interface a Source's typed Result MAY
// implement to contribute hazard / risk findings to HazardProfile.
//
// Sources whose risk surface is heterogeneous (e.g. BDNB's urban-
// heritage flags) deliberately DO NOT implement this interface — only
// canonical natural / industrial hazards belong here.
type HazardReporter interface {
	HazardReport() HazardReport
}

// HazardReport is one source's risk contribution. Each slice holds
// canonical, snake_case risk identifiers — Source packages decide their
// own vocabulary, HazardProfile only takes the set union and lets UI /
// consumers translate labels.
type HazardReport struct {
	// NaturalRisks lists natural-hazard identifiers the source confirms
	// affect the listing (e.g. "inondation", "seisme", "feux_foret").
	NaturalRisks []string

	// IndustrialRisks lists technological-hazard identifiers the source
	// confirms affect the listing (e.g. "icpe", "seveso", "nucleaire").
	IndustrialRisks []string

	// Confidence is the source's self-reported certainty for this report
	// (currently informational — HazardProfile derives the consolidated
	// confidence from contributor count).
	Confidence Confidence
}

// HazardOptions configures HazardProfile. Start minimal; new knobs land
// here as concrete sources reveal needs.
type HazardOptions struct{}

// HazardConsolidated is the synthesised hazard view across all
// contributors.
type HazardConsolidated struct {
	// NaturalRisks is the deduplicated, sorted union of every
	// contributor's NaturalRisks slice.
	NaturalRisks []string

	// IndustrialRisks is the deduplicated, sorted union of every
	// contributor's IndustrialRisks slice.
	IndustrialRisks []string

	// Inputs lists each contributing source in deterministic name order.
	// Empty when no source implements HazardReporter.
	Inputs []HazardInput

	// Confidence escalates with the number of contributing sources:
	// 1 → Low, 2 → Medium, ≥ 3 → High.
	Confidence Confidence
}

// HazardInput is one source's verbatim contribution after the dossier
// scan. Surfaced so callers (UI, doctor predicates) can attribute each
// risk back to its origin.
type HazardInput struct {
	Source string
	Report HazardReport
}

// HazardProfile synthesises a consolidated hazard view from a Dossier.
//
// Iterates Results in deterministic name order, picks up everything
// that implements HazardReporter and returned StatusOK or StatusOKEmpty,
// takes the set union of NaturalRisks / IndustrialRisks (deduplicated +
// sorted), and assigns Confidence proportional to the contributor count.
//
// Variadic opts mirrors PricePerM2 for symmetry; today HazardOptions is
// empty so callers can omit it.
func HazardProfile(d gazetteer.Dossier, opts ...HazardOptions) HazardConsolidated {
	// Iterate results in name order for deterministic output.
	names := make([]string, 0, len(d.Results))
	for name := range d.Results {
		names = append(names, name)
	}
	sort.Strings(names)

	var inputs []HazardInput
	naturalSet := map[string]struct{}{}
	industrialSet := map[string]struct{}{}

	for _, name := range names {
		r := d.Results[name]
		if r.Status != gazetteer.StatusOK && r.Status != gazetteer.StatusOKEmpty {
			continue
		}
		rep, ok := r.Data.(HazardReporter)
		if !ok {
			continue
		}
		report := rep.HazardReport()
		inputs = append(inputs, HazardInput{Source: name, Report: report})
		for _, nr := range report.NaturalRisks {
			naturalSet[nr] = struct{}{}
		}
		for _, ir := range report.IndustrialRisks {
			industrialSet[ir] = struct{}{}
		}
	}

	conf := ConfidenceLow
	switch {
	case len(inputs) >= 3:
		conf = ConfidenceHigh
	case len(inputs) >= 2:
		conf = ConfidenceMedium
	}

	return HazardConsolidated{
		NaturalRisks:    setToSortedSlice(naturalSet),
		IndustrialRisks: setToSortedSlice(industrialSet),
		Inputs:          inputs,
		Confidence:      conf,
	}
}

// setToSortedSlice flattens a set-as-map into a deterministically sorted
// slice. Helper used by HazardProfile to keep wire output stable.
func setToSortedSlice(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
