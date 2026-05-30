package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// inputCheck maps a catalog "inputs" token to a predicate on the Listing, so
// `query --explain` can tell whether a source that returned nothing was missing
// a required input or genuinely has no data for the address.
type inputCheck struct {
	token string // substring that appears in a descriptor's Inputs prose
	label string // how to name the Listing field in the diagnosis
	has   func(gazetteer.Listing) bool
}

var inputChecks = []inputCheck{
	{"IRIS", "Listing.IRIS", func(l gazetteer.Listing) bool { return l.IRIS != "" }},
	{"lat/lon", "lat/lon", func(l gazetteer.Listing) bool { return l.Lat != nil && l.Lon != nil }},
	{"INSEE", "INSEE", func(l gazetteer.Listing) bool { return l.INSEE != "" }},
	{"surface", "surface", func(l gazetteer.Listing) bool { return l.SurfaceM2 != nil }},
	{"rooms", "rooms", func(l gazetteer.Listing) bool { return l.Rooms != nil }},
	{"property_type", "property_type", func(l gazetteer.Listing) bool { return l.PropertyType != "" }},
	{"address", "address", func(l gazetteer.Listing) bool { return l.Address != "" }},
}

// printDiagnosis explains, per source, why it did or didn't return data —
// cross-referencing each non-OK source's required inputs (from the catalog)
// against what the normalised Listing actually carries. This is the troubleshoot
// path: it distinguishes "you forgot an input" from "no data for this address".
func printDiagnosis(w io.Writer, d gazetteer.Dossier) {
	l := d.Listing

	fmt.Fprintln(w, "Listing (after normalisation):")
	for _, c := range listingFields(l) {
		fmt.Fprintf(w, "  %-14s %s\n", c.label+":", c.value)
	}
	fmt.Fprintln(w)

	names := make([]string, 0, len(d.Results))
	for name := range d.Results {
		names = append(names, name)
	}
	sort.Strings(names)

	var ok, empty, failed int
	fmt.Fprintln(w, "Per-source diagnosis:")
	for _, name := range names {
		r := d.Results[name]
		switch {
		case isOKWithData(r):
			ok++
			continue // only explain the ones that produced nothing
		case r.IsEmpty() || r.Status == gazetteer.StatusOKEmpty:
			empty++
			fmt.Fprintf(w, "  %-18s empty   — %s\n", name, emptyVerdict(name, l))
		default:
			failed++
			reason := string(r.Status)
			if r.Err != nil {
				reason = truncate(unwrap(r.Err.Error()), 120)
			}
			fmt.Fprintf(w, "  %-18s %-7s — %s\n", name, abbreviateStatus(r.Status), reason)
		}
	}
	fmt.Fprintf(w, "\n%d source(s) returned data, %d empty, %d failed.\n", ok, empty, failed)
}

// emptyVerdict produces the one-line cause for an empty source: the missing
// required inputs if any, else "inputs present → no data for this address".
func emptyVerdict(name string, l gazetteer.Listing) string {
	desc, ok := sourceDescriptors[name]
	if !ok {
		return "no data for this address"
	}
	missing := missingInputs(desc.Inputs, l)
	if len(missing) > 0 {
		return fmt.Sprintf("Listing is missing %s, which this source needs (inputs: %v)", strings.Join(missing, " + "), desc.Inputs)
	}
	cov := desc.Coverage
	if cov == "" {
		cov = "see catalog"
	}
	return fmt.Sprintf("inputs present → no data for this address (coverage: %s)", cov)
}

// missingInputs returns the canonical Listing fields a source's inputs mention
// but the Listing lacks. An "X or Y" input is satisfied if either is present.
func missingInputs(inputs []string, l gazetteer.Listing) []string {
	var missing []string
	for _, in := range inputs {
		// Collect the checks this input clause mentions.
		var mentioned []inputCheck
		anyPresent := false
		for _, c := range inputChecks {
			if strings.Contains(in, c.token) {
				mentioned = append(mentioned, c)
				if c.has(l) {
					anyPresent = true
				}
			}
		}
		// A clause is unsatisfied only when it mentions field(s) and NONE is
		// present (handles "INSEE or address" as a single OR requirement).
		if len(mentioned) > 0 && !anyPresent {
			labels := make([]string, len(mentioned))
			for i, c := range mentioned {
				labels[i] = c.label
			}
			missing = append(missing, strings.Join(labels, "/"))
		}
	}
	return dedupeStrings(missing)
}

// isOKWithData reports a source that ran and produced a non-empty Result.
func isOKWithData(r gazetteer.Result) bool {
	switch r.Status {
	case gazetteer.StatusOK, "":
		return !r.IsEmpty()
	default:
		return false
	}
}

type labelValue struct{ label, value string }

// listingFields renders the canonical Listing fields (present value or "—") in
// the order the diagnosis reasons about them.
func listingFields(l gazetteer.Listing) []labelValue {
	latlon := "—"
	if l.Lat != nil && l.Lon != nil {
		latlon = fmt.Sprintf("%.5f,%.5f", *l.Lat, *l.Lon)
	}
	return []labelValue{
		{"address", orDash(l.Address)},
		{"INSEE", orDash(l.INSEE)},
		{"lat/lon", latlon},
		{"Listing.IRIS", orDash(l.IRIS)},
		{"surface", orDashF(l.SurfaceM2)},
		{"rooms", orDashI(l.Rooms)},
		{"property_type", orDash(string(l.PropertyType))},
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func orDashF(f *float64) string {
	if f == nil {
		return "—"
	}
	return fmt.Sprintf("%g", *f)
}

func orDashI(i *int) string {
	if i == nil {
		return "—"
	}
	return fmt.Sprintf("%d", *i)
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
