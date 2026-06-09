package osm

import "strings"

// LineLabel turns a (TransitType, raw ref) pair into a long-form,
// user-visible line label: "Métro 5", "RER A", "T3a", "Transilien J".
//
// It owns an OSM-dataset quirk: some OSM contributors store the full
// label in the ref tag (e.g. ref="RER B", ref="Transilien H",
// ref="Métro 5") instead of the bare line reference ("B", "H", "5").
// LineLabel detects those pre-prefixed refs and returns them as-is so
// the label is never double-prefixed ("RER RER B"). Tram refs come in
// two flavours too — "T3a" (already prefixed) vs bare numerics like
// "1" (Lyon/Bordeaux style) — bare numbers are prefixed with "T" so
// the label stays consistent.
//
// LineLabel differs from Station.Display: Display renders a compact
// station-centric label ("Lourmel (M8)") whose output may be persisted
// by consumers, while LineLabel renders a standalone long-form line
// name ("Métro 8") for line lists. They intentionally use different
// prefix schemes; do not swap one for the other.
func LineLabel(tt TransitType, ref string) string {
	// Some OSM contributors store the full label in the ref tag
	// (e.g. ref="RER B", ref="Transilien H"). Detect that and return as-is.
	switch {
	case strings.HasPrefix(ref, "RER "):
		return ref
	case strings.HasPrefix(ref, "Transilien "):
		return ref
	case strings.HasPrefix(ref, "Métro "):
		return ref
	}
	switch tt {
	case TransitTypeMetro:
		if ref == "" {
			return "Métro"
		}
		return "Métro " + ref
	case TransitTypeRER:
		if ref == "" {
			return "RER"
		}
		return "RER " + ref
	case TransitTypeTransilien:
		if ref == "" {
			return "Transilien"
		}
		return "Transilien " + ref
	case TransitTypeTram:
		if ref == "" {
			return "Tram"
		}
		// Tram refs come in two flavours: "T3a" (already prefixed) or bare
		// numerics like "1", "2" (Lyon/Bordeaux style). Prefix bare numbers
		// with "T" so the label is consistent.
		if len(ref) > 0 && ref[0] >= '0' && ref[0] <= '9' {
			return "T" + ref
		}
		return ref // already "T1", "T3a" — self-explanatory
	case TransitTypeTrain:
		if ref == "" {
			return "Train"
		}
		return "Train " + ref
	}
	return ref
}
