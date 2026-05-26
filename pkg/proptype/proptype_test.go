package proptype_test

import (
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/pkg/proptype"
)

// TestNormalize_RoundTrip checks that every canonical output value
// re-normalises back to itself — i.e. the alias table includes its own
// canonical strings. Catches a future drift where a canonical is renamed
// without updating the alias map (the most insidious silent-leak class).
func TestNormalize_RoundTrip(t *testing.T) {
	t.Parallel()
	canon := []proptype.PropertyType{
		proptype.Apartment, proptype.House, proptype.Land,
		proptype.Parking, proptype.Commercial, proptype.Mixed,
		proptype.Parts, proptype.Garage, proptype.Cave, proptype.Other,
	}
	for _, p := range canon {
		got := proptype.Normalize(p.String())
		if got != p {
			t.Errorf("Normalize(%q) = %q ; want %q", p.String(), got, p)
		}
	}
}

// TestNormalize_AliasCoverage pins every alias previously seen across
// the codebase to its canonical. New call sites discovering a fresh
// alias should ADD a row here ; do NOT delete a row to make the test
// pass.
func TestNormalize_AliasCoverage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want proptype.PropertyType
	}{
		// Apartment — French + English + abbreviations + flat/loft/studio
		{"apartment", proptype.Apartment},
		{"appartement", proptype.Apartment},
		{"appart", proptype.Apartment},
		{"appt", proptype.Apartment},
		{"apt", proptype.Apartment},
		{"flat", proptype.Apartment},
		{"loft", proptype.Apartment},
		{"studio", proptype.Apartment},
		{"studette", proptype.Apartment},

		// House — maison/villa/pavillon
		{"house", proptype.House},
		{"maison", proptype.House},
		{"villa", proptype.House},
		{"pavillon", proptype.House},

		// Land
		{"land", proptype.Land},
		{"terrain", proptype.Land},
		{"parcelle", proptype.Land},
		{"land_only", proptype.Land},

		// Parking — distinct from Garage in this normaliser
		{"parking", proptype.Parking},
		{"place de parking", proptype.Parking},

		// Garage canonical (legacy)
		{"garage", proptype.Garage},
		{"box", proptype.Garage},

		// Cave canonical (legacy)
		{"cave", proptype.Cave},

		// Commercial
		{"commercial", proptype.Commercial},
		{"local", proptype.Commercial},
		{"local commercial", proptype.Commercial},
		{"bureau", proptype.Commercial},
		{"boutique", proptype.Commercial},

		// Mixed
		{"mixed", proptype.Mixed},
		{"mixte", proptype.Mixed},

		// Parts (share transfer)
		{"parts", proptype.Parts},
		{"part", proptype.Parts},
		{"shares", proptype.Parts},
		{"entity_sale", proptype.Parts},

		// Other
		{"other", proptype.Other},
	}
	for _, c := range cases {
		got := proptype.Normalize(c.in)
		if got != c.want {
			t.Errorf("Normalize(%q) = %q ; want %q", c.in, got, c.want)
		}
	}
}

// TestNormalize_CaseAndWhitespace checks case- + whitespace-tolerance.
func TestNormalize_CaseAndWhitespace(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want proptype.PropertyType
	}{
		{"Appartement", proptype.Apartment},
		{"APPARTEMENT", proptype.Apartment},
		{"  maison ", proptype.House},
		{"TERRAIN", proptype.Land},
		{"\tHouse\n", proptype.House},
		{"LOCAL COMMERCIAL", proptype.Commercial},
	}
	for _, c := range cases {
		got := proptype.Normalize(c.in)
		if got != c.want {
			t.Errorf("Normalize(%q) = %q ; want %q", c.in, got, c.want)
		}
	}
}

