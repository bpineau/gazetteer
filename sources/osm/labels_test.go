package osm

import "testing"

// TestLineLabel pins the long-form label normalisation, including the
// OSM ref-tag quirk (contributors storing the full label in ref) and
// the tram bare-number rule.
func TestLineLabel(t *testing.T) {
	tests := []struct {
		name string
		tt   TransitType
		ref  string
		want string
	}{
		// Bare refs per type: the common, well-tagged case.
		{"metro bare number", TransitTypeMetro, "5", "Métro 5"},
		{"metro bare alnum", TransitTypeMetro, "3bis", "Métro 3bis"},
		{"rer bare letter", TransitTypeRER, "A", "RER A"},
		{"transilien bare letter", TransitTypeTransilien, "J", "Transilien J"},
		{"train bare ref", TransitTypeTrain, "TER", "Train TER"},

		// Pre-prefixed refs: contributors stored the full label in the
		// ref tag — must NOT be double-prefixed.
		{"rer pre-prefixed", TransitTypeRER, "RER B", "RER B"},
		{"transilien pre-prefixed", TransitTypeTransilien, "Transilien H", "Transilien H"},
		{"metro pre-prefixed", TransitTypeMetro, "Métro 5", "Métro 5"},
		// The guard fires on the ref alone, regardless of the type bucket.
		{"pre-prefixed wins over type", TransitTypeTrain, "RER B", "RER B"},

		// Tram: "T3a" already carries the T prefix; bare numerics
		// (Lyon/Bordeaux style) get one.
		{"tram already prefixed", TransitTypeTram, "T3a", "T3a"},
		{"tram already prefixed simple", TransitTypeTram, "T1", "T1"},
		{"tram bare number", TransitTypeTram, "1", "T1"},
		{"tram bare two digits", TransitTypeTram, "12", "T12"},

		// Empty refs fall back to the bare mode name.
		{"metro empty", TransitTypeMetro, "", "Métro"},
		{"rer empty", TransitTypeRER, "", "RER"},
		{"transilien empty", TransitTypeTransilien, "", "Transilien"},
		{"tram empty", TransitTypeTram, "", "Tram"},
		{"train empty", TransitTypeTrain, "", "Train"},

		// Unknown transit type: the ref passes through untouched.
		{"unknown type passthrough", TransitType("funicular"), "F1", "F1"},
		{"unknown type empty", TransitType("funicular"), "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := LineLabel(tc.tt, tc.ref); got != tc.want {
				t.Errorf("LineLabel(%q, %q) = %q, want %q", tc.tt, tc.ref, got, tc.want)
			}
		})
	}
}