// TestNormalize_UnknownInputs verifies that empty / whitespace-only /
// unrecognised strings collapse to Unknown.
func TestNormalize_UnknownInputs(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "   ", "\t\n", "hovercraft", "?", "n/a"} {
		got := proptype.Normalize(in)
		if got != proptype.Unknown {
			t.Errorf("Normalize(%q) = %q ; want Unknown", in, got)
		}
	}
}

// TestNormalizePtr covers the nil-tolerant variant.
func TestNormalizePtr(t *testing.T) {
	t.Parallel()
	if got := proptype.NormalizePtr(nil); got != proptype.Unknown {
		t.Errorf("NormalizePtr(nil) = %q ; want Unknown", got)
	}
	s := "Appartement"
	if got := proptype.NormalizePtr(&s); got != proptype.Apartment {
		t.Errorf("NormalizePtr(%q) = %q ; want Apartment", s, got)
	}
	empty := ""
	if got := proptype.NormalizePtr(&empty); got != proptype.Unknown {
		t.Errorf("NormalizePtr(%q) = %q ; want Unknown", empty, got)
	}
}

// TestIsKnown sanity-checks the IsKnown predicate matches the canonical set.
func TestIsKnown(t *testing.T) {
	t.Parallel()
	for _, p := range []proptype.PropertyType{
		proptype.Apartment, proptype.House, proptype.Land,
		proptype.Parking, proptype.Commercial, proptype.Mixed,
		proptype.Parts, proptype.Garage, proptype.Cave, proptype.Other,
	} {
		if !p.IsKnown() {
			t.Errorf("IsKnown(%q) = false ; want true", p)
		}
	}
	if proptype.Unknown.IsKnown() {
		t.Errorf("IsKnown(Unknown) = true ; want false")
	}
	if proptype.PropertyType("bogus").IsKnown() {
		t.Errorf("IsKnown(bogus) = true ; want false")
	}
}

// TestCanonicalSlugs_DoNotChange pins the wire format. Mutating any
// canonical slug breaks `auctions.property_type` reads — the test is a
// deliberate canary.
func TestCanonicalSlugs_DoNotChange(t *testing.T) {
	t.Parallel()
	expect := map[proptype.PropertyType]string{
		proptype.Apartment:  "apartment",
		proptype.House:      "house",
		proptype.Land:       "land",
		proptype.Parking:    "parking",
		proptype.Commercial: "commercial",
		proptype.Mixed:      "mixed",
		proptype.Parts:      "parts",
		proptype.Garage:     "garage",
		proptype.Cave:       "cave",
		proptype.Other:      "other",
		proptype.Unknown:    "",
	}
	for got, want := range expect {
		if string(got) != want {
			t.Errorf("canonical slug drift: %q ≠ %q", got, want)
		}
		// Belt-and-braces — also pin String().
		if got.String() != want {
			t.Errorf("String() drift: %q.String() = %q ; want %q",
				string(got), got.String(), want)
		}
	}
}

// TestNormalize_NoLeadingDashOrNoise pins the "genuinely unrecognisable"
// behaviour for inputs licitor's `classifyTitle` returns "" for. The
// package must NOT attempt fuzzy substring matches — only exact lookup
// on the normalised key.
func TestNormalize_NoFuzzyMatch(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"Une maison avec jardin", // multi-word free-text NOT in the alias table
		"appartement T3",         // contains "appartement" but as a phrase
		"residential apartment",  // contains "apartment" but is not the bare key
	} {
		got := proptype.Normalize(in)
		// `appartement T3` and similar phrases used to be silently misclassified
		// by some call sites that did substring matches ; the canonical
		// package must reject them.
		if got != proptype.Unknown {
			t.Errorf("Normalize(%q) = %q ; expected Unknown (no fuzzy match)", in, got)
		}
	}
	// Make sure the package does not import a regex / strings.Contains
	// codepath by accident — we only assert the table-lookup semantics
	// here.
	if !strings.EqualFold("apartment", string(proptype.Apartment)) {
		t.Fatalf("canary failed: Apartment slug drifted")
	}
}
